package app_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/app"
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/config"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const origin = "http://localhost:5173"
const adminPassword = "TestOnly-Admin-4829"
const userPassword = "TestOnly-Teacher-5829"

type fixture struct {
	methodologist   *browser
	educationLeader *browser
	pool            *pgxpool.Pool
	server          *httptest.Server
	admin           users.User
}

func setup(t *testing.T) *fixture {
	t.Helper()
	connection := os.Getenv("TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("TEST_DATABASE_URL is not set; PostgreSQL integration tests require a test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	base, err := pgxpool.New(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 8)
	if _, err = rand.Read(data); err != nil {
		t.Fatal(err)
	}
	schema := "aiq_test_" + hex.EncodeToString(data)
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		base.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(connection)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := base.Exec(cleanup, `DROP SCHEMA `+identifier+` CASCADE`); err != nil {
			t.Errorf("clean test schema: %v", err)
		}
		base.Close()
	})
	if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
		t.Fatal(err)
	}
	// Migrations must also be safe to repeat.
	if err = database.Migrate(ctx, pool, core.Migrations); err != nil {
		t.Fatal(err)
	}
	service := users.Service{Repo: &users.Repository{Pool: pool}}
	admin, err := service.Bootstrap(ctx, users.CreateInput{Email: "ADMIN@example.test", Name: "Test Admin", Password: adminPassword})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Bootstrap(ctx, users.CreateInput{Email: "other@example.test", Name: "Other", Password: adminPassword}); err == nil {
		t.Fatal("bootstrap must not run twice")
	}
	handler, _, err := app.Router(pool, config.Config{Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &fixture{pool: pool, server: server, admin: admin}
}

type browser struct {
	fixture *fixture
	client  *http.Client
	csrf    string
	ip      string
}

func (f *fixture) browser(t *testing.T) *browser {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := f.server.Client()
	clone := *client
	clone.Jar = jar
	b := &browser{fixture: f, client: &clone}
	status, body := b.do(t, "GET", "/auth/csrf", nil)
	if status != 200 {
		t.Fatalf("csrf: %d %s", status, body)
	}
	var result struct {
		CSRF string `json:"csrf_token"`
	}
	decode(t, body, &result)
	b.csrf = result.CSRF
	return b
}
func decode(t *testing.T, body []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode: %v %s", err, body)
	}
}
func (b *browser) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, b.fixture.server.URL+"/api/v1"+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth.Unsafe(method) {
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", b.csrf)
	}
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	result, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var token struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(result, &token)
	if token.CSRF != "" {
		b.csrf = token.CSRF
	}
	return res.StatusCode, result
}
func expect(t *testing.T, b *browser, method, path string, body any, want int) []byte {
	t.Helper()
	status, result := b.do(t, method, path, body)
	if status != want {
		t.Fatalf("%s %s: want %d, got %d: %s", method, path, want, status, result)
	}
	return result
}
func (b *browser) login(t *testing.T, email, password string) {
	t.Helper()
	expect(t, b, "POST", "/auth/login", map[string]string{"email": email, "password": password}, 200)
}
func (b *browser) sessionToken(t *testing.T) string {
	t.Helper()
	u, err := url.Parse(b.fixture.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.client.Jar.Cookies(u) {
		if c.Name == auth.SessionCookie {
			return c.Value
		}
	}
	t.Fatal("session cookie missing")
	return ""
}
func (f *fixture) adminBrowser(t *testing.T) *browser {
	b := f.browser(t)
	b.login(t, " AdMiN@EXAMPLE.TEST ", adminPassword)
	return b
}
func create(t *testing.T, b *browser, email string, role string) users.User {
	t.Helper()
	body := expect(t, b, "POST", "/users", map[string]any{"email": email, "name": "Test Teacher", "password": userPassword, "roles": []string{role}}, 201)
	var u users.User
	decode(t, body, &u)
	return u
}

func TestSessionLifecycleAndPermissions(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	admin := f.adminBrowser(t)
	teacher := create(t, admin, "TEACHER@example.test", "teacher")
	expect(t, admin, "POST", "/users", map[string]any{"email": "teacher@EXAMPLE.test", "name": "Duplicate", "password": userPassword, "roles": []string{"teacher"}}, 409)
	first, second := f.browser(t), f.browser(t)
	first.login(t, teacher.Email, userPassword)
	second.login(t, teacher.Email, userPassword)
	expect(t, first, "GET", "/auth/me", nil, 200)
	expect(t, first, "GET", "/users", nil, 403)
	expect(t, first, "POST", "/users", map[string]any{"email": "attack@example.test", "name": "Attack", "password": userPassword, "roles": []string{"admin"}}, 403)
	body := expect(t, first, "POST", "/auth/change-password", map[string]string{"old_password": "wrong", "password": "New-Teacher-Password-9372"}, 400)
	if !bytes.Contains(body, []byte("old_password")) {
		t.Fatal("wrong password must give field error")
	}
	expect(t, first, "GET", "/auth/me", nil, 200)
	expect(t, first, "POST", "/auth/change-password", map[string]string{"old_password": userPassword, "password": "New-Teacher-Password-9372"}, 204)
	expect(t, first, "GET", "/auth/me", nil, 401)
	expect(t, second, "GET", "/auth/me", nil, 401)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, teacher.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("all sessions revoked: %d %v", count, err)
	}
	first = f.browser(t)
	first.login(t, teacher.Email, "New-Teacher-Password-9372")
	second = f.browser(t)
	second.login(t, teacher.Email, "New-Teacher-Password-9372")
	expect(t, first, "GET", "/users", nil, 403)
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacher.ID), map[string]any{"blocked": true}, 200)
	expect(t, first, "GET", "/auth/me", nil, 401)
	expect(t, second, "GET", "/auth/me", nil, 401)
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1`, teacher.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("blocking must revoke every session: %d %v", count, err)
	}
	blocked := f.browser(t)
	expect(t, blocked, "POST", "/auth/login", map[string]string{"email": teacher.Email, "password": "New-Teacher-Password-9372"}, 401)
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacher.ID), map[string]any{"blocked": false}, 200)
	first = f.browser(t)
	first.login(t, teacher.Email, "New-Teacher-Password-9372")
	second = f.browser(t)
	second.login(t, teacher.Email, "New-Teacher-Password-9372")
	expect(t, first, "POST", "/auth/logout", nil, 204)
	expect(t, first, "GET", "/auth/me", nil, 401)
	expect(t, second, "GET", "/auth/me", nil, 200)
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacher.ID), map[string]any{"password": "Reset-Teacher-Password-7243"}, 200)
	expect(t, second, "GET", "/auth/me", nil, 401)
	second = f.browser(t)
	second.login(t, teacher.Email, "Reset-Teacher-Password-7243")
	var current users.User
	decode(t, expect(t, second, "GET", "/auth/me", nil, 200), &current)
	if !current.MustChangePassword {
		t.Fatal("reset must force password change")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", f.admin.ID), map[string]any{"blocked": true}, 409)
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", f.admin.ID), map[string]any{"roles": []string{"teacher"}}, 409)
	expect(t, admin, "GET", "/auth/me", nil, 200)
	expect(t, admin, "POST", "/auth/register", map[string]string{}, 404)
	var hash string
	if err := f.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, teacher.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == "Reset-Teacher-Password-7243" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("Reset-Teacher-Password-7243")) != nil {
		t.Fatal("password is not bcrypt-hashed")
	}
	var audits int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='auth.password_changed' AND actor_id=$1`, teacher.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("atomic audit missing: %d %v", audits, err)
	}
	if body := expect(t, admin, "GET", "/users", nil, 200); bytes.Contains(body, []byte("password_hash")) || bytes.Contains(body, []byte(hash)) {
		t.Fatal("hash exposed in API")
	}
}

