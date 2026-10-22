package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/curriculum"
	"aiq.local/core/internal/platform/httpx"
)

func TestM2VersionObjectScopesAndRequiredPublishedGroup(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Открытая", "status": "active"})
	other := programCreate(t, admin, map[string]any{"name": "Чужая", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	draft := createVersion(t, f.methodologist, p.ID, &g.ProgramVersionID)
	user, teacher := provision(t, f, admin, "m2-teacher@example.test", "teacher")
	_, stranger := provision(t, f, admin, "m2-stranger@example.test", "teacher")
	profile := teacherCreate(t, admin, map[string]any{"name": "Педагог", "user_id": user.ID})
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"teacher_id": profile.ID}, 200)
	expect(t, teacher, "GET", versionPath(p.ID, g.ProgramVersionID), nil, 200)
	expect(t, teacher, "GET", versionPath(p.ID, draft.ID), nil, 404)
	expect(t, stranger, "GET", versionPath(p.ID, g.ProgramVersionID), nil, 404)
	expect(t, teacher, "GET", versionPath(other.ID, g.ProgramVersionID), nil, 404)
	var versions curriculum.VersionPage
	decode(t, expect(t, teacher, "GET", fmt.Sprintf("/programs/%d/versions", p.ID), nil, 200), &versions)
	if len(versions.Items) != 1 || versions.Items[0].ID != g.ProgramVersionID {
		t.Fatal("draft leaked")
	}
	expect(t, admin, "POST", "/groups", map[string]any{"name": "Invalid", "program_id": p.ID, "capacity": 10, "program_version_id": draft.ID}, 409)
	expect(t, admin, "POST", "/groups", map[string]any{"name": "Foreign", "program_id": other.ID, "capacity": 10, "program_version_id": g.ProgramVersionID}, 409)
	// A newly created active catalog entry has no implicit publication.
	var newProgram struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, admin, "POST", "/programs", map[string]any{"name": "Без версии", "status": "active"}, 201), &newProgram)
	expect(t, admin, "POST", "/groups", map[string]any{"name": "No publication", "program_id": newProgram.ID, "capacity": 10}, 409)
	expect(t, teacher, "POST", groupPath(g.ID)+"/program-transfer/preview", map[string]any{"target_version_id": draft.ID}, 403)
	expect(t, admin, "POST", "/lessons", map[string]any{"group_id": g.ID, "teacher_id": profile.ID, "topic": "Чужой шаблон", "starts_at": time.Now().Add(time.Hour), "ends_at": time.Now().Add(2 * time.Hour), "version_module_key": "foreign-module", "template_key": "lesson-1"}, 400)
	// CSRF remains enforced for new routes.
	fresh := f.methodologist.csrf
	f.methodologist.csrf = ""
	expect(t, f.methodologist, "POST", fmt.Sprintf("/programs/%d/versions", p.ID), map[string]any{}, 403)
	f.methodologist.csrf = fresh
}

func TestM2ConcurrentDraftWritesAndRevokedSession(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Конкуренция", "status": "active"})
	v := createVersion(t, f.methodologist, p.ID, nil)
	var actorID int64
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email='fixture-methodologist@example.test'`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	actor := httpx.Actor{ID: actorID, Roles: []string{"methodologist"}, SessionHash: auth.Hash(f.methodologist.sessionToken(t))}
	ctx, cancel := context.WithTimeout(httpx.WithActor(context.Background(), actor), 10*time.Second)
	defer cancel()
	svc := curriculum.Service{Repo: &curriculum.Repository{Pool: f.pool}}
	results := make(chan error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, e := svc.Save(ctx, p.ID, v.ID, curriculum.SaveInput{Revision: v.Revision, Document: curriculumDocument("Изменение")})
			results <- e
		}()
	}
	close(start)
	success, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else {
			var api *httpx.Error
			if !errors.As(err, &api) || api.Code != "stale_revision" {
				t.Fatal(err)
			}
			conflicts++
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("lost update not prevented")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", actorID), map[string]any{"roles": []string{"teacher"}}, 200)
	_, err := svc.Create(ctx, p.ID, curriculum.CreateInput{})
	var api *httpx.Error
	if !errors.As(err, &api) || api.Status != 401 {
		t.Fatalf("stale actor accepted: %v", err)
	}
}

func TestM2TransferAuditFailureRollsBackGroupAndPlans(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Исходная", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	q := programCreate(t, admin, map[string]any{"name": "Целевая", "status": "active"})
	var target int64
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM program_versions WHERE program_id=$1`, q.ID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	teacher := teacherCreate(t, admin, map[string]any{"name": "Педагог"})
	start := time.Now().Add(24 * time.Hour)
	expect(t, admin, "POST", "/lessons", map[string]any{"group_id": g.ID, "teacher_id": teacher.ID, "topic": "Занятие", "starts_at": start, "ends_at": start.Add(time.Hour), "version_module_key": "module-1", "template_key": "lesson-1"}, 201)
	var pview curriculum.Preview
	decode(t, expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer/preview", map[string]any{"target_version_id": target}, 200), &pview)
	in := curriculum.TransferInput{TargetVersionID: target, PreviewToken: pview.Token, Reason: "Перевод", Mappings: []curriculum.Mapping{pview.Lessons[0].Suggested}}
	if _, err := f.pool.Exec(context.Background(), `CREATE FUNCTION reject_m2_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='group.program_transferred' THEN RAISE EXCEPTION 'fixture audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_m2_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_m2_audit()`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer", in, 500)
	if groupCard(t, admin, g.ID).ProgramVersionID != g.ProgramVersionID {
		t.Fatal("audit failure committed group")
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM group_program_transfers`).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial transfer history", err)
	}
	var oldVersion int64
	if err := f.pool.QueryRow(context.Background(), `SELECT program_version_id FROM lessons WHERE id=$1`, pview.Lessons[0].ID).Scan(&oldVersion); err != nil || oldVersion != g.ProgramVersionID {
		t.Fatal("partial lesson transfer", err)
	}
	if _, err := f.pool.Exec(context.Background(), `DROP TRIGGER reject_m2_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "POST", groupPath(g.ID)+"/program-transfer", in, 200)
	if groupCard(t, admin, g.ID).ProgramID != q.ID {
		t.Fatal("cross-program transfer failed")
	}
}
