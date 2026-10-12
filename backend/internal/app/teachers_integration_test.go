package app_test

import (
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/teachers"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"
)

func provision(t *testing.T, f *fixture, admin *browser, email, role string) (users.User, *browser) {
	t.Helper()
	u := create(t, admin, email, role)
	b := f.browser(t)
	b.login(t, email, userPassword)
	expect(t, b, "POST", "/auth/change-password", map[string]string{"old_password": userPassword, "password": "TestOnly-New-Account-9372"}, 204)
	expect(t, b, "GET", "/auth/csrf", nil, 200)
	b.login(t, email, "TestOnly-New-Account-9372")
	return u, b
}
func teacherCreate(t *testing.T, admin *browser, body map[string]any) teachers.Teacher {
	t.Helper()
	var value teachers.Teacher
	decode(t, expect(t, admin, "POST", "/teachers", body, 201), &value)
	return value
}
func teacherPage(t *testing.T, b *browser, path string) teachers.Page {
	t.Helper()
	var value teachers.Page
	decode(t, expect(t, b, "GET", path, nil, 200), &value)
	return value
}
func TestTeachersLifecycleSearchAndLinking(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	u, _ := provision(t, f, admin, "teacher-link@example.test", "teacher")
	first := teacherCreate(t, admin, map[string]any{"name": "  Анна Петрова  ", "phone": "8 (900) 123-45-67", "email": " TEACHER@example.test ", "specialization": " Мышление ", "user_id": u.ID})
	if first.Name != "Анна Петрова" || first.Phone != "+79001234567" || first.Email != "teacher@example.test" || first.Status != "active" || first.UserID == nil || *first.UserID != u.ID {
		t.Fatalf("normalization/link: %+v", first)
	}
	expect(t, admin, "POST", "/teachers", map[string]any{"name": "Duplicate link", "user_id": u.ID}, 409)
	expect(t, admin, "POST", "/teachers", map[string]any{"name": "Wrong role", "user_id": f.admin.ID}, 400)
	expect(t, admin, "POST", "/teachers", map[string]any{"name": "Missing user", "user_id": 99999}, 400)
	second := teacherCreate(t, admin, map[string]any{"name": "Анна Петрова"})
	teacherCreate(t, admin, map[string]any{"name": "Имя %_ буквально"})
	page := teacherPage(t, admin, "/teachers?search="+url.QueryEscape("АННА ПЕТ"))
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].ID != first.ID || page.Items[1].ID != second.ID {
		t.Fatalf("search/stable order: %+v", page)
	}
	if page := teacherPage(t, admin, "/teachers?search="+url.QueryEscape("%_")); page.Total != 1 {
		t.Fatalf("search must treat wildcards literally: %+v", page)
	}
	page = teacherPage(t, admin, "/teachers?search="+url.QueryEscape("анна")+"&page=2&page_size=1")
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != second.ID {
		t.Fatalf("pagination: %+v", page)
	}
	path := fmt.Sprintf("/teachers/%d", first.ID)
	expect(t, admin, "PATCH", path, map[string]any{"phone": nil, "email": nil, "specialization": nil, "user_id": nil}, 200)
	var cleared teachers.Teacher
	decode(t, expect(t, admin, "GET", path, nil, 200), &cleared)
	if cleared.UserID != nil || cleared.Phone != "" || cleared.Email != "" || cleared.Specialization != "" {
		t.Fatalf("explicit clear failed: %+v", cleared)
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/teachers/%d", second.ID), map[string]any{"user_id": u.ID}, 200)
	expect(t, admin, "PATCH", path, map[string]any{"status": "archived"}, 200)
	if page := teacherPage(t, admin, "/teachers?status=archived"); page.Total != 1 || page.Items[0].ID != first.ID {
		t.Fatalf("archive missing: %+v", page)
	}
	if page := teacherPage(t, admin, "/teachers?status=active"); page.Total != 2 {
		t.Fatalf("archived profile remains assignable list: %+v", page)
	}
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = teachers.RequireActive(ctx, tx, first.ID); err == nil {
		t.Fatal("archived teacher accepted for new assignment")
	} else {
		var appErr *httpx.Error
		if !errors.As(err, &appErr) || appErr.Code != "teacher_archived" {
			t.Fatal(err)
		}
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "GET", path, nil, 200)
	expect(t, admin, "DELETE", path, nil, 404)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_type='teacher' AND object_id=$1 AND action='teacher.archived'`, first.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("atomic archive audit: %d %v", count, err)
	}
	for _, body := range []map[string]any{{}, {"name": " "}, {"name": nil}, {"status": nil}, {"status": "deleted"}, {"user_id": 0}, {"email": "invalid"}, {"unexpected": "value"}} {
		expect(t, admin, "PATCH", path, body, 400)
	}
	expect(t, admin, "GET", "/teachers/not-a-number", nil, 400)
	expect(t, admin, "GET", "/teachers/99999", nil, 404)
	for _, query := range []string{"page=0", "page_size=101", "status=deleted", "search=" + url.QueryEscape(string(make([]byte, 201)))} {
		expect(t, admin, "GET", "/teachers?"+query, nil, 400)
	}
	var filtered users.Page
	decode(t, expect(t, admin, "GET", "/users?role=teacher", nil, 200), &filtered)
	if filtered.Total != 1 || filtered.Items[0].ID != u.ID {
		t.Fatalf("user role lookup: %+v", filtered)
	}
	expect(t, admin, "GET", "/users?role=owner", nil, 400)
}

func TestTeachersHTTPRoleAndObjectPermissions(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	u, teacher := provision(t, f, admin, "teacher-scope@example.test", "teacher")
	_, leader := provision(t, f, admin, "leader-scope@example.test", "leader")
	_, curator := provision(t, f, admin, "curator-scope@example.test", "curator")
	own := teacherCreate(t, admin, map[string]any{"name": "Own profile", "user_id": u.ID})
	other := teacherCreate(t, admin, map[string]any{"name": "Other profile"})
	anonymous := f.browser(t)
	for _, b := range []*browser{anonymous, curator} {
		status := 403
		if b == anonymous {
			status = 401
		}
		expect(t, b, "GET", "/teachers", nil, status)
		expect(t, b, "GET", fmt.Sprintf("/teachers/%d", own.ID), nil, status)
	}
	if page := teacherPage(t, leader, "/teachers"); page.Total != 2 {
		t.Fatal("leader cannot read all teachers")
	}
	if page := teacherPage(t, teacher, "/teachers"); page.Total != 1 || page.Items[0].ID != own.ID {
		t.Fatalf("teacher directory leaks other profiles: %+v", page)
	}
	expect(t, teacher, "GET", fmt.Sprintf("/teachers/%d", own.ID), nil, 200)
	expect(t, teacher, "GET", fmt.Sprintf("/teachers/%d", other.ID), nil, 404)
	if page := teacherPage(t, teacher, "/teachers?search=Other&user_id="+fmt.Sprint(f.admin.ID)); page.Total != 0 {
		t.Fatal("query parameters bypassed row scope")
	}
	for _, b := range []*browser{leader, teacher, curator} {
		expect(t, b, "POST", "/teachers", map[string]any{"name": "Attack"}, 403)
		expect(t, b, "PATCH", fmt.Sprintf("/teachers/%d", own.ID), map[string]any{"status": "archived"}, 403)
		expect(t, b, "GET", "/users", nil, 403)
	}
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) VALUES($1,'leader')`, u.ID); err != nil {
		t.Fatal(err)
	}
	if page := teacherPage(t, teacher, "/teachers"); page.Total != 2 {
		t.Fatal("server did not refresh combined roles from database")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", u.ID), map[string]any{"blocked": true}, 200)
	expect(t, teacher, "GET", "/teachers", nil, 401)
	// CSRF is required on the new module, not just on authentication.
	admin.csrf = ""
	expect(t, admin, "POST", "/teachers", map[string]any{"name": "Missing CSRF"}, 403)
}

