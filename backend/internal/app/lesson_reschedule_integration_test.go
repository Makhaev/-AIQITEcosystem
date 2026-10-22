package app_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/platform/access"
)

func rescheduleBody(start, end any, ids []int64) map[string]any {
	return map[string]any{"starts_at": start, "ends_at": end, "enrollment_ids": ids, "reason": "  Согласовано изменение учебного дня  "}
}

func rescheduleState(t *testing.T, f *fixture, id int64) string {
	t.Helper()
	var value string
	err := f.pool.QueryRow(context.Background(), `SELECT jsonb_build_object(
 'lesson',to_jsonb(l),
 'attendance',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM attendance_records a WHERE lesson_id=l.id),
 'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM lesson_reschedules h WHERE lesson_id=l.id),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM audit_logs a WHERE object_type='lesson' AND object_id=l.id))::text
 FROM lessons l WHERE id=$1`, id).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRescheduleMoscowRosterAndHistory(t *testing.T) {
	a := setupAttendance(t)
	l := lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, "2024-06-10T21:30:00Z", "2024-06-10T22:30:00Z"))
	path := lessonPath(l.ID)
	initial := lessonJournal(t, a.admin, l.ID)
	first, second := journalStudent(t, initial, a.firstID), journalStudent(t, initial, a.secondID)
	expect(t, a.own, "PUT", path+"/attendance", marksBody(markValue(a.firstID, "present", "Сохранить комментарий"), markValue(a.secondID, "absent", "История исключения")), 204)
	// Different UTC dates can still be the same Moscow day: ordinary PATCH is allowed.
	expect(t, a.admin, "PATCH", path, map[string]any{"starts_at": "2024-06-11T10:00:00Z", "ends_at": "2024-06-11T11:00:00Z"}, 200)
	before := rescheduleState(t, a.f, l.ID)
	rejected := expect(t, a.admin, "PATCH", path, map[string]any{"starts_at": "2024-06-11T21:00:00Z", "ends_at": "2024-06-11T22:00:00Z"}, 409)
	if !strings.Contains(string(rejected), "перенос с обновлением состава") || rescheduleState(t, a.f, l.ID) != before {
		t.Fatal("ordinary PATCH must reject a new Moscow day without changing anything")
	}
	// A new enrollment for the same pupil must reuse the saved attendance row.
	expect(t, a.admin, "PATCH", fmt.Sprintf("%s/enrollments/%d", groupPath(a.groupID), first.EnrollmentID), map[string]any{"ends_on": "2024-06-11"}, 200)
	var nextEnrollment struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, a.admin, "POST", groupPath(a.groupID)+"/enrollments", map[string]any{"student_id": a.firstID, "starts_on": "2024-06-12"}, 201), &nextEnrollment)
	newcomer := studentCreate(t, a.admin, map[string]any{"name": "Новый участник"})
	var newEnrollment struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, a.admin, "POST", groupPath(a.groupID)+"/enrollments", map[string]any{"student_id": newcomer.ID, "starts_on": "2024-06-12"}, 201), &newEnrollment)
	expect(t, a.admin, "PATCH", fmt.Sprintf("%s/enrollments/%d", groupPath(a.groupID), newEnrollment.ID), map[string]any{"ends_on": "2024-06-12"}, 200)
	ids := []int64{nextEnrollment.ID, newEnrollment.ID}
	body := rescheduleBody("2024-06-11T21:00:00Z", "2024-06-11T22:00:00Z", ids)
	expect(t, a.admin, "POST", path+"/reschedule", body, 200)
	after := lessonJournal(t, a.admin, l.ID)
	kept, added := journalStudent(t, after, a.firstID), journalStudent(t, after, newcomer.ID)
	if after.LessonDate != "2024-06-12" || len(after.Items) != 2 || kept.ID != first.ID || kept.EnrollmentID != nextEnrollment.ID || kept.Status != "present" || kept.Comment != "Сохранить комментарий" || added.Status != "unmarked" || added.Comment != "" {
		t.Fatalf("reschedule lost marks or used the UTC enrollment day: %+v", after)
	}
	var excluded bool
	var status, comment string
	if err := a.f.pool.QueryRow(context.Background(), `SELECT included,status,comment FROM attendance_records WHERE id=$1`, second.ID).Scan(&excluded, &status, &comment); err != nil || excluded || status != "absent" || comment != "История исключения" {
		t.Fatal("excluded participant history lost")
	}
	var oldIDs, newIDs []int64
	var reason string
	if err := a.f.pool.QueryRow(context.Background(), `SELECT previous_roster,roster,reason FROM lesson_reschedules WHERE lesson_id=$1`, l.ID).Scan(&oldIDs, &newIDs, &reason); err != nil || !reflect.DeepEqual(oldIDs, []int64{first.EnrollmentID, second.EnrollmentID}) || !reflect.DeepEqual(newIDs, ids) || reason != strings.TrimSpace(body["reason"].(string)) {
		t.Fatalf("reschedule history incomplete: %v %v %q %v", oldIDs, newIDs, reason, err)
	}
	var audit string
	if err := a.f.pool.QueryRow(context.Background(), `SELECT to_jsonb(a)::text FROM audit_logs a WHERE action='lesson.rescheduled' AND object_id=$1`, l.ID).Scan(&audit); err != nil || strings.Contains(audit, reason) {
		t.Fatal("reschedule reason must stay outside audit")
	}
	for _, query := range []string{`UPDATE lesson_reschedules SET reason='changed' WHERE lesson_id=$1`, `DELETE FROM lesson_reschedules WHERE lesson_id=$1`} {
		if _, err := a.f.pool.Exec(context.Background(), query, l.ID); err == nil {
			t.Fatal("reschedule history was mutable")
		}
	}
	// Reintroducing a previously excluded pupil restores their original mark and ID.
	body["enrollment_ids"] = []int64{nextEnrollment.ID, second.EnrollmentID, newEnrollment.ID}
	expect(t, a.admin, "POST", path+"/reschedule", body, 200)
	restored := journalStudent(t, lessonJournal(t, a.admin, l.ID), a.secondID)
	if restored.ID != second.ID || restored.Status != "absent" || restored.Comment != "История исключения" {
		t.Fatal("returning pupil lost the saved attendance row")
	}
	// A fully empty roster is intentional and retains excluded rows for history.
	body["enrollment_ids"] = []int64{}
	expect(t, a.admin, "POST", path+"/reschedule", body, 200)
	if len(lessonJournal(t, a.admin, l.ID).Items) != 0 {
		t.Fatal("explicit empty roster was ignored")
	}
	expect(t, a.admin, "POST", path+"/complete", nil, 200)
	card := overview(t, a.admin, a.secondID, "?from=2024-06-12&to=2024-06-12")
	if len(card.Attendance.Items) != 0 || card.Attendance.Percent != nil || card.Attendance.Absent != 0 {
		t.Fatal("excluded attendance affected Student 360 statistics")
	}
	var count int
	if err := a.f.pool.QueryRow(context.Background(), `SELECT count(*) FROM attendance_records WHERE lesson_id=$1`, l.ID).Scan(&count); err != nil || count != 3 {
		t.Fatal("attendance rows were duplicated or discarded")
	}
}

