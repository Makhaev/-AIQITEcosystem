package app_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/app"
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/platform/config"
	"golang.org/x/crypto/bcrypt"
)

type resetMail struct {
	links   chan string
	failure error
	gate    chan struct{}
}

func (m *resetMail) Ready() error { return nil }
func (m *resetMail) SendPasswordReset(ctx context.Context, email, link string) error {
	if m.gate != nil {
		select {
		case <-m.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.links <- link
	return m.failure
}
func resetFixture(t *testing.T, mail *resetMail) *fixture {
	t.Helper()
	f := setup(t)
	h, _, err := app.RouterWithSenders(f.pool, config.Config{Origin: origin}, nil, mail)
	if err != nil {
		t.Fatal(err)
	}
	f.server.Close()
	f.server = httptest.NewServer(h)
	t.Cleanup(f.server.Close)
	return f
}
func resetToken(t *testing.T, mail *resetMail) string {
	t.Helper()
	select {
	case link := <-mail.links:
		u, err := url.Parse(link)
		if err != nil || u.RawQuery != "" || !strings.HasPrefix(u.Fragment, "/reset-password?") {
			t.Fatal("invalid reset link")
		}
		q, _ := url.ParseQuery(strings.SplitN(u.Fragment, "?", 2)[1])
		token := q.Get("token")
		if !auth.ValidToken(token) {
			t.Fatal("invalid token format")
		}
		return token
	case <-time.After(5 * time.Second):
		t.Fatal("reset email not delivered")
		return ""
	}
}
func requestReset(t *testing.T, b *browser, mail *resetMail) string {
	t.Helper()
	expect(t, b, "POST", "/auth/password-reset/request", map[string]string{"email": " ADMIN@example.test "}, 202)
	return resetToken(t, mail)
}
func confirmReset(t *testing.T, b *browser, token, password string, want int) []byte {
	t.Helper()
	return expect(t, b, "POST", "/auth/password-reset/confirm", map[string]string{"token": token, "password": password}, want)
}
func accountHash(t *testing.T, f *fixture) string {
	t.Helper()
	var hash string
	if err := f.pool.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE id=$1`, f.admin.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return hash
}

func TestPasswordResetFlow(t *testing.T) {
	mail := &resetMail{links: make(chan string, 10), gate: make(chan struct{})}
	f := resetFixture(t, mail)
	b := f.browser(t)
	first := f.adminBrowser(t)
	second := f.adminBrowser(t)
	// SMTP is deliberately blocked; both responses still finish immediately.
	known := expect(t, b, "POST", "/auth/password-reset/request", map[string]string{"email": "ADMIN@example.test"}, 202)
	unknown := expect(t, b, "POST", "/auth/password-reset/request", map[string]string{"email": "unknown@example.test"}, 202)
	if !bytes.Equal(known, unknown) {
		t.Fatal("email existence disclosed")
	}
	close(mail.gate)
	token := resetToken(t, mail)
	var stored []byte
	var lifetime float64
	if err := f.pool.QueryRow(context.Background(), `SELECT token_hash,extract(epoch FROM expires_at-created_at) FROM password_reset_tokens WHERE user_id=$1`, f.admin.ID).Scan(&stored, &lifetime); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, auth.Hash(token)) || lifetime < 1799 || lifetime > 1801 {
		t.Fatal("hash or 30-minute lifetime incorrect")
	}
	before := accountHash(t, f)
	confirmReset(t, b, token, "short", 400)
	confirmReset(t, b, token, strings.Repeat("я", 37), 400)
	confirmReset(t, b, token, adminPassword, 400)
	if accountHash(t, f) != before {
		t.Fatal("invalid password changed account")
	}
	password := "New-Reset-Password-92741"
	confirmReset(t, b, token, password, 204)
	expect(t, first, "GET", "/auth/me", nil, 401)
	expect(t, second, "GET", "/auth/me", nil, 401)
	b = f.browser(t)
	confirmReset(t, b, token, "Replay-Password-92841", 400)
	expect(t, b, "POST", "/auth/login", map[string]string{"email": "admin@example.test", "password": adminPassword}, 401)
	b.login(t, "admin@example.test", password)
	var sessions, links, events int
	var forced bool
	if err := f.pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM sessions WHERE user_id=$1),(SELECT count(*) FROM password_reset_tokens WHERE user_id=$1),(SELECT count(*) FROM audit_logs WHERE action='auth.password_reset' AND object_id=$1),must_change_password FROM users WHERE id=$1`, f.admin.ID).Scan(&sessions, &links, &events, &forced); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || links != 0 || events != 1 || forced {
		t.Fatal("reset state incorrect")
	}
}

func TestPasswordResetInvalidAndRevoked(t *testing.T) {
	for _, scenario := range []string{"expired", "unknown", "malformed", "blocked", "replaced", "admin_changed", "ordinary_changed"} {
		t.Run(scenario, func(t *testing.T) {
			mail := &resetMail{links: make(chan string, 10)}
			f := resetFixture(t, mail)
			b := f.browser(t)
			token := requestReset(t, b, mail)
			switch scenario {
			case "expired":
				_, err := f.pool.Exec(context.Background(), `UPDATE password_reset_tokens SET created_at=clock_timestamp()-interval '31 minutes',expires_at=clock_timestamp()-interval '1 second'`)
				if err != nil {
					t.Fatal(err)
				}
			case "unknown":
				token, _ = auth.Token()
			case "malformed":
				token = "invalid-token"
			case "blocked":
				_, err := f.pool.Exec(context.Background(), `UPDATE users SET blocked=true WHERE id=$1`, f.admin.ID)
				if err != nil {
					t.Fatal(err)
				}
			case "replaced":
				_ = requestReset(t, b, mail)
			case "admin_changed":
				admin := f.adminBrowser(t)
				expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", f.admin.ID), map[string]string{"password": "Admin-Temporary-92741"}, 200)
			case "ordinary_changed":
				admin := f.adminBrowser(t)
				expect(t, admin, "POST", "/auth/change-password", map[string]string{"old_password": adminPassword, "password": "Ordinary-Changed-92741"}, 204)
			}
			before := accountHash(t, f)
			confirmReset(t, b, token, "Must-Not-Apply-92841", 400)
			if accountHash(t, f) != before {
				t.Fatal("invalid token changed password")
			}
		})
	}
}

func TestPasswordResetConcurrentUse(t *testing.T) {
	mail := &resetMail{links: make(chan string, 10)}
	f := resetFixture(t, mail)
	b := f.browser(t)
	token := requestReset(t, b, mail)
	service, err := auth.NewService(&auth.Repository{Pool: f.pool})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- service.ResetPassword(context.Background(), token, "Concurrent-New-92741", "test-ip")
		}()
	}
	successes := 0
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			successes++
		} else if err.Error() != "reset_invalid" {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful resets: %d", successes)
	}
}

