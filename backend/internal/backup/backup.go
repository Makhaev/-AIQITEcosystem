// Package backup owns publication and verification of whole-database archives.
package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	URL, Store, State, BinDir string
	Hour, Minute              int // Europe/Moscow
}
type Table struct {
	Rows   int64  `json:"rows"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	ID             string           `json:"id"`
	CreatedAt      time.Time        `json:"created_at"`
	Server         string           `json:"postgresql"`
	DumpVersion    string           `json:"pg_dump"`
	RestoreVersion string           `json:"pg_restore"`
	Bytes          int64            `json:"bytes"`
	SHA256         string           `json:"sha256"`
	Seconds        float64          `json:"backup_seconds"`
	Tables         map[string]Table `json:"tables"`
}
type Status struct {
	State        string    `json:"state"`
	Stage        string    `json:"stage"`
	Attempt      time.Time `json:"attempt_at"`
	Success      time.Time `json:"last_success_at"`
	ScheduledDay string    `json:"scheduled_day"`
	Backup       string    `json:"backup"`
}

// Do not expose URLs, child stderr (may contain row values), or passwords in logs.
func (c Config) command(ctx context.Context, db string, out io.Writer, name string, args ...string) error {
	pc, err := pgx.ParseConfig(c.URL)
	if err != nil {
		return errors.New("connection_config")
	}
	if db != "" {
		u, e := url.Parse(c.URL)
		if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			return errors.New("postgres_url_required")
		}
		u.User = url.User(pc.User)
		query := u.Query()
		query.Del("password")
		query.Del("dbname")
		query.Del("database")
		u.RawQuery = query.Encode()
		if db != "source" {
			u.Path = "/" + db
			u.RawPath = ""
		}
		args = append(args, "--dbname="+u.String())
	}
	path := name
	if c.BinDir != "" {
		path = filepath.Join(c.BinDir, name)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = os.Environ()
	// Password stays in the child environment; argv contains only a sanitized DSN.
	cmd.Env = append(cmd.Env, "PGDATABASE="+c.URL, "PGPASSWORD="+pc.Password, "PGCONNECT_TIMEOUT=10")
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s_failed", name)
	}
	return nil
}

func (c Config) locked(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.Connect(ctx, c.URL)
	if err != nil {
		return nil, errors.New("database_unavailable")
	}
	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(8021012)").Scan(&locked); err != nil || !locked {
		conn.Close(context.Background())
		return nil, errors.New("backup_busy")
	}
	return conn, nil
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Canonical row hashes and counts are taken from the SAME exported snapshot as
// pg_dump. Ordered JSON also checks values and relationships, not just counts.
func Inventory(ctx context.Context, q querier) (map[string]Table, error) {
	rows, err := q.Query(ctx, `SELECT schemaname,tablename FROM pg_tables WHERE schemaname NOT IN ('pg_catalog','information_schema') ORDER BY 1,2`)
	if err != nil {
		return nil, err
	}
	var names [][2]string
	for rows.Next() {
		var n [2]string
		if err = rows.Scan(&n[0], &n[1]); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, n)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := map[string]Table{}
	for _, n := range names {
		r, err := q.Query(ctx, "SELECT to_jsonb(t)::text FROM "+pgx.Identifier{n[0], n[1]}.Sanitize()+` t ORDER BY to_jsonb(t)::text COLLATE "C"`)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		var count int64
		for r.Next() {
			var s string
			if err = r.Scan(&s); err != nil {
				r.Close()
				return nil, err
			}
			io.WriteString(h, s+"\n")
			count++
		}
		r.Close()
		if err = r.Err(); err != nil {
			return nil, err
		}
		result[n[0]+"."+n[1]] = Table{count, hex.EncodeToString(h.Sum(nil))}
	}
	return result, nil
}

func (c Config) ReadStatus() (Status, error) {
	var s Status
	err := readJSON(filepath.Join(c.State, "status.json"), &s)
	return s, err
}
func (c Config) Health(now time.Time) error {
	s, err := c.ReadStatus()
	if err != nil || s.State == "failed" || s.Success.IsZero() || now.Sub(s.Success) > 26*time.Hour || (s.State == "running" && now.Sub(s.Attempt) > 2*time.Hour) {
		return errors.New("backup_unhealthy")
	}
	// An old green status must not mask an unwritable/full status volume.
	probe, err := os.CreateTemp(c.State, ".health-*")
	if err != nil {
		return errors.New("status_unwritable")
	}
	_, err = probe.Write([]byte("health\n"))
	closeErr := probe.Close()
	removeErr := os.Remove(probe.Name())
	if err != nil || closeErr != nil || removeErr != nil {
		return errors.New("status_unwritable")
	}
	return checkStore(c.Store)
}

func (c Config) Run(ctx context.Context, scheduledDay string) (m Manifest, err error) {
	started := time.Now().UTC()
	if err = os.MkdirAll(c.State, 0700); err != nil {
		return m, errors.New("state_unavailable")
	}
	conn, err := c.locked(ctx)
	// A competing invocation must not overwrite the active worker's status.
	if err != nil {
		if err.Error() != "backup_busy" {
			s, _ := c.ReadStatus()
			s.State = "failed"
			s.Stage = "connect"
			s.Attempt = started
			_ = writeJSON(filepath.Join(c.State, "status.json"), s)
		}
		return m, err
	}
	defer conn.Close(context.Background())
	s, _ := c.ReadStatus()
	s.State = "running"
	s.Stage = "storage"
	s.Attempt = started
	defer func() {
		if err != nil {
			s.State = "failed"
		} else {
			s.State = "ok"
			s.Stage = "complete"
			s.Success = time.Now().UTC()
			s.Backup = m.ID
			if scheduledDay != "" {
				s.ScheduledDay = scheduledDay
			}
		}
		if e := writeJSON(filepath.Join(c.State, "status.json"), s); e != nil {
			err = errors.New("status_write_failed")
		}
	}()
	if err = writeJSON(filepath.Join(c.State, "status.json"), s); err != nil {
		return m, err
	}
	if err = checkStore(c.Store); err != nil {
		return m, err
	}
	// A single application database owns this storage root.
	tmp, err := os.MkdirTemp(c.Store, ".partial-")
	if err != nil {
		return m, err
	}
	defer os.RemoveAll(tmp)
	s.Stage = "snapshot"
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return m, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SET LOCAL TIME ZONE 'UTC'"); err != nil {
		return m, err
	}
	var snapshot string
	if err = tx.QueryRow(ctx, "SELECT pg_export_snapshot(), version()").Scan(&snapshot, &m.Server); err != nil {
		return m, err
	}
	m.CreatedAt = started
	m.ID = "aiq-" + started.Format("20060102T150405.000000000Z")
	var v strings.Builder
	if err = c.command(ctx, "", &v, "pg_dump", "--version"); err != nil {
		return m, err
	}
	m.DumpVersion = strings.TrimSpace(v.String())
	v.Reset()
	if err = c.command(ctx, "", &v, "pg_restore", "--version"); err != nil {
		return m, err
	}
	m.RestoreVersion = strings.TrimSpace(v.String())
	s.Stage = "dump"
	file := filepath.Join(tmp, "database.dump")
	if err = c.command(ctx, "source", io.Discard, "pg_dump", "--no-password", "--format=custom", "--snapshot="+snapshot, "--file="+file); err != nil {
		return m, err
	}
	if err = os.Chmod(file, 0600); err != nil {
		return m, err
	}
	s.Stage = "inventory"
	if m.Tables, err = Inventory(ctx, tx); err != nil {
		return m, err
	}
	if err = tx.Commit(ctx); err != nil {
		return m, err
	}
	s.Stage = "archive_validation"
	// Read/decompress ALL archive data, not only the table of contents.
	if err = c.command(ctx, "", io.Discard, "pg_restore", "--exit-on-error", "--file=-", file); err != nil {
		return m, err
	}
	if m.SHA256, m.Bytes, err = checksum(file); err != nil {
		return m, err
	}
	f, e := os.OpenFile(file, os.O_RDWR, 0)
	if e != nil {
		return m, e
	}
	e = f.Sync()
	f.Close()
	if e != nil {
		return m, e
	}
	h, n, e := checksum(file)
	if e != nil || h != m.SHA256 || n != m.Bytes {
		return m, errors.New("checksum_readback_failed")
	}
	m.Seconds = time.Since(started).Seconds()
	if err = writeJSON(filepath.Join(tmp, "manifest.json"), m); err != nil {
		return m, err
	}
	s.Stage = "publish"
	if err = os.Rename(tmp, filepath.Join(c.Store, m.ID)); err != nil {
		return m, err
	}
	if err = syncDir(c.Store); err != nil {
		return m, err
	}
	if _, err = validate(filepath.Join(c.Store, m.ID)); err != nil {
		return m, err
	}
	s.Stage = "rotation"
	names, err := copies(c.Store)
	if err != nil {
		return m, err
	}
	// Only committed directories participate; partial/error files never count.
	for len(names) > 7 {
		if err = os.RemoveAll(filepath.Join(c.Store, names[0])); err != nil {
			return m, err
		}
		names = names[1:]
	}
	return m, nil
}

func (c Config) Due(now time.Time) string {
	zone, _ := time.LoadLocation("Europe/Moscow")
	local := now.In(zone)
	due := time.Date(local.Year(), local.Month(), local.Day(), c.Hour, c.Minute, 0, 0, zone)
	if local.Before(due) {
		due = due.AddDate(0, 0, -1)
	}
	day := due.Format("2006-01-02")
	s, _ := c.ReadStatus()
	if s.ScheduledDay >= day {
		return ""
	}
	return day
}

// One catch-up run after downtime; errors are retried after fifteen minutes.
// Tick is also used by the acceptance test to cross actual daily boundaries.
func (c Config) Tick(ctx context.Context, now time.Time) (bool, error) {
	day := c.Due(now)
	if day == "" {
		return false, nil
	}
	_, err := c.Run(ctx, day)
	return true, err
}

func (c Config) Serve(ctx context.Context, report func(Status, error)) {
	for ctx.Err() == nil {
		job, stop := context.WithTimeout(ctx, 2*time.Hour)
		ran, err := c.Tick(job, time.Now())
		stop()
		if ran {
			s, _ := c.ReadStatus()
			report(s, err)
		}
		delay := 30 * time.Second
		if err != nil {
			delay = 15 * time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