func TestTeachersReauthorizeInsideTransactions(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	profile := teacherCreate(t, admin, map[string]any{"name": "Protected"})
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))}
	ctx := httpx.WithActor(context.Background(), actor)
	service := teachers.Service{Repo: &teachers.Repository{Pool: f.pool}}
	expect(t, admin, "POST", "/auth/logout", nil, 204)
	_, createErr := service.Create(ctx, teachers.CreateInput{Name: "Unauthorized"})
	_, updateErr := service.Update(ctx, profile.ID, teachers.UpdateInput{Name: teachers.Optional[string]{Set: true, Value: ptr("Changed")}})
	_, readErr := service.Get(ctx, profile.ID)
	_, listErr := service.List(ctx, teachers.Filter{Page: 1, Size: 20})
	for _, err := range []error{createErr, updateErr, readErr, listErr} {
		var appErr *httpx.Error
		if !errors.As(err, &appErr) || appErr.Status != 401 {
			t.Fatalf("revoked session reused: %v", err)
		}
	}
	var name string
	if err := f.pool.QueryRow(context.Background(), `SELECT name FROM teachers WHERE id=$1`, profile.ID).Scan(&name); err != nil || name != "Protected" {
		t.Fatalf("rejected update changed profile: %q %v", name, err)
	}
}
func ptr[T any](value T) *T { return &value }

func TestArchivalSerializesWithNewTeacherAssignment(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	profile := teacherCreate(t, admin, map[string]any{"name": "Teacher history"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Test-only consumer represents a historical lesson reference. No lesson API is mocked.
	if _, err := f.pool.Exec(ctx, `CREATE TABLE assignment_history_test(teacher_id bigint NOT NULL REFERENCES teachers(id))`); err != nil {
		t.Fatal(err)
	}
	assignment, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer assignment.Rollback(context.Background())
	if _, err = teachers.RequireActive(ctx, assignment, profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = assignment.Exec(ctx, `INSERT INTO assignment_history_test(teacher_id) VALUES($1)`, profile.ID); err != nil {
		t.Fatal(err)
	}
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))}
	result := make(chan error, 1)
	go func() {
		service := teachers.Service{Repo: &teachers.Repository{Pool: f.pool}}
		_, err := service.Update(httpx.WithActor(ctx, actor), profile.ID, teachers.UpdateInput{Status: teachers.Optional[string]{Set: true, Value: ptr("archived")}})
		result <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM teachers%FOR UPDATE%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("archive did not wait for assignment: %v", err)
		case <-ctx.Done():
			t.Fatal("archive did not reach row lock")
		case <-ticker.C:
		}
	}
	if err := assignment.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("archive failed to finish")
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM assignment_history_test h JOIN teachers t ON t.id=h.teacher_id WHERE t.status='archived'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("archive destroyed teacher history: %d %v", count, err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := teachers.RequireActive(ctx, tx, profile.ID); err == nil {
		t.Fatal("new assignment accepted after archive")
	}
	if access.ScopeFor(actor, access.Teachers, access.Manage) != access.All {
		t.Fatal("admin permissions missing")
	}
}