func TestCSRFSessionHashAndExpiry(t *testing.T) {
	f := setup(t)
	b := f.adminBrowser(t)
	u, _ := url.Parse(f.server.URL)
	raw := b.sessionToken(t)
	if !auth.ValidToken(raw) {
		t.Fatal("cookie is not a random 256-bit token")
	}
	var stored []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT token_hash FROM sessions WHERE user_id=$1`, f.admin.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(stored, []byte(raw)) || !bytes.Equal(stored, auth.Hash(raw)) {
		t.Fatal("session must store only SHA-256 hash")
	}
	original := b.csrf
	b.csrf = ""
	expect(t, b, "POST", "/auth/logout", nil, 403)
	b.csrf = original
	req, _ := http.NewRequest("POST", f.server.URL+"/api/v1/auth/logout", nil)
	req.Header.Set("Origin", "https://attacker.test")
	req.Header.Set("X-CSRF-Token", original)
	res, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatal("foreign origin allowed")
	}
	// Even a matching cookie and header cannot replace the session-bound CSRF token.
	forged, _ := auth.Token()
	b.client.Jar.SetCookies(u, []*http.Cookie{{Name: auth.CSRFCookie, Value: forged, Path: "/"}})
	b.csrf = forged
	expect(t, b, "POST", "/auth/logout", nil, 403)
	expect(t, b, "GET", "/auth/csrf", nil, 200)
	expect(t, b, "GET", "/auth/me", nil, 200)
	if _, err = f.pool.Exec(context.Background(), `UPDATE sessions SET expires_at=now()-interval '1 second' WHERE user_id=$1`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, b, "GET", "/auth/me", nil, 401)
}

func TestLoginLockAndUnknownEmail(t *testing.T) {
	f := setup(t)
	b := f.browser(t)
	var known, unknown struct{ Code, Message string }
	for i := 0; i < 5; i++ {
		decode(t, expect(t, b, "POST", "/auth/login", map[string]string{"email": f.admin.Email, "password": "Wrong-password-7392"}, 401), &known)
	}
	decode(t, expect(t, b, "POST", "/auth/login", map[string]string{"email": "unknown@example.test", "password": "Wrong-password-7392"}, 401), &unknown)
	if known != unknown {
		t.Fatal("login leaks existence of account")
	}
	expect(t, b, "POST", "/auth/login", map[string]string{"email": f.admin.Email, "password": adminPassword}, 401)
	var until time.Time
	if err := f.pool.QueryRow(context.Background(), `SELECT locked_until FROM users WHERE id=$1`, f.admin.ID).Scan(&until); err != nil || until.Before(time.Now()) {
		t.Fatal("temporary account lock missing")
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE users SET locked_until=now()-interval '1 second',failure_window_start=now()-interval '16 minutes' WHERE id=$1`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	b.login(t, f.admin.Email, adminPassword)
	if _, err := f.pool.Exec(context.Background(), `UPDATE login_rate_limits SET attempts=30`); err != nil {
		t.Fatal(err)
	}
	expect(t, b, "POST", "/auth/login", map[string]string{"email": f.admin.Email, "password": adminPassword}, 429)
}

