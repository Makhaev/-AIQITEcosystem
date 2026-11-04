package app_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReacceptanceF01ToF05AccessAndPersistence(t *testing.T) {
	a := setupAttendance(t)
	first, curator := provision(t, a.f, a.admin, "new-curator@example.test", "curator")
	replacement, next := provision(t, a.f, a.admin, "next-curator@example.test", "curator")
	outsider, foreign := provision(t, a.f, a.admin, "foreign-curator@example.test", "curator")
	_, leader := provision(t, a.f, a.admin, "branch-leader@example.test", "leader")
	var branch struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, a.admin, "POST", "/branches", map[string]any{"name": "Central"}, 201), &branch)
	expect(t, a.admin, "PUT", "/branches/9223372036854775807", map[string]any{"name": "Missing"}, 404)
	expect(t, curator, "POST", "/branches", map[string]any{"name": "Forbidden"}, 403)
	expect(t, a.admin, "PATCH", groupPath(a.groupID), map[string]any{"branch_id": branch.ID, "curator_id": first.ID}, 200)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": outsider.ID, "phone": " +7 (900) 123-45-67 ", "email": " OWN@EXAMPLE.TEST "}, 200)
	expect(t, curator, "GET", groupPath(a.groupID), nil, 200)
	expect(t, foreign, "GET", fmt.Sprintf("/students/%d", a.firstID), nil, 404)
	var pupil map[string]any
	decode(t, expect(t, curator, "GET", fmt.Sprintf("/students/%d", a.firstID), nil, 200), &pupil)
	if pupil["phone"] != "+79001234567" || pupil["email"] != "own@example.test" {
		t.Fatalf("contacts not normalised: %+v", pupil)
	}
	ids, ok := pupil["curator_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != float64(first.ID) {
		t.Fatalf("effective group curator must be exposed instead of fallback assignment: %+v", pupil)
	}
	for _, invalid := range []map[string]any{{"phone": "123"}, {"email": "Name <own@example.test>"}} {
		expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), invalid, 400)
	}
	lesson := a.lesson(t, -3*time.Hour, -2*time.Hour)
	lessonJournal(t, a.own, lesson.ID)
	restricted := expect(t, a.own, "GET", fmt.Sprintf("/students/%d", a.firstID), nil, 200)
	if bytes.Contains(restricted, []byte("own@example.test")) || bytes.Contains(restricted, []byte(`"phone"`)) {
		t.Fatal("student contacts leaked to teacher")
	}
	notePath := fmt.Sprintf("/students/%d/notes", a.firstID)
	var note struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, curator, "POST", notePath, map[string]any{"body": "Initial internal note"}, 201), &note)
	expect(t, curator, "PATCH", fmt.Sprintf("%s/%d", notePath, note.ID), map[string]any{"body": "Corrected internal note", "version": 1}, 200)
	expect(t, curator, "PATCH", fmt.Sprintf("%s/%d", notePath, note.ID), map[string]any{"body": "Stale", "version": 1}, 409)
	history := expect(t, curator, "GET", notePath, nil, 200)
	if !bytes.Contains(history, []byte("Initial internal note")) || !bytes.Contains(history, []byte("Corrected internal note")) {
		t.Fatal("note history missing")
	}
	expect(t, a.own, "GET", notePath, nil, 403)
	var directory struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	decode(t, expect(t, curator, "GET", fmt.Sprintf("/tasks/assignees?object_type=student&object_id=%d", a.firstID), nil, 200), &directory)
	found := false
	for _, item := range directory.Items {
		if item.ID == first.ID {
			found = true
		}
		if item.ID == outsider.ID {
			t.Fatal("ineligible assignee exposed")
		}
	}
	if !found {
		t.Fatal("self assignment missing")
	}
	body := taskBody(first.ID, time.Now().Add(time.Hour))
	body["object_type"] = "student"
	body["object_id"] = a.firstID
	self := taskCreate(t, curator, body)
	expect(t, curator, "PATCH", taskPath(self.ID)+"/status", map[string]any{"status": "in_progress"}, 200)
	expect(t, curator, "PATCH", taskPath(self.ID)+"/status", map[string]any{"status": "done"}, 200)
	body["assignee_id"] = a.f.admin.ID
	colleagueTask := taskCreate(t, curator, body)
	expect(t, a.admin, "GET", taskPath(colleagueTask.ID), nil, 200)
	expect(t, a.admin, "PATCH", taskPath(colleagueTask.ID)+"/status", map[string]any{"status": "in_progress"}, 200)
	body["assignee_id"] = outsider.ID
	expect(t, curator, "POST", "/tasks", body, 400)
	body["assignee_id"] = first.ID
	expect(t, foreign, "POST", "/tasks", body, 403)
	expect(t, a.own, "PUT", lessonPath(lesson.ID)+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "absent", "")), 204)
	expect(t, a.own, "POST", lessonPath(lesson.ID)+"/complete", nil, 200)
	date := time.Now().In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02")
	// Include the preceding Moscow date when this test runs near midnight:
	// the completed lesson is three hours old, whereas task closure is current.
	from := time.Now().Add(-3 * time.Hour).In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02")
	metricsPath := fmt.Sprintf("/dashboard/operations?branch_id=%d&date_from=%s&date_to=%s", branch.ID, from, date)
	var metrics struct {
		Groups []struct {
			Attendance *float64 `json:"attendance_percent"`
			Low        bool     `json:"low_attendance"`
		} `json:"groups"`
		Closed  int `json:"tasks_closed"`
		Low     int `json:"low_attendance_groups"`
		Working int `json:"tasks_in_progress"`
	}
	decode(t, expect(t, leader, "GET", metricsPath, nil, 200), &metrics)
	if metrics.Closed != 1 || metrics.Working != 1 || metrics.Low != 1 || len(metrics.Groups) != 1 || metrics.Groups[0].Attendance == nil || *metrics.Groups[0].Attendance != 50 {
		t.Fatalf("metrics disagree with control set: %+v", metrics)
	}
	decode(t, expect(t, leader, "GET", metricsPath+"&threshold=50", nil, 200), &metrics)
	if metrics.Low != 0 {
		t.Fatal("threshold equality counted as low")
	}
	expect(t, curator, "GET", metricsPath, nil, 403)
	decode(t, expect(t, leader, "GET", fmt.Sprintf("/dashboard/operations?branch_id=%d&date_from=2000-01-01&date_to=2000-01-01", branch.ID), nil, 200), &metrics)
	if metrics.Closed != 0 || metrics.Low != 0 || metrics.Groups[0].Attendance != nil || metrics.Working != 1 {
		t.Fatalf("empty period must preserve current task state without invented attendance: %+v", metrics)
	}
	expect(t, leader, "GET", metricsPath+"&threshold=NaN", nil, 400)
	expect(t, a.admin, "PATCH", groupPath(a.groupID), map[string]any{"curator_id": replacement.ID}, 200)
	expect(t, curator, "GET", notePath, nil, 404)
	expect(t, curator, "GET", groupPath(a.groupID), nil, 404)
	if result := expect(t, next, "GET", notePath, nil, 200); !bytes.Contains(result, []byte("Initial internal note")) {
		t.Fatal("new curator lost note history")
	}
	expect(t, next, "PATCH", fmt.Sprintf("%s/%d", notePath, note.ID), map[string]any{"body": "Not author", "version": 2}, 403)
	// Adding a contact never creates or links a login account.
	var count int
	if err := a.f.pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email='own@example.test'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("contact created account")
	}
	// Archiving a branch or blocking a curator must not force an unrelated
	// group edit to remove its historical assignment.
	expect(t, a.admin, "PUT", fmt.Sprintf("/branches/%d", branch.ID), map[string]any{"name": "Central", "status": "archived"}, 200)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", replacement.ID), map[string]any{"blocked": true}, 200)
	expect(t, a.admin, "PATCH", groupPath(a.groupID), map[string]any{"name": "Renamed with preserved assignments"}, 200)
}

func TestReacceptanceT01RawFileBoundaries(t *testing.T) {
	a := setupAttendance(t)
	for _, size := range []int{1 << 20, 10 << 20} {
		var file struct {
			ID int64 `json:"id"`
		}
		decode(t, expect(t, a.admin, "POST", "/files", map[string]any{"student_id": a.firstID, "name": "boundary.txt", "data": []byte(strings.Repeat("a", size))}, 201), &file)
		expect(t, a.foreign, "GET", fmt.Sprintf("/files/%d/content", file.ID), nil, 404)
	}
	expect(t, a.admin, "POST", "/files", map[string]any{"student_id": a.firstID, "name": "too-large.txt", "data": []byte(strings.Repeat("a", (10<<20)+1))}, 400)
}
