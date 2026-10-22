package app_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/modules/tasks"
)

func taskBody(assignee int64, due time.Time) map[string]any {
	return map[string]any{"title": "Подготовить материалы", "description": "Передать готовые рабочие листы", "assignee_id": assignee, "due_at": due.Format(time.RFC3339Nano)}
}
func taskCreate(t *testing.T, b *browser, body map[string]any) tasks.Task {
	t.Helper()
	var result tasks.Task
	decode(t, expect(t, b, "POST", "/tasks", body, 201), &result)
	return result
}
func taskWithStatus(t *testing.T, b *browser, body map[string]any, status string) tasks.Task {
	t.Helper()
	v := taskCreate(t, b, body)
	if status == "done" || status == "in_progress" {
		expect(t, b, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "in_progress"}, 200)
	}
	if status == "done" || status == "cancelled" {
		expect(t, b, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": status}, 200)
	}
	return taskGet(t, b, v.ID)
}
func taskPath(id int64) string { return fmt.Sprintf("/tasks/%d", id) }
func taskGet(t *testing.T, b *browser, id int64) tasks.Task {
	t.Helper()
	var v tasks.Task
	decode(t, expect(t, b, "GET", taskPath(id), nil, 200), &v)
	return v
}
func taskPage(t *testing.T, b *browser, query string) tasks.Page[tasks.Task] {
	t.Helper()
	var v tasks.Page[tasks.Task]
	decode(t, expect(t, b, "GET", "/tasks"+query, nil, 200), &v)
	return v
}

