package app

import (
	"context"
	"net/http"
	"time"

	"aiq.local/core/internal/modules/attendance"
	"aiq.local/core/internal/modules/audit"
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/curriculum"
	"aiq.local/core/internal/modules/dashboard"
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/modules/groups"
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/modules/lessons"
	"aiq.local/core/internal/modules/parents"
	"aiq.local/core/internal/modules/programs"
	"aiq.local/core/internal/modules/progress"
	"aiq.local/core/internal/modules/students"
	"aiq.local/core/internal/modules/tasks"
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
	mux := http.NewServeMux()
	ls := &learning.Service{Repo: &learning.Repository{Pool: pool}}
	learningHandler := &learning.Handler{Service: ls}
	mux.Handle("POST /api/v1/students/{id}/education", h.RequirePermission(access.Students, access.Read, learningHandler.Create))
	mux.Handle("GET /api/v1/students/{id}/education/{kind}", h.RequirePermission(access.Students, access.Read, learningHandler.List))
	mux.Handle("GET /api/v1/learning/{id}", h.RequirePermission(access.Students, access.Read, learningHandler.Card))
	mux.Handle("PUT /api/v1/learning/{id}", h.RequirePermission(access.Students, access.Read, learningHandler.Card))
	mux.Handle("POST /api/v1/learning/{id}/actions", h.RequirePermission(access.Students, access.Read, learningHandler.Action))
	studentHandler := &students.Handler{Service: &students.Service{Repo: &students.Repository{Pool: pool}}, Learning: ls}
	parentHandler := &parents.Handler{Service: &parents.Service{Repo: &parents.Repository{Pool: pool}}}
	ph := &programs.Handler{Service: &programs.Service{Repo: &programs.Repository{Pool: pool}}}
	ch := &curriculum.Handler{Service: &curriculum.Service{Repo: &curriculum.Repository{Pool: pool}}}
	gh := &groups.Handler{Service: &groups.Service{Repo: &groups.Repository{Pool: pool}}}
	lh := &lessons.Handler{Service: &lessons.Service{Repo: &lessons.Repository{Pool: pool}}}
	attendanceHandler := &attendance.Handler{Service: &attendance.Service{Repo: &attendance.Repository{Pool: pool}}}
	taskHandler := &tasks.Handler{Service: &tasks.Service{Repo: &tasks.Repository{Pool: pool}}}
	dashboardHandler := &dashboard.Handler{Service: &dashboard.Service{Repo: &dashboard.Repository{Pool: pool}}}
	auditHandler := &audit.Handler{Service: &audit.Service{Repo: &audit.Repository{Pool: pool}}}

	prh := &progress.Handler{Service: &progress.Service{Repo: &progress.Repository{Pool: pool}}}
	mux.Handle("GET /api/v1/groups/{id}/progress", h.RequirePermission(access.Groups, access.Read, prh.Overview))
	mux.Handle("GET /api/v1/lessons/{id}/report", h.RequirePermission(access.Lessons, access.Read, prh.Report))
	mux.Handle("PUT /api/v1/lessons/{id}/report", h.RequirePermission(access.Attendance, access.Mark, prh.Report))
	fh := &files.Handler{Service: &files.Service{Repo: &files.Repository{Pool: pool}}}
	mux.Handle("POST /api/v1/files", h.RequirePermission(access.Students, access.Read, fh.Create))
	mux.Handle("GET /api/v1/students/{id}/files", h.RequirePermission(access.Students, access.Read, fh.List))
	mux.Handle("GET /api/v1/files/{id}/content", h.RequirePermission(access.Students, access.Read, fh.Get))
	mux.Handle("PUT /api/v1/files/{id}/access", h.RequirePermission(access.Students, access.Read, fh.Grant))
	mux.Handle("POST /api/v1/files/{id}/revoke", h.RequirePermission(access.Students, access.Read, fh.Revoke))
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
	mux.Handle("GET /api/v1/students", h.RequirePermission(access.Students, access.Read, studentHandler.Students))
	mux.Handle("POST /api/v1/students", h.RequirePermission(access.Students, access.Manage, studentHandler.CreateStudent))
	mux.Handle("GET /api/v1/students/{id}", h.RequirePermission(access.Students, access.Read, studentHandler.Student))
	studentOverview := h.RequirePermission(access.Students, access.Read, studentHandler.Student360)
	mux.Handle("GET /api/v1/students/{id}/overview", studentOverview)
	// Existing clients keep the same response and access checks, without a redirect.
	mux.Handle("GET /api/v1/students/{id}/360", studentOverview)
	mux.Handle("PATCH /api/v1/students/{id}", h.RequirePermission(access.Students, access.Manage, studentHandler.UpdateStudent))
	mux.Handle("POST /api/v1/students/{id}/parents", h.RequirePermission(access.Students, access.Manage, studentHandler.CreateLink))
	mux.Handle("PATCH /api/v1/students/{id}/parents/{parent_id}", h.RequirePermission(access.Students, access.Manage, studentHandler.UpdateLink))
	mux.Handle("GET /api/v1/parents", h.RequirePermission(access.Parents, access.Read, parentHandler.Parents))
	mux.Handle("POST /api/v1/parents", h.RequirePermission(access.Parents, access.Manage, parentHandler.CreateParent))
	mux.Handle("POST /api/v1/parents/contact-matches", h.RequirePermission(access.Parents, access.Manage, parentHandler.ContactMatches))
	mux.Handle("GET /api/v1/parents/{id}", h.RequirePermission(access.Parents, access.Read, parentHandler.Parent))
	mux.Handle("PATCH /api/v1/parents/{id}", h.RequirePermission(access.Parents, access.Manage, parentHandler.UpdateParent))
	mux.Handle("GET /api/v1/programs", h.RequirePermission(access.Programs, access.Read, ph.List))
	mux.Handle("GET /api/v1/programs/{id}/versions", h.RequirePermission(access.Curriculum, access.Read, ch.Versions))
	mux.Handle("POST /api/v1/programs/{id}/versions", h.RequirePermission(access.Curriculum, access.Manage, ch.Versions))
	mux.Handle("GET /api/v1/programs/{id}/versions/{version_id}", h.RequirePermission(access.Curriculum, access.Read, ch.Version))
	mux.Handle("PUT /api/v1/programs/{id}/versions/{version_id}", h.RequirePermission(access.Curriculum, access.Manage, ch.Version))
	// The action-specific role is checked again inside the transaction.
	mux.Handle("POST /api/v1/programs/{id}/versions/{version_id}/actions", h.RequirePermission(access.Curriculum, access.Read, ch.Action))
	mux.Handle("POST /api/v1/groups/{id}/program-transfer/preview", h.RequirePermission(access.Groups, access.Manage, ch.PreviewTransfer))
	mux.Handle("POST /api/v1/groups/{id}/program-transfer", h.RequirePermission(access.Groups, access.Manage, ch.Transfer))
	mux.Handle("GET /api/v1/groups/{id}/program-transfers", h.RequirePermission(access.Groups, access.Manage, ch.Transfers))
	mux.Handle("POST /api/v1/programs", h.RequirePermission(access.Programs, access.Manage, ph.Create))
	mux.Handle("GET /api/v1/programs/{id}", h.RequirePermission(access.Programs, access.Read, ph.Get))
	mux.Handle("PATCH /api/v1/programs/{id}", h.RequirePermission(access.Programs, access.Manage, ph.Update))
	mux.Handle("POST /api/v1/programs/{id}/modules", h.RequirePermission(access.Programs, access.Manage, ph.CreateModule))
	mux.Handle("GET /api/v1/programs/{id}/modules/{module_id}", h.RequirePermission(access.Programs, access.Read, ph.GetModule))
	mux.Handle("PATCH /api/v1/programs/{id}/modules/{module_id}", h.RequirePermission(access.Programs, access.Manage, ph.UpdateModule))
	mux.Handle("GET /api/v1/groups", h.RequirePermission(access.Groups, access.Read, gh.List))
	mux.Handle("POST /api/v1/groups", h.RequirePermission(access.Groups, access.Manage, gh.Create))
	mux.Handle("GET /api/v1/groups/{id}", h.RequirePermission(access.Groups, access.Read, gh.Get))
	mux.Handle("PATCH /api/v1/groups/{id}", h.RequirePermission(access.Groups, access.Manage, gh.Update))
	mux.Handle("POST /api/v1/groups/{id}/enrollments", h.RequirePermission(access.Groups, access.Manage, gh.Enroll))
	mux.Handle("PATCH /api/v1/groups/{id}/enrollments/{enrollment_id}", h.RequirePermission(access.Groups, access.Manage, gh.Close))
	mux.Handle("POST /api/v1/groups/{id}/enrollments/{enrollment_id}/transfer", h.RequirePermission(access.Groups, access.Manage, gh.Transfer))
	mux.Handle("GET /api/v1/lessons", h.RequirePermission(access.Lessons, access.Read, lh.List))
	mux.Handle("POST /api/v1/lessons", h.RequirePermission(access.Lessons, access.Manage, lh.Create))
	mux.Handle("GET /api/v1/lessons/{id}", h.RequirePermission(access.Lessons, access.Read, lh.Get))
	mux.Handle("PATCH /api/v1/lessons/{id}", h.RequirePermission(access.Lessons, access.Manage, lh.Update))
	mux.Handle("GET /api/v1/lessons/{id}/attendance", h.RequirePermission(access.Attendance, access.Read, attendanceHandler.Journal))
	mux.Handle("PUT /api/v1/lessons/{id}/attendance", h.RequirePermission(access.Attendance, access.Mark, attendanceHandler.Mark))
	mux.Handle("PUT /api/v1/lessons/{id}/roster", h.RequirePermission(access.Lessons, access.Manage, attendanceHandler.CorrectRoster))
	mux.Handle("POST /api/v1/lessons/{id}/complete", h.RequirePermission(access.Attendance, access.Mark, lh.Complete))
	mux.Handle("POST /api/v1/lessons/{id}/cancel", h.RequirePermission(access.Lessons, access.Manage, lh.Cancel))
	mux.Handle("POST /api/v1/lessons/{id}/reschedule", h.RequirePermission(access.Lessons, access.Manage, lh.Reschedule))
	mux.Handle("POST /api/v1/lessons/{id}/snapshot", h.RequirePermission(access.Lessons, access.Manage, lh.CorrectSnapshot))
	mux.Handle("GET /api/v1/lessons/{id}/snapshot-history", h.RequirePermission(access.Lessons, access.Manage, lh.SnapshotHistory))
	mux.Handle("GET /api/v1/tasks", h.RequirePermission(access.Tasks, access.Read, taskHandler.List))
	mux.Handle("POST /api/v1/tasks", h.RequirePermission(access.Tasks, access.Manage, taskHandler.Create))
	mux.Handle("GET /api/v1/tasks/assignees", h.RequirePermission(access.Tasks, access.Manage, taskHandler.Assignees))
	mux.Handle("GET /api/v1/tasks/{id}", h.RequirePermission(access.Tasks, access.Read, taskHandler.Get))
	mux.Handle("PATCH /api/v1/tasks/{id}", h.RequirePermission(access.Tasks, access.ChangeStatus, taskHandler.Update))
	mux.Handle("PATCH /api/v1/tasks/{id}/status", h.RequirePermission(access.Tasks, access.ChangeStatus, taskHandler.Status))
	mux.Handle("GET /api/v1/dashboard", h.RequirePermission(access.Dashboard, access.Read, dashboardHandler.Get))
	mux.Handle("GET /api/v1/audit-logs", h.RequirePermission(access.Audit, access.Read, auditHandler.List))
	mux.Handle("GET /api/v1/audit-logs/{id}", h.RequirePermission(access.Audit, access.Read, auditHandler.Get))
	mux.Handle("/api/v1/audit-logs", h.RequirePermission(access.Audit, access.Read, auditHandler.ReadOnly))
	mux.Handle("/api/v1/audit-logs/{id}", h.RequirePermission(access.Audit, access.Read, auditHandler.ReadOnly))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.E(404, "not_found", "Маршрут не найден."))
	})
	return httpx.Observe(h.CSRF(mux)), repo, nil
}
