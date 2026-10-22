package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"aiq.local/core/internal/platform/access"
	"github.com/jackc/pgx/v5"
)

// Compare entire rows, including timestamps, as well as successful audit events.
func taskWriteSnapshot(t *testing.T, f *fixture) string {
	t.Helper()
	var value string
	err := f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'tasks', (SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM tasks k),
 'audit', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a WHERE object_type='task'))::text`).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertAssignmentDenied(t *testing.T, body []byte) {
	t.Helper()
	var failure map[string]any
	decode(t, body, &failure)
	fields, ok := failure["field_errors"].(map[string]any)
	if len(failure) != 4 || failure["code"] != "assignee_object_access_denied" || !ok || len(fields) != 1 || fields["assignee_id"] != "Выберите сотрудника с доступом к связанному объекту." || failure["message"] != "Ответственный не имеет доступа к выбранной записи." {
		t.Fatalf("assignment error disclosed extra data or violated contract: %s", body)
	}
}

func assertTaskObjectHidden(t *testing.T, body []byte) {
	t.Helper()
	var v map[string]any
	decode(t, body, &v)
	for _, key := range []string{"object_type", "object_id", "object_name"} {
		if value, present := v[key]; !present || value != nil {
			t.Fatalf("expected null %s: %s", key, body)
		}
	}
}

func TestTaskAssignmentAccessContractAndAtomicity(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, time.Hour, 2*time.Hour)
	ctx := context.Background()
	var teacherID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	curator, curatorBrowser := provision(t, a.f, a.admin, "r02-curator@example.test", "curator")
	foreignCurator, _ := provision(t, a.f, a.admin, "r02-foreign-curator@example.test", "curator")
	blocked, _ := provision(t, a.f, a.admin, "r02-blocked@example.test", "leader")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", blocked.ID), map[string]any{"blocked": true}, 200)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": curator.ID}, 200)
	_, leader := provision(t, a.f, a.admin, "r02-leader@example.test", "leader")
	parent := parentCreate(t, a.admin, map[string]any{"name": "Private parent", "email": "private-parent@example.test", "phone": "+79001234567"})
	linkParent(t, a.admin, a.firstID, parent.ID, true)

	for _, object := range []struct {
		kind string
		id   int64
	}{{"student", a.firstID}, {"group", a.groupID}, {"lesson", l.ID}} {
		t.Run(object.kind, func(t *testing.T) {
			body := taskBody(teacherID, a.base.Add(time.Hour))
			body["object_type"], body["object_id"] = object.kind, object.id
			valid := taskCreate(t, leader, body)
			if valid.Description != body["description"] {
				t.Fatal("object data was copied into task description")
			}
			// Both restricted teacher access and assigned curator access suffice.
			body["assignee_id"] = curator.ID
			taskCreate(t, a.admin, body)
			for _, assignee := range []int64{a.foreignUserID, foreignCurator.ID, blocked.ID, 999999} {
				body["assignee_id"] = assignee
				before := taskWriteSnapshot(t, a.f)
				assertAssignmentDenied(t, expect(t, leader, "POST", "/tasks", body, 400))
				assertAssignmentDenied(t, expect(t, leader, "PATCH", taskPath(valid.ID), map[string]any{"assignee_id": assignee, "title": "Must roll back"}, 400))
				if taskWriteSnapshot(t, a.f) != before {
					t.Fatal("rejected create/reassignment changed tasks or audit")
				}
			}
			unlinked := taskCreate(t, a.admin, taskBody(foreignCurator.ID, a.base.Add(time.Hour)))
			before := taskWriteSnapshot(t, a.f)
			assertAssignmentDenied(t, expect(t, a.admin, "PATCH", taskPath(unlinked.ID), map[string]any{"object_type": object.kind, "object_id": object.id, "description": "Must roll back"}, 400))
			body["assignee_id"], body["object_id"] = teacherID, 999999
			assertAssignmentDenied(t, expect(t, leader, "POST", "/tasks", body, 400))
			// In PATCH, the type is inherited from the stored assignment.
			assertAssignmentDenied(t, expect(t, leader, "PATCH", taskPath(valid.ID), map[string]any{"object_id": 999999}, 400))
			for _, author := range []*browser{a.own, curatorBrowser} {
				expect(t, author, "POST", "/tasks", body, 403)
				expect(t, author, "PATCH", taskPath(valid.ID), map[string]any{"assignee_id": teacherID}, 403)
			}
			if taskWriteSnapshot(t, a.f) != before {
				t.Fatal("rejected relink/missing object/author changed tasks or audit")
			}
			// The merged new pair is checked: changing both fields to a valid pair works.
			expect(t, leader, "PATCH", taskPath(unlinked.ID), map[string]any{"assignee_id": teacherID, "object_type": object.kind, "object_id": object.id}, 200)
		})
	}
	var card map[string]any
	decode(t, expect(t, a.own, "GET", fmt.Sprintf("/students/%d", a.firstID), nil, 200), &card)
	if len(card) != 3 {
		t.Fatal("task expanded restricted student access")
	}
	expect(t, a.own, "GET", fmt.Sprintf("/parents/%d", parent.ID), nil, 403)
	before := taskWriteSnapshot(t, a.f)
	body := taskBody(teacherID, a.base.Add(time.Hour))
	body["object_type"], body["object_id"], body["group_id"] = "student", a.firstID, a.groupID
	expect(t, a.admin, "POST", "/tasks", body, 400)
	if taskWriteSnapshot(t, a.f) != before {
		t.Fatal("request with multiple relations persisted")
	}
}

func TestTaskTeacherWithdrawalAndAccountBlocking(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, time.Hour, 2*time.Hour)
	var teacherID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, object := range []struct {
		kind string
		id   int64
	}{{"student", a.firstID}, {"group", a.groupID}, {"lesson", l.ID}} {
		body := taskBody(teacherID, a.base.Add(time.Hour))
		body["object_type"], body["object_id"] = object.kind, object.id
		ids = append(ids, taskCreate(t, a.admin, body).ID)
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/teachers/%d", a.teacherID), map[string]any{"user_id": nil}, 200)
	page := taskPage(t, a.own, "")
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatal("withdrawal hid own tasks")
	}
	for _, item := range page.Items {
		if item.ObjectType != nil || item.ObjectID != nil || item.ObjectName != nil {
			t.Fatal("list leaked withdrawn object")
		}
		assertTaskObjectHidden(t, expect(t, a.own, "GET", taskPath(item.ID), nil, 200))
		for _, status := range []string{"in_progress", "in_progress", "done"} {
			assertTaskObjectHidden(t, expect(t, a.own, "PATCH", taskPath(item.ID)+"/status", map[string]any{"status": status}, 200))
		}
		if taskGet(t, a.admin, item.ID).ObjectID == nil {
			t.Fatal("projection removed stored relation")
		}
	}
	for _, path := range []string{fmt.Sprintf("/students/%d", a.firstID), fmt.Sprintf("/students/%d/overview", a.firstID), groupPath(a.groupID), lessonPath(l.ID)} {
		expect(t, a.own, "GET", path, nil, 404)
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", teacherID), map[string]any{"blocked": true}, 200)
	before := taskWriteSnapshot(t, a.f)
	expect(t, a.own, "GET", "/tasks", nil, 401)
	expect(t, a.own, "GET", taskPath(ids[0]), nil, 401)
	expect(t, a.own, "PATCH", taskPath(ids[0])+"/status", map[string]any{"status": "done"}, 401)
	if taskWriteSnapshot(t, a.f) != before {
		t.Fatal("blocked account changed task/audit")
	}
}

// Wait for an observed database lock, not a scheduling delay, before revoking.
func waitTaskManagementLock(t *testing.T, ctx context.Context, f *fixture, gate pgx.Tx) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=8021002 AND NOT granted AND $1=ANY(pg_blocking_pids(pid)))`, int32(gate.Conn().PgConn().PID())).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("task did not wait for access mutation")
		case <-ticker.C:
		}
	}
}

