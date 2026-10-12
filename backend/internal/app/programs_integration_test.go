package app_test

import (
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/programs"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"strings"
	"testing"
	"time"
)

func programCreate(t *testing.T, b *browser, body map[string]any) programs.Program {
	t.Helper()
	var v programs.Program
	decode(t, expect(t, b, "POST", "/programs", body, 201), &v)
	return v
}
func moduleCreate(t *testing.T, b *browser, programID int64, position int, name string) programs.Module {
	t.Helper()
	var v programs.Module
	decode(t, expect(t, b, "POST", fmt.Sprintf("/programs/%d/modules", programID), map[string]any{"name": name, "description": "Описание модуля", "position": position}, 201), &v)
	return v
}
func programCard(t *testing.T, b *browser, id int64) programs.Card {
	t.Helper()
	var v programs.Card
	decode(t, expect(t, b, "GET", fmt.Sprintf("/programs/%d", id), nil, 200), &v)
	return v
}
func programPage(t *testing.T, b *browser, path string) programs.Page {
	t.Helper()
	var v programs.Page
	decode(t, expect(t, b, "GET", path, nil, 200), &v)
	return v
}
func modulePath(programID, moduleID int64) string {
	return fmt.Sprintf("/programs/%d/modules/%d", programID, moduleID)
}
func groupRecord(t *testing.T, f *fixture, programID int64, teacherID *int64) int64 {
	t.Helper()
	var id int64
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO groups(name,program_id,teacher_id,capacity,status) VALUES('Учебная группа',$1,$2,10,'active') RETURNING id`, programID, teacherID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func dbConstraint(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != code || pg.ConstraintName != constraint {
		t.Fatalf("want %s/%s, got %v", code, constraint, err)
	}
}

func TestProgramsLifecycleOrderedModulesAndValidation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "  Развитие мышления  ", "description": "  Первая строка\nВторая строка  "})
	q := programCreate(t, admin, map[string]any{"name": "Развитие мышления", "status": "active"})
	if p.Status != "draft" || p.Name != "Развитие мышления" || p.Description != "Первая строка\nВторая строка" || p.ID == q.ID {
		t.Fatalf("normalization/default/name duplication: %+v", p)
	}
	third := moduleCreate(t, admin, p.ID, 3, "Третий модуль")
	first := moduleCreate(t, admin, p.ID, 1, "Первый модуль")
	moduleCreate(t, admin, q.ID, 1, "Первый модуль")
	expect(t, admin, "POST", fmt.Sprintf("/programs/%d/modules", p.ID), map[string]any{"name": "Дубликат", "position": 1}, 409)
	c := programCard(t, admin, p.ID)
	if len(c.Modules) != 2 || c.Modules[0].ID != first.ID || c.Modules[1].ID != third.ID {
		t.Fatalf("ordered modules: %+v", c.Modules)
	}
	expect(t, admin, "PATCH", modulePath(p.ID, third.ID), map[string]any{"position": 1}, 409)
	expect(t, admin, "PATCH", modulePath(p.ID, third.ID), map[string]any{"position": 2, "name": " Второй модуль ", "description": nil}, 200)
	c = programCard(t, admin, p.ID)
	if c.Modules[1].Position != 2 || c.Modules[1].Name != "Второй модуль" || c.Modules[1].Description != "" {
		t.Fatalf("module edit/clear: %+v", c.Modules)
	}
	expect(t, admin, "GET", modulePath(q.ID, third.ID), nil, 404)
	expect(t, admin, "PATCH", modulePath(q.ID, third.ID), map[string]any{"name": "Чужой"}, 404)
	expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", p.ID), map[string]any{"status": "active", "description": nil}, 200)
	if page := programPage(t, admin, "/programs?search="+url.QueryEscape("РАЗВИТИЕ МЫШ")+"&page=2&page_size=1"); page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != q.ID {
		t.Fatalf("search/pagination: %+v", page)
	}
	programCreate(t, admin, map[string]any{"name": "Название %_ буквально"})
	if page := programPage(t, admin, "/programs?search="+url.QueryEscape("%_")); page.Total != 1 {
		t.Fatal("wildcard search not literal")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", p.ID), map[string]any{"status": "archived"}, 200)
	if page := programPage(t, admin, "/programs?status=archived"); page.Total != 1 || page.Items[0].ID != p.ID {
		t.Fatal("archive filter")
	}
	if len(programCard(t, admin, p.ID).Modules) != 2 {
		t.Fatal("archive removed modules")
	}
	expect(t, admin, "POST", fmt.Sprintf("/programs/%d/modules", p.ID), map[string]any{"name": "Запрещён", "position": 4}, 409)
	expect(t, admin, "PATCH", modulePath(p.ID, third.ID), map[string]any{"name": "Запрещён"}, 409)
	expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", p.ID), map[string]any{"status": "draft"}, 200)
	expect(t, admin, "PATCH", modulePath(p.ID, third.ID), map[string]any{"description": "Восстановлена"}, 200)
	for _, body := range []map[string]any{{}, {"name": " "}, {"name": strings.Repeat("Я", 201)}, {"name": "X", "status": "deleted"}, {"name": "X", "description": strings.Repeat("Я", 5001)}, {"name": "X", "unknown": 1}} {
		expect(t, admin, "POST", "/programs", body, 400)
	}
	for _, body := range []map[string]any{{}, {"name": nil}, {"status": nil}, {"status": ""}, {"status": "deleted"}} {
		expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", p.ID), body, 400)
	}
	for _, body := range []map[string]any{{}, {"name": nil}, {"position": nil}, {"position": 0}, {"position": -1}, {"position": 1.5}, {"position": 2147483648}, {"program_id": q.ID}, {"description": strings.Repeat("Я", 5001)}} {
		expect(t, admin, "PATCH", modulePath(p.ID, third.ID), body, 400)
	}
	expect(t, admin, "POST", fmt.Sprintf("/programs/%d/modules", p.ID), map[string]any{"name": "Нет номера"}, 400)
	for _, path := range []string{"/programs/0", "/programs/bad", "/programs?page=0", "/programs?page_size=101", "/programs?status=deleted"} {
		expect(t, admin, "GET", path, nil, 400)
	}
	expect(t, admin, "GET", "/programs/99999", nil, 404)
	expect(t, admin, "GET", modulePath(p.ID, 99999), nil, 404)
	expect(t, admin, "POST", "/programs/99999/modules", map[string]any{"name": "X", "position": 1}, 404)
	expect(t, admin, "DELETE", modulePath(p.ID, third.ID), nil, 404)
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE object_type='program' AND object_id=$1 AND action='program.archived'`, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("archive audit: %d %v", count, err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE object_type='module' AND object_id=$1 AND action='module.updated'`, third.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("atomic module audit: %d %v", count, err)
	}
}

func TestProgramGroupAndLessonDatabaseRelationships(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	active := programCreate(t, admin, map[string]any{"name": "Active", "status": "active"})
	draft := programCreate(t, admin, map[string]any{"name": "Draft"})
	other := programCreate(t, admin, map[string]any{"name": "Other", "status": "active"})
	m := moduleCreate(t, admin, active.ID, 1, "Correct")
	foreign := moduleCreate(t, admin, other.ID, 1, "Foreign")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher"})
	_, err := f.pool.Exec(ctx, `INSERT INTO groups(name,program_id,capacity) VALUES('Invalid',$1,10)`, draft.ID)
	dbConstraint(t, err, "23514", "groups_active_program")
	g := groupRecord(t, f, active.ID, &teacher.ID)
	_, err = f.pool.Exec(ctx, `UPDATE groups SET program_id=$2 WHERE id=$1`, g, draft.ID)
	dbConstraint(t, err, "23514", "groups_active_program")
	insert := `INSERT INTO lessons(group_id,module_id,topic,teacher_id,starts_at,ends_at) VALUES($1,$2,'Topic',$3,'2026-10-01T08:00:00Z','2026-10-01T09:00:00Z')`
	_, err = f.pool.Exec(ctx, insert, g, foreign.ID, teacher.ID)
	dbConstraint(t, err, "23503", "lessons_module_program")
	if _, err = f.pool.Exec(ctx, insert, g, m.ID, teacher.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, insert, g, nil, teacher.ID); err != nil {
		t.Fatal("optional module rejected:", err)
	}
	_, err = f.pool.Exec(ctx, `UPDATE lessons SET module_id=$1 WHERE group_id=$2`, foreign.ID, g)
	dbConstraint(t, err, "23503", "lessons_module_program")
	_, err = f.pool.Exec(ctx, `UPDATE lessons SET program_id=$1 WHERE group_id=$2`, other.ID, g)
	dbConstraint(t, err, "23514", "lessons_program_matches_group")
	_, err = f.pool.Exec(ctx, `UPDATE groups SET program_id=$1 WHERE id=$2`, other.ID, g)
	dbConstraint(t, err, "23503", "lessons_group_program")
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = programs.RequireActive(ctx, tx, active.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = programs.RequireActive(ctx, tx, draft.ID); err == nil {
		t.Fatal("guard permits draft group assignment")
	}
	if err = programs.RequireGroupModule(ctx, tx, g, &m.ID); err != nil {
		t.Fatal(err)
	}
	if err = programs.RequireGroupModule(ctx, tx, g, &foreign.ID); err == nil {
		t.Fatal("guard permits foreign module")
	}
	if err = programs.RequireGroupModule(ctx, tx, g, nil); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/programs/%d", active.ID), map[string]any{"status": "archived"}, 200)
	_, err = f.pool.Exec(ctx, `INSERT INTO groups(name,program_id,capacity) VALUES('Archived',$1,10)`, active.ID)
	dbConstraint(t, err, "23514", "groups_active_program")
	if _, err = f.pool.Exec(ctx, `UPDATE groups SET capacity=12,program_id=program_id WHERE id=$1`, g); err != nil {
		t.Fatal("historical assignment cannot be retained:", err)
	}
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM lessons l JOIN groups g ON g.id=l.group_id JOIN programs p ON p.id=g.program_id WHERE p.status='archived'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("archive destroyed history: %d %v", count, err)
	}
}

func TestProgramsHTTPObjectScopesAndRevokedSessions(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	teacherUser, teacherBrowser := provision(t, f, admin, "program-teacher@example.test", "teacher")
	curatorUser, curatorBrowser := provision(t, f, admin, "program-curator@example.test", "curator")
	_, leader := provision(t, f, admin, "program-leader@example.test", "leader")
	own := programCreate(t, admin, map[string]any{"name": "Own", "status": "active"})
	other := programCreate(t, admin, map[string]any{"name": "Other", "status": "active"})
	ownModule := moduleCreate(t, admin, own.ID, 1, "Own module")
	otherModule := moduleCreate(t, admin, other.ID, 1, "Other module")
	if programPage(t, teacherBrowser, "/programs").Total != 0 || programPage(t, curatorBrowser, "/programs").Total != 0 {
		t.Fatal("unassigned roles see programs")
	}
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher", "user_id": teacherUser.ID})
	g := groupRecord(t, f, own.ID, &teacher.ID)
	student := studentCreate(t, admin, map[string]any{"name": "Own student", "curator_id": curatorUser.ID})
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES($1,$2,'2026-09-01')`, g, student.ID); err != nil {
		t.Fatal(err)
	}
	for _, b := range []*browser{teacherBrowser, curatorBrowser} {
		page := programPage(t, b, "/programs?program_id="+fmt.Sprint(other.ID))
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != own.ID {
			t.Fatalf("restricted list: %+v", page)
		}
		programCard(t, b, own.ID)
		expect(t, b, "GET", fmt.Sprintf("/programs/%d", other.ID), nil, 404)
		expect(t, b, "GET", modulePath(own.ID, ownModule.ID), nil, 200)
		expect(t, b, "GET", modulePath(other.ID, otherModule.ID), nil, 404)
		expect(t, b, "GET", modulePath(own.ID, otherModule.ID), nil, 404)
	}
	if programPage(t, leader, "/programs").Total != 2 {
		t.Fatal("leader cannot read all programs")
	}
	for _, b := range []*browser{teacherBrowser, curatorBrowser, leader} {
		expect(t, b, "POST", "/programs", map[string]any{"name": "Forbidden"}, 403)
		expect(t, b, "PATCH", fmt.Sprintf("/programs/%d", own.ID), map[string]any{"status": "archived"}, 403)
		expect(t, b, "POST", fmt.Sprintf("/programs/%d/modules", own.ID), map[string]any{"name": "Forbidden", "position": 2}, 403)
		expect(t, b, "PATCH", modulePath(own.ID, ownModule.ID), map[string]any{"name": "Forbidden"}, 403)
	}
	// A lesson teacher can read the program even when not the group's main teacher.
	secondTeacherUser, lessonTeacher := provision(t, f, admin, "program-lesson@example.test", "teacher")
	secondTeacher := teacherCreate(t, admin, map[string]any{"name": "Lesson teacher", "user_id": secondTeacherUser.ID})
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO lessons(group_id,module_id,topic,teacher_id,starts_at,ends_at) VALUES($1,$2,'Own lesson',$3,'2026-10-01T08:00:00Z','2026-10-01T09:00:00Z')`, g, ownModule.ID, secondTeacher.ID); err != nil {
		t.Fatal(err)
	}
	if programPage(t, lessonTeacher, "/programs").Total != 1 {
		t.Fatal("own lesson does not grant program read")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", student.ID), map[string]any{"curator_id": nil}, 200)
	expect(t, curatorBrowser, "GET", fmt.Sprintf("/programs/%d", own.ID), nil, 404)
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) VALUES($1,'leader')`, curatorUser.ID); err != nil {
		t.Fatal(err)
	}
	if programPage(t, curatorBrowser, "/programs").Total != 2 {
		t.Fatal("fresh combined roles not applied")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", teacherUser.ID), map[string]any{"blocked": true}, 200)
	expect(t, teacherBrowser, "GET", "/programs", nil, 401)
	anonymous := f.browser(t)
	expect(t, anonymous, "GET", "/programs", nil, 401)
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))}
	ctx := httpx.WithActor(context.Background(), actor)
	admin.csrf = ""
	expect(t, admin, "POST", "/programs", map[string]any{"name": "CSRF"}, 403)
	expect(t, admin, "PATCH", modulePath(own.ID, ownModule.ID), map[string]any{"name": "CSRF"}, 403)
	expect(t, admin, "GET", "/auth/csrf", nil, 200)
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	decode(t, expect(t, admin, "GET", "/auth/csrf", nil, 200), &csrf)
	admin.csrf = csrf.Token
	expect(t, admin, "POST", "/auth/logout", nil, 204)
	service := programs.Service{Repo: &programs.Repository{Pool: f.pool}}
	_, getErr := service.Get(ctx, own.ID)
	_, listErr := service.List(ctx, programs.Filter{Page: 1, Size: 20})
	_, createErr := service.Create(ctx, programs.CreateInput{Name: "Revoked"})
	_, updateErr := service.UpdateModule(ctx, own.ID, ownModule.ID, programs.ModuleUpdate{Name: programs.Optional[string]{Set: true, Value: ptr("Revoked")}})
	for _, err := range []error{getErr, listErr, createErr, updateErr} {
		var e *httpx.Error
		if !errors.As(err, &e) || e.Status != 401 {
			t.Fatalf("revoked session reused: %v", err)
		}
	}
}

