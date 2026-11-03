package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/learning"
)

func TestParentHomeChildIsolationAttendanceAndEvents(t *testing.T) {
	a := setupAttendance(t)
	mail := familyMail(t, a.f)
	a.admin = a.f.adminBrowser(t)
	a.own = a.f.browser(t)
	a.own.login(t, "attendance-own@example.test", "TestOnly-New-Account-9372")
	p := parentCreate(t, a.admin, map[string]any{"name": "Two children", "email": "parent-home@example.test"})
	parent := familyUser(t, a.f, a.admin, mail, "parent-home@example.test", "parent", p.ID)
	student := familyUser(t, a.f, a.admin, mail, "parent-home-student@example.test", "student", a.firstID)
	for _, sid := range []int64{a.firstID, a.secondID} {
		linkParent(t, a.admin, sid, p.ID, true)
		familyLink(t, a.admin, sid, p.ID, true)
	}
	base := fmt.Sprintf("/family/students/%d", a.firstID)
	second := fmt.Sprintf("/family/students/%d", a.secondID)
	// Exactly one Moscow day; UTC's preceding evening belongs to that day.
	for i, status := range []string{"present", "late", "absent", "excused", "absent"} {
		start := time.Date(2020, 3, 31, 21, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
		if i == 4 {
			start = start.Add(20 * time.Hour) // next Moscow day, excluded
		}
		l := lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339)))
		finishOverviewLesson(t, a, l, status)
	}
	scheduled := a.lesson(t, -4*time.Hour, -3*time.Hour)
	expect(t, a.admin, "PUT", lessonPath(scheduled.ID)+"/attendance", marksBody(markValue(a.firstID, "absent", "PRIVATE JOURNAL")), 204)
	future := a.lesson(t, time.Hour, 2*time.Hour)
	cancelled := a.lesson(t, 3*time.Hour, 4*time.Hour)
	expect(t, a.admin, "POST", lessonPath(cancelled.ID)+"/cancel", nil, 200)
	var version int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT program_version_id FROM groups WHERE id=$1`, a.groupID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	late := true
	tomorrow := time.Now().In(time.FixedZone("Moscow", 10800)).AddDate(0, 0, 1).Format("2006-01-02")
	obs := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "observations", LessonID: &scheduled.ID, ReviewerID: &a.f.admin.ID, Content: learning.Content{Title: "First child feedback", Fact: "Solved exercise", Interpretation: "PRIVATE INTERPRETATION", Context: "PRIVATE CONTEXT", OccurredOn: "2020-04-01", ModelVersion: "v1", SkillKey: "explain", SkillName: "Explanation"}})
	obs = learningAction(t, a.own, obs, "submit")
	obs = learningAction(t, a.admin, obs, "approve")
	obs = learningAction(t, a.admin, obs, "publish")
	var unshared family.Home
	decode(t, expect(t, parent, "GET", base+"/parent-home", nil, 200), &unshared)
	if len(unshared.Blocks["observations"]) != 0 {
		t.Fatal("observation published internally but not to parents leaked")
	}
	expect(t, a.admin, "PUT", fmt.Sprintf("/learning/%d/family-access", obs.ID), family.Audience{Revision: obs.Revision, Parents: true}, 204)
	assignment := learningCreate(t, a.admin, a.firstID, learning.Input{Kind: "assignments", ReviewerID: &a.f.admin.ID, Content: learning.Content{Title: "First child deadline", Instruction: "Explain", Criteria: "Reason", DueOn: tomorrow, Format: "text", AllowLate: &late, ProgramVersionID: &version, TemplateKey: "lesson-1"}})
	learningCreate(t, a.admin, a.firstID, learning.Input{Kind: "assignments", ReviewerID: &a.f.admin.ID, Content: learning.Content{Title: "Expired deadline", Instruction: "Explain", Criteria: "Reason", DueOn: "2000-01-01", Format: "text", AllowLate: &late, ProgramVersionID: &version, TemplateKey: "lesson-1"}})
	var report family.Report
	decode(t, expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/family-reports", a.firstID), family.ReportInput{Title: "First child report", Body: "Published result"}, 201), &report)
	report = publishFamilyReport(t, a.f, a.admin, a.admin, report)
	expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/family-reports", a.firstID), family.ReportInput{Title: "PRIVATE DRAFT", Body: "PRIVATE DRAFT BODY"}, 201)
	expect(t, parent, "POST", base+"/requests", family.RequestInput{Subject: "First child question", Body: "Parent request"}, 201)
	expect(t, student, "POST", base+"/requests", family.RequestInput{Subject: "PRIVATE STUDENT REQUEST", Body: "PRIVATE BODY"}, 201)
	var home family.Home
	raw := expect(t, parent, "GET", base+"/parent-home?from=2020-04-01&to=2020-04-01", nil, 200)
	decode(t, raw, &home)
	if strings.Contains(string(raw), "PRIVATE") || len(home.Parent.Reports) != 1 || len(home.Parent.Requests) != 1 || len(home.Programs) == 0 || len(home.Blocks["observations"]) != 1 {
		t.Fatalf("wrong parent projection: %s", raw)
	}
	at := home.Parent.Attendance
	if at.Total != 4 || at.Percent == nil || *at.Percent != 50 || at.Present != 1 || at.Late != 1 || at.Absent != 1 || at.Excused != 1 {
		t.Fatalf("wrong Moscow period attendance: %+v", at)
	}
	if len(home.Parent.Lessons) != 1 || home.Parent.Lessons[0].ID != future.ID || home.Parent.Totals["events"] != 2 {
		t.Fatalf("wrong upcoming events: %+v", home.Parent)
	}
	familyTotal(t, parent, base+"/lessons?from=2020-04-01&to=2020-04-01", 4)
	familyTotal(t, parent, base+"/events?search=First", 1)
	familyTotal(t, parent, second+"/events?search=First", 0)
	decode(t, expect(t, parent, "GET", second+"/parent-home?from=2020-04-01&to=2020-04-01", nil, 200), &home)
	if home.Parent.Attendance.Percent == nil || *home.Parent.Attendance.Percent != 100 || len(home.Parent.Reports) != 0 || len(home.Parent.Requests) != 0 || len(home.Blocks["assignments"]) != 0 {
		t.Fatalf("children mixed: %+v", home)
	}
	decode(t, expect(t, parent, "GET", base+"/parent-home?from=1999-01-01&to=1999-01-01", nil, 200), &home)
	if home.Parent.Attendance.Percent != nil || home.Parent.Attendance.Total != 0 {
		t.Fatal("empty period must not produce zero percent")
	}
	for _, query := range []string{"?from=2020-04-02&to=2020-04-01", "?from=2020-02-30&to=2020-03-01", "?from=2020-04-01"} {
		expect(t, parent, "GET", base+"/parent-home"+query, nil, 400)
	}
	// Closing a work removes its deadline immediately.
	learningAction(t, a.admin, assignment, "withdraw")
	learningAction(t, a.admin, obs, "unpublish")
	familyTotal(t, parent, base+"/events?search=First", 0)
	report = reportAction(t, a.admin, report, "withdraw", "Withdraw report")
	decode(t, expect(t, parent, "GET", base+"/parent-home", nil, 200), &home)
	if len(home.Parent.Reports) != 0 || len(home.Blocks["observations"]) != 0 {
		t.Fatal("withdrawn publication remained on home")
	}
	expect(t, student, "GET", base+"/parent-home", nil, 404)
	for _, path := range []string{"/parent-home", "/events"} {
		expect(t, parent, "GET", "/family/students/999999"+path, nil, 404)
	}
	familyLink(t, a.admin, a.firstID, p.ID, false)
	expect(t, parent, "GET", base+"/parent-home", nil, 404)
	expect(t, parent, "GET", base+"/events", nil, 404)
	expect(t, parent, "GET", second+"/parent-home", nil, 200)
}
