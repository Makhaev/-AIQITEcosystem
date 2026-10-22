package app_test

import (
	"aiq.local/core/internal/modules/students"
	"fmt"
	"math"
	"testing"
	"time"
)

func TestDashboardR09SixMarksMatchStudentOverviewWithoutRounding(t *testing.T) {
	a := setupAttendance(t)
	program := programCreate(t, a.admin, map[string]any{"name": "R09 single pupil", "status": "active"})
	group := groupCreate(t, a.admin, program.ID, 10, "active")
	expect(t, a.admin, "POST", groupPath(group.ID)+"/enrollments", map[string]any{"student_id": a.firstID, "starts_on": "2000-01-01"}, 201)
	for i, status := range []string{"present", "present", "present", "late", "absent", "excused"} {
		start := time.Date(2024, 6, i+1, 12, 0, 0, 0, time.UTC)
		lesson := lessonCreate(t, a.admin, lessonBody(group.ID, a.teacherID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339)))
		expect(t, a.admin, "PUT", lessonPath(lesson.ID)+"/attendance", marksBody(markValue(a.firstID, status, "")), 204)
		expect(t, a.admin, "POST", lessonPath(lesson.ID)+"/complete", nil, 200)
	}
	var student students.StudentOverview
	decode(t, expect(t, a.admin, "GET", fmt.Sprintf("/students/%d/overview?from=2024-06-01&to=2024-06-30", a.firstID), nil, 200), &student)
	center := dashboardGet(t, a.admin, "?date_from=2024-06-01&date_to=2024-06-30")
	marks := student.Attendance
	if marks.Present != 3 || marks.Late != 1 || marks.Absent != 1 || marks.Excused != 1 || len(marks.Items) != 6 {
		t.Fatalf("wrong acceptance marks: %+v", marks)
	}
	if marks.Percent == nil || center.AttendancePercent == nil || *marks.Percent != *center.AttendancePercent || math.Abs(*center.AttendancePercent-200.0/3) > 1e-12 {
		t.Fatalf("Dashboard and Student 360 must agree on 4/6 without rounding: dashboard=%+v student=%+v", center, marks)
	}
	if *center.AttendancePercent == 66.67 {
		t.Fatal("API rounded the percentage before presentation")
	}
	// Empty attendance does not erase current entity/task counters, and the
	// student counts stay zero rather than introducing an artificial 0%.
	decode(t, expect(t, a.admin, "GET", fmt.Sprintf("/students/%d/overview?from=2024-07-01&to=2024-07-31", a.firstID), nil, 200), &student)
	empty := dashboardGet(t, a.admin, "?date_from=2024-07-01&date_to=2024-07-31")
	marks = student.Attendance
	if empty.AttendancePercent != nil || marks.Percent != nil || marks.Present+marks.Late+marks.Absent+marks.Excused+marks.Unmarked != 0 || len(marks.Items) != 0 {
		t.Fatalf("empty data is not null/zero: dashboard=%+v student=%+v", empty, marks)
	}
	if empty.ActiveStudents != center.ActiveStudents || empty.ActiveGroups != center.ActiveGroups || empty.ActiveTeachers != center.ActiveTeachers || empty.LessonsToday != center.LessonsToday || empty.OverdueTasks != center.OverdueTasks {
		t.Fatal("attendance period changed current counters")
	}
}

func TestDashboardR09MoscowMonthIsHalfOpen(t *testing.T) {
	a := setupAttendance(t)
	for i, value := range []string{"2024-01-31T20:00:00Z", "2024-01-31T21:00:00Z", "2024-02-29T20:00:00Z", "2024-02-29T21:00:00Z"} {
		start, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		lesson := lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, start.Format(time.RFC3339), start.Add(30*time.Minute).Format(time.RFC3339)))
		status := "absent"
		if i == 1 || i == 2 {
			status = "present"
		}
		finishOverviewLesson(t, a, lesson, status)
	}
	center := dashboardGet(t, a.admin, "?date_from=2024-02-01&date_to=2024-02-29")
	if center.AttendancePercent == nil || *center.AttendancePercent != 100 {
		t.Fatalf("Moscow February must include its first and last days, and exclude adjacent months: %+v", center)
	}
	var student students.StudentOverview
	decode(t, expect(t, a.admin, "GET", fmt.Sprintf("/students/%d/overview?from=2024-02-01&to=2024-02-29", a.firstID), nil, 200), &student)
	if student.Attendance.Present != 2 || student.Attendance.Absent != 0 || student.Attendance.Percent == nil || *student.Attendance.Percent != *center.AttendancePercent {
		t.Fatalf("Student 360 and Dashboard disagree at Moscow month boundaries: %+v", student.Attendance)
	}
}