func TestProgramArchiveSerializesWithGroupAssignmentAndModuleConflict(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Concurrent", "status": "active"})
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))}
	ctx, cancel := context.WithTimeout(httpx.WithActor(context.Background(), actor), 8*time.Second)
	defer cancel()
	service := programs.Service{Repo: &programs.Repository{Pool: f.pool}}
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := service.CreateModule(ctx, p.ID, programs.ModuleInput{Name: "Concurrent", Position: 1})
			results <- err
		}()
	}
	close(start)
	success, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else {
			var e *httpx.Error
			if !errors.As(err, &e) || e.Code != "module_position_exists" {
				t.Fatal(err)
			}
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("concurrent module numbers accepted")
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `INSERT INTO groups(name,program_id,capacity) VALUES('Pending',$1,10)`, p.ID); err != nil {
		t.Fatal(err)
	}
	archive := make(chan error, 1)
	go func() {
		_, err := service.Update(ctx, p.ID, programs.UpdateInput{Status: programs.Optional[string]{Set: true, Value: ptr("archived")}})
		archive <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM programs%FOR UPDATE%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-archive:
			t.Fatalf("archive did not wait for group assignment: %v", err)
		case <-ctx.Done():
			t.Fatal("archive never reached program row lock")
		case <-ticker.C:
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-archive:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("archive blocked after group commit")
	}
	_, err = f.pool.Exec(ctx, `INSERT INTO groups(name,program_id,capacity) VALUES('After archive',$1,10)`, p.ID)
	dbConstraint(t, err, "23514", "groups_active_program")
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM groups WHERE program_id=$1`, p.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("history lost: %d %v", count, err)
	}
}
