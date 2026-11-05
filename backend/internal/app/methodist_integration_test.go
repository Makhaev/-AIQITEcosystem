package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestMethodistProgramsEditingProgressAndReportsPermissions(t *testing.T) {
	a := setupAttendance(t)
	user, methodist := provision(t, a.f, a.admin, "methodist@example.test", "methodist")
	_, curator := provision(t, a.f, a.admin, "methodist-curator@example.test", "curator")
	_, parent := provision(t, a.f, a.admin, "methodist-parent@example.test", "parent")
	lesson := a.lesson(t, -3*time.Hour, -2*time.Hour)
	lessonJournal(t, a.own, lesson.ID)
	expect(t, a.own, "PUT", lessonPath(lesson.ID)+"/attendance", marksBody(markValue(a.firstID, "present", "Learning note"), markValue(a.secondID, "absent", "")), 204)
	expect(t, a.own, "POST", lessonPath(lesson.ID)+"/complete", nil, 200)
	teacherID := resultID(t, expect(t, a.admin, "POST", "/reports", map[string]any{"lesson_id": lesson.ID, "student_id": a.firstID, "summary": "Teacher evidence"}, 201))
	reportID := resultID(t, expect(t, a.admin, "POST", "/curator-reports", map[string]any{"student_id": a.firstID, "summary": "Curator evidence", "goals": "Individual report goal", "recommendations": "Practice"}, 201))
	contact := parentCreate(t, a.admin, map[string]any{"name": "PRIVATE FAMILY NAME", "email": "private-family@example.test"})
	expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/parents", a.firstID), map[string]any{"parent_id": contact.ID, "relationship": "Parent"}, 201)
	var summary struct {
		Counts struct {
			Students, Programs int
			TeacherReports     int `json:"teacher_reports"`
			CuratorReports     int `json:"curator_reports"`
		} `json:"counts"`
	}
	decode(t, expect(t, methodist, "GET", "/methodist/workspace", nil, 200), &summary)
	if summary.Counts.Students != 2 || summary.Counts.Programs != 1 || summary.Counts.TeacherReports != 1 || summary.Counts.CuratorReports != 1 {
		t.Fatalf("incorrect counts: %+v", summary)
	}
	for _, endpoint := range []string{"/methodist/workspace", "/methodist/programs", "/methodist/students", "/methodist/students/" + fmt.Sprint(a.firstID), "/methodist/teacher-reports", "/methodist/curator-reports"} {
		for _, b := range []*browser{curator, parent, a.own} {
			expect(t, b, "GET", endpoint, nil, 403)
		}
		response := expect(t, methodist, "GET", endpoint, nil, 200)
		for _, secret := range []string{"private-family@example.test", "PRIVATE FAMILY NAME", "password_hash", "session_hash"} {
			if strings.Contains(string(response), secret) {
				t.Fatalf("nonacademic data leaked: %s", secret)
			}
		}
	}
	rows, total := personalPage(t, methodist, "/methodist/students?page_size=1&page=2")
	if total != 2 || len(rows) != 1 {
		t.Fatal("student pagination incorrect")
	}
	_, total = personalPage(t, methodist, "/methodist/students?search=%25")
	if total != 0 {
		t.Fatal("wildcard search broadened")
	}
	expect(t, methodist, "GET", "/methodist/students/999999", nil, 404)
	var detail struct {
		Progress struct {
			Current struct {
				Attendance struct {
					Percent *float64 `json:"percent"`
				} `json:"attendance"`
			} `json:"current"`
		} `json:"progress"`
	}
	decode(t, expect(t, methodist, "GET", fmt.Sprintf("/methodist/students/%d", a.firstID), nil, 200), &detail)
	if detail.Progress.Current.Attendance.Percent == nil || *detail.Progress.Current.Attendance.Percent != 100 {
		t.Fatal("progress missing")
	}
	var programID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT program_id FROM groups WHERE id=$1`, a.groupID).Scan(&programID); err != nil {
		t.Fatal(err)
	}
	module := map[string]any{"name": "Program module", "position": 1, "description": "Methodology"}
	expect(t, a.admin, "POST", fmt.Sprintf("/programs/%d/modules", programID), module, 201)
	expect(t, methodist, "GET", "/programs", nil, 200)
	expect(t, methodist, "GET", fmt.Sprintf("/programs/%d", programID), nil, 200)
	programs, total := personalPage(t, methodist, "/methodist/programs")
	if total != 1 || len(programs) != 1 || programs[0]["module_count"] != float64(1) || programs[0]["student_count"] != float64(2) || programs[0]["completed_lessons"] != float64(1) || programs[0]["lesson_count"] != float64(1) {
		t.Fatalf("incorrect teaching programs: %+v", programs)
	}
	_, total = personalPage(t, methodist, "/methodist/programs?search=%25")
	if total != 0 {
		t.Fatal("program wildcard broadened search")
	}
	expect(t, methodist, "GET", "/methodist/programs?search="+strings.Repeat("a", 201), nil, 400)
	teachers, total := personalPage(t, methodist, fmt.Sprintf("/methodist/teacher-reports?student_id=%d", a.firstID))
	if total != 1 || teachers[0]["summary"] != "Teacher evidence" {
		t.Fatal("teacher draft missing")
	}
	for _, kind := range []string{"teacher", "curator"} {
		endpoint := "/methodist/" + kind + "-reports"
		_, total = personalPage(t, methodist, endpoint+"?publication=draft")
		if total != 1 {
			t.Fatal("draft filter lost report")
		}
		_, total = personalPage(t, methodist, endpoint+"?publication=published")
		if total != 0 {
			t.Fatal("draft appears published")
		}
		_, total = personalPage(t, methodist, endpoint+"?search=%25")
		if total != 0 {
			t.Fatal("report wildcard broadened search")
		}
		expect(t, methodist, "GET", endpoint+"?publication=invalid", nil, 400)
	}
	reports, total := personalPage(t, methodist, fmt.Sprintf("/methodist/curator-reports?student_id=%d", a.firstID))
	if total != 1 || reports[0]["goals"] != "Individual report goal" {
		t.Fatal("curator draft missing")
	}
	for _, key := range []string{"recipients", "feedback", "created_by"} {
		if _, ok := reports[0][key]; ok {
			t.Fatalf("unnecessary report data: %s", key)
		}
	}
	_, total = personalPage(t, methodist, fmt.Sprintf("/methodist/curator-reports?student_id=%d", a.secondID))
	if total != 0 {
		t.Fatal("student filter mixed reports")
	}
	expect(t, methodist, "GET", "/methodist/teacher-reports?from=2026-02-30", nil, 400)
	expect(t, methodist, "GET", "/methodist/curator-reports?student_id=-1", nil, 400)
	for _, path := range []string{"/users", "/parents", "/students", "/curator/students", "/reports", "/curator-reports"} {
		expect(t, methodist, "GET", path, nil, 403)
	}
	created := resultID(t, expect(t, methodist, "POST", "/programs", map[string]any{"name": "Methodist program"}, 201))
	expect(t, methodist, "PATCH", fmt.Sprintf("/programs/%d", created), map[string]any{"description": "Updated methodology"}, 200)
	moduleID := resultID(t, expect(t, methodist, "POST", fmt.Sprintf("/programs/%d/modules", created), map[string]any{"name": "Methodist module", "position": 1}, 201))
	expect(t, methodist, "PATCH", fmt.Sprintf("/programs/%d/modules/%d", created, moduleID), map[string]any{"description": "Exercise content"}, 200)
	expect(t, methodist, "PATCH", fmt.Sprintf("/programs/%d", created), map[string]any{"status": "archived"}, 200)
	expect(t, methodist, "POST", fmt.Sprintf("/programs/%d/modules", created), map[string]any{"name": "Archived module", "position": 2}, 409)
	expect(t, methodist, "PATCH", fmt.Sprintf("/programs/%d", programID), map[string]any{"description": "Methodist revision"}, 200)
	expect(t, methodist, "POST", fmt.Sprintf("/reports/%d/publish", teacherID), nil, 403)
	expect(t, methodist, "POST", fmt.Sprintf("/curator-reports/%d/publish", reportID), map[string]any{"parent_ids": []int64{contact.ID}}, 403)
	expect(t, methodist, "POST", "/curator-reports", map[string]any{"student_id": a.firstID, "summary": "Unauthorized"}, 403)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", user.ID), map[string]any{"roles": []string{"parent"}}, 200)
	expect(t, methodist, "GET", "/methodist/workspace", nil, 401)
}