func TestTaskAssignmentConcurrentRevocation(t *testing.T) {
	for _, operation := range []string{"create", "reassign", "relink"} {
		for _, revoke := range []string{"roles", "blocking", "teacher_account", "lesson_teacher", "enrollment", "roster", "curator", "group_teacher"} {
			t.Run(operation+"/"+revoke, func(t *testing.T) {
				a := setupAttendance(t)
				l := a.lesson(t, time.Hour, 2*time.Hour)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var assignee int64
				if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&assignee); err != nil {
					t.Fatal(err)
				}
				kind, objectID := "student", a.firstID
				var sql string
				var args []any
				switch revoke {
				case "roles":
					sql, args = `UPDATE user_roles SET role='curator' WHERE user_id=$1`, []any{assignee}
				case "blocking":
					sql, args = `UPDATE users SET blocked=true WHERE id=$1`, []any{assignee}
				case "teacher_account":
					sql, args = `UPDATE teachers SET user_id=NULL WHERE id=$1`, []any{a.teacherID}
				case "lesson_teacher":
					other := teacherCreate(t, a.admin, map[string]any{"name": "Replacement", "user_id": a.foreignUserID})
					kind, objectID = "lesson", l.ID
					sql, args = `UPDATE lessons SET teacher_id=$2 WHERE id=$1`, []any{l.ID, other.ID}
				case "enrollment":
					sql, args = `UPDATE group_enrollments SET ends_on='2000-01-01' WHERE student_id=$1`, []any{a.firstID}
				case "roster":
					lessonJournal(t, a.admin, l.ID)
					sql, args = `UPDATE lesson_participants SET included=false WHERE lesson_id=$1 AND student_id=$2`, []any{l.ID, a.firstID}
				case "curator":
					u, _ := provision(t, a.f, a.admin, "r02-race-curator@example.test", "curator")
					assignee = u.ID
					expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": assignee}, 200)
					sql, args = `UPDATE students SET curator_id=NULL WHERE id=$1`, []any{a.firstID}
				case "group_teacher":
					g := groupCreate(t, a.admin, groupCard(t, a.admin, a.groupID).ProgramID, 10, "active")
					expect(t, a.admin, "PATCH", groupPath(g.ID), map[string]any{"teacher_id": a.teacherID}, 200)
					kind, objectID = "group", g.ID
					sql, args = `UPDATE groups SET teacher_id=NULL WHERE id=$1`, []any{g.ID}
				}
				method, path := "POST", "/tasks"
				body := taskBody(assignee, a.base.Add(time.Hour))
				body["object_type"], body["object_id"] = kind, objectID
				if operation != "create" {
					initial := taskBody(assignee, a.base.Add(time.Hour))
					if operation == "reassign" {
						initial["assignee_id"], initial["object_type"], initial["object_id"] = a.f.admin.ID, kind, objectID
						body = map[string]any{"assignee_id": assignee, "title": "Must roll back"}
					} else {
						body = map[string]any{"object_type": kind, "object_id": objectID, "title": "Must roll back"}
					}
					method, path = "PATCH", taskPath(taskCreate(t, a.admin, initial).ID)
				}
				before := taskWriteSnapshot(t, a.f)
				gate, err := a.f.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer gate.Rollback(context.Background())
				if err = access.LockManagement(ctx, gate); err != nil {
					t.Fatal(err)
				}
				type response struct {
					status int
					body   []byte
				}
				result := make(chan response, 1)
				go func() { status, body := a.admin.do(t, method, path, body); result <- response{status, body} }()
				waitTaskManagementLock(t, ctx, a.f, gate)
				if _, err = gate.Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
				if err = gate.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case response := <-result:
					if response.status != 400 {
						t.Fatalf("stale access accepted: %d %s", response.status, response.body)
					}
					assertAssignmentDenied(t, response.body)
				case <-ctx.Done():
					t.Fatal("task did not resume")
				}
				if taskWriteSnapshot(t, a.f) != before {
					t.Fatal("rejected concurrent assignment changed tasks/audit")
				}
			})
		}
	}
}

