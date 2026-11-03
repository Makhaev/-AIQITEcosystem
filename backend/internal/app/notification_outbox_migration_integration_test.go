package app_test

import (
	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
)

func TestNotificationOutboxMigrationPreservesHandoffsAndDeduplicates(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var schema string
	if err := f.pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	schema += "_m411"
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
		if _, e := f.pool.Exec(context.Background(), `DROP SCHEMA `+identifier+` CASCADE`); e != nil {
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
		if entry.Name() >= "029" {
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
 INSERT INTO users(email,name,password_hash) VALUES('migration@example.test','Parent','unused');
 INSERT INTO students(name,name_search) VALUES('One','one'),('Two','two');
 INSERT INTO notifications(user_id,student_id,kind,object_id,event_version,audience,email_state,attempts,sent_at,read_at) VALUES
 (1,1,'participation_confirmed',1,'1','family','sent',2,'2026-10-01T09:00Z','2026-10-01T10:00Z'),
 (1,2,'participation_confirmed',1,'1','family','pending',1,NULL,NULL),
 (1,1,'participation_confirmed',2,'1','family','pending',7,NULL,NULL),
 (1,1,'participation_confirmed',3,'1','family','suppressed',0,NULL,NULL),
 (1,1,'participation_confirmed',4,'1','family','pending',2,NULL,NULL),
 (1,NULL,'password_reset',1,'1','account','external',0,NULL,NULL);
 CREATE TABLE notification_delivery_retries(id integer);
 `); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool, core.Migrations); err == nil {
		t.Fatal("expected conflicting table migration failure")
	}
	var valid bool
	if err = pool.QueryRow(ctx, `SELECT to_regclass('notification_deliveries') IS NULL AND (SELECT count(*)=6 FROM notifications) AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='notifications' AND column_name='email_state')`).Scan(&valid); err != nil || !valid {
		t.Fatal("partial migration", err)
	}
	if _, err = pool.Exec(ctx, `DROP TABLE notification_delivery_retries`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
			t.Fatal(err)
		}
	}
	if err = pool.QueryRow(ctx, `SELECT
 (SELECT count(*)=9 FROM notification_deliveries) AND
 (SELECT status='sent' AND attempts=3 AND sent_at='2026-10-01T09:00Z' AND delivered_at IS NULL FROM notification_deliveries WHERE channel='email' AND object_id=1) AND
 (SELECT status='failed' AND attempts=7 AND last_error='attempts_exhausted' AND next_attempt_at IS NULL FROM notification_deliveries WHERE channel='email' AND object_id=2) AND
 (SELECT status='failed' AND last_error='recipient_unavailable' FROM notification_deliveries WHERE channel='email' AND object_id=3) AND
 (SELECT status='pending' AND attempts=2 AND next_attempt_at IS NOT NULL FROM notification_deliveries WHERE channel='email' AND object_id=4) AND
 (SELECT count(*)=6 FROM notifications) AND
 (SELECT read_at='2026-10-01T10:00Z' FROM notifications WHERE student_id=1 AND kind='participation_confirmed' AND object_id=1) AND
 NOT EXISTS(SELECT 1 FROM notification_deliveries WHERE channel='email' AND kind='password_reset')`).Scan(&valid); err != nil || !valid {
		t.Fatal("legacy state changed", err)
	}
}
