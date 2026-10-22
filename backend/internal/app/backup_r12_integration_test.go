package app_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/app"
	"aiq.local/core/internal/backup"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/config"
	"aiq.local/core/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Explicit opt-in; never use TEST_DATABASE_URL's schemas as a whole-DB fixture.
// Every created database has a random name and is dropped only by its creator.
func TestR12BackupAcceptance(t *testing.T) {
	maintenance := os.Getenv("R12_TEST_DATABASE_URL")
	if maintenance == "" {
		t.Skip("R12_TEST_DATABASE_URL required (CREATEDB on isolated PostgreSQL)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	base, err := pgx.Connect(ctx, maintenance)
	if err != nil {
		t.Fatal("connect fixture", err)
	}
	defer base.Close(context.Background())
	var token [8]byte
	if _, err = rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	name := "aiq_r12_" + hex.EncodeToString(token[:])
	if _, err = base.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE template0"); err != nil {
		t.Fatal(err)
	}
	drop := func(db string) {
		if _, e := base.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{db}.Sanitize()+" WITH (FORCE)"); e != nil {
			t.Error("fixture cleanup", e)
		}
	}
	defer drop(name)
	u, err := url.Parse(maintenance)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	sourceURL := u.String()
	pool, err := pgxpool.New(ctx, sourceURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
		t.Fatal(err)
	}
	svc := users.Service{Repo: &users.Repository{Pool: pool}}
	adminUser, err := svc.Bootstrap(ctx, users.CreateInput{Email: "admin@example.test", Name: "R12 Admin", Password: adminPassword})
	if err != nil {
		t.Fatal(err)
	}
	handler, _, err := app.Router(pool, config.Config{Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	f := &fixture{pool: pool, server: server, admin: adminUser}
	admin := f.adminBrowser(t)
	teacherUser, _ := provision(t, f, admin, "r12-teacher@example.test", "teacher")
	teacher := teacherCreate(t, admin, map[string]any{"name": "R12 teacher", "user_id": teacherUser.ID})
	student := studentCreate(t, admin, map[string]any{"name": "R12 pupil"})
	parent := parentCreate(t, admin, map[string]any{"name": "R12 parent", "phone": "+79991234567"})
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", student.ID), map[string]any{"parent_id": parent.ID, "relationship": "parent", "is_primary": true}, 201)
	p := programCreate(t, admin, map[string]any{"name": "R12 program", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": student.ID, "starts_on": "2000-01-01"}, 201)
	now := time.Now().UTC()
	l := lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, now.Add(-3*time.Hour).Format(time.RFC3339Nano), now.Add(-2*time.Hour).Format(time.RFC3339Nano)))
	expect(t, admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(student.ID, "present", "R12 saved mark")), 204)
	expect(t, admin, "POST", lessonPath(l.ID)+"/complete", nil, 200)
	expect(t, admin, "POST", lessonPath(l.ID)+"/snapshot", map[string]any{"program_name": "R12 corrected program", "topic": "R12 corrected topic", "reason": "Recovery fixture"}, 200)
	task := taskCreate(t, admin, taskBody(adminUser.ID, now.Add(time.Hour)))
	store := t.TempDir()
	state := t.TempDir()
	if err = os.Chmod(store, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(store, ".aiq-backup-store"), []byte("aiq-backup-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c := backup.Config{URL: sourceURL, Store: store, State: state, BinDir: os.Getenv("PG_BIN"), Hour: 3}
	report := map[string]any{"started_at": time.Now().UTC(), "kind": "native-postgresql-r12", "production_storage_verified": false}
	var checks []string
	defer func() {
		report["checks"] = checks
		report["passed"] = !t.Failed()
		if path := os.Getenv("R12_REPORT_PATH"); path != "" {
			b, _ := json.MarshalIndent(report, "", "  ")
			if e := os.WriteFile(path, append(b, '\n'), 0600); e != nil {
				t.Error(e)
			}
		}
	}()
	list := func() []string {
		entries, e := os.ReadDir(store)
		if e != nil {
			t.Fatal(e)
		}
		var names []string
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "aiq-") {
				names = append(names, entry.Name())
			}
		}
		return names
	}
	zone, _ := time.LoadLocation("Europe/Moscow")
	// Eight due dates use the very same scheduler path as the daemon.
	var firstID string
	for i := 8; i >= 1; i-- {
		due := time.Now().In(zone).AddDate(0, 0, -i)
		due = time.Date(due.Year(), due.Month(), due.Day(), 12, 0, 0, 0, zone)
		ran, e := c.Tick(ctx, due)
		if e != nil || !ran {
			t.Fatalf("scheduled backup: %v", e)
		}
		if i == 8 {
			s, _ := c.ReadStatus()
			firstID = s.Backup
		}
		if ran, e = c.Tick(ctx, due); e != nil || ran {
			t.Fatal("duplicate daily run")
		}
	}
	if len(list()) != 7 {
		t.Fatal("retention must keep exactly seven")
	}
	for _, id := range list() {
		if id == firstID {
			t.Fatal("rotation kept oldest backup")
		}
	}
	checks = append(checks, "eight_daily_due_dates_keep_seven", "no_duplicate_daily_backup")
	// Real wall clock, real worker: wait across the next scheduled minute.
	next := time.Now().In(zone).Add(time.Minute).Truncate(time.Minute)
	c.Hour = next.Hour()
	c.Minute = next.Minute()
	// Seed the previous due day for this changed test-only schedule, including
	// the midnight boundary. Production uses a stable BACKUP_TIME.
	seed, _ := c.ReadStatus()
	seed.ScheduledDay = next.AddDate(0, 0, -1).Format("2006-01-02")
	seedJSON, _ := json.Marshal(seed)
	if err = os.WriteFile(filepath.Join(state, "status.json"), seedJSON, 0600); err != nil {
		t.Fatal(err)
	}
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	event := make(chan error, 1)
	go func() {
		defer close(done)
		c.Serve(workerCtx, func(_ backup.Status, e error) {
			select {
			case event <- e:
			default:
			}
		})
	}()
	select {
	case e := <-event:
		if e != nil {
			stop()
			<-done
			t.Fatal(e)
		}
	case <-time.After(95 * time.Second):
		stop()
		<-done
		t.Fatal("real scheduler did not fire")
	}
	stop()
	<-done
	if time.Now().Before(next) {
		t.Fatal("scheduler ran before due time")
	}
	report["scheduled_at"] = next
	report["scheduled_completed_at"] = time.Now().UTC()
	checks = append(checks, "real_clock_scheduled_worker_run")
	before := list()
	saved := map[string][]byte{}
	for _, id := range before {
		b, e := os.ReadFile(filepath.Join(store, id, "database.dump"))
		if e != nil {
			t.Fatal(e)
		}
		saved[id] = b
	}
	assertPreserved := func() {
		t.Helper()
		if !reflect.DeepEqual(before, list()) {
			t.Fatal("failure rotated backups")
		}
		for id, b := range saved {
			after, e := os.ReadFile(filepath.Join(store, id, "database.dump"))
			if e != nil || !reflect.DeepEqual(b, after) {
				t.Fatal("failure changed saved backup")
			}
		}
		if c.Health(time.Now()) == nil {
			t.Fatal("failure is not visible")
		}
	}
	if err = os.Remove(filepath.Join(store, ".aiq-backup-store")); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Run(ctx, ""); err == nil {
		t.Fatal("missing storage accepted")
	}
	assertPreserved()
	if err = os.WriteFile(filepath.Join(store, ".aiq-backup-store"), []byte("aiq-backup-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := c
	bad.BinDir = filepath.Join(store, "missing-tools")
	if _, err = bad.Run(ctx, ""); err == nil {
		t.Fatal("missing tools accepted")
	}
	assertPreserved()
	checks = append(checks, "storage_and_utility_failure_preserve_seven_and_fail_health")
	// Source role can connect but cannot read tables: pg_dump really fails.
	role := "r12_reader_" + hex.EncodeToString(token[:])
	password := "R12-" + hex.EncodeToString(token[:])
	if _, err = base.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, e := base.Exec(context.Background(), "DROP ROLE "+pgx.Identifier{role}.Sanitize())
		if e != nil {
			t.Error(e)
		}
	}()
	bad = c
	readerURL := *u
	readerURL.User = url.UserPassword(role, password)
	bad.URL = readerURL.String()
	if _, err = bad.Run(ctx, ""); err == nil {
		t.Fatal("unprivileged dump accepted")
	}
	s, _ := c.ReadStatus()
	if s.Stage != "dump" {
		t.Fatalf("expected pg_dump failure, got %s", s.Stage)
	}
	assertPreserved()
	checks = append(checks, "pg_dump_error_preserves_previous_archives")
	entries, err := os.ReadDir(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".partial-") {
			t.Fatal("failed dump left partial archive")
		}
	}
	// A concurrent worker must neither dump nor overwrite the active status.
	locker, err := pgx.Connect(ctx, sourceURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = locker.Exec(ctx, "SELECT pg_advisory_lock(8021012)"); err != nil {
		locker.Close(ctx)
		t.Fatal(err)
	}
	prior, _ := c.ReadStatus()
	_, busyErr := c.Run(ctx, "")
	afterBusy, _ := c.ReadStatus()
	locker.Close(ctx)
	if busyErr == nil || !reflect.DeepEqual(prior, afterBusy) {
		t.Fatal("concurrent backup changed active status")
	}
	checks = append(checks, "concurrent_backup_excluded_and_partial_files_removed")
	// A successful retry clears the visible failure only after publication.
	if _, err = base.Exec(ctx, "GRANT pg_read_all_data TO "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	m, err := bad.Run(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Health(time.Now()) != nil || len(list()) != 7 {
		t.Fatal("retry/rotation unhealthy")
	}
	checks = append(checks, "successful_retry_clears_failure")
	checks = append(checks, "backup_with_read_only_role")
	archive := filepath.Join(store, m.ID, "database.dump")
	original, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(archive, original[:len(original)/2], 0600); err != nil {
		t.Fatal(err)
	}
	if r, e := c.Restore(ctx, m.ID); e == nil || r.Database != "" {
		t.Fatal("corrupt archive reached destination creation")
	}
	if err = os.WriteFile(archive, original, 0600); err != nil {
		t.Fatal(err)
	}
	checks = append(checks, "corrupt_archive_rejected_before_database_creation")
	r, err := c.Restore(ctx, m.ID)
	if r.Database != "" {
		defer drop(r.Database)
	}
	if err != nil {
		t.Fatal("restore", err)
	}
	report["restore"] = r
	for _, table := range []string{"users", "students", "parents", "student_parents", "group_enrollments", "lessons", "lesson_participants", "attendance", "lesson_snapshot_versions", "tasks", "audit_logs"} {
		if m.Tables["public."+table].Rows == 0 {
			t.Fatalf("empty acceptance fixture %s", table)
		}
	}
	checks = append(checks, r.Checks...)
	rURL := *u
	rURL.Path = "/" + r.Database
	restored, err := pgxpool.New(ctx, rURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	h, _, err := app.Router(restored, config.Config{Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	rs := httptest.NewServer(h)
	defer rs.Close()
	rf := &fixture{pool: restored, server: rs, admin: adminUser}
	rb := rf.adminBrowser(t)
	expect(t, rb, "GET", "/auth/me", nil, 200)
	for _, path := range []string{fmt.Sprintf("/students/%d/overview", student.ID), lessonPath(l.ID) + "/attendance", lessonPath(l.ID) + "/snapshot-history", taskPath(task.ID), "/audit-logs"} {
		expect(t, rb, "GET", path, nil, 200)
	}
	journal := lessonJournal(t, rb, l.ID)
	if journalStudent(t, journal, student.ID).Status != "present" {
		t.Fatal("restored mark mismatch")
	}
	if taskGet(t, rb, task.ID).Title != task.Title {
		t.Fatal("restored task mismatch")
	}
	checks = append(checks, "restored_login_and_session", "student_history_attendance_tasks_audit_http")
	// Source is untouched even though recovered login writes sessions and audit.
	if _, err = pool.Exec(ctx, "SET TIME ZONE 'UTC'"); err != nil {
		t.Fatal(err)
	}
	actual, err := backup.Inventory(ctx, pool)
	if err != nil || !reflect.DeepEqual(actual, m.Tables) {
		t.Fatal("recovery modified source")
	}
	checks = append(checks, "source_database_unchanged")
}
