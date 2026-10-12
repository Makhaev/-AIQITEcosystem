package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, connection string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration")
	}
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database is unavailable")
	}
	return pool, nil
}
func Translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Пользователь не найден.")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return httpx.E(409, "email_exists", "Такой email уже используется.")
	}
	return err
}
func Audit(ctx context.Context, tx pgx.Tx, actor *int64, action string, object int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,object_id,request_id) VALUES($1,$2,$3,$4)`, actor, action, object, httpx.RequestID(ctx))
	return err
}
func AuditObject(ctx context.Context, tx pgx.Tx, actor *int64, action, objectType string, object int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,object_type,object_id,request_id) VALUES($1,$2,$3,$4,$5)`, actor, action, objectType, object, httpx.RequestID(ctx))
	return err
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, files embed.FS) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8021001)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var done bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := files.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s failed: %w", name, err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES($1)`, name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
