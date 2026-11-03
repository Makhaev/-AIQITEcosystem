package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/modules/auth"
)

func TestAdminPasswordLoginWithoutSecondFactor(t *testing.T) {
	for _, legacyEnrollment := range []bool{false, true} {
		name := "new_account"
		if legacyEnrollment {
			name = "previously_enrolled_account"
		}
		t.Run(name, func(t *testing.T) {
			f := setup(t) // Router is configured without an encryption key.
			ctx := context.Background()
			if legacyEnrollment {
				// Historic factors must neither require a key nor block password login.
				if _, err := f.pool.Exec(ctx, `INSERT INTO user_mfa(user_id,secret_cipher) VALUES($1,$2)`, f.admin.ID, []byte("obsolete-encrypted-factor")); err != nil {
					t.Fatal(err)
				}
			}
			b := f.browser(t)
			login := expect(t, b, "POST", "/auth/login", map[string]string{"email": f.admin.Email, "password": adminPassword}, 200)
			for _, raw := range [][]byte{login, expect(t, b, "GET", "/auth/me", nil, 200)} {
				if strings.Contains(string(raw), "mfa_") {
					t.Fatal("removed MFA fields remain in account response")
				}
			}
			var expires time.Time
			if err := f.pool.QueryRow(ctx, `SELECT expires_at FROM sessions WHERE token_hash=$1`, auth.Hash(b.sessionToken(t))).Scan(&expires); err != nil {
				t.Fatal(err)
			}
			if remaining := time.Until(expires); remaining < 11*time.Hour || remaining > auth.SessionLifetime {
				t.Fatalf("unexpected administrative session lifetime: %s", remaining)
			}
			// Exercise both HTTP authorization and transactional read/write checks.
			expect(t, b, "GET", "/users", nil, 200)
			expect(t, b, "GET", "/dashboard", nil, 200)
			expect(t, b, "POST", "/students", map[string]string{"name": "Password-only admin write"}, 201)
			for _, route := range []string{"setup", "confirm", "verify", "recovery-codes", "reset"} {
				expect(t, b, "POST", "/auth/mfa/"+route, map[string]string{"code": "123456"}, 404)
			}
			_, teacher := provision(t, f, b, "password-teacher@example.test", "teacher")
			expect(t, teacher, "GET", "/users", nil, 403)
			expect(t, f.browser(t), "GET", "/users", nil, 401)
		})
	}
}

func TestAdminPasswordResetReturnsDirectlyToWorkspace(t *testing.T) {
	mail := &resetMail{links: make(chan string, 10)}
	f := resetFixture(t, mail)
	admin := f.adminBrowser(t)
	anonymous := f.browser(t)
	token := requestReset(t, anonymous, mail)
	newPassword := "Admin-After-Reset-9372"
	confirmReset(t, anonymous, token, newPassword, 204)
	expect(t, admin, "GET", "/users", nil, 401)
	expect(t, anonymous, "GET", "/auth/csrf", nil, 200)
	anonymous.login(t, f.admin.Email, newPassword)
	expect(t, anonymous, "GET", "/users", nil, 200)
}
