package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"aiq.local/core/internal/platform/access"
)

func assertStudentProjection(t *testing.T, value map[string]any, full bool) {
	t.Helper()
	for _, key := range []string{"id", "name", "status"} {
		if _, ok := value[key]; !ok {
			t.Fatalf("missing identity field %s", key)
		}
	}
	for _, key := range []string{"birth_date", "curator_id", "curator_name", "created_at", "updated_at"} {
		_, present := value[key]
		if present != full {
			t.Fatalf("private field %s presence=%v, full=%v", key, present, full)
		}
	}
}

func TestCompletionStudentProjectionByObjectAndCombinedRoles(t *testing.T) {
	a := setupAttendance(t)
	a.lesson(t, time.Hour, 2*time.Hour)
	ctx := context.Background()
	var teacherUserID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherUserID); err != nil {
		t.Fatal(err)
	}
	check := func(b *browser, fullForFirst, fullForSecond bool) {
		t.Helper()
		var page struct {
			Items []map[string]any `json:"items"`
		}
		decode(t, expect(t, b, "GET", "/students", nil, 200), &page)
		if len(page.Items) != 2 {
			t.Fatalf("expected both own pupils, got %d", len(page.Items))
		}
		for _, v := range page.Items {
			full := fullForSecond
			if int64(v["id"].(float64)) == a.firstID {
				full = fullForFirst
			}
			assertStudentProjection(t, v, full)
			if !full && len(v) != 3 {
				t.Fatalf("restricted list has %d fields", len(v))
			}
			for _, suffix := range []string{"", "/overview", "/360"} {
				var card map[string]any
				decode(t, expect(t, b, "GET", fmt.Sprintf("/students/%d%s", int64(v["id"].(float64)), suffix), nil, 200), &card)
				assertStudentProjection(t, card, full)
				if suffix == "" {
					if !full && len(card) != 3 {
						t.Fatal("restricted card must contain exactly id/name/status")
					}
					if _, present := card["parents"]; present != full {
						t.Fatal("family block violates object permission")
					}
				} else {
					if card["full_access"] != full {
						t.Fatal("incorrect object permission")
					}
					if _, present := card["age"]; present != full {
						t.Fatal("age projection violates pupil permission")
					}
					if !full {
						for key := range card {
							switch key {
							case "id", "name", "status", "full_access", "enrollments", "upcoming_lessons", "attendance", "education":
							default:
								t.Fatalf("unexpected restricted overview key %s", key)
							}
						}
					}
				}
			}
		}
	}
	check(a.own, false, false)
	if _, err := a.f.pool.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'curator')`, teacherUserID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"birth_date": "2013-05-10", "curator_id": teacherUserID}, 200)
	check(a.own, true, false)
	check(a.admin, true, true)
	_, leader := provision(t, a.f, a.admin, "projection-leader@example.test", "leader")
	check(leader, true, true)
	if _, err := a.f.pool.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'leader')`, teacherUserID); err != nil {
		t.Fatal(err)
	}
	check(a.own, true, true)
	foreign := studentCreate(t, a.admin, map[string]any{"name": "Unrelated pupil"})
	for _, suffix := range []string{"", "/overview", "/360"} {
		expect(t, a.foreign, "GET", fmt.Sprintf("/students/%d%s", foreign.ID, suffix), nil, 404)
	}
}

