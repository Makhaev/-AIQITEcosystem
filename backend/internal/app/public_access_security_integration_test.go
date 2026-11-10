package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aiq.local/core/internal/app"
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/config"
)

func publicAccessRequest(t *testing.T, handler http.Handler, path, ip string, body any) *httptest.ResponseRecorder {
	t.Helper()
	csrfRequest := httptest.NewRequest("GET", "/api/v1/auth/csrf", nil)
	csrfResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfResponse, csrfRequest)
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	decode(t, csrfResponse.Body.Bytes(), &csrf)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/"+path, bytes.NewReader(data))
	r.RemoteAddr = "192.0.2.10:43210"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", csrf.Token)
	r.Header.Set("X-Real-IP", ip)
	for _, cookie := range csrfResponse.Result().Cookies() {
		r.AddCookie(cookie)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r.WithContext(ctx))
	return w
}

func TestPublicAccessRateLimitsIsolateClientsBehindTrustedProxy(t *testing.T) {
	f := setup(t)
	token, err := auth.Token()
	if err != nil {
		t.Fatal(err)
	}
	for _, trust := range []bool{false, true} {
		for _, endpoint := range []struct {
			name string
			body any
			want int
		}{
			{"recovery-request", map[string]string{"email": "missing@example.test"}, 202},
			{"accept-link", map[string]string{"token": token, "password": "TestOnly-Unused-Password"}, 400},
		} {
			name := endpoint.name + "/direct"
			if trust {
				name = endpoint.name + "/trusted-proxy"
			}
			t.Run(name, func(t *testing.T) {
				if _, err := f.pool.Exec(context.Background(), `DELETE FROM login_rate_limits`); err != nil {
					t.Fatal(err)
				}
				handler, _, err := app.Router(f.pool, config.Config{Origin: origin, TrustProxy: trust})
				if err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 31; i++ {
					want := endpoint.want
					if i == 30 {
						want = http.StatusTooManyRequests
					}
					w := publicAccessRequest(t, handler, endpoint.name, "198.51.100.1", endpoint.body)
					if w.Code != want {
						t.Fatalf("attempt %d: want %d, got %d: %s", i+1, want, w.Code, w.Body)
					}
				}
				want := http.StatusTooManyRequests
				if trust {
					want = endpoint.want
				}
				w := publicAccessRequest(t, handler, endpoint.name, "198.51.100.2", endpoint.body)
				if w.Code != want {
					t.Fatalf("second IP: want %d, got %d: %s", want, w.Code, w.Body)
				}
			})
		}
	}
}

func TestUnknownAccessLinkDoesNotBlockManagement(t *testing.T) {
	f := setup(t)
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := access.LockManagement(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	token, err := auth.Token()
	if err != nil {
		t.Fatal(err)
	}
	handler, _, err := app.Router(f.pool, config.Config{Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	w := publicAccessRequest(t, handler, "accept-link", "198.51.100.1", map[string]string{"token": token, "password": "TestOnly-Unused-Password"})
	var result struct {
		Code string `json:"code"`
	}
	decode(t, w.Body.Bytes(), &result)
	if w.Code != 400 || result.Code != "invalid_link" {
		t.Fatalf("invalid token reached management lock: %d %s", w.Code, w.Body)
	}
}