func TestTaskAssignmentRechecksAuthorAfterWaiting(t *testing.T) {
	for _, operation := range []string{"create", "reassign", "relink"} {
		for _, revoke := range []string{"roles", "blocking"} {
			t.Run(operation+"/"+revoke, func(t *testing.T) {
				f := setup(t)
				admin := f.adminBrowser(t)
				leader, author := provision(t, f, admin, "r02-author@example.test", "leader")
				student := studentCreate(t, admin, map[string]any{"name": "Restricted object"})
				method, path := "POST", "/tasks"
				body := taskBody(f.admin.ID, time.Now().Add(time.Hour))
				body["object_type"], body["object_id"] = "student", student.ID
				if operation != "create" {
					method, path = "PATCH", taskPath(taskCreate(t, admin, taskBody(f.admin.ID, time.Now().Add(time.Hour))).ID)
					if operation == "reassign" {
						body = map[string]any{"assignee_id": f.admin.ID}
					} else {
						body = map[string]any{"object_type": "student", "object_id": student.ID}
					}
				}
				before := taskWriteSnapshot(t, f)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				gate, err := f.pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer gate.Rollback(context.Background())
				if err = access.LockManagement(ctx, gate); err != nil {
					t.Fatal(err)
				}
				result := make(chan int, 1)
				go func() { status, _ := author.do(t, method, path, body); result <- status }()
				waitTaskManagementLock(t, ctx, f, gate)
				sql, want := `UPDATE user_roles SET role='teacher' WHERE user_id=$1`, 403
				if revoke == "blocking" {
					sql, want = `UPDATE users SET blocked=true WHERE id=$1`, 401
				}
				// Keep the session to prove transaction authorization is independent
				// of both the middleware's old actor and session revocation.
				if _, err = gate.Exec(ctx, sql, leader.ID); err != nil {
					t.Fatal(err)
				}
				if err = gate.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case status := <-result:
					if status != want {
						t.Fatalf("stale author accepted: got %d, want %d", status, want)
					}
				case <-ctx.Done():
					t.Fatal("task did not resume")
				}
				if taskWriteSnapshot(t, f) != before {
					t.Fatal("unauthorized author changed tasks/audit")
				}
			})
		}
	}
}

func TestTaskAssignmentAuditFailureRollsBack(t *testing.T) {
	a := setupAttendance(t)
	body := taskBody(a.f.admin.ID, a.base.Add(time.Hour))
	body["object_type"], body["object_id"] = "student", a.firstID
	linked := taskCreate(t, a.admin, body)
	unlinked := taskCreate(t, a.admin, taskBody(a.f.admin.ID, a.base.Add(time.Hour)))
	if _, err := a.f.pool.Exec(context.Background(), `CREATE FUNCTION reject_task_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.object_type='task' THEN RAISE EXCEPTION 'injected'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_task_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_task_audit()`); err != nil {
		t.Fatal(err)
	}
	before := taskWriteSnapshot(t, a.f)
	expect(t, a.admin, "POST", "/tasks", body, 500)
	expect(t, a.admin, "PATCH", taskPath(linked.ID), map[string]any{"object_id": a.secondID}, 500)
	expect(t, a.admin, "PATCH", taskPath(unlinked.ID), map[string]any{"object_type": "student", "object_id": a.firstID}, 500)
	if taskWriteSnapshot(t, a.f) != before {
		t.Fatal("audit failure left task changes")
	}
}
