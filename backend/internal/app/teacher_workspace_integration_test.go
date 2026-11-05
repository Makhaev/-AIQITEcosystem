package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func personalPage(t *testing.T, b *browser, path string) ([]map[string]any, int) {
	t.Helper()
	var result struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	decode(t, expect(t, b, "GET", path, nil, 200), &result)
	return result.Items, result.Total
}
func TestTeacherWorkspaceScopesRosterAndPagination(t *testing.T) {
	a := setupAttendance(t)
	ownLesson := a.lesson(t, -3*time.Hour, -2*time.Hour)
	lessonJournal(t, a.own, ownLesson.ID)
	hidden := studentCreate(t, a.admin, map[string]any{"name": "Позднее зачисление"})
	expect(t, a.admin, "POST", groupPath(a.groupID)+"/enrollments", map[string]any{"student_id": hidden.ID, "starts_on": "2000-01-01"}, 201)
	p := programCreate(t, a.admin, map[string]any{"name": "Вторая программа", "status": "active"})
	mainGroup := groupCreate(t, a.admin, p.ID, 10, "active")
	expect(t, a.admin, "PATCH", groupPath(mainGroup.ID), map[string]any{"teacher_id": a.teacherID, "name": "Моя основная группа"}, 200)
	expect(t, a.admin, "POST", groupPath(mainGroup.ID)+"/enrollments", map[string]any{"student_id": hidden.ID, "starts_on": "2000-01-01"}, 201)
	foreignProfile := teacherCreate(t, a.admin, map[string]any{"name": "Other teacher", "user_id": a.foreignUserID})
	foreignGroup := groupCreate(t, a.admin, p.ID, 10, "active")
	foreignChild := studentCreate(t, a.admin, map[string]any{"name": "Чужой ребёнок"})
	expect(t, a.admin, "POST", groupPath(foreignGroup.ID)+"/enrollments", map[string]any{"student_id": foreignChild.ID, "starts_on": "2000-01-01"}, 201)
	lessonCreate(t, a.admin, lessonBody(foreignGroup.ID, foreignProfile.ID, a.base.Add(-6*time.Hour).Format(time.RFC3339), a.base.Add(-5*time.Hour).Format(time.RFC3339)))
	future := a.lesson(t, 2*time.Hour, 3*time.Hour)
	// Frozen first roster contains only the two original pupils; exclude the
	// later pupil from the future snapshot too to test both directions.
	futureJournal := lessonJournal(t, a.own, future.ID)
	enrollments := []int64{}
	for _, pupil := range futureJournal.Items {
		if pupil.StudentID == a.firstID || pupil.StudentID == a.secondID {
			enrollments = append(enrollments, pupil.EnrollmentID)
		}
	}
	expect(t, a.admin, "PUT", lessonPath(future.ID)+"/roster", map[string]any{"enrollment_ids": enrollments, "reason": "Оставить исходный состав"}, 200)
	got, total := personalPage(t, a.own, "/teacher/groups?page_size=1")
	if len(got) != 1 || total != 2 {
		t.Fatalf("personal groups: %v total=%d", got, total)
	}
	next, nextTotal := personalPage(t, a.own, "/teacher/groups?page_size=1&page=2")
	if len(next) != 1 || nextTotal != 2 || got[0]["id"] == next[0]["id"] {
		t.Fatal("group pagination is not stable")
	}
	_, total = personalPage(t, a.own, "/teacher/groups?search="+"%25")
	if total != 0 {
		t.Fatal("literal wildcard search escaped its filter")
	}
	pupils, total := personalPage(t, a.own, "/teacher/students")
	if total != 2 || len(pupils) != 2 {
		t.Fatalf("pupils leaked across roster: %v total=%d", pupils, total)
	}
	for _, pupil := range pupils {
		if len(pupil) != 4 {
			t.Fatalf("private student fields in teacher response: %v", pupil)
		}
		for _, key := range []string{"parents", "birth_date", "curator_id", "phone", "email", "history", "user_id"} {
			if _, ok := pupil[key]; ok {
				t.Fatalf("private field %s", key)
			}
		}
	}
	empty, total := personalPage(t, a.own, fmt.Sprintf("/teacher/students?group_id=%d", mainGroup.ID))
	if len(empty) != 0 || total != 0 {
		t.Fatal("a primary group without own lessons revealed pupils")
	}
	expect(t, a.own, "GET", fmt.Sprintf("/teacher/students?group_id=%d", foreignGroup.ID), nil, 404)
	expect(t, a.own, "GET", "/teacher/students?group_id=99999999", nil, 404)
	for _, path := range []string{"/teacher/groups?teacher_id=123", "/teacher/students?user_id=123", "/teacher/students?group_id=bad", "/teacher/groups?page_size=101", "/teacher/groups?status=bad", "/teacher/students?page=0"} {
		expect(t, a.own, "GET", path, nil, 400)
	}
	summary := expect(t, a.own, "GET", "/teacher/workspace", nil, 200)
	var dashboard struct {
		Profile  map[string]any   `json:"profile"`
		Counts   map[string]int   `json:"counts"`
		Upcoming []map[string]any `json:"upcoming_lessons"`
	}
	decode(t, summary, &dashboard)
	if dashboard.Counts["groups"] != 2 || dashboard.Counts["students"] != 2 || len(dashboard.Upcoming) != 1 || int64(dashboard.Upcoming[0]["id"].(float64)) != future.ID {
		t.Fatalf("wrong overview: %s", summary)
	}
	// Giving the teacher an additional admin role must not expand a PERSONAL view.
	substitute := lessonCreate(t, a.admin, lessonBody(foreignGroup.ID, a.teacherID, a.base.Add(4*time.Hour).Format(time.RFC3339), a.base.Add(5*time.Hour).Format(time.RFC3339)))
	_, total = personalPage(t, a.own, "/teacher/groups")
	if total != 3 {
		t.Fatal("substitute group is missing")
	}
	_, total = personalPage(t, a.own, fmt.Sprintf("/teacher/students?group_id=%d", foreignGroup.ID))
	if total != 1 {
		t.Fatal("substitute pupil is missing")
	}
	expect(t, a.admin, "POST", lessonPath(substitute.ID)+"/cancel", nil, 200)
	expect(t, a.own, "GET", fmt.Sprintf("/teacher/students?group_id=%d", foreignGroup.ID), nil, 404)
	var teacherUserID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) VALUES($1,'admin')`, teacherUserID); err != nil {
		t.Fatal(err)
	}
	_, total = personalPage(t, a.own, "/teacher/groups")
	if total != 2 {
		t.Fatal("admin role widened the personal workspace")
	}
	_, total = personalPage(t, a.own, "/teacher/students")
	if total != 2 {
		t.Fatal("admin role widened personal pupils")
	}
	// Assignments, role changes and session revocation are effective immediately.
	expect(t, a.admin, "PATCH", groupPath(mainGroup.ID), map[string]any{"teacher_id": nil}, 200)
	_, total = personalPage(t, a.own, "/teacher/groups")
	if total != 1 {
		t.Fatal("removed assignment retained group access")
	}
	if _, err := a.f.pool.Exec(context.Background(), `DELETE FROM user_roles WHERE user_id=$1 AND role='teacher'`, teacherUserID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.own, "GET", "/teacher/workspace", nil, 403)
}

func TestTeacherWorkspaceEmptyProfileAndDeniedRoles(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	teacherUser, teacher := provision(t, f, admin, "personal-unlinked@example.test", "teacher")
	summary := expect(t, teacher, "GET", "/teacher/workspace", nil, 200)
	if !strings.Contains(string(summary), `"profile":null`) {
		t.Fatalf("unlinked account should have empty profile: %s", summary)
	}
	_, total := personalPage(t, teacher, "/teacher/groups")
	if total != 0 {
		t.Fatal("unlinked account saw groups")
	}
	_, total = personalPage(t, teacher, "/teacher/students")
	if total != 0 {
		t.Fatal("unlinked account saw pupils")
	}
	for _, role := range []string{"leader", "curator", "student", "parent"} {
		_, b := provision(t, f, admin, "workspace-"+role+"@example.test", role)
		for _, path := range []string{"/teacher/workspace", "/teacher/groups", "/teacher/students"} {
			expect(t, b, "GET", path, nil, 403)
		}
	}
	expect(t, admin, "GET", "/teacher/workspace", nil, 403)
	expect(t, f.browser(t), "GET", "/teacher/workspace", nil, 401)
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacherUser.ID), map[string]any{"blocked": true}, 200)
	expect(t, teacher, "GET", "/teacher/groups", nil, 401)
}
