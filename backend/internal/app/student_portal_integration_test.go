package app_test

import (
	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/learning"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestStudentPortalSubmissionAndHome(t *testing.T) {
	a := setupAttendance(t)
	mail := familyMail(t, a.f)
	a.admin = a.f.adminBrowser(t)
	a.own = a.f.browser(t)
	a.own.login(t, "attendance-own@example.test", "TestOnly-New-Account-9372")
	ctx := context.Background()
	l := a.lesson(t, -time.Hour, time.Hour)
	pupil := familyUser(t, a.f, a.admin, mail, "portal-self@example.test", "student", a.firstID)
	other := familyUser(t, a.f, a.admin, mail, "portal-other@example.test", "student", a.secondID)
	parentCard := parentCreate(t, a.admin, map[string]any{"name": "Portal parent", "email": "portal-parent@example.test"})
	expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/parents", a.firstID), map[string]any{"parent_id": parentCard.ID, "relationship": "parent", "is_primary": true}, 201)
	parent := familyUser(t, a.f, a.admin, mail, "portal-parent@example.test", "parent", parentCard.ID)
	familyLink(t, a.admin, a.firstID, parentCard.ID, true)
	var reviewer, version int64
	if e := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&reviewer); e != nil {
		t.Fatal(e)
	}
	if e := a.f.pool.QueryRow(ctx, `SELECT program_version_id FROM groups WHERE id=$1`, a.groupID).Scan(&version); e != nil {
		t.Fatal(e)
	}
	makeAssignment := func(format, due string, late bool) learning.Card {
		return learningCreate(t, a.own, a.firstID, learning.Input{Kind: "assignments", LessonID: &l.ID, ReviewerID: &reviewer, Content: learning.Content{Title: "Portal " + format, Instruction: "Read and explain", Criteria: "Explain why", DueOn: due, AllowLate: &late, Format: format, ProgramVersionID: &version, TemplateKey: "lesson-1"}})
	}
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	ass := makeAssignment("text", tomorrow, false)
	endpoint := fmt.Sprintf("/family/assignments/%d/attempts", ass.ID)
	in := learning.StudentAttempt{Revision: ass.Revision, Answer: "First independent answer"}
	expect(t, other, "POST", endpoint, in, 404)
	expect(t, parent, "POST", endpoint, in, 403)
	expect(t, a.admin, "POST", endpoint, in, 403)
	expect(t, pupil, "POST", endpoint, map[string]any{"revision": 1, "answer": "Forgery", "reviewer_id": reviewer}, 400)
	savedCSRF := pupil.csrf
	pupil.csrf = "invalid"
	expect(t, pupil, "POST", endpoint, in, 403)
	pupil.csrf = savedCSRF
	var count int
	if e := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM learning_records WHERE parent_id=$1`, ass.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("denied request committed", count, e)
	}
	var result struct{ ID int64 }
	decode(t, expect(t, pupil, "POST", endpoint, in, 201), &result)
	var first learning.Card
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", result.ID), nil, 200), &first)
	if first.Content.Channel != "student_portal" || first.Content.Answer != in.Answer || first.State != "submitted" {
		t.Fatal("wrong attempt", first)
	}
	first = learningAction(t, a.own, first, "return")
	var shown family.Record
	decode(t, expect(t, pupil, "GET", fmt.Sprintf("/family/learning/%d", first.ID), nil, 200), &shown)
	if shown.Feedback != "Verified decision" || shown.Parent == nil || shown.Parent.ID != ass.ID {
		t.Fatal("missing review feedback/assignment", shown)
	}
	expect(t, pupil, "POST", endpoint, in, 409)
	decode(t, expect(t, pupil, "GET", fmt.Sprintf("/family/learning/%d", ass.ID), nil, 200), &shown)
	in.Revision = shown.Revision
	in.Answer = "Second independent answer"
	decode(t, expect(t, pupil, "POST", endpoint, in, 201), &result)
	if result.ID == first.ID {
		t.Fatal("overwritten attempt")
	}
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/learning/%d", result.ID), nil, 200), &ass)
	_ = learningAction(t, a.own, ass, "approve")
	expect(t, pupil, "POST", endpoint, learning.StudentAttempt{Revision: shown.Revision + 1, Answer: "Closed"}, 400)
	raw := string(expect(t, pupil, "GET", fmt.Sprintf("/family/learning/%d", first.ID), nil, 200))
	if !strings.Contains(raw, "First independent answer") {
		t.Fatal("old answer lost")
	}
	familyTotal(t, pupil, fmt.Sprintf("/family/students/%d/learning?kind=attempts&assignment_id=%d", a.firstID, shown.ID), 2)
	expired := makeAssignment("text", "2000-01-01", false)
	expect(t, pupil, "POST", fmt.Sprintf("/family/assignments/%d/attempts", expired.ID), learning.StudentAttempt{Revision: 1, Answer: "Late"}, 409)
	link := makeAssignment("link", "2000-01-01", true)
	linkURL := fmt.Sprintf("/family/assignments/%d/attempts", link.ID)
	expect(t, pupil, "POST", linkURL, learning.StudentAttempt{Revision: 1, Answer: "javascript:alert(1)"}, 400)
	expect(t, pupil, "POST", linkURL, learning.StudentAttempt{Revision: 1, Answer: "https://example.test/work"}, 201)
	file := makeAssignment("file", tomorrow, false)
	fileURL := fmt.Sprintf("/family/assignments/%d/attempts", file.ID)
	expect(t, pupil, "POST", fileURL, learning.StudentAttempt{Revision: 1, Answer: "No file"}, 400)
	expect(t, pupil, "POST", fileURL, learning.StudentAttempt{Revision: 1, File: &learning.StudentFile{Name: "bad.html", Data: []byte("<html><body>not permitted</body></html>")}}, 415)
	if e := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM private_files WHERE student_id=$1`, a.firstID).Scan(&count); e != nil || count != 0 {
		t.Fatal("failed upload left file", e, count)
	}
	decode(t, expect(t, pupil, "POST", fileURL, learning.StudentAttempt{Revision: 1, File: &learning.StudentFile{Name: "answer.txt", Data: []byte("Student private answer")}}, 201), &result)
	decode(t, expect(t, pupil, "GET", fmt.Sprintf("/family/learning/%d", result.ID), nil, 200), &shown)
	if len(shown.Files) != 1 {
		t.Fatal("uploaded attachment missing")
	}
	download := strings.TrimPrefix(shown.Files[0].URL, "/api/v1")
	if string(expect(t, pupil, "GET", download, nil, 200)) != "Student private answer" {
		t.Fatal("wrong file")
	}
	expect(t, other, "GET", download, nil, 404)
	expect(t, a.own, "GET", fmt.Sprintf("/files/%d/content", shown.Files[0].ID), nil, 200)
	project := learningCreate(t, a.own, a.firstID, learning.Input{Kind: "projects", LessonID: &l.ID, ReviewerID: &reviewer, Content: learning.Content{Title: "My project", StartsOn: "2000-01-01", Contribution: "Built a robot", Criteria: "Moves"}})
	var home family.Home
	base := fmt.Sprintf("/family/students/%d/home", a.firstID)
	decode(t, expect(t, pupil, "GET", base, nil, 200), &home)
	if home.NextLesson == nil || home.NextLesson.ID != l.ID || len(home.Programs) != 1 || len(home.Blocks["projects"]) != 1 || home.Blocks["projects"][0].ID != project.ID || home.Totals["attempts"] != 4 {
		t.Fatalf("bad home: %+v", home)
	}
	if home.Totals["assignments"] != 3 {
		t.Fatal("completed assignment shown as active")
	}
	expect(t, other, "GET", base, nil, 404)
	expect(t, parent, "GET", base, nil, 404)
	for _, denied := range []string{"sources", "reviewer_id", "outcome", "comment", "actor_id", "history"} {
		// Program summaries must not leak internal report details or the roster.
		if strings.Contains(string(expect(t, pupil, "GET", base, nil, 200)), `"`+denied+`":`) && denied != "sources" {
			t.Fatal("internal home field", denied)
		}
	}
	var uid, invitation int64
	if e := a.f.pool.QueryRow(ctx, `SELECT user_id,invitation_id FROM student_accounts WHERE student_id=$1`, a.firstID).Scan(&uid, &invitation); e != nil {
		t.Fatal(e)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := a.f.pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	assertDenied := func(status int) {
		expect(t, pupil, "GET", base, nil, status)
		expect(t, pupil, "GET", download, nil, status)
		expect(t, pupil, "POST", fileURL, learning.StudentAttempt{Revision: 1, File: &learning.StudentFile{Name: "no.txt", Data: []byte("Denied")}}, status)
	}
	exec(`DELETE FROM user_roles WHERE user_id=$1 AND role='student'`, uid)
	assertDenied(403)
	exec(`INSERT INTO user_roles(user_id,role) VALUES($1,'student')`, uid)
	exec(`DELETE FROM student_accounts WHERE student_id=$1`, a.firstID)
	assertDenied(404)
	exec(`INSERT INTO student_accounts(student_id,user_id,invitation_id) VALUES($1,$2,$3)`, a.firstID, uid, invitation)
	exec(`UPDATE users SET blocked=true WHERE id=$1`, uid)
	assertDenied(401)
	exec(`UPDATE users SET blocked=false WHERE id=$1`, uid)
	expect(t, a.own, "POST", fmt.Sprintf("/files/%d/revoke", shown.Files[0].ID), nil, 204)
	expect(t, pupil, "GET", download, nil, 404)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"status": "archived"}, 200)
	expect(t, pupil, "GET", base, nil, 404)
	expect(t, pupil, "GET", download, nil, 404)
	expect(t, pupil, "POST", fileURL, learning.StudentAttempt{Revision: 1, File: &learning.StudentFile{Name: "again.txt", Data: []byte("No access")}}, 404)
}
