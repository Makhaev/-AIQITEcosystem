package app_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	core "aiq.local/core"
	"aiq.local/core/internal/app"
	"aiq.local/core/internal/modules/attendance"
	"aiq.local/core/internal/modules/students"
	"aiq.local/core/internal/platform/config"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestArchitectureMigrationPreservesParticipantsMarksAndHistory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var current string
	if err := f.pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	legacy := current + "_r10"
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
	entries, err := core.Migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `CREATE TABLE schema_migrations(name text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "016" {
			continue
		}
		body, err := core.Migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("legacy %s: %v", entry.Name(), err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, "migrations/"+entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	// Copy only the fictional fixture administrator so both the migrated HTTP
	// server and the pre-migration data have the same public identity.
	copyAdmin := `INSERT INTO users(email,name,password_hash,must_change_password,blocked) SELECT email,name,password_hash,must_change_password,blocked FROM ` + pgx.Identifier{current, "users"}.Sanitize() + ` WHERE id=1; INSERT INTO user_roles(user_id,role) VALUES(1,'admin')`
	if _, err = pool.Exec(ctx, copyAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
INSERT INTO students(name,name_search) VALUES('Текущее имя','текущее имя'),('Исключённый ученик','исключённый ученик');
INSERT INTO programs(name,name_search,status) VALUES('Программа','программа','active');
INSERT INTO teachers(name,name_search) VALUES('Педагог','педагог');
INSERT INTO groups(name,name_search,program_id,capacity,status) VALUES('Группа','группа',1,10,'active');
INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES(1,1,'2000-01-01'),(1,2,'2000-01-01');
INSERT INTO lessons(group_id,program_id,teacher_id,topic,topic_search,starts_at,ends_at)
VALUES(1,1,1,'Историческая тема','историческая тема','2024-06-01T08:00Z','2024-06-01T09:00Z'),
      (1,1,1,'Открытый журнал','открытый журнал','2024-06-02T08:00Z','2024-06-02T09:00Z');
INSERT INTO attendance(id,lesson_id,group_id,enrollment_id,student_id,student_name,status,comment,included,created_at,updated_at) OVERRIDING SYSTEM VALUE
VALUES(41,1,1,1,1,'Имя на момент занятия','present','Сохранить комментарий',true,'2024-06-01T08:00Z','2024-06-01T09:00Z'),
      (42,1,1,2,2,'Старое исключённое имя','absent','История исключения',false,'2024-06-01T08:00Z','2024-06-02T09:00Z'),
      (43,2,1,1,1,'Сохранённый состав','unmarked','',true,'2024-06-02T08:00Z','2024-06-02T08:00Z');
SELECT setval(pg_get_serial_sequence('attendance','id'),900,true);
UPDATE lessons SET roster_initialized_at='2024-06-01T08:00Z' WHERE id=1;
UPDATE lessons SET roster_initialized_at='2024-06-02T08:00Z' WHERE id=2;
UPDATE lessons SET status='completed',snapshot_state='captured',snapshot_program_name='Название до переименования',snapshot_topic='Историческая тема',snapshot_captured_at='2024-06-01T09:00Z' WHERE id=1;
INSERT INTO lesson_snapshot_versions(lesson_id,actor_id,program_name,topic,captured_at,state,reason) VALUES(1,1,'Предыдущая версия','Прежняя тема','2024-06-01T09:00Z','captured','Историческое исправление');
INSERT INTO lesson_reschedules(lesson_id,actor_id,old_starts_at,old_ends_at,starts_at,ends_at,previous_roster,roster,reason) VALUES(2,1,'2024-06-01T08:00Z','2024-06-01T09:00Z','2024-06-02T08:00Z','2024-06-02T09:00Z','{1,2}','{1}','Исторический перенос');
INSERT INTO audit_logs(actor_id,action,object_type,object_id,changed_fields) VALUES(1,'attendance.updated','lesson',1,'{attendance.status}');
INSERT INTO tasks(title,title_search,description,creator_id,assignee_id,due_at) VALUES('Задача','задача','Результат',1,1,'2024-06-01T08:00Z');
UPDATE tasks SET status='in_progress'; UPDATE tasks SET status='done';`); err != nil {
		t.Fatal(err)
	}
	readRows := func(relation string) string {
		t.Helper()
		var result string
		projection := "to_jsonb(v)"
		if relation == "lessons" {
			// Compare every pre-existing field; M2 adds independent plan fields.
			projection += " - 'program_version_id' - 'version_module_key' - 'template_key' - 'plan_snapshot' - 'plan_assignment_id'"
		}
		if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(`+projection+` ORDER BY id),'[]'::jsonb)::text FROM `+relation+` v`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := readRows("attendance")
	history := map[string]string{}
	for _, relation := range []string{"lessons", "lesson_snapshot_versions", "lesson_reschedules", "audit_logs", "tasks", "group_enrollments"} {
		history[relation] = readRows(relation)
	}
	// Force a failure after the table rename/backfill to prove that the whole
	// migration rolls back, including its ledger, identity and historical data.
	if _, err = pool.Exec(ctx, `CREATE TABLE attendance_records(id bigint)`); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool, core.Migrations); err == nil {
		t.Fatal("expected a migration conflict")
	}
	var rolledBack bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('lesson_participants') IS NULL AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE name='migrations/016_lesson_participants.sql')`).Scan(&rolledBack); err != nil || !rolledBack {
		t.Fatalf("partial migration persisted: %v", err)
	}
	if readRows("attendance") != before {
		t.Fatal("failed migration changed original participants/marks")
	}
	if _, err = pool.Exec(ctx, `DROP TABLE attendance_records`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
			t.Fatal(err)
		}
	}
	if readRows("attendance_records") != before {
		t.Fatal("migration changed IDs, names, inclusion, marks, comments or timestamps")
	}
	for relation, old := range history {
		if readRows(relation) != old {
			t.Fatalf("migration changed %s", relation)
		}
	}
	var participants, marks, forbiddenColumns int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM lesson_participants),(SELECT count(*) FROM attendance),(SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND ((table_name='lesson_participants' AND column_name IN ('status','comment')) OR (table_name='attendance' AND column_name IN ('lesson_id','group_id','enrollment_id','student_id','student_name','included'))))`).Scan(&participants, &marks, &forbiddenColumns); err != nil || participants != 3 || marks != 3 || forbiddenColumns != 0 {
		t.Fatalf("tables were not separated: participants=%d marks=%d columns=%d err=%v", participants, marks, forbiddenColumns, err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO attendance(id) VALUES(999999)`)
	dbConstraint(t, err, "23503", "attendance_participant_fkey")
	_, err = pool.Exec(ctx, `INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id) VALUES(1,1,1,1)`)
	dbConstraint(t, err, "23505", "lesson_participants_lesson_student_unique")
	var nextID int64
	if err = pool.QueryRow(ctx, `INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id) VALUES(2,1,2,2) RETURNING id`).Scan(&nextID); err != nil || nextID <= 900 {
		t.Fatalf("identity sequence was reset: id=%d err=%v", nextID, err)
	}
	var initialStatus string
	if err = pool.QueryRow(ctx, `SELECT status FROM attendance WHERE id=$1`, nextID).Scan(&initialStatus); err != nil || initialStatus != "unmarked" {
		t.Fatalf("new participant has no initial mark: %q %v", initialStatus, err)
	}
	handler, _, err := app.Router(pool, config.Config{Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	legacyFixture := &fixture{pool: pool, server: server, admin: f.admin}
	admin := legacyFixture.adminBrowser(t)
	var journal attendance.Journal
	decode(t, expect(t, admin, "GET", "/lessons/1/attendance", nil, 200), &journal)
	if len(journal.Items) != 1 || journal.Items[0].ID != 41 || journal.Items[0].Name != "Имя на момент занятия" || journal.Items[0].Status != "present" || journal.Items[0].Comment != "Сохранить комментарий" {
		t.Fatalf("public journal/history changed: %+v", journal)
	}
	var overview students.StudentOverview
	decode(t, expect(t, admin, "GET", "/students/1/overview?from=2024-06-01&to=2024-06-30", nil, 200), &overview)
	if overview.Attendance.Present != 1 || len(overview.Attendance.Items) != 1 || overview.Attendance.Items[0].Topic != "Историческая тема" {
		t.Fatalf("migrated overview lost historical marks: %+v", overview.Attendance)
	}
	if _, err = pool.Exec(ctx, `UPDATE attendance SET status='late',comment='Новое значение' WHERE id=41`); err != nil {
		t.Fatal(err)
	}
	var name string
	if err = pool.QueryRow(ctx, `SELECT student_name FROM lesson_participants WHERE id=41`).Scan(&name); err != nil || name != "Имя на момент занятия" {
		t.Fatal("mark update changed the saved participant")
	}
	t.Log(fmt.Sprintf("Preserved three historical identities/marks, all snapshots/reschedules/audit/tasks; sequence continued at %d", nextID))
}
