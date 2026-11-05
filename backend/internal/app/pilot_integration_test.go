package app_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/app"
	"aiq.local/core/internal/platform/config"
)

const familyPassword = "PilotOnly-Family-2026"

type pilotLink struct {
	UserID  int64     `json:"user_id"`
	URL     string    `json:"url"`
	Expires time.Time `json:"expires_at"`
}

func linkToken(link pilotLink) string {
	pieces := strings.Split(link.URL, "/#/access/")
	if len(pieces) != 2 {
		return ""
	}
	return pieces[1]
}
func invite(t *testing.T, f *fixture, admin *browser, role string, profile int64, email string) (pilotLink, *browser) {
	t.Helper()
	var link pilotLink
	decode(t, expect(t, admin, "POST", "/invitations", map[string]any{"role": role, "profile_id": profile, "email": email, "name": "Pilot " + role}, 201), &link)
	guest := f.browser(t)
	expect(t, guest, "POST", "/auth/accept-link", map[string]any{"token": linkToken(link), "password": familyPassword}, 204)
	guest.login(t, email, familyPassword)
	return link, guest
}
func items(t *testing.T, b *browser, path string) []map[string]any {
	t.Helper()
	var result struct {
		Items []map[string]any `json:"items"`
	}
	decode(t, expect(t, b, "GET", path, nil, 200), &result)
	return result.Items
}
func resultID(t *testing.T, body []byte) int64 {
	t.Helper()
	var value struct {
		ID int64 `json:"id"`
	}
	decode(t, body, &value)
	return value.ID
}

