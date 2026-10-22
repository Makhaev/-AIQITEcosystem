package app_test

import (
	"aiq.local/core/internal/domain/people"
	"aiq.local/core/internal/modules/lessons"
	"aiq.local/core/internal/modules/students"
	"bytes"
	"context"
	"fmt"
	"math"
	"testing"
	"time"
)

func overview(t *testing.T, b *browser, id int64, period string) students.StudentOverview {
	t.Helper()
	var result students.StudentOverview
	decode(t, expect(t, b, "GET", fmt.Sprintf("/students/%d/360%s", id, period), nil, 200), &result)
	return result
}
func finishOverviewLesson(t *testing.T, a attendanceSetup, l lessons.Lesson, status string) {
	t.Helper()
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, status, "Проверка"), markValue(a.secondID, "present", "")), 204)
	expect(t, a.admin, "POST", lessonPath(l.ID)+"/complete", nil, 200)
}
func TestStudent360RolePrivacyAndAttendanceCalculation(t *testing.T) {
	a := setupAttendance(t)
	curator, ownCurator := provision(t, a.f, a.admin, "overview-curator@example.test", "curator")
	_, otherCurator := provision(t, a.f, a.admin, "overview-other@example.test", "curator")
	_, leader := provision(t, a.f, a.admin, "overview-leader@example.test", "leader")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": curator.ID, "birth_date": "2013-05-10"}, 200)
	p := parentCreate(t, a.admin, map[string]any{"name": "Private parent", "email": "private-parent@example.test", "phone": "+79001112233"})
	linkParent(t, a.admin, a.firstID, p.ID, true)
	present := a.lesson(t, -10*24*time.Hour, -10*24*time.Hour+time.Hour)
	late := a.lesson(t, -9*24*time.Hour, -9*24*time.Hour+time.Hour)
	absent := a.lesson(t, -8*24*time.Hour, -8*24*time.Hour+time.Hour)
	finishOverviewLesson(t, a, present, "present")
	finishOverviewLesson(t, a, late, "late")
	finishOverviewLesson(t, a, absent, "absent")
	// Scheduled marks and excluded rows are not part of the formula.
	scheduled := a.lesson(t, -7*24*time.Hour, -7*24*time.Hour+time.Hour)
	expect(t, a.admin, "PUT", lessonPath(scheduled.ID)+"/attendance", marksBody(markValue(a.firstID, "absent", "")), 204)
	excluded := a.lesson(t, -6*24*time.Hour, -6*24*time.Hour+time.Hour)
	finishOverviewLesson(t, a, excluded, "absent")
	expect(t, a.admin, "PUT", lessonPath(excluded.ID)+"/roster", map[string]any{"enrollment_ids": []int64{}, "reason": "Исключён из состава"}, 200)
	foreignTeacher := teacherCreate(t, a.admin, map[string]any{"name": "Other teacher", "user_id": a.foreignUserID})
	p2 := programCreate(t, a.admin, map[string]any{"name": "Other program", "status": "active"})
	g2 := groupCreate(t, a.admin, p2.ID, 10, "active")
	expect(t, a.admin, "POST", groupPath(g2.ID)+"/enrollments", map[string]any{"student_id": a.firstID, "starts_on": "2000-01-01"}, 201)
	other := lessonCreate(t, a.admin, lessonBody(g2.ID, foreignTeacher.ID, a.base.Add(-5*24*time.Hour).Format(time.RFC3339Nano), a.base.Add(-5*24*time.Hour+time.Hour).Format(time.RFC3339Nano)))
	expect(t, a.admin, "PUT", lessonPath(other.ID)+"/attendance", marksBody(markValue(a.firstID, "excused", "")), 204)
	expect(t, a.admin, "POST", lessonPath(other.ID)+"/complete", nil, 200)
	future := a.lesson(t, time.Hour, 2*time.Hour)
	period := "?from=2000-01-01&to=2099-12-31"
	for _, b := range []*browser{a.admin, leader, ownCurator} {
		v := overview(t, b, a.firstID, period)
		if !v.FullAccess || len(v.Parents) != 1 || v.Parents[0].Email != p.Email || len(v.History) == 0 || len(v.Enrollments) != 2 || len(v.Upcoming) != 1 || v.Upcoming[0].ID != future.ID {
			t.Fatalf("full overview incomplete: %+v", v)
		}
		if v.Attendance.Percent == nil || *v.Attendance.Percent != 50 || v.Attendance.Present != 1 || v.Attendance.Late != 1 || v.Attendance.Absent != 1 || v.Attendance.Excused != 1 || len(v.Attendance.Items) != 4 {
			t.Fatalf("completed-only formula incorrect: %+v", v.Attendance)
		}
	}
	restricted := overview(t, a.own, a.firstID, period+"&full_access=true")
	if restricted.FullAccess || len(restricted.Parents) != 0 || len(restricted.History) != 0 || len(restricted.Enrollments) != 1 || restricted.Attendance.Percent == nil || math.Abs(*restricted.Attendance.Percent-200.0/3) > 0.00001 || len(restricted.Attendance.Items) != 3 {
		t.Fatalf("teacher scope leaked records: %+v", restricted)
	}
	body := expect(t, a.own, "GET", fmt.Sprintf("/students/%d/360%s", a.firstID, period), nil, 200)
	for _, private := range [][]byte{[]byte(`"parents"`), []byte(`"history"`), []byte(`"birth_date"`), []byte(`"age"`), []byte(`"curator_id"`), []byte(`"curator_name"`), []byte(`"created_at"`), []byte(`"updated_at"`), []byte(p.Email), []byte(p.Phone), []byte("Other program")} {
		if bytes.Contains(body, private) {
			t.Fatalf("teacher response leaked private data: %s", body)
		}
	}
	var list people.Page[people.Student]
	decode(t, expect(t, a.own, "GET", "/students", nil, 200), &list)
	if list.Total != 2 {
		t.Fatalf("teacher cannot discover own pupils: %+v", list)
	}
	expect(t, otherCurator, "GET", fmt.Sprintf("/students/%d/360", a.firstID), nil, 404)
	expect(t, ownCurator, "GET", fmt.Sprintf("/students/%d/360", a.secondID), nil, 404)
	expect(t, a.foreign, "GET", fmt.Sprintf("/students/%d/360", a.secondID), nil, 404)
	// Being a curator of another pupil does not upgrade one's teaching card.
	if _, err := a.f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) SELECT user_id,'curator' FROM teachers WHERE id=$1`, a.teacherID); err != nil {
		t.Fatal(err)
	}
	if overview(t, a.own, a.firstID, period).FullAccess {
		t.Fatal("mixed roles exposed full card")
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": nil}, 200)
	expect(t, ownCurator, "GET", fmt.Sprintf("/students/%d/360", a.firstID), nil, 404)
	for _, query := range []string{"?from=2026-02-30", "?to=bad", "?from=2026-10-12&to=2026-10-11"} {
		expect(t, a.admin, "GET", fmt.Sprintf("/students/%d/360%s", a.firstID, query), nil, 400)
	}
	expect(t, a.admin, "GET", "/students/0/360", nil, 400)
	expect(t, a.admin, "GET", "/students/99999/360", nil, 404)
	expect(t, a.f.browser(t), "GET", fmt.Sprintf("/students/%d/360", a.firstID), nil, 401)
}

func TestStudent360AgeReadAndPrivacy(t *testing.T) {
	a := setupAttendance(t)
	a.lesson(t, time.Hour, 2*time.Hour)
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/students/%d", a.firstID)
	for _, tc := range []struct {
		name  string
		birth any
		want  any
	}{
		{"missing", nil, nil},
		{"known", "2013-01-01", float64(time.Now().In(zone).Year() - 2013)},
		{"changed", "2014-01-01", float64(time.Now().In(zone).Year() - 2014)},
		{"cleared", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expect(t, a.admin, "PATCH", path, map[string]any{"birth_date": tc.birth}, 200)
			for _, endpoint := range []string{"/overview", "/360"} {
				// Attendance filters must not change the date used to calculate age.
				for _, period := range []string{"", "?from=2000-01-01&to=2000-01-31"} {
					var full map[string]any
					decode(t, expect(t, a.admin, "GET", path+endpoint+period, nil, 200), &full)
					if age, exists := full["age"]; !exists || age != tc.want {
						t.Fatalf("%s%s age=%v present=%v expected=%v", endpoint, period, age, exists, tc.want)
					}
				}
				var restricted map[string]any
				decode(t, expect(t, a.own, "GET", path+endpoint+"?full_access=true", nil, 200), &restricted)
				if _, exists := restricted["age"]; exists {
					t.Fatalf("%s leaked age to teacher: %+v", endpoint, restricted)
				}
			}
		})
	}
}

func TestStudent360MoscowPeriodNoDataAndSavedComposition(t *testing.T) {
	a := setupAttendance(t)
	// 21:00 UTC is midnight of the following Moscow calendar day.
	l := lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, "2024-06-10T21:00:00Z", "2024-06-10T22:00:00Z"))
	finishOverviewLesson(t, a, l, "present")
	inside := overview(t, a.admin, a.firstID, "?from=2024-06-11&to=2024-06-11")
	if inside.Attendance.Percent == nil || *inside.Attendance.Percent != 100 || len(inside.Attendance.Items) != 1 {
		t.Fatal("Moscow period lost midnight lesson")
	}
	before := overview(t, a.admin, a.firstID, "?from=2024-06-10&to=2024-06-10")
	if before.Attendance.Percent != nil || len(before.Attendance.Items) != 0 {
		t.Fatal("no data must be null, not zero; end date must be inclusive in Moscow")
	}
	// An admin can clear a completed mark; unmarked is outside the denominator.
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, "unmarked", "")), 204)
	cleared := overview(t, a.admin, a.firstID, "?from=2024-06-11&to=2024-06-11")
	if cleared.Attendance.Percent != nil || cleared.Attendance.Unmarked != 1 {
		t.Fatal("unmarked was counted as absent")
	}
	future := a.lesson(t, 24*time.Hour, 25*time.Hour)
	lessonJournal(t, a.admin, future.ID)
	var enrollments struct {
		Enrollments []struct {
			ID        int64 `json:"id"`
			StudentID int64 `json:"student_id"`
		} `json:"enrollments"`
	}
	decode(t, expect(t, a.admin, "GET", groupPath(a.groupID), nil, 200), &enrollments)
	for _, e := range enrollments.Enrollments {
		if e.StudentID == a.firstID {
			expect(t, a.admin, "PATCH", fmt.Sprintf("%s/enrollments/%d", groupPath(a.groupID), e.ID), map[string]any{"ends_on": a.base.Format("2006-01-02")}, 200)
		}
	}
	newStudent := studentCreate(t, a.admin, map[string]any{"name": "After snapshot"})
	expect(t, a.admin, "POST", groupPath(a.groupID)+"/enrollments", map[string]any{"student_id": newStudent.ID, "starts_on": a.base.Format("2006-01-02")}, 201)
	if v := overview(t, a.admin, a.firstID, ""); len(v.Upcoming) != 1 || v.Upcoming[0].ID != future.ID {
		t.Fatal("enrollment change modified saved upcoming composition")
	}
	if v := overview(t, a.admin, newStudent.ID, ""); len(v.Upcoming) != 0 {
		t.Fatal("new pupil leaked into saved composition")
	}
	expect(t, a.own, "GET", fmt.Sprintf("/students/%d/360", newStudent.ID), nil, 404)
	// Completed history stays attached to its original group after enrollment closes.
	if v := overview(t, a.admin, a.firstID, "?from=2024-06-11&to=2024-06-11"); len(v.Attendance.Items) != 1 || v.Attendance.Items[0].GroupID != a.groupID {
		t.Fatal("attendance history lost after withdrawal")
	}
}
