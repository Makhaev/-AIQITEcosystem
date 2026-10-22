package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScheduleAndStatus(t *testing.T) {
	c := Config{State: t.TempDir(), Store: t.TempDir(), Hour: 3}
	if err := os.Chmod(c.Store, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Store, ".aiq-backup-store"), []byte("aiq-backup-v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	boundary := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC) // 03:00 Moscow
	if err := writeJSON(filepath.Join(c.State, "status.json"), Status{State: "ok", Success: boundary, ScheduledDay: "2026-10-18"}); err != nil {
		t.Fatal(err)
	}
	if c.Due(boundary.Add(-time.Second)) != "" || c.Due(boundary) != "2026-10-19" {
		t.Fatal("Moscow boundary")
	}
	if c.Due(boundary.Add(72*time.Hour)) != "2026-10-22" {
		t.Fatal("catch-up must cover latest due date")
	}
	if c.Health(boundary) != nil || c.Health(boundary.Add(27*time.Hour)) == nil {
		t.Fatal("stale success health")
	}
	if err := writeJSON(filepath.Join(c.State, "status.json"), Status{State: "failed", Success: boundary}); err != nil {
		t.Fatal(err)
	}
	if c.Health(boundary) == nil {
		t.Fatal("recent success masked failure")
	}
	if r, err := c.Restore(context.Background(), "../../production"); err == nil || r.Database != "" {
		t.Fatal("unsafe archive ID")
	}
}

func TestUnpublishedDirectoriesNeverRotate(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".partial-crashed", "operator-notes", "aiq-not-a-backup"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	names, err := copies(dir)
	if err != nil || len(names) != 0 {
		t.Fatal("temporary/unrelated directory included")
	}
}
