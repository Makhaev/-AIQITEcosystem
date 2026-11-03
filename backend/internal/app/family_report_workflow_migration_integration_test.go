package app_test

import (
	"context"
	"testing"

	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFamilyReportWorkflowMigrationPreservesLegacyPublications(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var schema string
	if err := f.pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	schema += "_m408"
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
		if entry.Name() >= "026" {
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
 INSERT INTO users(email,name,password_hash) VALUES('legacy@example.test','Автор','unused'),('review@example.test','Проверяющий','unused');
 INSERT INTO students(name,name_search) VALUES('Ученик','ученик');
 INSERT INTO family_reports(student_id,title,body,published,created_by,reviewer_id,revision,created_at,published_at)
 VALUES(1,'Опубликованный','Исходный текст',true,1,2,7,NULL,'2026-10-01T10:00Z'),(1,'Черновик','Текст черновика',false,1,2,3,NULL,NULL);
 INSERT INTO family_report_history(report_id,revision,content,actor_id) VALUES(1,7,'{"body":"Исходный текст"}',2);
 `); err != nil {
		t.Fatal(err)
	}
	// Force a migration failure after ALTER/backfill to check transaction rollback.
	if _, err = pool.Exec(ctx, `CREATE TABLE family_report_editions(id integer)`); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool, core.Migrations); err == nil {
		t.Fatal("expected migration conflict")
	}
	var unchanged bool
	if err = pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='family_reports' AND column_name='state') AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE name='migrations/026_family_report_workflow.sql') AND (SELECT body='Исходный текст' AND published AND revision=7 FROM family_reports WHERE id=1)`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("partial migration", err)
	}
	if _, err = pool.Exec(ctx, `DROP TABLE family_report_editions`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
			t.Fatal(err)
		}
	}
	var preserved bool
	if err = pool.QueryRow(ctx, `SELECT
 (SELECT state='published' AND published_version=1 AND revision=7 AND approved_by IS NULL AND approved_at IS NULL AND created_at IS NULL FROM family_reports WHERE id=1)
 AND (SELECT state='draft' AND published_version IS NULL AND revision=3 FROM family_reports WHERE id=2)
 AND (SELECT content->>'body'='Исходный текст' AND content->>'author_name'='Автор' AND content->>'reviewer_name'='Проверяющий' AND (content->>'legacy')::boolean AND content->>'approved_by' IS NULL AND content->>'approved_at' IS NULL FROM family_report_editions WHERE report_id=1 AND version=1)
 AND (SELECT published_at='2026-10-01T10:00Z' AND published_by IS NULL FROM family_report_publications WHERE report_id=1 AND version=1)
 AND (SELECT count(*)=0 FROM family_report_notifications)
 AND (SELECT count(*)=0 FROM family_report_decisions)
 AND (SELECT count(*)=2 FROM family_report_editors WHERE report_id=1 AND version=1)
 AND (SELECT count(*)=1 FROM family_report_history)
 `).Scan(&preserved); err != nil || !preserved {
		t.Fatal("legacy data not preserved", err)
	}
	for _, query := range []string{
		`UPDATE family_report_editions SET content='{}' WHERE report_id=1`,
		`UPDATE family_reports SET body='changed' WHERE id=1`,
		`DELETE FROM family_report_editors WHERE report_id=1`,
	} {
		if _, err = pool.Exec(ctx, query); err == nil {
			t.Fatal("immutable data changed", query)
		}
	}
}
