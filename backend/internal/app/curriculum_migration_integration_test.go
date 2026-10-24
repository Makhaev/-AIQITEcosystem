package app_test

import (
	"context"
	"testing"

	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestM2MigrationFromPopulatedCoreIsAtomicAndRepeatable(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var current string
	if err := f.pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	legacy := current + "_m2"
	identifier := pgx.Identifier{legacy}.Sanitize()
	if _, err := f.pool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatal(err)
	}
	cfg := f.pool.Config()
	cfg.ConnConfig.RuntimeParams["search_path"] = legacy
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := f.pool.Exec(context.Background(), `DROP SCHEMA `+identifier+` CASCADE`); err != nil {
			t.Error(err)
		}
	})
	if _, err = pool.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	entries, err := core.Migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "017" {
			continue
		}
		body, e := core.Migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(body)); e != nil {
			t.Fatalf("%s: %v", entry.Name(), e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, "migrations/"+entry.Name()); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = pool.Exec(ctx, `
INSERT INTO users(email,name,password_hash) VALUES('migration@example.test','Автор Core','unused');
INSERT INTO user_roles(user_id,role) VALUES(1,'admin');
INSERT INTO programs(name,name_search,status) VALUES('Core программа','core программа','active'),('Черновик','черновик','draft'),('Архив','архив','archived');
INSERT INTO program_modules(program_id,name,position) VALUES(1,'Модуль Core',1);
INSERT INTO teachers(name,name_search) VALUES('Педагог','педагог');
INSERT INTO students(name,name_search) VALUES('Ученик','ученик');
INSERT INTO groups(name,name_search,program_id,capacity,status) VALUES('Группа','группа',1,10,'active');
INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES(1,1,'2000-01-01');
INSERT INTO lessons(group_id,module_id,teacher_id,topic,topic_search,starts_at,ends_at)
VALUES(1,1,1,'Проведённое','проведённое','2024-01-01T08:00Z','2024-01-01T09:00Z'),
      (1,1,1,'Будущее','будущее',clock_timestamp()+interval '2 days',clock_timestamp()+interval '2 days 1 hour');
INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id,student_name) VALUES(1,1,1,1,'Историческое имя');
UPDATE attendance SET status='present',comment='Сохранённая отметка';
UPDATE lessons SET roster_initialized_at='2024-01-01T08:00Z' WHERE id=1;
UPDATE lessons SET status='completed',snapshot_state='captured',snapshot_program_name='Старое название',snapshot_module_name='Старый модуль',snapshot_topic='Старая тема',snapshot_captured_at='2024-01-01T09:00Z' WHERE id=1;
INSERT INTO lesson_snapshot_versions(lesson_id,actor_id,program_name,topic,state,reason) VALUES(1,1,'Прежнее название','Прежняя тема','captured','Исправление');
INSERT INTO audit_logs(actor_id,action,object_type,object_id,changed_fields) VALUES(1,'lesson.completed','lesson',1,'{status}');
INSERT INTO tasks(title,title_search,description,creator_id,assignee_id,due_at) VALUES('Задача','задача','Описание задачи',1,1,clock_timestamp());
`); err != nil {
		t.Fatal(err)
	}
	read := func(relation string, upgraded bool) string {
		t.Helper()
		projection := "to_jsonb(v)"
		if upgraded && relation == "groups" {
			projection += " - 'program_version_id'"
		}
		if upgraded && relation == "lessons" {
			projection += " - 'program_version_id' - 'version_module_key' - 'template_key' - 'plan_snapshot' - 'plan_assignment_id'"
		}
		var result string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(`+projection+` ORDER BY id),'[]'::jsonb)::text FROM `+relation+` v`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := map[string]string{}
	for _, relation := range []string{"programs", "program_modules", "groups", "lessons", "group_enrollments", "lesson_participants", "attendance", "lesson_snapshot_versions", "audit_logs", "tasks"} {
		before[relation] = read(relation, false)
	}
	// Fail after version creation/backfill and group changes, then verify that
	// neither the ledger nor the original data were partially committed.
	if _, err = pool.Exec(ctx, `CREATE TABLE group_program_transfers(id bigint)`); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool, core.Migrations); err == nil {
		t.Fatal("expected migration conflict")
	}
	var rolledBack bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('program_versions') IS NULL AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE name='migrations/017_program_versions.sql')`).Scan(&rolledBack); err != nil || !rolledBack {
		t.Fatal("partial migration", err)
	}
	for relation, expected := range before {
		if read(relation, false) != expected {
			t.Fatalf("failed upgrade changed %s", relation)
		}
	}
	if _, err = pool.Exec(ctx, `DROP TABLE group_program_transfers`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
			t.Fatal(err)
		}
		for relation, expected := range before {
			if read(relation, true) != expected {
				t.Fatalf("upgrade changed Core history in %s", relation)
			}
		}
	}
	var imported, linked, unknown bool
	if err = pool.QueryRow(ctx, `SELECT count(*)=3 AND bool_and(legacy_import AND created_by IS NULL AND approved_by IS NULL AND published_at IS NULL AND number=1) FROM program_versions`).Scan(&imported); err != nil || !imported {
		t.Fatal("migration invented approval or duplicated versions", err)
	}
	if err = pool.QueryRow(ctx, `SELECT bool_and(l.program_version_id=g.program_version_id AND v.program_id=l.program_id) FROM lessons l JOIN groups g ON g.id=l.group_id JOIN program_versions v ON v.id=l.program_version_id`).Scan(&linked); err != nil || !linked {
		t.Fatal("missing version bindings", err)
	}
	if err = pool.QueryRow(ctx, `SELECT plan_snapshot->>'origin'='legacy_unknown' AND NOT (plan_snapshot ? 'template') FROM lessons WHERE id=1`).Scan(&unknown); err != nil || !unknown {
		t.Fatal("migration invented historical teaching content", err)
	}
}