func TestConcurrentLoginAndPasswordChange(t *testing.T) {
	f := setup(t)
	b := f.adminBrowser(t)
	raw := b.sessionToken(t)
	service, err := auth.NewService(&auth.Repository{Pool: f.pool})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := service.Login(context.Background(), f.admin.Email, adminPassword, "192.0.2.2")
		results <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		results <- service.ChangePassword(context.Background(), f.admin.ID, raw, adminPassword, "New-Admin-Password-8374")
	}()
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			var appErr *httpx.Error
			if !errors.As(err, &appErr) || appErr.Code != "invalid_credentials" {
				t.Fatal(err)
			}
		}
	}
	var count int
	if err = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id=$1`, f.admin.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old-password session survived change: %d %v", count, err)
	}
}

func TestLoginReplacesBrowserSession(t *testing.T) {
	f := setup(t)
	b := f.adminBrowser(t)
	previous := b.sessionToken(t)
	previousCSRF := b.csrf
	repo := auth.Repository{Pool: f.pool}
	// Failed reauthentication must not revoke the working browser session.
	expect(t, b, "POST", "/auth/login", map[string]string{"email": f.admin.Email, "password": "Wrong-password-7392"}, 401)
	if _, err := repo.Current(context.Background(), auth.Hash(previous)); err != nil {
		t.Fatal(err)
	}
	b.login(t, f.admin.Email, adminPassword)
	if b.sessionToken(t) == previous || b.csrf == previousCSRF {
		t.Fatal("login must rotate both session and CSRF tokens")
	}
	_, err := repo.Current(context.Background(), auth.Hash(previous))
	var appErr *httpx.Error
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("replaced session remains valid: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id=$1`, f.admin.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("browser login must replace its previous session: %d %v", count, err)
	}
	current := b.sessionToken(t)
	expect(t, b, "POST", "/auth/logout", nil, 204)
	_, err = repo.Current(context.Background(), auth.Hash(current))
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("logged-out token can still be replayed: %v", err)
	}
}

