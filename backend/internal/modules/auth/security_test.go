package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCSRFRejectsAnonymousCrossOriginLogin(t *testing.T) {
	h := Handler{Origin: "https://aiq.example", Secure: true}
	token, _ := Token()
	called := false
	next := h.CSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) }))
	cases := []struct {
		name, method, origin, token, header string
		status                              int
	}{
		{"valid POST", "POST", "https://aiq.example", token, token, 204},
		{"valid PATCH", "PATCH", "https://aiq.example", token, token, 204},
		{"missing origin", "POST", "", token, token, 403},
		{"foreign origin", "POST", "https://evil.example", token, token, 403},
		{"different port", "POST", "https://aiq.example:8443", token, token, 403},
		{"missing header", "POST", "https://aiq.example", token, "", 403},
		{"missing cookie", "POST", "https://aiq.example", "", token, 403},
		{"malformed token", "POST", "https://aiq.example", "invalid", "invalid", 403},
		{"mismatched token", "POST", "https://aiq.example", token, "different", 403},
		{"unprotected PUT", "PUT", "https://aiq.example", "", "", 403},
		{"unprotected DELETE", "DELETE", "https://aiq.example", "", "", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(tc.method, "https://aiq.example/api/v1/auth/login", nil)
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("X-CSRF-Token", tc.header)
			if tc.token != "" {
				req.AddCookie(&http.Cookie{Name: CSRFCookie, Value: tc.token})
			}
			res := httptest.NewRecorder()
			next.ServeHTTP(res, req)
			if res.Code != tc.status || called != (tc.status == 204) {
				t.Fatalf("status=%d called=%v", res.Code, called)
			}
			if tc.status == 403 {
				var body map[string]any
				if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || body["code"] != "csrf_failed" {
					t.Fatal("expected structured CSRF error")
				}
			}
		})
	}
}
func TestCookiesAreHostOnlyAndSecureForHTTPS(t *testing.T) {
	for _, secure := range []bool{false, true} {
		h := Handler{Secure: secure}
		res := httptest.NewRecorder()
		h.setCookie(res, SessionCookie, "token", time.Now().Add(SessionLifetime))
		c := res.Result().Cookies()[0]
		if !c.HttpOnly || c.Secure != secure || c.Domain != "" || c.Path != "/" || c.SameSite != http.SameSiteStrictMode {
			t.Fatalf("unsafe cookie: %+v", c)
		}
		cleared := httptest.NewRecorder()
		h.clearCookies(cleared)
		cookies := cleared.Result().Cookies()
		if cookies[len(cookies)-1].MaxAge != -1 {
			t.Fatal("clear cookie missing")
		}
	}
}
