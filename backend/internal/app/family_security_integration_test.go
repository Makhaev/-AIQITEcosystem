package app_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/modules/family"
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/modules/learning"
)

func TestFamilyTeacherCannotCreateWorkForCancelledLesson(t *testing.T) {
	a := setupAttendance(t)
	// Another permitted lesson keeps access to the pupil. Pupil access must not
	// authorize a write against the cancelled lesson, even for its own teacher.
	a.lesson(t, -time.Hour, time.Hour)
	cancelled := a.lesson(t, 2*time.Hour, 3*time.Hour)
	expect(t, a.admin, "POST", lessonPath(cancelled.ID)+"/cancel", nil, 200)
	var versionID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT program_version_id FROM groups WHERE id=$1`, a.groupID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	late := true
	in := learning.Input{Kind: "assignments", LessonID: &cancelled.ID, ReviewerID: &a.f.admin.ID, Content: learning.Content{
		Title: "Must not be saved", Instruction: "Explain", Criteria: "Reason",
		DueOn:            time.Now().AddDate(0, 0, 1).Format("2006-01-02"),
		ProgramVersionID: &versionID, TemplateKey: "lesson-1", Format: "text", AllowLate: &late,
	}}
	status, body := a.own.do(t, "POST", fmt.Sprintf("/students/%d/education", a.firstID), in)
	var count int
	if err := a.f.pool.QueryRow(context.Background(), `SELECT count(*) FROM learning_records WHERE lesson_id=$1`, cancelled.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("denied request persisted %d records (HTTP %d): %s", count, status, body)
	}
	if status != 403 {
		t.Fatalf("cancelled lesson write: want 403, got %d: %s", status, body)
	}
}

// Reuse the same authenticated client throughout: neither a cached role nor a
// previously successful URL may preserve access after a relationship changes.
func TestFamilyEveryRouteRechecksAccess(t *testing.T) {
	a := setupAttendance(t)
	mail := familyMail(t, a.f)
	a.admin = a.f.adminBrowser(t)
	l := a.lesson(t, -time.Hour, time.Hour)
	p := parentCreate(t, a.admin, map[string]any{"name": "Parent", "email": "route-parent@example.test"})
	expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/parents", a.firstID), map[string]any{"parent_id": p.ID, "relationship": "parent", "is_primary": true}, 201)
	parent := familyUser(t, a.f, a.admin, mail, "route-parent@example.test", "parent", p.ID)
	student := familyUser(t, a.f, a.admin, mail, "route-student@example.test", "student", a.firstID)
	familyLink(t, a.admin, a.firstID, p.ID, true)
	ctx := context.Background()
	var uid, invitationID, versionID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id,invitation_id FROM parent_accounts WHERE parent_id=$1`, p.ID).Scan(&uid, &invitationID); err != nil {
		t.Fatal(err)
	}
	if err := a.f.pool.QueryRow(ctx, `SELECT program_version_id FROM groups WHERE id=$1`, a.groupID).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := a.f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// An additional staff role must never widen the family response.
	exec(t, `INSERT INTO user_roles(user_id,role) VALUES($1,'leader')`, uid)
	var file files.File
	decode(t, expect(t, a.admin, "POST", "/files", files.Input{StudentID: a.firstID, Name: "Permitted.txt", Data: []byte("Family-only attachment")}, 201), &file)
	expect(t, a.admin, "PUT", fmt.Sprintf("/files/%d/family-access", file.ID), family.Audience{Students: true, Parents: true}, 204)
	late := true
	ass := learningCreate(t, a.admin, a.firstID, learning.Input{Kind: "assignments", LessonID: &l.ID, ReviewerID: &a.f.admin.ID, Content: learning.Content{
		Title: "Permitted assignment", Instruction: "Explain", Criteria: "Reason", DueOn: time.Now().AddDate(0, 0, 1).Format("2006-01-02"),
		ProgramVersionID: &versionID, TemplateKey: "lesson-1", Format: "text", AllowLate: &late,
	}, FileIDs: []int64{file.ID}})
	base := fmt.Sprintf("/family/students/%d", a.firstID)
	var report family.Report
	decode(t, expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/family-reports", a.firstID), family.ReportInput{Title: "Permitted report", Body: "Family text", FileIDs: []int64{file.ID}}, 201), &report)
	report = publishFamilyReport(t, a.f, a.admin, a.admin, report)
	expect(t, parent, "POST", base+"/requests", family.RequestInput{Subject: "Private request", Body: "Only this parent"}, 201)
	familyTotal(t, student, base+"/requests?search=Private", 0)
	paths := []string{
		base + "/parent-home", base + "/events?search=Permitted",
		base + "/lessons?search=", base + "/learning?search=Permitted", base + "/progress", base + "/reports?search=Permitted", base + "/requests?search=Private",
		fmt.Sprintf("/family/learning/%d", ass.ID), fmt.Sprintf("/family/learning/%d/files/%d", ass.ID, file.ID),
		fmt.Sprintf("/family/reports/%d", report.ID), fmt.Sprintf("/family/reports/%d/files/%d", report.ID, file.ID),
	}
	check := func(t *testing.T, b *browser, path string, want int) {
		t.Helper()
		// Conditional/HEAD requests must also be authorized; no cache validator
		// may turn an old permitted response into a 304 after revocation.
		for _, method := range []string{"GET", "HEAD"} {
			req, err := http.NewRequest(method, a.f.server.URL+"/api/v1"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("If-None-Match", "*")
			res, err := b.client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != want {
				t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, res.StatusCode, body)
			}
			if !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
				t.Fatalf("cacheable family response: %s", path)
			}
			if want != 200 && (strings.Contains(string(body), "Family text") || strings.Contains(string(body), "Permitted") || strings.Contains(string(body), "Only this parent")) {
				t.Fatalf("denial leaked content: %s", body)
			}
		}
	}
	for _, path := range paths {
		check(t, parent, path, 200)
	}
	for _, tc := range []struct {
		name, deny, restore string
		args                []any
		status              int
	}{
		{"confirmation", `UPDATE family_access SET confirmed=false WHERE student_id=$1 AND parent_id=$2`, `UPDATE family_access SET confirmed=true WHERE student_id=$1 AND parent_id=$2`, []any{a.firstID, p.ID}, 404},
		{"archived_parent", `UPDATE parents SET status='archived' WHERE id=$1`, `UPDATE parents SET status='active' WHERE id=$1`, []any{p.ID}, 404},
		{"archived_student", `UPDATE students SET status='archived' WHERE id=$1`, `UPDATE students SET status='active' WHERE id=$1`, []any{a.firstID}, 404},
		{"account_binding", `DELETE FROM parent_accounts WHERE parent_id=$1 AND user_id=$2 AND invitation_id=$3`, `INSERT INTO parent_accounts(parent_id,user_id,invitation_id) VALUES($1,$2,$3)`, []any{p.ID, uid, invitationID}, 404},
		{"role", `DELETE FROM user_roles WHERE user_id=$1 AND role='parent'`, `INSERT INTO user_roles(user_id,role) VALUES($1,'parent')`, []any{uid}, 403},
		{"blocked_account", `UPDATE users SET blocked=true WHERE id=$1`, `UPDATE users SET blocked=false WHERE id=$1`, []any{uid}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec(t, tc.deny, tc.args...)
			defer exec(t, tc.restore, tc.args...)
			for _, path := range paths {
				check(t, parent, path, tc.status)
			}
			if tc.status == 404 {
				familyTotal(t, parent, "/family/students?search=First", 0)
			} else {
				check(t, parent, "/family/students?search=First", tc.status)
			}
			expect(t, parent, "POST", base+"/requests", family.RequestInput{Subject: "Denied", Body: "Denied"}, tc.status)
		})
	}
	for _, path := range paths {
		check(t, parent, path, 200)
	}
}