func TestAdminWritesRecheckSessionAfterRevocation(t *testing.T) {
	for _, revoke := range []string{"logout", "password_change", "expired", "foreign_session", "missing_session"} {
		t.Run(revoke, func(t *testing.T) {
			f := setup(t)
			b := f.adminBrowser(t)
			target := create(t, b, "target@example.test", "teacher")
			// Model a request that passed middleware before its session was revoked.
			actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(b.sessionToken(t))}
			switch revoke {
			case "logout":
				expect(t, b, "POST", "/auth/logout", nil, 204)
			case "password_change":
				expect(t, b, "POST", "/auth/change-password", map[string]string{"old_password": adminPassword, "password": "New-Admin-Password-8374"}, 204)
			case "expired":
				if _, err := f.pool.Exec(context.Background(), `UPDATE sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, actor.SessionHash); err != nil {
					t.Fatal(err)
				}
			case "foreign_session":
				other := f.browser(t)
				other.login(t, target.Email, userPassword)
				actor.SessionHash = auth.Hash(other.sessionToken(t))
			case "missing_session":
				actor.SessionHash = nil
			}
			ctx := httpx.WithActor(context.Background(), actor)
			service := users.Service{Repo: &users.Repository{Pool: f.pool}}
			_, createErr := service.Create(ctx, users.CreateInput{Email: "unauthorized@example.test", Name: "Unauthorized", Password: userPassword, Roles: []string{"admin"}})
			name := "Unauthorized rename"
			_, updateErr := service.Update(ctx, target.ID, users.UpdateInput{Name: &name})
			for _, err := range []error{createErr, updateErr} {
				var appErr *httpx.Error
				if !errors.As(err, &appErr) || appErr.Status != 401 {
					t.Fatalf("stale authorization allowed an admin write: %v", err)
				}
			}
			var count int
			var storedName string
			if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&count); err != nil || count != 2 {
				t.Fatalf("rejected create changed users: %d %v", count, err)
			}
			if err := f.pool.QueryRow(context.Background(), `SELECT name FROM users WHERE id=$1`, target.ID).Scan(&storedName); err != nil || storedName != target.Name {
				t.Fatalf("rejected update changed user: %q %v", storedName, err)
			}
		})
	}
}

func TestAdminWriteRechecksExpiryAfterWaiting(t *testing.T) {
	f := setup(t)
	b := f.adminBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(b.sessionToken(t))}
	gate, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Rollback(context.Background())
	if _, err := gate.Exec(ctx, `SELECT pg_advisory_xact_lock(8021002)`); err != nil {
		t.Fatal(err)
	}
	service := users.Service{Repo: &users.Repository{Pool: f.pool}}
	result := make(chan error, 1)
	go func() {
		_, err := service.Create(httpx.WithActor(ctx, actor), users.CreateInput{Email: "queued@example.test", Name: "Queued", Password: userPassword, Roles: []string{"teacher"}})
		result <- err
	}()
	// Wait for the mutation's transaction to start and block behind our gate.
	// Then expire its session, so transaction-start now() would incorrectly allow it.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=8021002 AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("write returned before transaction gate opened: %v", err)
		case <-ctx.Done():
			t.Fatal("write did not reach the transaction gate")
		case <-ticker.C:
		}
	}
	if _, err := f.pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp() WHERE token_hash=$1`, actor.SessionHash); err != nil {
		t.Fatal(err)
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		var appErr *httpx.Error
		if !errors.As(err, &appErr) || appErr.Status != 401 {
			t.Fatalf("session expired while waiting but write was authorized: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("write did not finish after opening transaction gate")
	}
}

func TestConcurrentAdminWriteAndLogout(t *testing.T) {
	f := setup(t)
	b := f.adminBrowser(t)
	token := b.sessionToken(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(token)}
	userService := users.Service{Repo: &users.Repository{Pool: f.pool}}
	authRepo := auth.Repository{Pool: f.pool}
	start := make(chan struct{})
	written, loggedOut := make(chan error, 1), make(chan error, 1)
	go func() {
		<-start
		_, err := userService.Create(httpx.WithActor(ctx, actor), users.CreateInput{Email: "concurrent@example.test", Name: "Concurrent", Password: userPassword, Roles: []string{"teacher"}})
		written <- err
	}()
	go func() {
		<-start
		loggedOut <- authRepo.Logout(ctx, actor.SessionHash, actor.ID)
	}()
	close(start)
	select {
	case err := <-loggedOut:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("concurrent logout did not finish")
	}
	select {
	case err := <-written:
		// The write may commit before logout, or be rejected after logout.
		var appErr *httpx.Error
		if err != nil && (!errors.As(err, &appErr) || appErr.Status != 401) {
			t.Fatalf("concurrent write failed unexpectedly: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("concurrent admin write did not finish")
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE token_hash=$1`, actor.SessionHash).Scan(&count); err != nil || count != 0 {
		t.Fatalf("logout did not revoke the session: %d %v", count, err)
	}
}
