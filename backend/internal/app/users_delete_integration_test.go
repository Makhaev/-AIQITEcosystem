package app_test

import (
	"context"
	"fmt"
	"testing"

	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/httpx"
)

func TestUserDeletionPreservesHistoryAndRevokesAccess(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	admin := f.adminBrowser(t)
	u := create(t, admin, "remove@example.test", "teacher")
	first, second := f.browser(t), f.browser(t)
	first.login(t, u.Email, userPassword)
	second.login(t, u.Email, userPassword)
	var teacherID int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO teachers(name,name_search,user_id) VALUES('History','history',$1) RETURNING id`, u.ID).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO access_links(token_hash,user_id,purpose,expires_at) VALUES(decode(repeat('ab',32),'hex'),$1,'recovery',now()+interval '1 hour')`, u.ID); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/users/%d", u.ID)
	expect(t, admin, "DELETE", path, nil, 204)
	expect(t, first, "GET", "/auth/me", nil, 401)
	expect(t, second, "GET", "/auth/me", nil, 401)
	anonymous := f.browser(t)
	expect(t, anonymous, "POST", "/auth/login", map[string]string{"email": u.Email, "password": userPassword}, 401)
	expect(t, admin, "PATCH", path, map[string]any{"blocked": false}, 404)
	expect(t, admin, "DELETE", path, nil, 404)
	for _, query := range []string{"/users", "/users?role=teacher"} {
		var page users.Page
		decode(t, expect(t, admin, "GET", query, nil, 200), &page)
		for _, item := range page.Items {
			if item.ID == u.ID {
				t.Fatal("deleted account is visible")
			}
		}
		want := 1
		if query != "/users" {
			want = 0
		}
		if page.Total != want {
			t.Fatalf("list total: got %d want %d", page.Total, want)
		}
	}
	var safe bool
	if err := f.pool.QueryRow(ctx, `SELECT u.blocked AND u.deleted_at IS NOT NULL
	 AND NOT EXISTS(SELECT 1 FROM sessions WHERE user_id=u.id)
	 AND NOT EXISTS(SELECT 1 FROM access_links WHERE user_id=u.id AND used_at IS NULL)
	 AND EXISTS(SELECT 1 FROM teachers WHERE id=$2 AND user_id=u.id)
	 AND (SELECT count(*) FROM audit_logs WHERE action='user.deleted' AND object_id=u.id)=1
	 AND EXISTS(SELECT 1 FROM audit_logs WHERE actor_id=u.id AND action='auth.login')
	 FROM users u WHERE id=$1`, u.ID, teacherID).Scan(&safe); err != nil || !safe {
		t.Fatalf("deletion invariants: %v %v", safe, err)
	}
	// A deleted identity's email stays reserved; a new identity cannot inherit it.
	expect(t, admin, "POST", "/users", map[string]any{"email": u.Email, "name": "Other", "password": userPassword, "roles": []string{"teacher"}}, 409)
}

func TestUserDeletionPermissionsAndSelfProtection(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	self := fmt.Sprintf("/users/%d", f.admin.ID)
	expect(t, admin, "DELETE", self, nil, 409)
	expect(t, admin, "GET", "/auth/me", nil, 200)
	for _, id := range []string{"0", "-1", "invalid"} {
		expect(t, admin, "DELETE", "/users/"+id, nil, 400)
	}
	expect(t, admin, "DELETE", "/users/999999999", nil, 404)
	u := create(t, admin, "unauthorized-remove@example.test", "teacher")
	teacher := f.browser(t)
	teacher.login(t, u.Email, userPassword)
	expect(t, teacher, "DELETE", self, nil, 403)
	anonymous := f.browser(t)
	expect(t, anonymous, "DELETE", self, nil, 401)
	savedCSRF := admin.csrf
	admin.csrf = "invalid"
	expect(t, admin, "DELETE", fmt.Sprintf("/users/%d", u.ID), nil, 403)
	admin.csrf = savedCSRF
	// Repository authorization is mandatory even if the HTTP layer is bypassed.
	s := &users.Service{Repo: &users.Repository{Pool: f.pool}}
	if err := s.Delete(context.Background(), u.ID); err == nil {
		t.Fatal("repository accepted an anonymous deletion")
	}
	stale := httpx.WithActor(context.Background(), httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}})
	if err := s.Delete(stale, u.ID); err == nil {
		t.Fatal("repository accepted an actor without a valid session")
	}
	// Another administrator may be removed while the current one remains active.
	other := create(t, admin, "second-admin@example.test", "admin")
	expect(t, admin, "DELETE", fmt.Sprintf("/users/%d", other.ID), nil, 204)
	expect(t, admin, "GET", "/auth/me", nil, 200)
}
