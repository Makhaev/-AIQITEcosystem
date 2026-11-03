package app_test

import (
	"context"
	"testing"

	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFamilyRequestsMigrationPreservesLegacyCorrespondence(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var schema string
	if err := f.pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	schema += "_m409"
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := f.pool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
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
		if entry.Name() >= "027" {
			continue
		}
		body, e := core.Migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(body)); e != nil {
			t.Fatal(entry.Name(), e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, "migrations/"+entry.Name()); e != nil {
			t.Fatal(e)
		}
	}
	if _, err = pool.Exec(ctx, `
 INSERT INTO users(email,name,password_hash) VALUES('old-parent@example.test','Parent','unused');
 INSERT INTO students(name,name_search) VALUES('Child','child');
 INSERT INTO family_requests(student_id,created_by,subject,body,reply,revision,created_at) VALUES
 (1,1,'Old question','Original question','Original answer',4,'2026-10-01T09:00Z'),
 (1,1,'Unanswered','Still waiting','',1,'2026-10-02T09:00Z');
 `); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `CREATE TABLE family_request_events(id integer)`); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool, core.Migrations); err == nil {
		t.Fatal("expected migration failure")
	}
	var rollback bool
	if err = pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='family_requests' AND column_name='status') AND to_regclass('family_request_messages') IS NULL AND (SELECT revision=4 AND reply='Original answer' FROM family_requests WHERE id=1)`).Scan(&rollback); err != nil || !rollback {
		t.Fatal("partial migration", err)
	}
	if _, err = pool.Exec(ctx, `DROP TABLE family_request_events`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
			t.Fatal(err)
		}
	}
	var preserved bool
	if err = pool.QueryRow(ctx, `SELECT
 (SELECT status='answered' AND answered_at IS NULL AND closed_at IS NULL AND assignee_id IS NULL AND revision=4 AND created_at='2026-10-01T09:00Z' FROM family_requests WHERE id=1)
 AND (SELECT status='new' AND revision=1 FROM family_requests WHERE id=2)
 AND (SELECT count(*)=3 FROM family_request_messages)
 AND (SELECT author_id IS NULL AND created_at IS NULL AND body='Original answer' FROM family_request_messages WHERE request_id=1 AND kind='answer')
 AND (SELECT author_id=1 AND created_at='2026-10-01T09:00Z' AND body='Original question' FROM family_request_messages WHERE request_id=1 AND kind='message')
 AND (SELECT count(*)=3 FROM family_request_events)
 `).Scan(&preserved); err != nil || !preserved {
		t.Fatal("legacy correspondence altered", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE family_request_messages SET body='changed' WHERE request_id=1`); err == nil {
		t.Fatal("mutable legacy message")
	}
}