func TestPasswordResetLimits(t *testing.T) {
	f := setup(t)
	repo := &auth.Repository{Pool: f.pool}
	s, err := auth.NewService(repo)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		if err := s.RequestPasswordReset(ctx, "unknown@example.test", "request-ip"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RequestPasswordReset(ctx, "admin@example.test", "request-ip"); err == nil || err.Error() != "rate_limited" {
		t.Fatal("missing request IP limit")
	}
	for i := 0; i < 30; i++ {
		if err := s.ResetPassword(ctx, "bad", "Strong-Password-92741", "attempt-ip"); err == nil || err.Error() != "reset_invalid" {
			t.Fatal("unexpected invalid-token outcome")
		}
	}
	if err := s.ResetPassword(ctx, "bad", "Strong-Password-92741", "attempt-ip"); err == nil || err.Error() != "rate_limited" {
		t.Fatal("missing attempt IP limit")
	}
	token, _ := auth.Token()
	for i := 0; i < 5; i++ {
		_ = s.ResetPassword(ctx, token, "short", fmt.Sprintf("ip-%d", i))
	}
	if err := s.ResetPassword(ctx, token, "Strong-Password-92741", "fresh-ip"); err == nil || err.Error() != "rate_limited" {
		t.Fatal("missing token limit")
	}
	var attempts int
	if err := f.pool.QueryRow(ctx, `SELECT attempts FROM password_reset_rate_limits WHERE key_hash=$1`, auth.Hash("request-email:unknown@example.test")).Scan(&attempts); err != nil || attempts != 20 {
		t.Fatal("email limits not counted for unknown addresses")
	}
	_, err = f.pool.Exec(ctx, `UPDATE password_reset_rate_limits SET window_start=now()-interval '31 minutes'`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestPasswordReset(ctx, "unknown@example.test", "request-ip"); err != nil {
		t.Fatal("limit did not expire")
	}
}

type lockedLog struct {
	sync.Mutex
	bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.Lock()
	defer l.Unlock()
	return l.Buffer.Write(p)
}
func (l *lockedLog) text() string { l.Lock(); defer l.Unlock(); return l.Buffer.String() }

func TestPasswordResetMailFailureAndLogs(t *testing.T) {
	mail := &resetMail{links: make(chan string, 10), failure: errors.New("synthetic SMTP secret that must never be logged")}
	f := resetFixture(t, mail)
	b := f.browser(t)
	var logs lockedLog
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	token := requestReset(t, b, mail)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.text(), "password reset delivery failed") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM password_reset_tokens`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed delivery left usable token")
	}
	password := "Secret-New-Password-92741"
	confirmReset(t, b, token, password, 400)
	if !strings.Contains(logs.text(), "password reset delivery failed") {
		t.Fatal("missing sanitized delivery diagnostic")
	}
	for _, secret := range []string{token, password, "admin@example.test", mail.failure.Error()} {
		if strings.Contains(logs.text(), secret) {
			t.Fatal("secret in logs")
		}
	}
	if bcrypt.CompareHashAndPassword([]byte(accountHash(t, f)), []byte(adminPassword)) != nil {
		t.Fatal("mail failure changed password")
	}
}

func TestPasswordResetEmailThrottleAndBlockedAccount(t *testing.T) {
	mail := &resetMail{links: make(chan string, 10)}
	f := resetFixture(t, mail)
	b := f.browser(t)
	for i := 0; i < 5; i++ {
		_ = requestReset(t, b, mail)
	}
	before := accountHash(t, f)
	var stored []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT token_hash FROM password_reset_tokens WHERE user_id=$1`, f.admin.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	limited := expect(t, b, "POST", "/auth/password-reset/request", map[string]string{"email": "admin@example.test"}, 202)
	unknown := expect(t, b, "POST", "/auth/password-reset/request", map[string]string{"email": "unknown@example.test"}, 202)
	if !bytes.Equal(limited, unknown) {
		t.Fatal("email throttle disclosed existence")
	}
	var current []byte
	if err := f.pool.QueryRow(context.Background(), `SELECT token_hash FROM password_reset_tokens WHERE user_id=$1`, f.admin.ID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, stored) || accountHash(t, f) != before {
		t.Fatal("throttled request changed account or link")
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE users SET blocked=true WHERE id=$1`, f.admin.ID); err != nil {
		t.Fatal(err)
	}
	issued, err := (&auth.Repository{Pool: f.pool}).IssueReset(context.Background(), "admin@example.test", auth.Hash("synthetic-reset-token"))
	if err != nil || issued {
		t.Fatal("blocked account received link")
	}
	blocked := expect(t, b, "POST", "/auth/password-reset/request", map[string]string{"email": "admin@example.test"}, 202)
	if !bytes.Equal(blocked, unknown) {
		t.Fatal("blocked account disclosed")
	}
}

func TestPasswordResetExpiryRecheckedAtMutation(t *testing.T) {
	mail := &resetMail{links: make(chan string, 10)}
	f := resetFixture(t, mail)
	token := requestReset(t, f.browser(t), mail)
	before := accountHash(t, f)
	// Simulate expiry while checking the previous password, after initial validation.
	err := (&auth.Repository{Pool: f.pool}).ResetPassword(context.Background(), auth.Hash(token), "must-not-be-written", func(string) bool {
		if _, e := f.pool.Exec(context.Background(), `UPDATE password_reset_tokens SET created_at=clock_timestamp()-interval '31 minutes',expires_at=clock_timestamp()-interval '1 second'`); e != nil {
			t.Fatal(e)
		}
		return false
	})
	if err == nil || err.Error() != "reset_invalid" || accountHash(t, f) != before {
		t.Fatal("token expired during validation changed account")
	}
}
