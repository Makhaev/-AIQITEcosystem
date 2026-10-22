package app_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/modules/tasks"
)

// Unit-test the SQL time policy directly with a supplied clock, without writes.
func TestTaskClosureTimePolicy(t *testing.T) {
	f := setup(t)
	old := time.Date(2024, 2, 29, 21, 0, 0, 0, time.UTC)
	now := old.Add(48 * time.Hour)
	for _, tc := range []struct {
		name           string
		from           any
		to             string
		previous, want *time.Time
	}{
		{"new", nil, "todo", nil, nil},
		{"start", "todo", "in_progress", nil, nil},
		{"finish", "in_progress", "done", nil, &now},
		{"cancel todo", "todo", "cancelled", nil, &now},
		{"cancel in progress", "in_progress", "cancelled", nil, &now},
		{"closed to closed", "done", "cancelled", &old, &old},
		{"legacy closed to closed", "done", "cancelled", nil, nil},
		{"reopen", "done", "in_progress", &old, nil},
		{"legacy reopen", "done", "in_progress", nil, nil},
		{"repeat todo", "todo", "todo", nil, nil},
		{"repeat in progress", "in_progress", "in_progress", nil, nil},
		{"repeat done", "done", "done", &old, &old},
		{"repeat cancelled", "cancelled", "cancelled", &old, &old},
		{"repeat legacy done", "done", "done", nil, nil},
		{"repeat legacy cancelled", "cancelled", "cancelled", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got *time.Time
			if err := f.pool.QueryRow(context.Background(), `SELECT task_closure_time($1,$2,$3,$4)`, tc.from, tc.to, tc.previous, now).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (tc.want == nil) || got != nil && !got.Equal(*tc.want) {
				t.Fatalf("closure=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestTaskLifecycleBothPatchRoutes(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	edges := map[string]bool{"todo/in_progress": true, "todo/cancelled": true, "in_progress/done": true, "in_progress/cancelled": true, "done/in_progress": true, "done/cancelled": true}
	for _, suffix := range []string{"", "/status"} {
		for _, from := range []string{"todo", "in_progress", "done", "cancelled"} {
			for _, to := range []string{"todo", "in_progress", "done", "cancelled"} {
				t.Run(suffix+"/"+from+"/"+to, func(t *testing.T) {
					v := taskWithStatus(t, admin, taskBody(f.admin.ID, time.Now().Add(time.Hour)), from)
					before := taskWriteSnapshot(t, f)
					want := 200
					if from != to && !edges[from+"/"+to] {
						want = 409
					}
					body := expect(t, admin, "PATCH", taskPath(v.ID)+suffix, map[string]any{"status": to}, want)
					if want != 200 || from == to {
						if taskWriteSnapshot(t, f) != before {
							t.Fatal("invalid/repeated status changed task or audit")
						}
						return
					}
					var got tasks.Task
					decode(t, body, &got)
					if to == "in_progress" {
						if got.ClosedAt != nil {
							t.Fatal("open task has closure")
						}
					} else if from == "done" {
						if got.ClosedAt == nil || !got.ClosedAt.Equal(*v.ClosedAt) {
							t.Fatal("closed-to-closed changed closure")
						}
					} else if got.ClosedAt == nil || got.ClosedAt.Before(v.CreatedAt) {
						t.Fatal("new closure has no server timestamp")
					}
				})
			}
		}
	}
	for _, status := range []string{"in_progress", "done", "cancelled"} {
		body := taskBody(f.admin.ID, time.Now().Add(time.Hour))
		body["status"] = status
		expect(t, admin, "POST", "/tasks", body, 400)
	}
	v := taskCreate(t, admin, taskBody(f.admin.ID, time.Now().Add(time.Hour)))
	for _, suffix := range []string{"", "/status"} {
		expect(t, admin, "PATCH", taskPath(v.ID)+suffix, map[string]any{"status": "in_progress", "closed_at": time.Now()}, 400)
	}
	// A mixed PATCH must also reject the edge and roll back unrelated field edits.
	before := taskWriteSnapshot(t, f)
	expect(t, admin, "PATCH", taskPath(v.ID), map[string]any{"status": "done", "title": "Must not persist"}, 409)
	if taskWriteSnapshot(t, f) != before {
		t.Fatal("mixed PATCH bypassed transition rules")
	}
}

func TestTaskCreatorAfterRoleChangeBothRoutes(t *testing.T) {
	a := setupAttendance(t)
	a.lesson(t, time.Hour, 2*time.Hour)
	creator, owner := provision(t, a.f, a.admin, "creator-r07@example.test", "leader")
	var assignee int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&assignee); err != nil {
		t.Fatal(err)
	}
	body := taskBody(assignee, time.Now().Add(time.Hour))
	body["object_type"], body["object_id"] = "student", a.firstID
	v := taskCreate(t, owner, body)
	path := taskPath(v.ID)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", creator.ID), map[string]any{"roles": []string{"curator"}}, 200)
	owner = a.f.browser(t)
	owner.login(t, creator.Email, "TestOnly-New-Account-9372")
	got := taskGet(t, owner, v.ID)
	if got.ObjectID != nil || got.ObjectType != nil || got.ObjectName != nil {
		t.Fatal("creator task exposed inaccessible linked object")
	}
	if taskPage(t, owner, "").Total != 1 {
		t.Fatal("creator lost task discovery after role change")
	}
	expect(t, owner, "GET", fmt.Sprintf("/students/%d", a.firstID), nil, 404)
	for _, fields := range []map[string]any{
		{"title": "Forbidden"}, {"description": "Forbidden"}, {"assignee_id": assignee}, {"due_at": time.Now()},
		{"object_type": nil, "object_id": nil}, {"status": "cancelled", "title": "Forbidden"},
	} {
		expect(t, owner, "PATCH", path, fields, 403)
	}
	expect(t, owner, "PATCH", path, map[string]any{"status": "in_progress"}, 403)
	expect(t, a.own, "PATCH", path, map[string]any{"status": "in_progress"}, 200)
	expect(t, a.own, "PATCH", path, map[string]any{"status": "cancelled"}, 403)
	expect(t, a.own, "PATCH", path+"/status", map[string]any{"status": "done"}, 200)
	first := taskGet(t, owner, v.ID).ClosedAt
	expect(t, a.own, "PATCH", path, map[string]any{"status": "in_progress"}, 403)
	decode(t, expect(t, owner, "PATCH", path, map[string]any{"status": "in_progress"}, 200), &got)
	if got.ClosedAt != nil || got.ObjectID != nil {
		t.Fatal("reopen kept closure or exposed object")
	}
	expect(t, a.own, "PATCH", path, map[string]any{"status": "done"}, 200)
	second := taskGet(t, owner, v.ID).ClosedAt
	if first == nil || second == nil || !second.After(*first) {
		t.Fatal("second closure did not get a new time")
	}
	decode(t, expect(t, owner, "PATCH", path+"/status", map[string]any{"status": "cancelled"}, 200), &got)
	if !got.ClosedAt.Equal(*second) || got.ObjectID != nil {
		t.Fatal("cancellation changed closed time or exposed object")
	}
	expect(t, owner, "PATCH", path, map[string]any{"status": "in_progress"}, 409)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", creator.ID), map[string]any{"blocked": true}, 200)
	expect(t, owner, "GET", path, nil, 401)
	expect(t, owner, "PATCH", path, map[string]any{"status": "cancelled"}, 401)
}

func TestTaskLifecycleAuditRollbackAndConcurrentRepeat(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	v := taskWithStatus(t, admin, taskBody(f.admin.ID, time.Now().Add(time.Hour)), "in_progress")
	before := taskWriteSnapshot(t, f)
	installCompletionAuditFailure(t, f, "task.status_changed")
	for _, suffix := range []string{"", "/status"} {
		expect(t, admin, "PATCH", taskPath(v.ID)+suffix, map[string]any{"status": "done"}, 500)
		if taskWriteSnapshot(t, f) != before {
			t.Fatal("failed audit persisted status or closed_at")
		}
	}
	if _, err := f.pool.Exec(context.Background(), `DROP TRIGGER reject_completion_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	results := make([]tasks.Task, 2)
	var wg sync.WaitGroup
	for i, suffix := range []string{"", "/status"} {
		wg.Add(1)
		go func(i int, suffix string) {
			defer wg.Done()
			<-gate
			decode(t, expect(t, admin, "PATCH", taskPath(v.ID)+suffix, map[string]any{"status": "done"}, 200), &results[i])
		}(i, suffix)
	}
	close(gate)
	wg.Wait()
	if results[0].ClosedAt == nil || !reflect.DeepEqual(results[0], results[1]) {
		t.Fatal("concurrent repetition changed closure")
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE object_type='task' AND object_id=$1 AND action='task.status_changed'`, v.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected start+finish audit only: %d %v", count, err)
	}
}
