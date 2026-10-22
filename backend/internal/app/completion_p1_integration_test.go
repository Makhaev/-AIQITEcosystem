package app_test

import (
	"aiq.local/core/internal/domain/people"
	"aiq.local/core/internal/modules/parents"
	"aiq.local/core/internal/modules/students"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	core "aiq.local/core"
	"aiq.local/core/internal/modules/lessons"
	"github.com/jackc/pgx/v5"
)

func TestCompletionContactMatchesAndOverviewAlias(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	first := parentCreate(t, admin, map[string]any{"name": "Одинаковое имя", "phone": "8 (999) 123-45-67", "email": "Match@Example.test"})
	second := parentCreate(t, admin, map[string]any{"name": "Другой представитель", "phone": "+7 999 123 45 67"})
	expect(t, admin, "PATCH", fmt.Sprintf("/parents/%d", second.ID), map[string]any{"status": "archived"}, 200)
	parentCreate(t, admin, map[string]any{"name": "Одинаковое имя", "email": "different@example.test"})
	var result people.Page[parents.ContactMatch]
	body := map[string]any{"phone": "89991234567", "email": "MATCH@EXAMPLE.TEST"}
	decode(t, expect(t, admin, "POST", "/parents/contact-matches?page_size=1", body, 200), &result)
	if result.Total != 2 || len(result.Items) != 1 || result.PageSize != 1 {
		t.Fatalf("matching/page: %+v", result)
	}
	decode(t, expect(t, admin, "POST", "/parents/contact-matches?page_size=1&page=2", body, 200), &result)
	if len(result.Items) != 1 {
		t.Fatal("second page missing")
	}
	body["exclude_id"] = first.ID
	var raw map[string]any
	decode(t, expect(t, admin, "POST", "/parents/contact-matches", body, 200), &raw)
	items := raw["items"].([]any)
	if len(items) != 1 {
		t.Fatal("self/name-only matches were not excluded")
	}
	item := items[0].(map[string]any)
	if item["status"] != "archived" || len(item) != 4 || item["phone"] != nil || item["email"] != nil {
		t.Fatal("match exposes contacts or omits archive")
	}
	if !reflect.DeepEqual(item["matched_fields"], []any{"phone"}) {
		t.Fatal("matched fields incorrect")
	}
	_, teacher := provision(t, f, admin, "matches-teacher@example.test", "teacher")
	expect(t, teacher, "POST", "/parents/contact-matches", body, 403)
	expect(t, admin, "POST", "/parents/contact-matches", map[string]any{}, 400)
	student := studentCreate(t, admin, map[string]any{"name": "Без даты"})
	path := fmt.Sprintf("/students/%d/", student.ID)
	var overview, alias map[string]any
	decode(t, expect(t, admin, "GET", path+"overview?from=2026-10-01&to=2026-10-31", nil, 200), &overview)
	decode(t, expect(t, admin, "GET", path+"360?from=2026-10-01&to=2026-10-31", nil, 200), &alias)
	if !reflect.DeepEqual(overview, alias) {
		t.Fatal("overview differs from alias")
	}
	if value, ok := overview["age"]; !ok || value != nil {
		t.Fatal("missing birth date must give age:null")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", student.ID), map[string]any{"birth_date": "2013-01-01"}, 200)
	decode(t, expect(t, admin, "GET", path+"overview", nil, 200), &overview)
	zone, _ := time.LoadLocation("Europe/Moscow")
	if overview["age"] != float64(time.Now().In(zone).Year()-2013) {
		t.Fatal("full age incorrect")
	}
	expect(t, teacher, "GET", path+"overview", nil, 404)
	expect(t, teacher, "GET", path+"360", nil, 404)
}

func TestCompletionSnapshotFreezeCorrectionAndAuditRollback(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	var programID int64
	if err := a.f.pool.QueryRow(ctx, `SELECT program_id FROM groups WHERE id=$1`, a.groupID).Scan(&programID); err != nil {
		t.Fatal(err)
	}
	module := moduleCreate(t, a.admin, programID, 1, "Модуль до переименования")
	body := lessonBody(a.groupID, a.teacherID, a.base.Add(-3*time.Hour).Format(time.RFC3339Nano), a.base.Add(-2*time.Hour).Format(time.RFC3339Nano))
	body["module_id"] = module.ID
	l := lessonCreate(t, a.admin, body)
	path := lessonPath(l.ID)
	expect(t, a.own, "PUT", path+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "late", "")), 204)
	var completed lessons.Lesson
	decode(t, expect(t, a.own, "POST", path+"/complete", nil, 200), &completed)
	if completed.SnapshotState != "captured" || completed.SnapshotCapturedAt == nil {
		t.Fatal("completion has no snapshot")
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/programs/%d", programID), map[string]any{"name": "Переименованная программа"}, 200)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/programs/%d/modules/%d", programID, module.ID), map[string]any{"name": "Переименованный модуль"}, 200)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/programs/%d", programID), map[string]any{"status": "archived"}, 200)
	var current lessons.Lesson
	decode(t, expect(t, a.admin, "GET", path, nil, 200), &current)
	if current.ProgramName != completed.ProgramName || current.ModuleName == nil || *current.ModuleName != *completed.ModuleName || current.Topic != completed.Topic {
		t.Fatal("reference rename changed historical snapshot")
	}
	var listed lessons.Page
	decode(t, expect(t, a.admin, "GET", "/lessons?status=completed", nil, 200), &listed)
	if len(listed.Items) != 1 || !reflect.DeepEqual(listed.Items[0], current) {
		t.Fatal("lesson list did not use historical snapshot")
	}
	journal := lessonJournal(t, a.own, l.ID)
	if len(journal.Items) != 2 || journalStudent(t, journal, a.firstID).Status != "present" {
		t.Fatal("reference archive changed journal")
	}
	decode(t, expect(t, a.own, "POST", path+"/complete", nil, 200), &current)
	if !reflect.DeepEqual(current, completed) {
		t.Fatal("repeat completion changed lesson or snapshot")
	}
	var card students.StudentOverview
	from, to := a.base.Add(-48*time.Hour).Format("2006-01-02"), a.base.Add(48*time.Hour).Format("2006-01-02")
	decode(t, expect(t, a.admin, "GET", fmt.Sprintf("/students/%d/overview?from=%s&to=%s", a.firstID, from, to), nil, 200), &card)
	if len(card.Attendance.Items) != 1 || card.Attendance.Items[0].ProgramName != completed.ProgramName || *card.Attendance.Items[0].ModuleName != *completed.ModuleName {
		t.Fatal("Student360 did not use snapshot")
	}
	expectAttendanceCode(t, a.admin, "PATCH", path, map[string]any{"topic": "Обход истории"}, 409, "snapshot_correction_required")
	correction := map[string]any{"program_name": "Историческая программа", "module_name": "Исторический модуль", "topic": "Исправленная тема", "reason": "Причина не должна попасть в audit"}
	expect(t, a.own, "POST", path+"/snapshot", correction, 403)
	expect(t, a.own, "GET", path+"/snapshot-history", nil, 403)
	expect(t, a.admin, "POST", path+"/snapshot", correction, 200)
	var history lessons.SnapshotPage
	decode(t, expect(t, a.admin, "GET", path+"/snapshot-history?page_size=1", nil, 200), &history)
	if history.Total != 1 || len(history.Items) != 1 || *history.Items[0].ProgramName != completed.ProgramName || *history.Items[0].Topic != completed.Topic || history.Items[0].ActorID == nil {
		t.Fatalf("old version not preserved: %+v", history)
	}
	if history.Items[0].Reason == nil || *history.Items[0].Reason != correction["reason"] || !history.Items[0].CapturedAt.Equal(*completed.SnapshotCapturedAt) {
		t.Fatal("correction reason or original capture time missing from protected history")
	}
	decode(t, expect(t, a.admin, "GET", path+"/snapshot-history?page=2&page_size=1", nil, 200), &history)
	if len(history.Items) != 0 || history.Total != 1 {
		t.Fatal("snapshot history pagination")
	}
	expect(t, a.admin, "GET", path+"/snapshot-history?page_size=101", nil, 400)
	decode(t, expect(t, a.admin, "POST", path+"/complete", nil, 200), &current)
	if current.ProgramName != "Историческая программа" || !current.SnapshotCapturedAt.Equal(*completed.SnapshotCapturedAt) {
		t.Fatal("repeat completion overwrote snapshot")
	}
	installCompletionAuditFailure(t, a.f, "lesson.snapshot_corrected")
	correction["topic"] = "Не сохранять"
	expect(t, a.admin, "POST", path+"/snapshot", correction, 500)
	decode(t, expect(t, a.admin, "GET", path, nil, 200), &current)
	decode(t, expect(t, a.admin, "GET", path+"/snapshot-history", nil, 200), &history)
	if current.Topic != "Исправленная тема" || history.Total != 1 {
		t.Fatal("failed audit left correction/version")
	}
	var safe string
	if err := a.f.pool.QueryRow(ctx, `SELECT row_to_json(a)::text FROM audit_logs a WHERE action='lesson.snapshot_corrected' AND object_id=$1`, l.ID).Scan(&safe); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(safe, "Причина не должна") || strings.Contains(safe, "Историческая программа") {
		t.Fatal("snapshot values/reason leaked into audit")
	}
}

