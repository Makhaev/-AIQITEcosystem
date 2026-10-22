package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"

	"aiq.local/core/internal/modules/audit"
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/platform/httpx"
)

func auditPage(t *testing.T, b *browser, query string) audit.Page {
	t.Helper()
	var v audit.Page
	decode(t, expect(t, b, "GET", "/audit-logs"+query, nil, 200), &v)
	return v
}

func assertAuditFields(t *testing.T, b *browser, action, kind string, id int64, fields ...string) audit.Entry {
	t.Helper()
	q := url.Values{"action": {action}, "object_type": {kind}, "object_id": {fmt.Sprint(id)}}
	p := auditPage(t, b, "?"+q.Encode())
	if p.Total < 1 {
		t.Fatalf("missing audit %s %s %d", action, kind, id)
	}
	v := p.Items[0]
	got := slices.Clone(v.ChangedFields)
	slices.Sort(got)
	slices.Sort(fields)
	if !slices.Equal(got, fields) {
		t.Fatalf("%s: fields %v, want %v", action, got, fields)
	}
	if v.ActorID == nil || v.ActorName == nil || len(v.RequestID) != 32 || v.OccurredAt.IsZero() {
		t.Fatalf("incomplete audit metadata: %+v", v)
	}
	return v
}

func TestAuditCriticalChangesAndPrivacy(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	curator, _ := provision(t, a.f, a.admin, "audit-curator@example.test", "curator")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": curator.ID}, 200)
	assertAuditFields(t, a.admin, "student.updated", "student", a.firstID, "curator_id")
	// Contact values are only in the operational cards, never in audit rows.
	expect(t, a.admin, "PATCH", fmt.Sprintf("/teachers/%d", a.teacherID), map[string]any{"phone": "+79001234567", "email": "private-teacher@example.test"}, 200)
	assertAuditFields(t, a.admin, "teacher.updated", "teacher", a.teacherID, "phone", "email")
	parent := parentCreate(t, a.admin, map[string]any{"name": "Parent", "phone": "+79007654321", "email": "private-parent@example.test"})
	linkParent(t, a.admin, a.firstID, parent.ID, true)
	assertAuditFields(t, a.admin, "student.parent_linked", "student", a.firstID, "parent_id", "relationship", "is_primary")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/parents/%d", parent.ID), map[string]any{"phone": "+79009876543"}, 200)
	assertAuditFields(t, a.admin, "parent.updated", "parent", parent.ID, "phone")
	expect(t, a.admin, "PATCH", groupPath(a.groupID), map[string]any{"teacher_id": a.teacherID}, 200)
	assertAuditFields(t, a.admin, "group.updated", "group", a.groupID, "teacher_id")
	group := groupCard(t, a.admin, a.groupID)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/programs/%d", group.ProgramID), map[string]any{"description": "private-parent@example.test"}, 200)
	assertAuditFields(t, a.admin, "program.updated", "program", group.ProgramID, "description")
	m := moduleCreate(t, a.admin, group.ProgramID, 1, "Module")
	expect(t, a.admin, "PATCH", modulePath(group.ProgramID, m.ID), map[string]any{"position": 2}, 200)
	assertAuditFields(t, a.admin, "module.updated", "module", m.ID, "position")
	l := a.lesson(t, -4*time.Hour, -3*time.Hour)
	expect(t, a.admin, "PATCH", lessonPath(l.ID), map[string]any{"topic": "Updated schedule"}, 200)
	assertAuditFields(t, a.admin, "lesson.updated", "lesson", l.ID, "topic")
	comment := "private-parent@example.test +79009876543 SECRET-TOKEN-FOR-TEST"
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, "late", comment), markValue(a.secondID, "absent", "")), 204)
	assertAuditFields(t, a.admin, "attendance.updated", "lesson", l.ID, "attendance.status", "attendance.comment")
	expect(t, a.admin, "POST", lessonPath(l.ID)+"/complete", nil, 200)
	assertAuditFields(t, a.admin, "lesson.completed", "lesson", l.ID, "status")
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, "present", comment)), 204)
	assertAuditFields(t, a.admin, "attendance.corrected", "lesson", l.ID, "attendance.status")
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": []int64{}, "reason": comment}, 200)
	assertAuditFields(t, a.admin, "lesson.roster_corrected", "lesson", l.ID, "roster")
	task := taskCreate(t, a.admin, taskBody(a.foreignUserID, a.base.Add(time.Hour)))
	expect(t, a.admin, "PATCH", taskPath(task.ID), map[string]any{"assignee_id": curator.ID, "description": comment}, 200)
	assertAuditFields(t, a.admin, "task.reassigned", "task", task.ID, "assignee_id", "description")
	expect(t, a.admin, "PATCH", taskPath(task.ID)+"/status", map[string]any{"status": "in_progress"}, 200)
	expect(t, a.admin, "PATCH", taskPath(task.ID)+"/status", map[string]any{"status": "done"}, 200)
	var taskAudit audit.Page
	decode(t, expect(t, a.admin, "GET", "/audit-logs?object_type=task&object_id="+fmt.Sprint(task.ID)+"&action=task.status_changed", nil, 200), &taskAudit)
	if len(taskAudit.Items) != 2 || !reflect.DeepEqual(taskAudit.Items[0].ChangedFields, []string{"closed_at", "status"}) {
		t.Fatal("closure audit fields")
	}
	target := groupCreate(t, a.admin, group.ProgramID, 10, "active")
	e := group.Enrollments[0]
	expect(t, a.admin, "POST", enrollmentPath(a.groupID, e.ID)+"/transfer", map[string]any{"target_group_id": target.ID, "transfers_on": a.base.Format("2006-01-02")}, 200)
	assertAuditFields(t, a.admin, "enrollment.transferred_out", "enrollment", e.ID, "ends_on")
	transfers := auditPage(t, a.admin, "?action=enrollment.transferred_in")
	if transfers.Total != 1 || !slices.Equal(transfers.Items[0].ChangedFields, []string{"group_id", "student_id", "starts_on"}) {
		t.Fatalf("transfer audit: %+v", transfers)
	}
	// One multi-field user update records the actual role/block/name/password changes.
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", a.foreignUserID), map[string]any{"name": "Updated user", "roles": []string{"curator"}, "blocked": true, "password": "Secret-New-Password-8472"}, 200)
	assertAuditFields(t, a.admin, "user.updated", "user", a.foreignUserID, "name", "roles", "blocked", "password")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.secondID), map[string]any{"name": "Second"}, 200)
	assertAuditFields(t, a.admin, "student.updated", "student", a.secondID)
	// Check persisted data, not just response redaction.
	var stored string
	if err := a.f.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a))::text FROM audit_logs a`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := a.f.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, a.foreignUserID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-teacher@example.test", "private-parent@example.test", "+79001234567", "+79007654321", "+79009876543", "SECRET-TOKEN-FOR-TEST", userPassword, "Secret-New-Password-8472", hash, a.admin.sessionToken(t), a.admin.csrf} {
		if bytes.Contains([]byte(stored), []byte(private)) {
			t.Fatal("audit contains a private value")
		}
		if bytes.Contains(expect(t, a.admin, "GET", "/audit-logs?page_size=100", nil, 200), []byte(private)) {
			t.Fatal("audit API exposes a private value")
		}
	}
}

func TestAuditRolesReadOnlyFiltersAndRequestCorrelation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	_, leader := provision(t, f, admin, "audit-leader@example.test", "leader")
	_, teacher := provision(t, f, admin, "audit-teacher@example.test", "teacher")
	_, curator := provision(t, f, admin, "audit-curator@example.test", "curator")
	s := studentCreate(t, admin, map[string]any{"name": "Before"})
	body, _ := json.Marshal(map[string]any{"name": "After"})
	req, _ := http.NewRequest("PATCH", f.server.URL+fmt.Sprintf("/api/v1/students/%d", s.ID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.Header.Set("X-CSRF-Token", admin.csrf)
	response, err := admin.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	requestID := response.Header.Get("X-Request-ID")
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("mutation failed: %d", response.StatusCode)
	}
	v := assertAuditFields(t, admin, "student.updated", "student", s.ID, "name")
	if v.RequestID != requestID {
		t.Fatal("audit request_id differs from HTTP response")
	}
	var entry audit.Entry
	decode(t, expect(t, admin, "GET", fmt.Sprintf("/audit-logs/%d", v.ID), nil, 200), &entry)
	if !reflect.DeepEqual(v, entry) {
		t.Fatal("detail differs from list")
	}
	filtered := auditPage(t, admin, "?actor_id="+fmt.Sprint(f.admin.ID)+"&action=student.updated&object_type=student&object_id="+fmt.Sprint(s.ID)+"&request_id="+requestID)
	if filtered.Total != 1 || filtered.Items[0].ID != v.ID {
		t.Fatal("combined filters lost request")
	}
	first := auditPage(t, admin, "?page_size=2")
	second := auditPage(t, admin, "?page_size=2&page=2")
	if first.Total < 4 || second.Total != first.Total || len(first.Items) != 2 || len(second.Items) != 2 || first.Items[1].ID <= second.Items[0].ID {
		t.Fatal("pagination is unstable")
	}
	for _, b := range []*browser{leader, teacher, curator} {
		for _, path := range []string{"/audit-logs", fmt.Sprintf("/audit-logs/%d", v.ID)} {
			expect(t, b, "GET", path, nil, 403)
			for _, method := range []string{"POST", "PATCH", "DELETE"} {
				expect(t, b, method, path, map[string]any{}, 403)
			}
		}
	}
	expect(t, f.browser(t), "GET", "/audit-logs", nil, 401)
	for _, path := range []string{"/audit-logs", fmt.Sprintf("/audit-logs/%d", v.ID)} {
		for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
			expect(t, admin, method, path, map[string]any{}, 405)
		}
	}
	for _, query := range []string{"?page=0", "?page_size=101", "?actor_id=bad", "?object_id=-1", "?date_from=2026-02-30", "?date_to=bad", "?date_from=2026-10-12&date_to=2026-10-11"} {
		expect(t, admin, "GET", "/audit-logs"+query, nil, 400)
	}
	expect(t, admin, "GET", "/audit-logs/0", nil, 400)
	expect(t, admin, "GET", "/audit-logs/999999", nil, 404)
	ctx := httpx.WithActor(context.Background(), httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))})
	expect(t, admin, "POST", "/auth/logout", nil, 204)
	_, err = (&audit.Repository{Pool: f.pool}).List(ctx, audit.Filter{Page: 1, Size: 20})
	var appErr *httpx.Error
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("revoked audit session accepted: %v", err)
	}
}

func TestAuditMoscowDatesAnonymousActorsAndDatabaseImmutability(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	for _, timestamp := range []string{"2024-06-10T20:59:59Z", "2024-06-10T21:00:00Z", "2024-06-11T20:59:59Z", "2024-06-11T21:00:00Z"} {
		if _, err := f.pool.Exec(ctx, `INSERT INTO audit_logs(occurred_at,action,object_type,object_id,request_id) VALUES($1,'boundary.event','student',1,'boundary')`, timestamp); err != nil {
			t.Fatal(err)
		}
	}
	p := auditPage(t, admin, "?date_from=2024-06-11&date_to=2024-06-11&action=boundary.event")
	if p.Total != 2 || p.Items[0].ActorID != nil || p.Items[0].ActorName != nil || p.Items[0].ChangedFields == nil {
		t.Fatalf("Moscow boundary or anonymous event: %+v", p)
	}
	for _, statement := range []string{`UPDATE audit_logs SET action='tampered' WHERE id=$1`, `DELETE FROM audit_logs WHERE id=$1`} {
		_, err := f.pool.Exec(ctx, statement, p.Items[0].ID)
		dbConstraint(t, err, "23514", "audit_logs_append_only")
	}
}

func TestAuditFailureRollsBackEveryCriticalMutation(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	g := groupCard(t, a.admin, a.groupID)
	m := moduleCreate(t, a.admin, g.ProgramID, 1, "Module")
	p := parentCreate(t, a.admin, map[string]any{"name": "Parent", "phone": "+79001234567"})
	l := a.lesson(t, -4*time.Hour, -3*time.Hour)
	lessonJournal(t, a.admin, l.ID)
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, "absent", ""), markValue(a.secondID, "absent", "")), 204)
	k := taskCreate(t, a.admin, taskBody(a.foreignUserID, a.base.Add(time.Hour)))
	expect(t, a.admin, "PATCH", taskPath(k.ID)+"/status", map[string]any{"status": "in_progress"}, 200)
	if _, err := a.f.pool.Exec(ctx, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$;
 CREATE TRIGGER injected_audit_failure BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_audit()`); err != nil {
		t.Fatal(err)
	}
	snapshot := func(table string) string {
		t.Helper()
		var state string
		if err := a.f.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,'[]') FROM `+table+` t`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	for _, test := range []struct {
		method, path, table string
		body                any
	}{
		{"POST", "/users", "users", map[string]any{"name": "New", "email": "rollback@example.test", "password": userPassword, "roles": []string{"teacher"}}},
		{"PATCH", fmt.Sprintf("/users/%d", a.foreignUserID), "users", map[string]any{"blocked": true, "roles": []string{"curator"}}},
		{"PATCH", fmt.Sprintf("/teachers/%d", a.teacherID), "teachers", map[string]any{"phone": "+79007654321"}},
		{"PATCH", fmt.Sprintf("/students/%d", a.firstID), "students", map[string]any{"name": "Changed"}},
		{"PATCH", fmt.Sprintf("/parents/%d", p.ID), "parents", map[string]any{"phone": "+79007654321"}},
		{"POST", fmt.Sprintf("/students/%d/parents", a.firstID), "student_parents", map[string]any{"parent_id": p.ID, "relationship": "Мать", "is_primary": true}},
		{"PATCH", fmt.Sprintf("/programs/%d", g.ProgramID), "programs", map[string]any{"name": "Changed"}},
		{"PATCH", modulePath(g.ProgramID, m.ID), "program_modules", map[string]any{"position": 2}},
		{"PATCH", groupPath(a.groupID), "groups", map[string]any{"teacher_id": a.teacherID}},
		{"PATCH", enrollmentPath(a.groupID, g.Enrollments[0].ID), "group_enrollments", map[string]any{"ends_on": "2026-09-30"}},
		{"PATCH", lessonPath(l.ID), "lessons", map[string]any{"topic": "Changed"}},
		{"POST", lessonPath(l.ID) + "/complete", "lessons", nil},
		{"PUT", lessonPath(l.ID) + "/attendance", "attendance", marksBody(markValue(a.firstID, "present", "Private comment"))},
		{"PATCH", taskPath(k.ID), "tasks", map[string]any{"assignee_id": a.f.admin.ID}},
		{"PATCH", taskPath(k.ID) + "/status", "tasks", map[string]any{"status": "done"}},
	} {
		t.Run(test.path+test.method, func(t *testing.T) {
			before, audits := snapshot(test.table), snapshot("audit_logs")
			expect(t, a.admin, test.method, test.path, test.body, 500)
			if snapshot(test.table) != before || snapshot("audit_logs") != audits {
				t.Fatal("audit failure left a partial mutation")
			}
		})
	}
	// Failed user blocking/role update must not revoke an existing session.
	expect(t, a.foreign, "GET", "/auth/me", nil, 200)
}

func TestAuditTransferSecondEventFailureRollsBackBothEvents(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	g := groupCard(t, a.admin, a.groupID)
	target := groupCreate(t, a.admin, g.ProgramID, 10, "active")
	if _, err := a.f.pool.Exec(ctx, `CREATE FUNCTION fail_transfer_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.action='enrollment.transferred_in' THEN RAISE EXCEPTION 'injected second event failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER injected_transfer_failure BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION fail_transfer_audit()`); err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "POST", enrollmentPath(a.groupID, g.Enrollments[0].ID)+"/transfer", map[string]any{"target_group_id": target.ID, "transfers_on": a.base.Format("2006-01-02")}, 500)
	if groupCard(t, a.admin, a.groupID).Enrollments[0].EndsOn != nil || len(groupCard(t, a.admin, target.ID).Enrollments) != 0 {
		t.Fatal("failed transfer changed composition")
	}
	if auditPage(t, a.admin, "?action=enrollment.transferred_out").Total != 0 {
		t.Fatal("first transfer event survived failed second event")
	}
}
