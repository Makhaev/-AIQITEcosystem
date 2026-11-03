package app_test

import (
	"aiq.local/core/internal/app"
	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/modules/invitations"
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/platform/config"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func familyMail(t *testing.T, f *fixture) *invitationMail {
	t.Helper()
	mail := &invitationMail{}
	h, _, e := app.RouterWithInvitationSender(f.pool, config.Config{Origin: origin}, mail)
	if e != nil {
		t.Fatal(e)
	}
	f.server.Close()
	f.server = httptest.NewServer(h)
	t.Cleanup(f.server.Close)
	return mail
}
func familyUser(t *testing.T, f *fixture, admin *browser, mail *invitationMail, email, role string, id int64) *browser {
	t.Helper()
	issue(t, admin, email, role, id)
	b := f.browser(t)
	expect(t, b, "POST", "/invitations/accept", invitations.AcceptInput{Token: mail.token(t), Password: userPassword}, 200)
	b.login(t, email, userPassword)
	return b
}
func familyLink(t *testing.T, b *browser, sid, pid int64, confirmed bool) {
	t.Helper()
	expect(t, b, "PUT", fmt.Sprintf("/students/%d/parents/%d/access", sid, pid), map[string]any{"confirmed": confirmed}, 204)
}
func familyTotal(t *testing.T, b *browser, path string, n int) {
	t.Helper()
	var p struct{ Total int }
	decode(t, expect(t, b, "GET", path, nil, 200), &p)
	if p.Total != n {
		t.Fatalf("%s total=%d want=%d", path, p.Total, n)
	}
}
func TestFamilyAccessRevocationReportsFilesAndRequests(t *testing.T) {
	f, admin, mail := setupInvitations(t)
	ctx := context.Background()
	cu, curator := provision(t, f, admin, "family-curator@example.test", "curator")
	next, nextCurator := provision(t, f, admin, "family-next@example.test", "curator")
	child := studentCreate(t, admin, map[string]any{"name": "Own child", "curator_id": cu.ID})
	other := studentCreate(t, admin, map[string]any{"name": "Hidden child"})
	p := parentCreate(t, admin, map[string]any{"name": "Parent", "email": "family@example.test"})
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", child.ID), map[string]any{"parent_id": p.ID, "relationship": "parent", "is_primary": true}, 201)
	parent := familyUser(t, f, admin, mail, "family@example.test", "parent", p.ID)
	student := familyUser(t, f, admin, mail, "child@example.test", "student", child.ID)
	familyTotal(t, parent, "/family/students", 0)
	familyTotal(t, student, "/family/students", 1)
	base := fmt.Sprintf("/family/students/%d", child.ID)
	expect(t, parent, "GET", base+"/reports", nil, 404)
	expect(t, curator, "PUT", fmt.Sprintf("/students/%d/parents/%d/access", child.ID, p.ID), map[string]bool{"confirmed": true}, 403)
	familyLink(t, admin, child.ID, p.ID, true)
	familyTotal(t, parent, "/family/students", 1)
	familyTotal(t, parent, "/family/students?search=Hidden", 0)
	for _, b := range []*browser{student, parent} {
		for _, path := range []string{fmt.Sprintf("/students/%d/overview", child.ID), "/parents", "/students?search=Own", "/users", "/lessons", "/learning/1"} {
			expect(t, b, "GET", path, nil, 403)
		}
		for _, kind := range []string{"lessons", "learning", "progress", "reports", "requests"} {
			expect(t, b, "GET", fmt.Sprintf("/family/students/%d/%s", other.ID, kind), nil, 404)
		}
	}
	var file, foreign files.File
	decode(t, expect(t, admin, "POST", "/files", files.Input{StudentID: child.ID, Name: "result.txt", Data: []byte("Public family result")}, 201), &file)
	decode(t, expect(t, admin, "POST", "/files", files.Input{StudentID: other.ID, Name: "HIDDEN.txt", Data: []byte("Other pupil")}, 201), &foreign)
	expect(t, curator, "PUT", fmt.Sprintf("/files/%d/family-access", file.ID), family.Audience{Students: true, Parents: true}, 404)
	expect(t, admin, "PUT", fmt.Sprintf("/files/%d/access", file.ID), files.Grant{UserID: cu.ID, Allow: true}, 204)
	expect(t, curator, "PUT", fmt.Sprintf("/files/%d/family-access", file.ID), family.Audience{Students: true, Parents: true}, 403)
	expect(t, admin, "PUT", fmt.Sprintf("/files/%d/family-access", file.ID), family.Audience{Students: true, Parents: true}, 204)
	var report family.Report
	input := family.ReportInput{Title: "Family report", Body: "Only approved family text", FileIDs: []int64{file.ID}}
	decode(t, expect(t, curator, "POST", fmt.Sprintf("/students/%d/family-reports", child.ID), input, 201), &report)
	path := fmt.Sprintf("/family/reports/%d", report.ID)
	download := fmt.Sprintf("%s/files/%d", path, file.ID)
	expect(t, parent, "GET", path, nil, 404)
	expect(t, parent, "GET", download, nil, 404)
	published := true
	report = publishFamilyReport(t, f, admin, curator, report)
	for _, b := range []*browser{parent, student} {
		expect(t, b, "GET", path, nil, 200)
		if string(expect(t, b, "GET", download, nil, 200)) != "Public family result" {
			t.Fatal("bad file")
		}
		expect(t, b, "GET", fmt.Sprintf("/files/%d/content", file.ID), nil, 403)
		expect(t, b, "GET", fmt.Sprintf("%s/files/%d", path, foreign.ID), nil, 404)
	}
	report = reportAction(t, admin, report, "withdraw", "Withdraw report")
	expect(t, parent, "GET", path, nil, 404)
	expect(t, student, "GET", download, nil, 404)
	familyTotal(t, parent, base+"/reports?search=Family", 0)
	report = reportAction(t, curator, report, "correct", "")
	report = publishFamilyReport(t, f, admin, curator, report)
	report = reportAction(t, curator, report, "correct", "")
	input.Revision = report.Revision
	input.ReviewerID = report.ReviewerID
	input.Body = "Revised unpublished text"
	decode(t, expect(t, curator, "PUT", fmt.Sprintf("/family-reports/%d", report.ID), input, 200), &report)
	expect(t, parent, "GET", path, nil, 200)
	expect(t, curator, "PUT", fmt.Sprintf("/family-reports/%d", report.ID), input, 409)
	report = publishFamilyReport(t, f, admin, curator, report)
	familyLink(t, admin, child.ID, p.ID, false)
	familyTotal(t, parent, "/family/students", 0)
	expect(t, parent, "GET", download, nil, 404)
	expect(t, parent, "GET", path, nil, 404)
	expect(t, student, "GET", download, nil, 200)
	familyLink(t, admin, child.ID, p.ID, true)
	expect(t, admin, "PUT", fmt.Sprintf("/files/%d/family-access", file.ID), family.Audience{Students: true, Parents: false}, 204)
	expect(t, parent, "GET", download, nil, 404)
	expect(t, student, "GET", download, nil, 200)
	var req family.Request
	decode(t, expect(t, parent, "POST", base+"/requests", family.RequestInput{Subject: "Parent question", Body: "Private to requester"}, 201), &req)
	familyTotal(t, student, base+"/requests", 0)
	familyTotal(t, parent, base+"/requests", 1)
	req = changeRequest(t, curator, req, true, "assign", &cu.ID, "", "")
	req = changeRequest(t, curator, req, true, "start", nil, "", "")
	expect(t, curator, "PUT", fmt.Sprintf("/family-requests/%d/reply", req.ID), family.ReplyInput{Revision: req.Revision, Reply: "Curator response"}, 204)
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", child.ID), map[string]any{"curator_id": next.ID}, 200)
	expect(t, curator, "GET", fmt.Sprintf("/students/%d/family-requests", child.ID), nil, 404)
	expect(t, curator, "POST", fmt.Sprintf("/family-reports/%d/publication", report.ID), family.Publication{Version: report.Version, Revision: report.Revision, Published: &published}, 404)
	familyTotal(t, nextCurator, fmt.Sprintf("/students/%d/family-requests", child.ID), 1)
	familyLink(t, admin, child.ID, p.ID, false)
	expect(t, parent, "POST", base+"/requests", family.RequestInput{Subject: "No longer allowed", Body: "No access"}, 404)
	familyLink(t, admin, child.ID, p.ID, true)
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", p.ID), map[string]any{"status": "archived"}, 200)
	expect(t, parent, "GET", path, nil, 404)
	expect(t, admin, "POST", fmt.Sprintf("/files/%d/revoke", file.ID), nil, 204)
	expect(t, student, "GET", download, nil, 404)
	var count int
	if e := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='family.access_changed'`).Scan(&count); e != nil || count < 4 {
		t.Fatalf("audit missing: %d %v", count, e)
	}
	if _, e := f.pool.Exec(ctx, `UPDATE family_report_history SET content='{}' WHERE report_id=$1`, report.ID); e == nil {
		t.Fatal("history mutable")
	}
}

func TestFamilyLearningProjectionAndTeacherScope(t *testing.T) {
	a := setupAttendance(t)
	mail := familyMail(t, a.f)
	a.admin = a.f.adminBrowser(t)
	a.own = a.f.browser(t)
	a.own.login(t, "attendance-own@example.test", "TestOnly-New-Account-9372")
	a.foreign = a.f.browser(t)
	a.foreign.login(t, "attendance-foreign@example.test", "TestOnly-New-Account-9372")
	l := a.lesson(t, -time.Hour, time.Hour)
	ctx := context.Background()
	adminID := a.f.admin.ID
	student := familyUser(t, a.f, a.admin, mail, "self@example.test", "student", a.firstID)
	other := familyUser(t, a.f, a.admin, mail, "other@example.test", "student", a.secondID)
	var teacherID, versionID int64
	if e := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherID); e != nil {
		t.Fatal(e)
	}
	if e := a.f.pool.QueryRow(ctx, `SELECT program_version_id FROM groups WHERE id=$1`, a.groupID).Scan(&versionID); e != nil {
		t.Fatal(e)
	}
	cu, cb := provision(t, a.f, a.admin, "family-learning-curator@example.test", "curator")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": cu.ID}, 200)
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	late := true
	goal := learningCreate(t, cb, a.firstID, learning.Input{Kind: "goals", Content: learning.Content{Title: "Own goal", DueOn: tomorrow, ModelVersion: "v1", SkillKey: "explain"}})
	ass := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "assignments", LessonID: &l.ID, ReviewerID: &teacherID, Content: learning.Content{Title: "Own assignment", Instruction: "Explain", Criteria: "Reason", DueOn: tomorrow, ProgramVersionID: &versionID, TemplateKey: "lesson-1", Format: "text", AllowLate: &late}})
	attempt := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "attempts", ParentID: &ass.ID, Content: learning.Content{Title: "Own answer", Answer: "My solution", Sender: "PRIVATE SENDER", Channel: "PRIVATE CHANNEL"}})
	for _, id := range []int64{goal.ID, ass.ID, attempt.ID} {
		expect(t, student, "GET", fmt.Sprintf("/family/learning/%d", id), nil, 200)
		expect(t, other, "GET", fmt.Sprintf("/family/learning/%d", id), nil, 404)
	}
	raw := string(expect(t, student, "GET", fmt.Sprintf("/family/learning/%d", attempt.ID), nil, 200))
	if strings.Contains(raw, "PRIVATE") || strings.Contains(raw, "history") || strings.Contains(raw, "reviewer_id") {
		t.Fatal("internal fields leaked")
	}
	familyTotal(t, other, fmt.Sprintf("/family/students/%d/learning?search=Own", a.secondID), 0)
	familyTotal(t, student, fmt.Sprintf("/family/students/%d/lessons", a.firstID), 1)
	var progress map[string]int
	decode(t, expect(t, student, "GET", fmt.Sprintf("/family/students/%d/progress", a.firstID), nil, 200), &progress)
	if progress["assignments"] != 1 || progress["goals"] != 1 {
		t.Fatal("wrong personal progress")
	}
	// A teacher is related to this pupil through another lesson, but is neither
	// entitled by reviewer assignment nor explicit record grant to these works.
	foreignTeacher := teacherCreate(t, a.admin, map[string]any{"name": "Foreign teacher", "user_id": a.foreignUserID})
	lessonCreate(t, a.admin, lessonBody(a.groupID, foreignTeacher.ID, a.base.Add(3*time.Hour).Format(time.RFC3339Nano), a.base.Add(4*time.Hour).Format(time.RFC3339Nano)))
	for _, id := range []int64{ass.ID, attempt.ID} {
		if _, e := a.f.pool.Exec(ctx, `UPDATE learning_records SET reviewer_id=$2 WHERE id=$1`, id, a.foreignUserID); e != nil {
			t.Fatal(e)
		}
		if _, e := a.f.pool.Exec(ctx, `INSERT INTO learning_grants(record_id,user_id) VALUES($1,$2)`, id, a.foreignUserID); e != nil {
			t.Fatal(e)
		}
		expect(t, a.foreign, "GET", fmt.Sprintf("/learning/%d", id), nil, 404)
		expect(t, cb, "GET", fmt.Sprintf("/learning/%d", id), nil, 200)
	}
	var file files.File
	decode(t, expect(t, a.own, "POST", "/files", files.Input{StudentID: a.firstID, Name: "Feedback.txt", Data: []byte("Visible attachment")}, 201), &file)
	attempt = learningAction(t, a.admin, attempt, "approve")
	today := time.Now().In(time.FixedZone("Moscow", 10800)).Format("2006-01-02")
	obs := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "observations", LessonID: &l.ID, ReviewerID: &adminID, Content: learning.Content{Title: "Published feedback", Fact: "Solved exercise", Interpretation: "PRIVATE INTERPRETATION", Context: "PRIVATE CONTEXT", OccurredOn: today, ModelVersion: "v1", SkillKey: "explain", SkillName: "Explanation"}, Sources: []learning.Reference{{ID: attempt.ID, Revision: attempt.Revision}}, FileIDs: []int64{file.ID}})
	path := fmt.Sprintf("/family/learning/%d", obs.ID)
	download := fmt.Sprintf("%s/files/%d", path, file.ID)
	expect(t, student, "GET", path, nil, 404)
	obs = learningAction(t, a.own, obs, "submit")
	obs = learningAction(t, a.admin, obs, "approve")
	obs = learningAction(t, a.admin, obs, "publish")
	expect(t, student, "GET", path, nil, 404)
	expect(t, a.admin, "PUT", fmt.Sprintf("/learning/%d/family-access", obs.ID), family.Audience{Revision: obs.Revision, Students: true, Parents: true}, 204)
	raw = string(expect(t, student, "GET", path, nil, 200))
	if strings.Contains(raw, "PRIVATE") || strings.Contains(raw, "Feedback.txt") {
		t.Fatal("internal notes or ungranted file leaked")
	}
	var projected family.Record
	decode(t, []byte(raw), &projected)
	if len(projected.Sources) != 1 || projected.Sources[0].ID != attempt.ID {
		t.Fatal("visible nested basis missing")
	}
	attempt = learningAction(t, a.admin, attempt, "withdraw")
	expect(t, student, "GET", fmt.Sprintf("/family/learning/%d", attempt.ID), nil, 404)
	familyTotal(t, student, fmt.Sprintf("/family/students/%d/learning?kind=attempts", a.firstID), 0)
	decode(t, expect(t, student, "GET", path, nil, 200), &projected)
	if len(projected.Sources) != 0 {
		t.Fatal("withdrawn nested basis leaked")
	}
	expect(t, a.own, "PUT", fmt.Sprintf("/files/%d/family-access", file.ID), family.Audience{Students: true}, 204)
	expect(t, student, "GET", download, nil, 200)
	familyTotal(t, student, fmt.Sprintf("/family/students/%d/learning?search=PRIVATE", a.firstID), 0)
	obs = learningAction(t, a.admin, obs, "unpublish")
	expect(t, student, "GET", path, nil, 404)
	expect(t, student, "GET", download, nil, 404)
	obs = learningAction(t, a.admin, obs, "publish")
	expect(t, student, "GET", path, nil, 404) // old approval never covers a new revision
	expect(t, a.admin, "PUT", fmt.Sprintf("/learning/%d/family-access", obs.ID), family.Audience{Revision: obs.Revision, Students: true}, 204)
	expect(t, student, "GET", path, nil, 200)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"status": "archived"}, 200)
	expect(t, student, "GET", path, nil, 404)
	expect(t, student, "GET", download, nil, 404)
}
