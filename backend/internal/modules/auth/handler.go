package auth

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
)

const (
	SessionCookie = "aiq_session"
	CSRFCookie    = "aiq_csrf"
)

type Handler struct {
	Service    *Service
	Secure     bool
	Origin     string
	TrustProxy bool
}

func cookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
func (h *Handler) setCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
}
func (h *Handler) clearCookies(w http.ResponseWriter) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		http.SetCookie(w, &http.Cookie{Name: name, Path: "/", HttpOnly: true, Secure: h.Secure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	}
}
func Unsafe(method string) bool { return method != "GET" && method != "HEAD" && method != "OPTIONS" }

// The origin allow-list and a host-only HttpOnly cookie defend the anonymous login
// too. Authenticated writes additionally verify the token bound to the DB session.
func (h *Handler) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if Unsafe(r.Method) {
			token := cookie(r, CSRFCookie)
			header := r.Header.Get("X-CSRF-Token")
			if r.Header.Get("Origin") != h.Origin || !ValidToken(token) || subtle.ConstantTimeCompare([]byte(token), []byte(header)) != 1 {
				httpx.Fail(w, r, httpx.E(403, "csrf_failed", "Защитный токен недействителен. Обновите страницу."))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (h *Handler) Require(admin, allowPasswordChange bool, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := h.Service.Current(r.Context(), cookie(r, SessionCookie))
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if Unsafe(r.Method) && subtle.ConstantTimeCompare(Hash(r.Header.Get("X-CSRF-Token")), session.CSRFHash) != 1 {
			httpx.Fail(w, r, httpx.E(403, "csrf_failed", "Защитный токен недействителен. Обновите страницу."))
			return
		}
		if session.User.MustChangePassword && !allowPasswordChange {
			httpx.Fail(w, r, httpx.E(403, "password_change_required", "Сначала смените временный пароль."))
			return
		}
		actor := httpx.Actor{ID: session.User.ID, Roles: session.User.Roles, MustChangePassword: session.User.MustChangePassword, SessionHash: Hash(cookie(r, SessionCookie))}
		if admin && !actor.Admin() {
			httpx.Fail(w, r, httpx.Forbidden())
			return
		}
		next(w, r.WithContext(httpx.WithActor(r.Context(), actor)))
	})
}
func (h *Handler) RequirePermission(resource access.Resource, action access.Action, next http.HandlerFunc) http.Handler {
	return h.Require(false, false, func(w http.ResponseWriter, r *http.Request) {
		if access.ScopeFor(httpx.CurrentActor(r.Context()), resource, action) == access.None {
			httpx.Fail(w, r, httpx.Forbidden())
			return
		}
		next(w, r)
	})
}
func (h *Handler) CSRFToken(w http.ResponseWriter, r *http.Request) {
	// Cross-origin requests cannot fetch the token: no CORS is enabled.
	if origin := r.Header.Get("Origin"); origin != "" && origin != h.Origin {
		httpx.Fail(w, r, httpx.Forbidden())
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		httpx.Fail(w, r, httpx.Forbidden())
		return
	}
	token := cookie(r, CSRFCookie)
	sessionToken := cookie(r, SessionCookie)
	if sessionToken != "" {
		session, err := h.Service.Current(r.Context(), sessionToken)
		if err != nil {
			var appErr *httpx.Error
			if !errors.As(err, &appErr) || appErr.Status != 401 {
				httpx.Fail(w, r, err)
				return
			}
			h.clearCookies(w)
			token = ""
		} else if !ValidToken(token) || subtle.ConstantTimeCompare(Hash(token), session.CSRFHash) != 1 {
			token, err = Token()
			if err != nil {
				httpx.Fail(w, r, err)
				return
			}
			if err = h.Service.RotateCSRF(r.Context(), sessionToken, token); err != nil {
				httpx.Fail(w, r, err)
				return
			}
			h.setCookie(w, CSRFCookie, token, session.Expires)
		}
	}
	if !ValidToken(token) {
		var err error
		token, err = Token()
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		h.setCookie(w, CSRFCookie, token, time.Now().Add(SessionLifetime))
	}
	httpx.JSON(w, 200, map[string]string{"csrf_token": token})
}
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if len(in.Password) > 72 {
		httpx.Fail(w, r, httpx.E(401, "invalid_credentials", "Неверный email или пароль."))
		return
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if h.TrustProxy {
		if client, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
			ip = client.Unmap().String()
		}
	}
	result, err := h.Service.Login(r.Context(), in.Email, in.Password, ip, cookie(r, SessionCookie))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h.setCookie(w, SessionCookie, result.Token, result.Expires)
	h.setCookie(w, CSRFCookie, result.CSRF, result.Expires)
	httpx.JSON(w, 200, map[string]any{"user": result.User, "csrf_token": result.CSRF})
}
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	session, err := h.Service.Current(r.Context(), cookie(r, SessionCookie))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, session.User)
}
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Service.Logout(r.Context(), cookie(r, SessionCookie), httpx.CurrentActor(r.Context()).ID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h.clearCookies(w)
	w.WriteHeader(204)
}
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OldPassword string `json:"old_password"`
		Password    string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if err := h.Service.ChangePassword(r.Context(), httpx.CurrentActor(r.Context()).ID, cookie(r, SessionCookie), in.OldPassword, in.Password); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h.clearCookies(w)
	w.WriteHeader(204)
}