func TestRescheduleValidationPermissionsAndRollback(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	path := lessonPath(l.ID) + "/reschedule"
	var enrollmentID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT id FROM group_enrollments WHERE group_id=$1 AND student_id=$2`, a.groupID, a.firstID).Scan(&enrollmentID); err != nil {
		t.Fatal(err)
	}
	foreignGroup := groupCreate(t, a.admin, l.ProgramID, 10, "active")
	var foreignEnrollment struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, a.admin, "POST", groupPath(foreignGroup.ID)+"/enrollments", map[string]any{"student_id": a.firstID, "starts_on": "2000-01-01"}, 201), &foreignEnrollment)
	future := a.base.AddDate(0, 0, 2)
	valid := func() map[string]any { return rescheduleBody(future, future.Add(time.Hour), []int64{enrollmentID}) }
	before := rescheduleState(t, a.f, l.ID)
	for _, tc := range []struct {
		name, field string
		value       any
	}{
		{"missing roster", "enrollment_ids", nil}, {"unknown enrollment", "enrollment_ids", []int64{999999}},
		{"foreign group enrollment", "enrollment_ids", []int64{foreignEnrollment.ID}}, {"missing reason", "reason", nil},
		{"duplicate pupil", "enrollment_ids", []int64{enrollmentID, enrollmentID}}, {"invalid id", "enrollment_ids", []int64{0}},
		{"blank reason", "reason", "  "}, {"long reason", "reason", strings.Repeat("я", 1001)},
		{"missing start", "starts_at", nil}, {"missing end", "ends_at", nil}, {"reversed interval", "ends_at", future.Add(-time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := valid()
			body[tc.field] = tc.value
			expect(t, a.admin, "POST", path, body, 400)
			if rescheduleState(t, a.f, l.ID) != before {
				t.Fatal("invalid request persisted dates, initialized roster, history or audit")
			}
		})
	}
	_, leader := provision(t, a.f, a.admin, "reschedule-leader@example.test", "leader")
	_, curator := provision(t, a.f, a.admin, "reschedule-curator@example.test", "curator")
	for _, b := range []*browser{a.own, a.foreign, leader, curator} {
		expect(t, b, "POST", path, valid(), 403)
	}
	expect(t, a.f.browser(t), "POST", path, valid(), 401)
	csrf := a.admin.csrf
	a.admin.csrf = ""
	expect(t, a.admin, "POST", path, valid(), 403)
	a.admin.csrf = csrf
	if rescheduleState(t, a.f, l.ID) != before {
		t.Fatal("unauthorized reschedule changed state")
	}
	conflict := a.lesson(t, 96*time.Hour, 97*time.Hour)
	body := rescheduleBody(conflict.StartsAt, conflict.EndsAt, []int64{enrollmentID})
	expect(t, a.admin, "POST", path, body, 409)
	if rescheduleState(t, a.f, l.ID) != before {
		t.Fatal("schedule conflict partially saved reschedule")
	}
	for _, action := range []string{"lesson.roster_corrected", "lesson.rescheduled"} {
		installCompletionAuditFailure(t, a.f, action)
		expect(t, a.admin, "POST", path, valid(), 500)
		if rescheduleState(t, a.f, l.ID) != before {
			t.Fatalf("%s failure did not roll back entire request", action)
		}
		if _, err := a.f.pool.Exec(context.Background(), `DROP TRIGGER reject_completion_audit ON audit_logs; DROP FUNCTION reject_completion_audit()`); err != nil {
			t.Fatal(err)
		}
	}
	expect(t, a.admin, "POST", path, valid(), 200)
	for _, b := range []*browser{a.admin, a.own} {
		expectAttendanceCode(t, b, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, "present", "")), 409, "lesson_not_started")
	}
	// The transaction-local bypass must not leak into ordinary requests on pooled connections.
	expectAttendanceCode(t, a.admin, "PATCH", lessonPath(l.ID), map[string]any{"starts_at": future.AddDate(0, 0, 1), "ends_at": future.AddDate(0, 0, 1).Add(time.Hour)}, 409, "journal_frozen")
	cancelled := a.lesson(t, 144*time.Hour, 145*time.Hour)
	expect(t, a.admin, "POST", lessonPath(cancelled.ID)+"/cancel", nil, 200)
	expectAttendanceCode(t, a.admin, "POST", lessonPath(cancelled.ID)+"/reschedule", valid(), 409, "reschedule_not_scheduled")
	completed := a.lesson(t, -6*time.Hour, -5*time.Hour)
	finishOverviewLesson(t, a, completed, "present")
	expectAttendanceCode(t, a.admin, "POST", lessonPath(completed.ID)+"/reschedule", valid(), 409, "reschedule_not_scheduled")
}

func TestRescheduleConcurrentOverlap(t *testing.T) {
	for _, shared := range []string{"group", "teacher"} {
		t.Run(shared, func(t *testing.T) {
			f := setup(t)
			admin := f.adminBrowser(t)
			p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
			g1 := groupCreate(t, admin, p.ID, 10, "active")
			g2 := groupCreate(t, admin, p.ID, 10, "active")
			t1 := teacherCreate(t, admin, map[string]any{"name": "First"})
			t2 := teacherCreate(t, admin, map[string]any{"name": "Second"})
			if shared == "group" {
				g2 = g1
			} else {
				t2 = t1
			}
			first := lessonCreate(t, admin, lessonBody(g1.ID, t1.ID, "2028-01-01T07:00:00Z", "2028-01-01T08:00:00Z"))
			second := lessonCreate(t, admin, lessonBody(g2.ID, t2.ID, "2028-01-01T09:00:00Z", "2028-01-01T10:00:00Z"))
			ids := []int64{first.ID, second.ID}
			before := make([]string, 2)
			for i, id := range ids {
				lessonJournal(t, admin, id)
				before[i] = rescheduleState(t, f, id)
			}
			body := rescheduleBody("2028-01-02T10:00:00Z", "2028-01-02T11:00:00Z", []int64{})
			gate := make(chan struct{})
			statuses := make([]int, 2)
			var wg sync.WaitGroup
			for i, id := range ids {
				wg.Add(1)
				go func(i int, id int64) {
					defer wg.Done()
					<-gate
					statuses[i], _ = admin.do(t, "POST", lessonPath(id)+"/reschedule", body)
				}(i, id)
			}
			close(gate)
			wg.Wait()
			if !((statuses[0] == 200 && statuses[1] == 409) || (statuses[0] == 409 && statuses[1] == 200)) {
				t.Fatalf("concurrent %s overlap: %v", shared, statuses)
			}
			for i, status := range statuses {
				if status == 409 && rescheduleState(t, f, ids[i]) != before[i] {
					t.Fatal("losing reschedule persisted partial changes")
				}
			}
			var overlaps int
			if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM lessons a JOIN lessons b ON a.id<b.id AND (a.group_id=b.group_id OR a.teacher_id=b.teacher_id) AND tstzrange(a.starts_at,a.ends_at,'[)') && tstzrange(b.starts_at,b.ends_at,'[)') WHERE a.status<>'cancelled' AND b.status<>'cancelled'`).Scan(&overlaps); err != nil || overlaps != 0 {
				t.Fatal("concurrent reschedule created overlapping lessons")
			}
		})
	}
}

