package app_test

import (
	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestAPIContractMigrationPreservesHistoryAndConstraints(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var schema string
	if err := f.pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	schema += "_api11"
	ident := pgx.Identifier{schema}.Sanitize()
	if _, err := f.pool.Exec(ctx, `CREATE SCHEMA `+ident); err != nil {
		t.Fatal(err)
	}
	cfg := f.pool.Config()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, e := f.pool.Exec(context.Background(), `DROP SCHEMA `+ident+` CASCADE`); e != nil {
			t.Error(e)
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
		if entry.Name() >= "032" {
			continue
		}
		body, e := core.Migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(body)); e != nil {
			t.Fatal(entry.Name(), e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO schema_migrations(name)VALUES($1)`, "migrations/"+entry.Name()); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = pool.Exec(ctx, `
 INSERT INTO users(email,name,password_hash)VALUES('history@example.test','Historical user','unused');
 INSERT INTO students(name,name_search)VALUES('Historical child','historical child');
 INSERT INTO invitations(email,role,student_id,token_hash,created_by,created_at,expires_at,used_at,used_by)VALUES('history@example.test','student',1,decode(repeat('01',32),'hex'),1,'2026-10-01T10:00Z','2026-10-03T10:00Z','2026-10-01T11:00Z',1);
 INSERT INTO password_reset_tokens(user_id,token_hash,created_at,expires_at)VALUES(1,decode(repeat('02',32),'hex'),'2026-10-01T12:00Z','2026-10-01T12:30Z');
 INSERT INTO family_reports(student_id,title,body,created_by,revision,published,state,version,published_version)VALUES(1,'Past report','Retained body',1,2,true,'published',1,1);
 INSERT INTO family_report_editions(report_id,version,content)VALUES(1,1,'{"title":"Past report","body":"Retained body"}');
 INSERT INTO family_report_publications(report_id,version,published_at,published_by)VALUES(1,1,'2026-10-01T11:00Z',1);
 INSERT INTO family_report_history(report_id,revision,content,actor_id)VALUES(1,2,'{"body":"Retained body"}',1);
 INSERT INTO family_report_decisions(report_id,version,action,reason,actor_id)VALUES(1,1,'publish','Historical decision',1);
 INSERT INTO free_events(title,title_search,created_by)VALUES('Historical event','historical event',1);
 INSERT INTO event_registrations(event_id,student_id,status,created_by,updated_by)VALUES(1,1,'requested',1,1);
 UPDATE event_registrations SET status='cancelled',revision=revision+1 WHERE id=1;
 INSERT INTO event_documents(registration_id,name,media_type,data,created_by)VALUES(1,'retained.txt','text/plain; charset=utf-8',convert_to('Retained document','UTF8'),1);
 INSERT INTO notifications(user_id,student_id,kind,object_id,event_version,audience)VALUES(1,1,'participation_cancelled',1,'1:2','family');
 `); err != nil {
		t.Fatal(err)
	}
	tables := []string{"invitations", "password_reset_tokens", "family_reports", "family_report_editions", "family_report_publications", "family_report_history", "family_report_decisions", "event_registrations", "event_registration_history", "event_documents", "notifications", "notification_deliveries"}
	snapshot := func(table string) string {
		t.Helper()
		var value string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM `+pgx.Identifier{table}.Sanitize()+` t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := map[string]string{}
	for _, table := range tables {
		before[table] = snapshot(table)
	}
	for i := 0; i < 2; i++ {
		if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range tables {
		if snapshot(table) != before[table] {
			t.Fatalf("migration changed %s history", table)
		}
	}
	for _, sql := range []string{
		`UPDATE password_reset_tokens SET expires_at=created_at WHERE user_id=1`,
		`INSERT INTO invitations(email,role,student_id,token_hash,created_by,created_at,expires_at)VALUES('different@example.test','student',1,decode(repeat('01',32),'hex'),1,'2026-10-01T10:00Z','2026-10-03T10:00Z')`,
		`INSERT INTO family_report_editions(report_id,version,content)VALUES(1,1,'{}')`,
		`UPDATE family_report_editions SET content='{}' WHERE report_id=1`,
		`UPDATE event_registrations SET status='confirmed',revision=revision+1 WHERE id=1`,
		`UPDATE event_registration_history SET status='confirmed' WHERE registration_id=1`,
		`UPDATE notification_deliveries SET revision=0`,
		`INSERT INTO notification_deliveries(kind,object_id,event_version,user_id,channel,status,next_attempt_at)SELECT kind,object_id,event_version,user_id,channel,'pending',clock_timestamp() FROM notification_deliveries LIMIT 1`,
	} {
		if _, err = pool.Exec(ctx, sql); err == nil {
			t.Fatalf("constraint accepted invalid mutation: %s", sql)
		}
	}
	var indexes int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname IN ('invitations_pending_expiry','family_report_decisions_version','event_documents_registration','event_registration_student','notification_deliveries_ready')`).Scan(&indexes); err != nil || indexes != 5 {
		t.Fatal("missing indexes", indexes, err)
	}
}
