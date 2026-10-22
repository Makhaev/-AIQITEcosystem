package app_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"aiq.local/core/internal/modules/lessons"
)

func TestLessonSnapshotCompletionAuditAtomicity(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	path := lessonPath(l.ID)
	expect(t, a.admin, "PUT", path+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "late", "")), 204)
	ctx := context.Background()
	state := func() string {
		t.Helper()
		var value string
		if err := a.f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
 'lesson',to_jsonb(l),'attendance',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM attendance_records a WHERE lesson_id=l.id),
 'audit_count',(SELECT count(*) FROM audit_logs),'versions',(SELECT count(*) FROM lesson_snapshot_versions))::text
 FROM lessons l WHERE id=$1`, l.ID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := state()
	installCompletionAuditFailure(t, a.f, "lesson.completed")
	expect(t, a.own, "POST", path+"/complete", nil, 500)
	if state() != before {
		t.Fatal("failed completion audit persisted status, snapshot, journal or audit")
	}
	expect(t, a.admin, "PATCH", path, map[string]any{"status": "completed"}, 500)
	if state() != before {
		t.Fatal("PATCH completion bypassed atomicity")
	}
	if _, err := a.f.pool.Exec(ctx, `DROP TRIGGER reject_completion_audit ON audit_logs`); err != nil {
		t.Fatal(err)
	}
	var completed lessons.Lesson
	decode(t, expect(t, a.own, "POST", path+"/complete", nil, 200), &completed)
	if completed.SnapshotState != "captured" || completed.SnapshotCapturedAt == nil || completed.ModuleName != nil || completed.ProgramName != l.ProgramName || completed.Topic != l.Topic {
		t.Fatalf("first successful completion did not capture optional-module snapshot: %+v", completed)
	}
	before = state()
	expect(t, a.own, "POST", path+"/complete", nil, 200)
	if state() != before {
		t.Fatal("idempotent completion changed snapshot, timestamp or audit")
	}
}

func TestLessonSnapshotCorrectionProtection(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	path := lessonPath(l.ID)
	finishOverviewLesson(t, a, l, "present")
	_, leader := provision(t, a.f, a.admin, "snapshot-leader@example.test", "leader")
	_, curator := provision(t, a.f, a.admin, "snapshot-curator@example.test", "curator")
	correction := map[string]any{"program_name": "Уточнённая программа", "module_name": nil, "topic": "Уточнённая тема", "reason": "  Проверено по учебному плану; private@example.test  "}
	for _, viewer := range []*browser{a.own, a.foreign, leader, curator} {
		expect(t, viewer, "POST", path+"/snapshot", correction, 403)
		expect(t, viewer, "GET", path+"/snapshot-history", nil, 403)
	}
	for _, reason := range []any{nil, "", "   ", strings.Repeat("я", 1001)} {
		invalid := map[string]any{"program_name": "Программа", "module_name": nil, "topic": "Тема", "reason": reason}
		expect(t, a.admin, "POST", path+"/snapshot", invalid, 400)
	}
	expect(t, a.admin, "PATCH", path, map[string]any{"snapshot_topic": "Обход"}, 400)
	expect(t, a.admin, "PATCH", path, map[string]any{"status": "scheduled"}, 200)
	expectAttendanceCode(t, a.admin, "PATCH", path, map[string]any{"topic": "Обход после открытия"}, 409, "snapshot_correction_required")
	module := moduleCreate(t, a.admin, l.ProgramID, 1, "Другой модуль")
	expectAttendanceCode(t, a.admin, "PATCH", path, map[string]any{"module_id": module.ID}, 409, "snapshot_correction_required")
	expect(t, a.admin, "POST", path+"/snapshot", correction, 409)
	expect(t, a.admin, "POST", path+"/complete", nil, 200)
	expect(t, a.admin, "POST", path+"/snapshot", correction, 200)
	var history lessons.SnapshotPage
	decode(t, expect(t, a.admin, "GET", path+"/snapshot-history", nil, 200), &history)
	if history.Total != 1 || history.Items[0].Reason == nil || *history.Items[0].Reason != strings.TrimSpace(correction["reason"].(string)) {
		t.Fatalf("reason not preserved in protected history: %+v", history)
	}
	ctx := context.Background()
	for _, query := range []string{`UPDATE lesson_snapshot_versions SET topic='tampered' WHERE lesson_id=$1`, `DELETE FROM lesson_snapshot_versions WHERE lesson_id=$1`} {
		if _, err := a.f.pool.Exec(ctx, query, l.ID); err == nil {
			t.Fatal("snapshot history allowed mutation")
		}
	}
	var fields []string
	var audit string
	if err := a.f.pool.QueryRow(ctx, `SELECT changed_fields,to_jsonb(a)::text FROM audit_logs a WHERE action='lesson.snapshot_corrected' AND object_id=$1`, l.ID).Scan(&fields, &audit); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fields, []string{"program_name", "topic"}) {
		t.Fatalf("audit must identify only changed fields: %v", fields)
	}
	for _, private := range []string{"Уточнённая программа", "Уточнённая тема", "private@example.test", "Проверено по учебному плану"} {
		if strings.Contains(audit, private) {
			t.Fatal("historical text, contact or reason leaked into audit")
		}
	}
	// History remains admin-only even after a successful correction.
	expect(t, leader, "GET", path+"/snapshot-history", nil, 403)
}

func TestLessonSnapshotLegacyUnknownReadAndCorrection(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	path := lessonPath(l.ID)
	expect(t, a.admin, "PUT", path+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "late", "")), 204)
	// Model a completed row backfilled by migration 012, with no reliable snapshot.
	if _, err := a.f.pool.Exec(context.Background(), `UPDATE lessons SET status='completed',snapshot_state='legacy_unknown' WHERE id=$1`, l.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/programs/%d", l.ProgramID), map[string]any{"name": "Текущее название"}, 200)
	var current lessons.Lesson
	decode(t, expect(t, a.admin, "POST", path+"/complete", nil, 200), &current)
	if current.SnapshotState != "legacy_unknown" || current.SnapshotCapturedAt != nil || current.ProgramName != "Текущее название" {
		t.Fatalf("legacy read invented historical values: %+v", current)
	}
	card := overview(t, a.admin, a.firstID, "?from=2000-01-01&to=2099-12-31")
	if len(card.Attendance.Items) != 1 || card.Attendance.Items[0].SnapshotState != "legacy_unknown" {
		t.Fatal("Student 360 lost unknown-history marker")
	}
	correction := map[string]any{"program_name": "Подтверждённая программа", "module_name": nil, "topic": "Подтверждённая тема", "reason": "Восстановлено по архиву"}
	decode(t, expect(t, a.admin, "POST", path+"/snapshot", correction, 200), &current)
	if current.SnapshotState != "captured" || current.SnapshotCapturedAt == nil {
		t.Fatal("explicit legacy correction did not create snapshot")
	}
	var history lessons.SnapshotPage
	decode(t, expect(t, a.admin, "GET", path+"/snapshot-history", nil, 200), &history)
	if history.Total != 1 || history.Items[0].State != "legacy_unknown" || history.Items[0].ProgramName != nil || history.Items[0].Topic != nil || history.Items[0].CapturedAt != nil {
		t.Fatalf("legacy previous version invented values: %+v", history)
	}
}