// This is the release acceptance scenario through real HTTP and PostgreSQL,
// with a separate cookie jar for every role and a second, unrelated family.
func TestPilotEndToEndAndFamilyIsolation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	teacher := teacherCreate(t, admin, map[string]any{"name": "Pilot teacher"})
	_, teacherBrowser := invite(t, f, admin, "teacher", teacher.ID, "pilot-teacher@example.test")
	_, foreignTeacher := provision(t, f, admin, "foreign-pilot@example.test", "teacher")
	program := programCreate(t, admin, map[string]any{"name": "Pilot program", "status": "active"})
	group := groupCreate(t, admin, program.ID, 10, "active")
	child := studentCreate(t, admin, map[string]any{"name": "Own child"})
	other := studentCreate(t, admin, map[string]any{"name": "Other child"})
	parent := parentCreate(t, admin, map[string]any{"name": "Own parent", "email": "own-parent@example.test"})
	otherParent := parentCreate(t, admin, map[string]any{"name": "Foreign parent", "email": "foreign-parent@example.test"})
	for _, pair := range [][2]int64{{child.ID, parent.ID}, {other.ID, otherParent.ID}} {
		expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", pair[0]), map[string]any{"parent_id": pair[1], "relationship": "Родитель", "is_primary": true}, 201)
		expect(t, admin, "POST", groupPath(group.ID)+"/enrollments", map[string]any{"student_id": pair[0], "starts_on": "2000-01-01"}, 201)
	}
	studentLink, student := invite(t, f, admin, "student", child.ID, "own-student@example.test")
	_, foreignStudent := invite(t, f, admin, "student", other.ID, "foreign-student@example.test")
	_, parentBrowser := invite(t, f, admin, "parent", parent.ID, "own-parent@example.test")
	_, foreignParent := invite(t, f, admin, "parent", otherParent.ID, "foreign-parent@example.test")
	now := time.Now().UTC()
	lesson := lessonCreate(t, admin, lessonBody(group.ID, teacher.ID, now.Add(-2*time.Hour).Format(time.RFC3339), now.Add(-time.Hour).Format(time.RFC3339)))
	lessonJournal(t, teacherBrowser, lesson.ID)
	expect(t, teacherBrowser, "PUT", lessonPath(lesson.ID)+"/attendance", marksBody(markValue(child.ID, "present", "Private teacher note"), markValue(other.ID, "present", "")), 204)
	body := map[string]any{"lesson_id": lesson.ID, "student_id": child.ID, "title": "Аргументация", "instructions": "Предложите два аргумента."}
	expect(t, foreignTeacher, "POST", "/assignments", body, 403)
	expect(t, parentBrowser, "POST", "/assignments", body, 403)
	assignmentID := resultID(t, expect(t, teacherBrowser, "POST", "/assignments", body, 201))
	assignmentPath := fmt.Sprintf("/assignments/%d", assignmentID)
	progress := items(t, student, "/learning/progress?student_id="+fmt.Sprint(other.ID))
	if len(progress) != 1 || progress[0]["student_id"] != float64(child.ID) || progress[0]["total"] != float64(1) || progress[0]["reviewed"] != float64(0) || progress[0]["assigned"] != float64(1) {
		t.Fatalf("incorrect own game progress: %+v", progress)
	}
	foreignProgress := items(t, foreignStudent, "/learning/progress")
	if len(foreignProgress) != 1 || foreignProgress[0]["total"] != float64(0) {
		t.Fatal("game progress leaked another student's assignments")
	}
	for _, viewer := range []*browser{admin, teacherBrowser, parentBrowser} {
		expect(t, viewer, "GET", "/learning/progress", nil, 403)
	}
	if len(items(t, student, "/assignments")) != 1 || len(items(t, foreignStudent, "/assignments")) != 0 || len(items(t, parentBrowser, "/assignments")) != 0 || len(items(t, foreignTeacher, "/assignments")) != 0 {
		t.Fatal("assignment scope leaked")
	}
	expect(t, foreignStudent, "POST", assignmentPath+"/submit", map[string]string{"answer": "Foreign answer"}, 409)
	expect(t, parentBrowser, "POST", assignmentPath+"/submit", map[string]string{"answer": "Parent answer"}, 403)
	expect(t, student, "POST", assignmentPath+"/submit", map[string]string{"answer": "Первый аргумент и второй аргумент."}, 204)
	expect(t, student, "POST", assignmentPath+"/submit", map[string]string{"answer": "Duplicate"}, 409)
	expect(t, foreignTeacher, "POST", assignmentPath+"/review", map[string]any{"feedback": "No access", "accepted": true}, 403)
	expect(t, teacherBrowser, "POST", assignmentPath+"/review", map[string]any{"feedback": "Уточните второй аргумент.", "accepted": false}, 204)
	expect(t, student, "POST", assignmentPath+"/submit", map[string]string{"answer": "Ответ после доработки."}, 204)
	reportBody := map[string]any{"lesson_id": lesson.ID, "student_id": child.ID, "summary": "Прогресс ученика и рекомендации семье."}
	reportID := resultID(t, expect(t, teacherBrowser, "POST", "/reports", reportBody, 201))
	reportPath := fmt.Sprintf("/reports/%d/publish", reportID)
	expect(t, teacherBrowser, "POST", reportPath, nil, 409)
	if len(items(t, parentBrowser, "/reports")) != 0 || len(items(t, student, "/reports")) != 0 {
		t.Fatal("draft report leaked to family")
	}
	expect(t, teacherBrowser, "POST", assignmentPath+"/review", map[string]any{"feedback": "Работа принята.", "accepted": true}, 204)
	progress = items(t, student, "/learning/progress")
	if len(progress) != 1 || progress[0]["reviewed"] != float64(1) || progress[0]["assigned"] != float64(0) || progress[0]["submitted"] != float64(0) {
		t.Fatalf("accepted work did not fill game progress: %+v", progress)
	}
	expect(t, teacherBrowser, "POST", reportPath, nil, 409) // lesson must be completed, not just marked
	expect(t, teacherBrowser, "POST", lessonPath(lesson.ID)+"/complete", nil, 200)
	expect(t, foreignTeacher, "POST", reportPath, nil, 403)
	expect(t, teacherBrowser, "POST", reportPath, nil, 204)
	expect(t, teacherBrowser, "POST", reportPath, nil, 409)
	expect(t, teacherBrowser, "POST", "/assignments", body, 409)
	expect(t, teacherBrowser, "POST", "/reports", reportBody, 404) // published reports cannot be silently overwritten
	published := items(t, parentBrowser, "/reports")
	if len(published) != 1 || published[0]["summary"] != reportBody["summary"] {
		t.Fatal("published result missing")
	}
	if len(items(t, foreignParent, "/reports")) != 0 || len(items(t, foreignStudent, "/reports")) != 0 {
		t.Fatal("foreign family read report")
	}
	for _, family := range []*browser{student, parentBrowser} {
		own := items(t, family, "/learning/students")
		if len(own) != 1 || int64(own[0]["id"].(float64)) != child.ID {
			t.Fatal("foreign child visible")
		}
		lessons := items(t, family, "/learning/lessons")
		if len(lessons) != 1 || lessons[0]["attendance"] != "present" {
			t.Fatal("family lesson scope incorrect")
		}
		expect(t, family, "GET", "/users", nil, 403)
		expect(t, family, "GET", fmt.Sprintf("/students/%d", other.ID), nil, 403)
	}
	// Invitation tokens cannot be reused after activation.
	expect(t, f.browser(t), "POST", "/auth/accept-link", map[string]string{"token": linkToken(studentLink), "password": "Another-Pilot-Password"}, 400)
	// Removing a family link immediately removes access, including historical reports.
	if _, err := f.pool.Exec(context.Background(), `DELETE FROM student_parents WHERE student_id=$1 AND parent_id=$2`, child.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if len(items(t, parentBrowser, "/reports")) != 0 || len(items(t, parentBrowser, "/learning/students")) != 0 {
		t.Fatal("removed family link still grants access")
	}
}

