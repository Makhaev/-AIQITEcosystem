package app

import (
	"context"
	"net/http"
	"strings"
	"time"

	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/curators"
	"aiq.local/core/internal/modules/families"
	"aiq.local/core/internal/modules/groups"
	"aiq.local/core/internal/modules/lessons"
	"aiq.local/core/internal/modules/pilot"
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
	tw := &teachers.WorkspaceHandler{Pool: pool}
	ch := &curators.Handler{Pool: pool}
	fh := &families.Handler{Repo: &families.Repository{Pool: pool}}
	ph := &programs.Handler{Service: &programs.Service{Repo: &programs.Repository{Pool: pool}}}
	gh := &groups.Handler{Repo: &groups.Repository{Pool: pool}}
	lh := &lessons.Handler{Repo: &lessons.Repository{Pool: pool}}
	pilotHandler := &pilot.Handler{Pool: pool, Origin: cfg.Origin}
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/methodist/workspace", h.RequirePermission(access.MethodistWorkspace, access.Read, ch.MethodistSummary))
	mux.Handle("GET /api/v1/methodist/programs", h.RequirePermission(access.MethodistWorkspace, access.Read, ch.MethodistPrograms))
	mux.Handle("GET /api/v1/methodist/students", h.RequirePermission(access.MethodistWorkspace, access.Read, ch.MethodistStudents))
	mux.Handle("GET /api/v1/methodist/students/{id}", h.RequirePermission(access.MethodistWorkspace, access.Read, ch.MethodistStudent))
	mux.Handle("GET /api/v1/methodist/teacher-reports", h.RequirePermission(access.MethodistWorkspace, access.Read, ch.MethodistTeacherReports))
	mux.Handle("GET /api/v1/methodist/curator-reports", h.RequirePermission(access.MethodistWorkspace, access.Read, ch.MethodistCuratorReports))
	mux.Handle("GET /api/v1/curator/workspace", h.RequirePermission(access.CuratorWorkspace, access.Read, ch.Summary))
	mux.Handle("GET /api/v1/curator/groups", h.RequirePermission(access.CuratorWorkspace, access.Read, ch.Groups))
	mux.Handle("GET /api/v1/curator/students", h.RequirePermission(access.CuratorWorkspace, access.Read, ch.Students))
	mux.Handle("GET /api/v1/curator/students/{id}", h.RequirePermission(access.CuratorWorkspace, access.Read, ch.Student))
	mux.Handle("GET /api/v1/curator-reports", h.RequirePermission(access.CuratorReports, access.Read, ch.Reports))
	mux.Handle("GET /api/v1/curator-reports/{id}", h.RequirePermission(access.CuratorReports, access.Read, ch.Report))
	mux.Handle("POST /api/v1/curator-reports", h.RequirePermission(access.CuratorReports, access.Manage, ch.SaveReport))
	mux.Handle("PATCH /api/v1/curator-reports/{id}", h.RequirePermission(access.CuratorReports, access.Manage, ch.SaveReport))
	mux.Handle("POST /api/v1/curator-reports/{id}/publish", h.RequirePermission(access.CuratorReports, access.Manage, ch.Publish))
	mux.Handle("POST /api/v1/curator-reports/{id}/feedback", h.RequirePermission(access.CuratorReports, access.Read, ch.Feedback))
	mux.Handle("GET /api/v1/teacher/workspace", h.RequirePermission(access.TeacherWorkspace, access.Read, tw.Summary))
	mux.Handle("GET /api/v1/teacher/groups", h.RequirePermission(access.TeacherWorkspace, access.Read, tw.Groups))
	mux.Handle("GET /api/v1/teacher/students", h.RequirePermission(access.TeacherWorkspace, access.Read, tw.Students))
	mux.HandleFunc("GET /api/v1/features", func(w http.ResponseWriter, r *http.Request) { httpx.JSON(w, 200, cfg.Features()) })
	mux.Handle("POST /api/v1/invitations", h.RequirePermission(access.Users, access.Manage, pilotHandler.Invitation))
	mux.Handle("GET /api/v1/access-requests", h.RequirePermission(access.Users, access.Read, pilotHandler.Requests))
	mux.Handle("POST /api/v1/access-links", h.RequirePermission(access.Users, access.Manage, pilotHandler.RecoveryLink))
	mux.HandleFunc("POST /api/v1/auth/recovery-request", pilotHandler.RequestRecovery)
	mux.HandleFunc("POST /api/v1/auth/accept-link", pilotHandler.AcceptLink)
	mux.Handle("GET /api/v1/learning/students", h.RequirePermission(access.Learning, access.Read, pilotHandler.Students))
	mux.Handle("GET /api/v1/learning/progress", h.RequirePermission(access.Learning, access.Read, pilotHandler.StudentProgress))
	mux.Handle("GET /api/v1/learning/lessons", h.RequirePermission(access.Learning, access.Read, pilotHandler.Lessons))
	mux.Handle("GET /api/v1/assignments", h.RequirePermission(access.Learning, access.Read, pilotHandler.Assignments))
	mux.Handle("POST /api/v1/assignments", h.RequirePermission(access.Learning, access.Manage, pilotHandler.CreateAssignment))
	mux.Handle("POST /api/v1/assignments/{id}/submit", h.RequirePermission(access.Learning, access.Mark, pilotHandler.Submit))
	mux.Handle("POST /api/v1/assignments/{id}/review", h.RequirePermission(access.Learning, access.Manage, pilotHandler.Review))
	mux.Handle("GET /api/v1/reports", h.RequirePermission(access.Learning, access.Read, pilotHandler.Reports))
	mux.Handle("POST /api/v1/reports", h.RequirePermission(access.Learning, access.Manage, pilotHandler.CreateReport))
	mux.Handle("POST /api/v1/reports/{id}/publish", h.RequirePermission(access.Learning, access.Manage, pilotHandler.Publish))
	// Reserve unverified feature routes. Disabling is enforced before authorization
	// and method routing, including direct calls to object subpaths.
	for _, prefix := range []string{"projects", "achievements", "skills", "events", "parent-requests", "metrics", "notifications", "materials", "settings", "tasks", "dashboard", "audit-logs"} {
		name := strings.ReplaceAll(prefix, "-", "_")
		if prefix == "audit-logs" {
			name = "audit"
		}
		closed := cfg.Gate(name, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.Fail(w, r, httpx.E(501, "not_implemented", "Функция ещё не реализована."))
		}))
		mux.Handle("/api/v1/"+prefix, closed)
		mux.Handle("/api/v1/"+prefix+"/", closed)
	}
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
	mux.Handle("GET /api/v1/students/{id}/360", cfg.Gate("student360", h.RequirePermission(access.Students, access.Read, fh.Student360)))
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
	mux.Handle("GET /api/v1/lessons/{id}/attendance", h.RequirePermission(access.Attendance, access.Read, lh.Journal))
	mux.Handle("PUT /api/v1/lessons/{id}/attendance", h.RequirePermission(access.Attendance, access.Mark, lh.Mark))
	mux.Handle("PUT /api/v1/lessons/{id}/roster", h.RequirePermission(access.Lessons, access.Manage, lh.CorrectRoster))
	mux.Handle("POST /api/v1/lessons/{id}/complete", h.RequirePermission(access.Attendance, access.Mark, lh.Complete))
	mux.Handle("POST /api/v1/lessons/{id}/cancel", h.RequirePermission(access.Lessons, access.Manage, lh.Cancel))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.Fail(w, r, httpx.E(404, "not_found", "Маршрут не найден."))
	})
	return httpx.Observe(h.CSRF(mux)), repo, nil
}