func TestCompletionNestedStudentVisibilityAndFrozenRoster(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, time.Hour, 2*time.Hour)
	lessonJournal(t, a.own, l.ID)
	// A pupil enrolled after journal fixation is outside this teacher's scope.
	outside := studentCreate(t, a.admin, map[string]any{"name": "Не участник занятий педагога", "birth_date": "2011-02-03"})
	e := enroll(t, a.admin, a.groupID, outside.ID)
	checkGroup := func(count int, visible bool) {
		t.Helper()
		body := expect(t, a.own, "GET", groupPath(a.groupID), nil, 200)
		var result struct {
			Enrollments []map[string]any `json:"enrollments"`
		}
		decode(t, body, &result)
		if len(result.Enrollments) != count || bytes.Contains(body, []byte(outside.Name)) != visible {
			t.Fatal("nested group pupils exceed teaching scope")
		}
		for _, item := range result.Enrollments {
			for _, key := range []string{"birth_date", "age", "curator_id", "curator_name", "parents", "history"} {
				if _, present := item[key]; present {
					t.Fatalf("nested pupil leaked %s", key)
				}
			}
		}
	}
	checkGroup(2, false)
	for _, suffix := range []string{"", "/overview", "/360"} {
		expect(t, a.own, "GET", fmt.Sprintf("/students/%d%s", outside.ID, suffix), nil, 404)
	}
	if len(lessonJournal(t, a.own, l.ID).Items) != 2 {
		t.Fatal("late enrollment changed journal")
	}
	original := lessonJournal(t, a.admin, l.ID)
	ids := []int64{e.ID}
	for _, item := range original.Items {
		ids = append(ids, item.EnrollmentID)
	}
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": ids, "reason": "Явное добавление участника"}, 200)
	checkGroup(3, true)
	var card map[string]any
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/students/%d", outside.ID), nil, 200), &card)
	if len(card) != 3 {
		t.Fatal("new visible pupil receives private fields")
	}
	// Shared parent exposes only the pupil for whom the combined actor is curator.
	var teacherUserID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) VALUES($1,'curator')`, teacherUserID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": teacherUserID}, 200)
	p := parentCreate(t, a.admin, map[string]any{"name": "Общий представитель", "email": "shared-example@example.test"})
	linkParent(t, a.admin, a.firstID, p.ID, true)
	linkParent(t, a.admin, outside.ID, p.ID, true)
	var parent struct {
		Children []map[string]any `json:"children"`
	}
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/parents/%d", p.ID), nil, 200), &parent)
	if len(parent.Children) != 1 || int64(parent.Children[0]["id"].(float64)) != a.firstID {
		t.Fatal("shared parent exposed teacher-only pupil")
	}
	assertStudentProjection(t, parent.Children[0], true)
}

func TestCompletionTaskObjectPermissionsAndWithdrawal(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, time.Hour, 2*time.Hour)
	ctx := context.Background()
	var teacherUserID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherUserID); err != nil {
		t.Fatal(err)
	}
	curator, curatorBrowser := provision(t, a.f, a.admin, "task-assigned-curator@example.test", "curator")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": curator.ID}, 200)
	for _, obj := range []struct {
		kind string
		id   int64
	}{{"student", a.firstID}, {"group", a.groupID}, {"lesson", l.ID}} {
		body := taskBody(a.foreignUserID, time.Now().Add(time.Hour))
		body["object_type"], body["object_id"] = obj.kind, obj.id
		var before int
		if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_type='task'`).Scan(&before); err != nil {
			t.Fatal(err)
		}
		response := expect(t, a.admin, "POST", "/tasks", body, 400)
		var failure struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"field_errors"`
		}
		decode(t, response, &failure)
		if failure.Code != "assignee_object_access_denied" || failure.Fields["assignee_id"] == "" {
			t.Fatal("assignment error contract")
		}
		var after int
		if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_type='task'`).Scan(&after); err != nil || before != after {
			t.Fatal("rejected assignment changed audit")
		}
		for _, assignee := range []int64{teacherUserID, curator.ID} {
			body["assignee_id"] = assignee
			v := taskCreate(t, a.admin, body)
			expect(t, a.admin, "PATCH", taskPath(v.ID), map[string]any{"assignee_id": a.foreignUserID, "title": "Must not commit"}, 400)
			if taskGet(t, a.admin, v.ID).Title != v.Title {
				t.Fatal("reassignment partly committed")
			}
		}
	}
	// Changing the link while leaving the assignee unchanged must also authorize it.
	unlinked := taskCreate(t, a.admin, taskBody(a.foreignUserID, time.Now().Add(time.Hour)))
	expect(t, a.admin, "PATCH", taskPath(unlinked.ID), map[string]any{"object_type": "student", "object_id": a.firstID}, 400)
	if taskGet(t, a.admin, unlinked.ID).ObjectID != nil {
		t.Fatal("invalid relation persisted")
	}
	// Read/detail/status responses mask old assignments after object access is lost.
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": nil}, 200)
	assertHidden := func(body []byte) {
		t.Helper()
		var v map[string]any
		decode(t, body, &v)
		for _, key := range []string{"object_type", "object_id", "object_name"} {
			if v[key] != nil {
				t.Fatalf("lost permission leaks %s", key)
			}
		}
	}
	page := taskPage(t, curatorBrowser, "")
	if page.Total != 3 {
		t.Fatalf("lost access removed own tasks: %d", page.Total)
	}
	for _, v := range page.Items {
		encoded, _ := json.Marshal(v)
		assertHidden(encoded)
		assertHidden(expect(t, curatorBrowser, "GET", taskPath(v.ID), nil, 200))
		assertHidden(expect(t, curatorBrowser, "PATCH", taskPath(v.ID)+"/status", map[string]any{"status": "in_progress"}, 200))
		if taskGet(t, a.admin, v.ID).ObjectID == nil {
			t.Fatal("projection destroyed stored relation")
		}
	}
}

func TestCompletionAssignmentWaitsForRelationshipChange(t *testing.T) {
	a := setupAttendance(t)
	curator, _ := provision(t, a.f, a.admin, "assignment-race@example.test", "curator")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": curator.ID}, 200)
	ctx := context.Background()
	tx, err := a.f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = access.LockManagement(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE students SET curator_id=NULL WHERE id=$1`, a.firstID); err != nil {
		t.Fatal(err)
	}
	body := taskBody(curator.ID, time.Now().Add(time.Hour))
	body["object_type"], body["object_id"] = "student", a.firstID
	completed := make(chan int, 1)
	go func() { status, _ := a.admin.do(t, "POST", "/tasks", body); completed <- status }()
	select {
	case status := <-completed:
		t.Fatalf("assignment bypassed management lock: %d", status)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-completed:
		if status != 400 {
			t.Fatalf("stale assignee rights accepted: %d", status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("assignment did not resume")
	}
}