func installCompletionAuditFailure(t *testing.T, f *fixture, action string) {
	t.Helper()
	// Test-only action literals; no application/user strings are interpolated.
	if _, err := f.pool.Exec(context.Background(), `CREATE FUNCTION reject_completion_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='`+action+`' THEN RAISE EXCEPTION 'test audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_completion_audit BEFORE INSERT ON audit_logs FOR EACH ROW EXECUTE FUNCTION reject_completion_audit()`); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionExplicitReschedulePreservesMarksAndRollsBack(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	path := lessonPath(l.ID)
	initial := lessonJournal(t, a.admin, l.ID)
	first := journalStudent(t, initial, a.firstID)
	second := journalStudent(t, initial, a.secondID)
	expect(t, a.own, "PUT", path+"/attendance", marksBody(markValue(a.firstID, "present", "Сохранить отметку"), markValue(a.secondID, "absent", "")), 204)
	newcomer := studentCreate(t, a.admin, map[string]any{"name": "Новый участник"})
	future := a.base.AddDate(0, 0, 2)
	var enrollmentID int64
	if err := a.f.pool.QueryRow(ctx, `INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES($1,$2,($3::timestamptz AT TIME ZONE 'Europe/Moscow')::date) RETURNING id`, a.groupID, newcomer.ID, future).Scan(&enrollmentID); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"starts_at": future, "ends_at": future.Add(time.Hour), "enrollment_ids": []int64{first.EnrollmentID, enrollmentID}, "reason": "Перенос с проверкой состава"}
	expect(t, a.admin, "PATCH", path, map[string]any{"starts_at": future, "ends_at": future.Add(time.Hour)}, 409)
	expect(t, a.own, "POST", path+"/reschedule", body, 403)
	expect(t, a.admin, "POST", path+"/reschedule", body, 200)
	after := lessonJournal(t, a.admin, l.ID)
	mark := journalStudent(t, after, a.firstID)
	if len(after.Items) != 2 || mark.ID != first.ID || mark.Status != "present" || mark.Comment != "Сохранить отметку" || journalStudent(t, after, newcomer.ID).Status != "unmarked" {
		t.Fatal("reschedule changed remaining marks/new participant")
	}
	var included bool
	var state string
	if err := a.f.pool.QueryRow(ctx, `SELECT included,status FROM attendance_records WHERE id=$1`, second.ID).Scan(&included, &state); err != nil || included || state != "absent" {
		t.Fatal("excluded marks discarded")
	}
	expectAttendanceCode(t, a.own, "PUT", path+"/attendance", marksBody(markValue(a.firstID, "late", "")), 409, "lesson_not_started")
	body["enrollment_ids"] = []int64{first.EnrollmentID, second.EnrollmentID, enrollmentID}
	expect(t, a.admin, "POST", path+"/reschedule", body, 200)
	if journalStudent(t, lessonJournal(t, a.admin, l.ID), a.secondID).ID != second.ID {
		t.Fatal("returning pupil created duplicate")
	}
	// Wrong participant must roll back the changed time as well as history/audit.
	bad := a.base.AddDate(0, 0, 3)
	body["starts_at"], body["ends_at"], body["enrollment_ids"] = bad, bad.Add(time.Hour), []int64{999999}
	expect(t, a.admin, "POST", path+"/reschedule", body, 400)
	var got lessons.Lesson
	decode(t, expect(t, a.admin, "GET", path, nil, 200), &got)
	if !got.StartsAt.Equal(future) {
		t.Fatal("invalid roster partially changed dates")
	}
	conflict := a.lesson(t, 96*time.Hour, 97*time.Hour)
	body["starts_at"], body["ends_at"], body["enrollment_ids"] = conflict.StartsAt, conflict.EndsAt, []int64{first.EnrollmentID}
	expect(t, a.admin, "POST", path+"/reschedule", body, 409)
	decode(t, expect(t, a.admin, "GET", path, nil, 200), &got)
	if !got.StartsAt.Equal(future) {
		t.Fatal("conflict partly moved lesson")
	}
	installCompletionAuditFailure(t, a.f, "lesson.rescheduled")
	body["starts_at"], body["ends_at"] = bad, bad.Add(time.Hour)
	expect(t, a.admin, "POST", path+"/reschedule", body, 500)
	if len(lessonJournal(t, a.admin, l.ID).Items) != 3 {
		t.Fatal("audit failure changed roster")
	}
	var count int
	if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM lesson_reschedules WHERE lesson_id=$1`, l.ID).Scan(&count); err != nil || count != 2 {
		t.Fatal("failed reschedule left history")
	}
}

func TestCompletionTaskLifecycleClosureAndCreatorAfterRoleChange(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	creator, owner := provision(t, f, admin, "lifecycle-leader@example.test", "leader")
	assignee, worker := provision(t, f, admin, "lifecycle-teacher@example.test", "teacher")
	body := taskBody(assignee.ID, time.Now().Add(time.Hour))
	body["status"] = "done"
	expect(t, owner, "POST", "/tasks", body, 400)
	body["status"] = "todo"
	k := taskCreate(t, owner, body)
	path := taskPath(k.ID)
	expect(t, admin, "PATCH", path, map[string]any{"status": "done"}, 409)
	expect(t, worker, "PATCH", path+"/status", map[string]any{"status": "done"}, 409)
	if taskGet(t, owner, k.ID).ClosedAt != nil {
		t.Fatal("new task closed")
	}
	expect(t, worker, "PATCH", path+"/status", map[string]any{"status": "in_progress"}, 200)
	expect(t, worker, "PATCH", path+"/status", map[string]any{"status": "done"}, 200)
	closed := taskGet(t, owner, k.ID)
	if closed.ClosedAt == nil {
		t.Fatal("done lacks closed_at")
	}
	var before, after int
	f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_type='task' AND object_id=$1`, k.ID).Scan(&before)
	expect(t, worker, "PATCH", path+"/status", map[string]any{"status": "done"}, 200)
	f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_type='task' AND object_id=$1`, k.ID).Scan(&after)
	if before != after || !taskGet(t, owner, k.ID).ClosedAt.Equal(*closed.ClosedAt) {
		t.Fatal("repeated status changed closure/audit")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", creator.ID), map[string]any{"roles": []string{"curator"}}, 200)
	// Role changes revoke sessions; authenticate the active creator again.
	owner = f.browser(t)
	owner.login(t, creator.Email, "TestOnly-New-Account-9372")
	expect(t, owner, "GET", path, nil, 200)
	expect(t, owner, "PATCH", path, map[string]any{"title": "Forbidden edit"}, 403)
	expect(t, worker, "PATCH", path+"/status", map[string]any{"status": "in_progress"}, 403)
	expect(t, owner, "PATCH", path+"/status", map[string]any{"status": "in_progress"}, 200)
	if taskGet(t, owner, k.ID).ClosedAt != nil {
		t.Fatal("reopen kept closure")
	}
	expect(t, worker, "PATCH", path+"/status", map[string]any{"status": "done"}, 200)
	if !taskGet(t, owner, k.ID).ClosedAt.After(*closed.ClosedAt) {
		t.Fatal("second closure did not get new time")
	}
	closeTime := taskGet(t, owner, k.ID).ClosedAt
	expect(t, owner, "PATCH", path+"/status", map[string]any{"status": "cancelled"}, 200)
	if !taskGet(t, owner, k.ID).ClosedAt.Equal(*closeTime) {
		t.Fatal("closed-to-closed reset timestamp")
	}
	expect(t, owner, "PATCH", path+"/status", map[string]any{"status": "in_progress"}, 409)
	_, err := f.pool.Exec(ctx, `UPDATE tasks SET status='in_progress' WHERE id=$1`, k.ID)
	dbConstraint(t, err, "23514", "tasks_transition")
}

func TestCompletionMigrationsKeepUnknownHistoricalValues(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var currentSchema string
	if err = tx.QueryRow(ctx, `SELECT current_schema()`).Scan(&currentSchema); err != nil {
		t.Fatal(err)
	}
	legacySchema := pgx.Identifier{currentSchema + "_legacy"}.Sanitize()
	if _, err = tx.Exec(ctx, `CREATE SCHEMA `+legacySchema+`; SET LOCAL search_path TO `+legacySchema); err != nil {
		t.Fatal(err)
	}
	entries, err := core.Migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "011" {
			continue
		}
		sql, e := core.Migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, string(sql)); e != nil {
			t.Fatalf("legacy migration %s: %v", entry.Name(), e)
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO users(email,name,password_hash) VALUES('legacy@example.test','Вымышленный автор','unused');
 INSERT INTO programs(name,name_search,status) VALUES('Программа','программа','active');
 INSERT INTO teachers(name,name_search) VALUES('Педагог','педагог');
 INSERT INTO groups(name,name_search,program_id,capacity,status) VALUES('Группа','группа',1,10,'active');
 INSERT INTO lessons(group_id,program_id,teacher_id,topic,topic_search,starts_at,ends_at,status) VALUES(1,1,1,'Старая тема','старая тема','2020-01-01T08:00:00Z','2020-01-01T09:00:00Z','completed');
 INSERT INTO tasks(title,title_search,description,creator_id,assignee_id,due_at,status) VALUES('Старая задача','старая задача','Результат',1,1,'2020-01-01','done')`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"011_tasks_lifecycle.sql", "012_lesson_history.sql", "013_snapshot_correction_history.sql", "014_reschedule_reason.sql", "015_task_closure_policy.sql"} {
		if name == "013_snapshot_correction_history.sql" {
			if _, err = tx.Exec(ctx, `INSERT INTO lesson_snapshot_versions(lesson_id,actor_id,state) VALUES(1,1,'legacy_unknown')`); err != nil {
				t.Fatal(err)
			}
		}
		if name == "014_reschedule_reason.sql" {
			if _, err = tx.Exec(ctx, `INSERT INTO lesson_reschedules(lesson_id,actor_id,old_starts_at,old_ends_at,starts_at,ends_at,previous_roster,roster) VALUES(1,1,'2020-01-01T08:00:00Z','2020-01-01T09:00:00Z','2020-01-02T08:00:00Z','2020-01-02T09:00:00Z','{10}','{20}')`); err != nil {
				t.Fatal(err)
			}
		}
		sql, e := core.Migrations.ReadFile("migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, string(sql)); e != nil {
			t.Fatal(e)
		}
	}
	var closure *time.Time
	var state string
	var snapshot *string
	if err = tx.QueryRow(ctx, `SELECT closed_at FROM tasks WHERE id=1`).Scan(&closure); err != nil || closure != nil {
		t.Fatal("invented legacy closure")
	}
	if err = tx.QueryRow(ctx, `SELECT snapshot_state,snapshot_program_name FROM lessons WHERE id=1`).Scan(&state, &snapshot); err != nil || state != "legacy_unknown" || snapshot != nil {
		t.Fatal("invented historical snapshot")
	}
	var oldVersions int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM lesson_snapshot_versions WHERE lesson_id=1 AND state='legacy_unknown' AND reason IS NULL AND program_name IS NULL`).Scan(&oldVersions); err != nil || oldVersions != 1 {
		t.Fatal("migration discarded or invented old snapshot history")
	}
	var oldReschedules int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM lesson_reschedules WHERE lesson_id=1 AND previous_roster='{10}' AND roster='{20}' AND reason IS NULL`).Scan(&oldReschedules); err != nil || oldReschedules != 1 {
		t.Fatal("migration discarded old reschedule history or invented a reason")
	}
	var raw json.RawMessage
	if err = tx.QueryRow(ctx, `SELECT to_jsonb(l) FROM lessons l WHERE id=1`).Scan(&raw); err != nil || !strings.Contains(string(raw), "Старая тема") {
		t.Fatal("migration discarded lesson")
	}
}
