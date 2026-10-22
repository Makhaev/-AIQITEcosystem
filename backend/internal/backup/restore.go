package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RestoreReport struct {
	Status     string    `json:"status"`
	Backup     Manifest  `json:"backup"`
	Database   string    `json:"database"`
	RestoredAt time.Time `json:"restored_at"`
	Seconds    float64   `json:"restore_seconds"`
	Server     string    `json:"target_postgresql"`
	Utility    string    `json:"pg_restore"`
	Checks     []string  `json:"checks"`
}

// Restore always creates a randomly named database from template0. No caller-
// supplied destination, --clean, --create, or DROP DATABASE can reach production.
// c.URL identifies a maintenance database on the recovery server. The operator
// may use a different server from the source and need not have source access.
func (c Config) Restore(ctx context.Context, id string) (report RestoreReport, err error) {
	started := time.Now().UTC()
	report.Status = "failed"
	report.RestoredAt = started
	defer func() { report.Seconds = time.Since(started).Seconds() }()
	if !backupName.MatchString(id) {
		return report, errors.New("invalid_backup_id")
	}
	if err = checkStore(c.Store); err != nil {
		return report, err
	}
	dir := filepath.Join(c.Store, id)
	// Open/read the selected archive before creating a destination. Operators
	// suspend rotation during a recovery drill (documented maintenance procedure).
	if report.Backup, err = validate(dir); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "sha256_and_size")
	var version strings.Builder
	if err = c.command(ctx, "", &version, "pg_restore", "--version"); err != nil {
		return report, err
	}
	report.Utility = strings.TrimSpace(version.String())
	conn, err := pgx.Connect(ctx, c.URL)
	if err != nil {
		return report, errors.New("recovery_connection_failed")
	}
	defer conn.Close(context.Background())
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		return report, err
	}
	report.Database = "aiq_restore_" + hex.EncodeToString(random[:])
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{report.Database}.Sanitize()+" TEMPLATE template0"); err != nil {
		return report, errors.New("create_recovery_database_failed")
	}
	// Both clients target the newly created database; credentials stay in env.
	pc, err := pgxpool.ParseConfig(c.URL)
	if err != nil {
		return report, err
	}
	pc.ConnConfig.Database = report.Database
	pc.ConnConfig.RuntimeParams["timezone"] = "UTC"
	if err = c.command(ctx, report.Database, io.Discard, "pg_restore", "--no-password", "--exit-on-error", "--single-transaction", "--no-owner", "--no-privileges", filepath.Join(dir, "database.dump")); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "pg_restore_single_transaction_constraints")
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return report, err
	}
	defer pool.Close()
	if err = pool.QueryRow(ctx, "SELECT version()").Scan(&report.Server); err != nil {
		return report, err
	}
	tables, err := Inventory(ctx, pool)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(tables, report.Backup.Tables) {
		return report, errors.New("restored_data_mismatch")
	}
	report.Checks = append(report.Checks, "all_table_counts_and_row_hashes")
	// Ensure current binary and backup schema agree BEFORE running migrations;
	// an old archive must not silently pass acceptance after being upgraded.
	var names []string
	rows, err := pool.Query(ctx, "SELECT name FROM schema_migrations ORDER BY name")
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return report, err
		}
		names = append(names, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return report, err
	}
	entries, err := core.Migrations.ReadDir("migrations")
	if err != nil {
		return report, err
	}
	var expected []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			expected = append(expected, "migrations/"+e.Name())
		}
	}
	if !reflect.DeepEqual(names, expected) {
		return report, errors.New("migration_version_mismatch")
	}
	if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
		return report, err
	}
	after, err := Inventory(ctx, pool)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(tables, after) {
		return report, errors.New("migration_replay_changed_data")
	}
	report.Checks = append(report.Checks, "migrations_exact_and_idempotent")
	report.Status = "database_verified_application_checks_required"
	return report, nil
}