func TestPilotRecoveryExpiryRevocationAndConcurrentUse(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	child := studentCreate(t, admin, map[string]any{"name": "Recovery student"})
	inviteLink, student := invite(t, f, admin, "student", child.ID, "recovery-pilot@example.test")
	guest := f.browser(t)
	exists := expect(t, guest, "POST", "/auth/recovery-request", map[string]string{"email": "recovery-pilot@example.test"}, 202)
	missing := expect(t, guest, "POST", "/auth/recovery-request", map[string]string{"email": "missing@example.test"}, 202)
	// Request IDs differ; response messages must not enumerate registered accounts.
	var existingMessage, missingMessage map[string]string
	decode(t, exists, &existingMessage)
	decode(t, missing, &missingMessage)
	if existingMessage["message"] != missingMessage["message"] || len(items(t, admin, "/access-requests")) != 1 {
		t.Fatal("recovery request behavior incorrect")
	}
	expect(t, student, "POST", "/access-links", map[string]int64{"user_id": inviteLink.UserID}, 403)
	issue := func() pilotLink {
		var link pilotLink
		decode(t, expect(t, admin, "POST", "/access-links", map[string]int64{"user_id": inviteLink.UserID}, 201), &link)
		return link
	}
	first := issue()
	second := issue()
	expect(t, guest, "POST", "/auth/accept-link", map[string]string{"token": linkToken(first), "password": "New-Pilot-Password-2048"}, 400)
	if _, err := f.pool.Exec(context.Background(), `UPDATE access_links SET expires_at=now()-interval '1 minute' WHERE user_id=$1 AND used_at IS NULL`, inviteLink.UserID); err != nil {
		t.Fatal(err)
	}
	expect(t, guest, "POST", "/auth/accept-link", map[string]string{"token": linkToken(second), "password": "New-Pilot-Password-2048"}, 400)
	third := issue()
	browsers := []*browser{f.browser(t), f.browser(t)}
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, b := range browsers {
		wg.Add(1)
		go func(b *browser) {
			defer wg.Done()
			code, _ := b.do(t, "POST", "/auth/accept-link", map[string]string{"token": linkToken(third), "password": "New-Pilot-Password-2048"})
			codes <- code
		}(b)
	}
	wg.Wait()
	close(codes)
	success, rejected := 0, 0
	for code := range codes {
		if code == 204 {
			success++
		}
		if code == 400 {
			rejected++
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("single use broken: success=%d rejected=%d", success, rejected)
	}
	expect(t, student, "GET", "/auth/me", nil, 401)
	guest.login(t, "recovery-pilot@example.test", "New-Pilot-Password-2048")
	expect(t, guest, "POST", "/auth/logout", nil, 204)
	expect(t, guest, "GET", "/auth/me", nil, 401)
	if len(items(t, admin, "/access-requests")) != 0 {
		t.Fatal("resolved request remains pending")
	}
}

func TestPilotFeatureGatesAndInvitationRestrictions(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	for _, feature := range []string{"projects", "achievements", "skills", "events", "parent-requests", "metrics", "notifications", "materials", "settings", "tasks", "dashboard", "audit-logs"} {
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			expect(t, admin, method, "/"+feature+"/123", map[string]any{}, 404)
		}
	}
	child := studentCreate(t, admin, map[string]any{"name": "Invite once"})
	_, student := invite(t, f, admin, "student", child.ID, "invite-once@example.test")
	body := map[string]any{"profile_id": child.ID, "role": "student", "email": "duplicate@example.test", "name": "Duplicate"}
	expect(t, admin, "POST", "/invitations", body, 409)
	expect(t, student, "POST", "/invitations", body, 403)
	body["role"] = "admin"
	expect(t, admin, "POST", "/invitations", body, 400)
	handler, _, err := app.Router(f.pool, config.Config{Origin: origin, Student360: false})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	res, err := server.Client().Get(server.URL + fmt.Sprintf("/api/v1/students/%d/360", child.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("disabled 360 returned %d", res.StatusCode)
	}
}
