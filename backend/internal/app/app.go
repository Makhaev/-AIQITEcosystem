package app

import (
	"context"
	"net/http"
	"time"

	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/families"
	"aiq.local/core/internal/modules/programs"
	"aiq.local/core/internal/modules/teachers"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/config"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Router(pool *pgxpool.Pool, cfg config.Config) (http.Handler, *auth.Repository, error) {
	repo := &auth.Repository{Pool: pool}
	service, err := auth.NewService(repo)
	if err != nil {
		return nil, nil, err
	}
	h := &auth.Handler{Service: service, Secure: cfg.SecureCookies, Origin: cfg.Origin, TrustProxy: cfg.TrustProxy}
	uh := &users.Handler{Service: &users.Service{Repo: &users.Repository{Pool: pool}}}
	th := &teachers.Handler{Service: &teachers.Service{Repo: &teachers.Repository{Pool: pool}}}
	fh := &families.Handler{Repo: &families.Repository{Pool: pool}}
	ph := &programs.Handler{Service: &programs.Service{Repo: &programs.Repository{Pool: pool}}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			httpx.Fail(w, r, httpx.E(503, "not_ready", "База данных недоступна."))
			return
		}
		httpx.JSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/v1/auth/csrf", h.CSRFToken)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)
	mux.Handle("GET /api/v1/auth/me", h.Require(false, true, h.Me))
	mux.Handle("POST /api/v1/auth/logout", h.Require(false, true, h.Logout))
	mux.Handle("POST /api/v1/auth/change-password", h.Require(false, true, h.ChangePassword))
	mux.Handle("GET /api/v1/users", h.RequirePermission(access.Users, access.Read, uh.List))
	mux.Handle("POST /api/v1/users", h.RequirePermission(access.Users, access.Manage, uh.Create))
	mux.Handle("PATCH /api/v1/users/{id}", h.RequirePermission(access.Users, access.Manage, uh.Update))
	mux.Handle("GET /api/v1/teachers", h.RequirePermission(access.Teachers, access.Read, th.List))
	mux.Handle("GET /api/v1/teachers/{id}", h.RequirePermission(access.Teachers, access.Read, th.Get))
	mux.Handle("POST /api/v1/teachers", h.RequirePermission(access.Teachers, access.Manage, th.Create))
	mux.Handle("PATCH /api/v1/teachers/{id}", h.RequirePermission(access.Teachers, access.Manage, th.Update))
	mux.Handle("GET /api/v1/students", h.RequirePermission(access.Students, access.Read, fh.Students))
	mux.Handle("POST /api/v1/students", h.RequirePermission(access.Students, access.Manage, fh.CreateStudent))
	mux.Handle("GET /api/v1/students/{id}", h.RequirePermission(access.Students, access.ReadFull, fh.Student))
	mux.Handle("PATCH /api/v1/students/{id}", h.RequirePermission(access.Students, access.Manage, fh.UpdateStudent))
	mux.Handle("POST /api/v1/students/{id}/parents", h.RequirePermission(access.Students, access.Manage, fh.CreateLink))
	mux.Handle("PATCH /api/v1/students/{id}/parents/{parent_id}", h.RequirePermission(access.Students, access.Manage, fh.UpdateLink))
	mux.Handle("GET /api/v1/parents", h.RequirePermission(access.Parents, access.Read, fh.Parents))
	mux.Handle("POST /api/v1/parents", h.RequirePermission(access.Parents, access.Manage, fh.CreateParent))
	mux.Handle("GET /api/v1/parents/{id}", h.RequirePermission(access.Parents, access.Read, fh.Parent))
	mux.Handle("PATCH /api/v1/parents/{id}", h.RequirePermission(access.Parents, access.Manage, fh.UpdateParent))
	mux.Handle("GET /api/v1/programs", h.RequirePermission(access.Programs, access.Read, ph.List))
	mux.Handle("POST /api/v1/programs", h.RequirePermission(access.Programs, access.Manage, ph.Create))
	mux.Handle("GET /api/v1/programs/{id}", h.RequirePermission(access.Programs, access.Read, ph.Get))
	mux.Handle("PATCH /api/v1/programs/{id}", h.RequirePermission(access.Programs, access.Manage, ph.Update))
	mux.Handle("POST /api/v1/programs/{id}/modules", h.RequirePermission(access.Programs, access.Manage, ph.CreateModule))
	mux.Handle("GET /api/v1/programs/{id}/modules/{module_id}", h.RequirePermission(access.Programs, access.Read, ph.GetModule))
	mux.Handle("PATCH /api/v1/programs/{id}/modules/{module_id}", h.RequirePermission(access.Programs, access.Manage, ph.UpdateModule))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.E(404, "not_found", "Маршрут не найден."))
	})
	return httpx.Observe(h.CSRF(mux)), repo, nil
}