func TestRescheduleRechecksEnrollmentAfterWaiting(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	j := lessonJournal(t, a.admin, l.ID)
	enrollmentID := journalStudent(t, j, a.firstID).EnrollmentID
	before := rescheduleState(t, a.f, l.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gate, err := a.f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Rollback(context.Background())
	if err = access.LockManagement(ctx, gate); err != nil {
		t.Fatal(err)
	}
	future := a.base.AddDate(0, 0, 2)
	result := make(chan int, 1)
	go func() {
		status, _ := a.admin.do(t, "POST", lessonPath(l.ID)+"/reschedule", rescheduleBody(future, future.Add(time.Hour), []int64{enrollmentID}))
		result <- status
	}()
	waitTaskManagementLock(t, ctx, a.f, gate)
	if _, err = gate.Exec(ctx, `UPDATE group_enrollments SET ends_on=($2::timestamptz AT TIME ZONE 'Europe/Moscow')::date WHERE id=$1`, enrollmentID, a.base); err != nil {
		t.Fatal(err)
	}
	if err = gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-result:
		if status != 400 {
			t.Fatalf("stale enrollment accepted: %d", status)
		}
	case <-ctx.Done():
		t.Fatal("reschedule did not resume")
	}
	if rescheduleState(t, a.f, l.ID) != before {
		t.Fatal("stale enrollment reschedule changed lesson/history")
	}
}