func TestTasksRolesLifecycleReassignmentAndFilters(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	leaderUser, leader := provision(t, f, admin, "task-leader@example.test", "leader")
	teacherUser, teacher := provision(t, f, admin, "task-teacher@example.test", "teacher")
	curatorUser, curator := provision(t, f, admin, "task-curator@example.test", "curator")
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	v := taskCreate(t, leader, taskBody(teacherUser.ID, past))
	if v.CreatorID != leaderUser.ID || v.CreatorName != leaderUser.Name || v.AssigneeID != teacherUser.ID || v.Status != "todo" || !v.Overdue || v.ObjectID != nil {
		t.Fatalf("create fields: %+v", v)
	}
	taskCreate(t, admin, taskBody(curatorUser.ID, future))
	taskWithStatus(t, admin, taskBody(teacherUser.ID, past), "done")
	cancelled := taskWithStatus(t, admin, taskBody(teacherUser.ID, past), "cancelled")
	if p := taskPage(t, leader, "?overdue=true"); p.Total != 1 || p.Items[0].ID != v.ID {
		t.Fatalf("overdue includes closed tasks: %+v", p)
	}
	if p := taskPage(t, leader, "?overdue=false"); p.Total != 3 {
		t.Fatal("non-overdue filter")
	}
	if p := taskPage(t, teacher, "?page_size=1&page=2"); p.Total != 3 || len(p.Items) != 1 {
		t.Fatal("own scope pagination")
	}
	if p := taskPage(t, teacher, fmt.Sprintf("?assignee_id=%d", curatorUser.ID)); p.Total != 0 {
		t.Fatal("assignee filter widened access")
	}
	if p := taskPage(t, leader, fmt.Sprintf("?assignee_id=%d&status=todo&overdue=false", curatorUser.ID)); p.Total != 1 {
		t.Fatal("combined filters")
	}
	expect(t, curator, "GET", taskPath(v.ID), nil, 404)
	for _, b := range []*browser{teacher, curator} {
		expect(t, b, "POST", "/tasks", taskBody(teacherUser.ID, future), 403)
		expect(t, b, "PATCH", taskPath(v.ID), map[string]any{"assignee_id": curatorUser.ID}, 403)
		expect(t, b, "GET", "/tasks/assignees", nil, 403)
	}
	// A task-specific directory does not expose login emails or administrator APIs.
	directory := expect(t, leader, "GET", "/tasks/assignees?page_size=1&page=2", nil, 200)
	var options tasks.Page[tasks.Assignee]
	decode(t, directory, &options)
	if options.Total != 4 || len(options.Items) != 1 || bytes.Contains(directory, []byte("email")) {
		t.Fatal("task assignee directory leaks fields or ignores pagination")
	}
	expect(t, leader, "GET", "/users", nil, 403)
	expect(t, curator, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done"}, 404)
	expect(t, teacher, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "cancelled"}, 403)
	expect(t, teacher, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "in_progress"}, 200)
	expect(t, teacher, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done", "assignee_id": curatorUser.ID}, 400)
	expect(t, teacher, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done"}, 200)
	expect(t, teacher, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done"}, 200)
	if taskGet(t, teacher, v.ID).Overdue {
		t.Fatal("completed task is overdue")
	}
	expect(t, teacher, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "in_progress"}, 403)
	expect(t, teacher, "PATCH", taskPath(cancelled.ID)+"/status", map[string]any{"status": "done"}, 409)
	expect(t, leader, "PATCH", taskPath(v.ID), map[string]any{"assignee_id": curatorUser.ID, "status": "in_progress"}, 200)
	expect(t, teacher, "GET", taskPath(v.ID), nil, 404)
	expect(t, teacher, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done"}, 404)
	expect(t, curator, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done"}, 200)
	if got := taskGet(t, leader, v.ID); got.CreatorID != leaderUser.ID || got.AssigneeID != curatorUser.ID {
		t.Fatal("reassignment changed creator")
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE object_type='task' AND object_id=$1 AND action='task.status_changed'`, v.ID).Scan(&count); err != nil || count != 3 {
		t.Fatalf("idempotent transition audit: %d %v", count, err)
	}
	// Overdue is recomputed from due_at without updating the task's stored status.
	boundary := taskCreate(t, admin, taskBody(teacherUser.ID, future))
	if _, err := f.pool.Exec(context.Background(), `UPDATE tasks SET due_at=now()-interval '1 second' WHERE id=$1`, boundary.ID); err != nil {
		t.Fatal(err)
	}
	if !taskGet(t, teacher, boundary.ID).Overdue {
		t.Fatal("overdue was persisted instead of computed")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacherUser.ID), map[string]any{"blocked": true}, 200)
	expect(t, leader, "PATCH", taskPath(v.ID), map[string]any{"assignee_id": teacherUser.ID}, 400)
	expect(t, teacher, "GET", "/tasks", nil, 401)
	expect(t, f.browser(t), "GET", "/tasks", nil, 401)
}

func TestTasksRelatedObjectsValidationAndAtomicEdits(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, time.Hour, 2*time.Hour)
	var ownUserID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&ownUserID); err != nil {
		t.Fatal(err)
	}
	v := taskCreate(t, a.admin, taskBody(ownUserID, time.Now().Add(time.Hour)))
	for _, obj := range []struct {
		kind string
		id   int64
	}{{"student", a.firstID}, {"group", a.groupID}, {"lesson", l.ID}} {
		expect(t, a.admin, "PATCH", taskPath(v.ID), map[string]any{"object_type": obj.kind, "object_id": obj.id}, 200)
		got := taskGet(t, a.admin, v.ID)
		if got.ObjectType == nil || *got.ObjectType != obj.kind || got.ObjectID == nil || *got.ObjectID != obj.id || got.ObjectName == nil {
			t.Fatal("related object not resolved")
		}
	}
	expect(t, a.admin, "PATCH", taskPath(v.ID), map[string]any{"object_type": nil, "object_id": nil}, 200)
	if taskGet(t, a.admin, v.ID).ObjectID != nil {
		t.Fatal("link cannot be cleared")
	}
	for _, body := range []map[string]any{{}, {"title": nil}, {"description": " "}, {"assignee_id": nil}, {"due_at": nil}, {"status": "unknown"}, {"status": ""}, {"object_type": "student"}, {"object_id": a.firstID}, {"object_type": "parent", "object_id": 1}, {"object_type": "group", "object_id": 0}, {"creator_id": a.foreignUserID}, {"title": strings.Repeat("Я", 201)}, {"description": strings.Repeat("😀", 5001)}, {"due_at": "2026-10-12T12:00:00"}} {
		expect(t, a.admin, "PATCH", taskPath(v.ID), body, 400)
	}
	expect(t, a.admin, "PATCH", taskPath(v.ID), map[string]any{"title": "Must roll back", "object_type": "student", "object_id": 99999}, 400)
	if got := taskGet(t, a.admin, v.ID); got.Title != v.Title || got.ObjectID != nil {
		t.Fatal("failed edit partly committed")
	}
	expect(t, a.admin, "PATCH", taskPath(v.ID), map[string]any{"description": strings.Repeat("😀", 5000)}, 200)
	for _, query := range []string{"?page=0", "?page_size=101", "?status=deleted", "?overdue=1", "?assignee_id=-1", "?assignee_id=abc"} {
		expect(t, a.admin, "GET", "/tasks"+query, nil, 400)
	}
	expect(t, a.admin, "GET", "/tasks/bad", nil, 400)
	expect(t, a.admin, "GET", "/tasks/99999", nil, 404)
	_, err := a.f.pool.Exec(context.Background(), `UPDATE tasks SET student_id=$2,group_id=$3 WHERE id=$1`, v.ID, a.firstID, a.groupID)
	dbConstraint(t, err, "23514", "tasks_one_related_object")
	_, err = a.f.pool.Exec(context.Background(), `UPDATE tasks SET creator_id=$2 WHERE id=$1`, v.ID, a.foreignUserID)
	dbConstraint(t, err, "23514", "tasks_creator_immutable")
	_, err = a.f.pool.Exec(context.Background(), `UPDATE tasks SET lesson_id=99999 WHERE id=$1`, v.ID)
	dbConstraint(t, err, "23503", "tasks_lesson_id_fkey")
	literal := taskBody(a.foreignUserID, time.Now())
	literal["title"] = "Проверка %_"
	taskCreate(t, a.admin, literal)
	if p := taskPage(t, a.admin, "?search=%25_"); p.Total != 1 {
		t.Fatal("search treated literal as wildcard")
	}
	a.admin.csrf = ""
	expect(t, a.admin, "POST", "/tasks", taskBody(a.foreignUserID, time.Now()), 403)
}

func TestTasksConcurrentReassignmentAndStatusRespectCurrentOwner(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	var ownUserID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&ownUserID); err != nil {
		t.Fatal(err)
	}
	v := taskCreate(t, a.admin, taskBody(ownUserID, time.Now().Add(time.Hour)))
	start := make(chan struct{})
	codes := make([]int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		codes[0], _ = a.admin.do(t, "PATCH", taskPath(v.ID), map[string]any{"assignee_id": a.foreignUserID})
	}()
	go func() {
		defer wg.Done()
		<-start
		codes[1], _ = a.own.do(t, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "in_progress"})
	}()
	close(start)
	wg.Wait()
	if codes[0] != 200 || (codes[1] != 200 && codes[1] != 404) {
		t.Fatalf("concurrent results: %v", codes)
	}
	if taskGet(t, a.admin, v.ID).AssigneeID != a.foreignUserID {
		t.Fatal("status change lost reassignment")
	}
	expect(t, a.own, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done"}, 404)
	if taskGet(t, a.foreign, v.ID).Status == "todo" {
		expect(t, a.foreign, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "in_progress"}, 200)
	}
	expect(t, a.foreign, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "done"}, 200)
}
