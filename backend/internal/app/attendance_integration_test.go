package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/modules/lessons"
	"github.com/jackc/pgx/v5"
)

type attendanceSetup struct {
	f                                                    *fixture
	admin, own, foreign                                  *browser
	groupID, teacherID, foreignUserID, firstID, secondID int64
	base                                                 time.Time
}

func setupAttendance(t *testing.T) attendanceSetup {
	t.Helper()
	f := setup(t)
	admin := f.adminBrowser(t)
	teacherUser, own := provision(t, f, admin, "attendance-own@example.test", "teacher")
	foreignUser, foreign := provision(t, f, admin, "attendance-foreign@example.test", "teacher")
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Own teacher", "user_id": teacherUser.ID})
	first := studentCreate(t, admin, map[string]any{"name": "First"})
	second := studentCreate(t, admin, map[string]any{"name": "Second"})
	for _, studentID := range []int64{first.ID, second.ID} {
		expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": studentID, "starts_on": "2000-01-01"}, 201)
	}
	return attendanceSetup{f: f, admin: admin, own: own, foreign: foreign, groupID: g.ID, teacherID: teacher.ID, foreignUserID: foreignUser.ID, firstID: first.ID, secondID: second.ID, base: time.Now().UTC().Truncate(time.Microsecond)}
}
func (a attendanceSetup) lesson(t *testing.T, start, end time.Duration) lessons.Lesson {
	t.Helper()
	return lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, a.base.Add(start).Format(time.RFC3339Nano), a.base.Add(end).Format(time.RFC3339Nano)))
}
func marksBody(items ...map[string]any) map[string]any { return map[string]any{"items": items} }
func markValue(studentID int64, status, comment string) map[string]any {
	return map[string]any{"student_id": studentID, "status": status, "comment": comment}
}
func journalStudent(t *testing.T, j lessons.Journal, studentID int64) lessons.Participant {
	t.Helper()
	for _, p := range j.Items {
		if p.StudentID == studentID {
			return p
		}
	}
	t.Fatalf("student %d missing from journal", studentID)
	return lessons.Participant{}
}
func expectAttendanceCode(t *testing.T, b *browser, method, path string, body any, status int, code string) {
	t.Helper()
	var response struct {
		Code string `json:"code"`
	}
	decode(t, expect(t, b, method, path, body, status), &response)
	if response.Code != code {
		t.Fatalf("want %s, got %s", code, response.Code)
	}
}
func TestAttendanceRepeatedBulkSaveValidationAndRollback(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	path := lessonPath(l.ID) + "/attendance"
	original := lessonJournal(t, a.own, l.ID)
	first := journalStudent(t, original, a.firstID)
	second := journalStudent(t, original, a.secondID)
	for _, status := range []string{"unmarked", "present", "absent", "excused", "late"} {
		expect(t, a.own, "PUT", path, marksBody(markValue(a.firstID, status, "  Комментарий  "), markValue(a.secondID, "present", "")), 204)
		actual := journalStudent(t, lessonJournal(t, a.admin, l.ID), a.firstID)
		if actual.ID != first.ID || actual.Status != status || actual.Comment != "Комментарий" {
			t.Fatalf("repeat update: %+v", actual)
		}
	}
	expect(t, a.own, "PUT", path, marksBody(markValue(a.firstID, "present", strings.Repeat("😀", 500))), 204)
	expect(t, a.own, "PUT", path, map[string]any{"items": []map[string]any{{"student_id": a.secondID, "status": "excused"}}}, 204)
	baseline := lessonJournal(t, a.admin, l.ID)
	originalFirst := journalStudent(t, baseline, a.firstID)
	ctx := context.Background()
	var auditCount int
	if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='attendance.updated' AND object_id=$1`, l.ID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	// The first UPDATE has executed when the invalid pupil is encountered. Both
	// grade changes and trigger-updated lesson timestamps must still roll back.
	expect(t, a.own, "PUT", path, marksBody(markValue(a.firstID, "absent", "Must roll back"), markValue(99999, "late", "")), 400)
	for _, body := range []any{marksBody(markValue(a.firstID, "absent", strings.Repeat("Я", 501))), marksBody(markValue(a.firstID, "unknown", "")), marksBody(markValue(a.firstID, "late", ""), markValue(a.firstID, "present", "")), map[string]any{"items": nil}} {
		expect(t, a.own, "PUT", path, body, 400)
	}
	after := lessonJournal(t, a.admin, l.ID)
	actual := journalStudent(t, after, a.firstID)
	if actual != originalFirst || journalStudent(t, after, a.secondID).ID != second.ID || len(after.Items) != 2 {
		t.Fatal("bulk error changed persisted grades or identities")
	}
	var count int
	if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='attendance.updated' AND object_id=$1`, l.ID).Scan(&count); err != nil || count != auditCount {
		t.Fatalf("failed bulk save wrote audit: %d %v", count, err)
	}
	_, err := a.f.pool.Exec(ctx, `INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id) VALUES($1,$2,$3,$4)`, l.ID, a.groupID, first.EnrollmentID, a.firstID)
	dbConstraint(t, err, "23505", "lesson_participants_lesson_student_unique")
}
func TestAttendanceTimingCompletionPermissionsAndCancellation(t *testing.T) {
	a := setupAttendance(t)
	future := a.lesson(t, time.Hour, 2*time.Hour)
	futurePath := lessonPath(future.ID)
	for _, b := range []*browser{a.admin, a.own} {
		expectAttendanceCode(t, b, "PUT", futurePath+"/attendance", marksBody(markValue(a.firstID, "present", "")), 409, "lesson_not_started")
		expectAttendanceCode(t, b, "POST", futurePath+"/complete", nil, 409, "lesson_not_ended")
	}
	active := a.lesson(t, -time.Hour, time.Hour)
	expect(t, a.own, "PUT", lessonPath(active.ID)+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "late", "")), 204)
	expectAttendanceCode(t, a.own, "POST", lessonPath(active.ID)+"/complete", nil, 409, "lesson_not_ended")
	expectAttendanceCode(t, a.admin, "PATCH", lessonPath(active.ID), map[string]any{"status": "completed"}, 409, "lesson_not_ended")
	body := lessonBody(a.groupID, a.teacherID, a.base.Add(3*time.Hour).Format(time.RFC3339Nano), a.base.Add(4*time.Hour).Format(time.RFC3339Nano))
	body["status"] = "completed"
	expectAttendanceCode(t, a.admin, "POST", "/lessons", body, 409, "lesson_not_ended")
	past := a.lesson(t, -3*time.Hour, -2*time.Hour)
	pastPath := lessonPath(past.ID)
	expectAttendanceCode(t, a.own, "POST", pastPath+"/complete", nil, 409, "attendance_unmarked")
	expectAttendanceCode(t, a.admin, "PATCH", pastPath, map[string]any{"status": "completed", "topic": "Must roll back"}, 409, "attendance_unmarked")
	var unchanged lessons.Lesson
	decode(t, expect(t, a.admin, "GET", pastPath, nil, 200), &unchanged)
	if unchanged.Topic != "Думаем вместе" || unchanged.RosterInitializedAt != nil {
		t.Fatal("failed completion persisted fields or snapshot")
	}
	journal := lessonJournal(t, a.own, past.ID)
	expect(t, a.own, "PUT", pastPath+"/attendance", marksBody(markValue(a.firstID, "present", "")), 204)
	expectAttendanceCode(t, a.admin, "POST", pastPath+"/complete", nil, 409, "attendance_unmarked")
	expect(t, a.foreign, "PUT", pastPath+"/attendance", marksBody(markValue(a.firstID, "absent", "")), 404)
	expect(t, a.foreign, "POST", pastPath+"/complete", nil, 404)
	expect(t, a.own, "PUT", pastPath+"/attendance", marksBody(markValue(a.secondID, "excused", "")), 204)
	expect(t, a.own, "POST", pastPath+"/complete", nil, 200)
	expect(t, a.own, "POST", pastPath+"/complete", nil, 200)
	expect(t, a.own, "PUT", pastPath+"/attendance", marksBody(markValue(a.firstID, "late", "")), 403)
	expect(t, a.admin, "PUT", pastPath+"/attendance", marksBody(markValue(a.firstID, "late", "Исправлено администратором")), 204)
	expectAttendanceCode(t, a.admin, "POST", pastPath+"/cancel", nil, 409, "lesson_has_marks")
	expectAttendanceCode(t, a.admin, "PATCH", pastPath, map[string]any{"status": "cancelled"}, 409, "lesson_has_marks")
	// Excluding marked pupils must not make cancellation possible.
	expect(t, a.admin, "PUT", pastPath+"/roster", map[string]any{"enrollment_ids": []int64{}, "reason": "Проверка скрытых отметок"}, 200)
	hidden := lessonJournal(t, a.admin, past.ID)
	if len(hidden.Items) != 0 || !hidden.HasMarks {
		t.Fatal("excluded marks were lost")
	}
	expectAttendanceCode(t, a.admin, "POST", pastPath+"/cancel", nil, 409, "lesson_has_marks")
	ctx := context.Background()
	var count int
	if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_id=$1 AND action='lesson.completed'`, past.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("idempotent completion audit: %d %v", count, err)
	}
	if err := a.f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE object_id=$1 AND action='attendance.corrected'`, past.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("administrator correction audit: %d %v", count, err)
	}
	// Combined curator/teacher roles still cannot edit or complete someone else's lesson.
	if _, err := a.f.pool.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'curator')`, a.foreignUserID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": a.foreignUserID}, 200)
	expect(t, a.admin, "PUT", pastPath+"/roster", map[string]any{"enrollment_ids": []int64{journalStudent(t, journal, a.firstID).EnrollmentID}, "reason": "Вернули ученика"}, 200)
	expect(t, a.foreign, "GET", pastPath+"/attendance", nil, 200)
	expect(t, a.foreign, "PUT", pastPath+"/attendance", marksBody(markValue(a.firstID, "absent", "")), 403)
	expect(t, a.foreign, "POST", pastPath+"/complete", nil, 403)
	unmarked := a.lesson(t, -5*time.Hour, -4*time.Hour)
	unmarkedPath := lessonPath(unmarked.ID)
	expect(t, a.own, "PUT", unmarkedPath+"/attendance", marksBody(markValue(a.firstID, "unmarked", "Комментарий без отметки")), 204)
	expect(t, a.admin, "POST", unmarkedPath+"/cancel", nil, 200)
	if got := lessonJournal(t, a.admin, unmarked.ID); len(got.Items) != 2 || got.HasMarks {
		t.Fatal("cancellation removed unmarked composition")
	}
	expectAttendanceCode(t, a.own, "PUT", unmarkedPath+"/attendance", marksBody(markValue(a.firstID, "present", "")), 409, "lesson_cancelled")
	expectAttendanceCode(t, a.own, "POST", unmarkedPath+"/complete", nil, 409, "lesson_cancelled")
}
func TestAttendanceDatabaseTransitionRaces(t *testing.T) {
	a := setupAttendance(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	marked := a.lesson(t, -3*time.Hour, -2*time.Hour)
	j := lessonJournal(t, a.admin, marked.ID)
	first := journalStudent(t, j, a.firstID)
	second := journalStudent(t, j, a.secondID)
	tx, err := a.f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE attendance SET status='present' WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() {
		_, err := a.f.pool.Exec(ctx, `UPDATE lessons SET status='cancelled' WHERE id=$1`, marked.ID)
		waiting <- err
	}()
	select {
	case err := <-waiting:
		t.Fatalf("cancel did not wait for marking: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	dbConstraint(t, <-waiting, "23514", "lessons_cancellation_marks")
	tx, err = a.f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE attendance SET status='late' WHERE id=$1`, second.ID); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := a.f.pool.Exec(ctx, `UPDATE lessons SET status='completed' WHERE id=$1`, marked.ID)
		waiting <- err
	}()
	select {
	case err := <-waiting:
		t.Fatalf("complete did not wait for persisted marks: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-waiting; err != nil {
		t.Fatal(err)
	}
	cancelled := a.lesson(t, -5*time.Hour, -4*time.Hour)
	cj := lessonJournal(t, a.admin, cancelled.ID)
	pupil := journalStudent(t, cj, a.firstID)
	tx, err = a.f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE lessons SET status='cancelled' WHERE id=$1`, cancelled.ID); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := a.f.pool.Exec(ctx, `UPDATE attendance SET status='present' WHERE id=$1`, pupil.ID)
		waiting <- err
	}()
	select {
	case err := <-waiting:
		t.Fatalf("mark did not wait for cancellation: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	dbConstraint(t, <-waiting, "23514", "attendance_lesson_cancelled")
	stale := a.lesson(t, -7*time.Hour, -6*time.Hour)
	sj := lessonJournal(t, a.admin, stale.ID)
	stalePupil := journalStudent(t, sj, a.firstID)
	rr, err := a.f.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Rollback(ctx)
	if _, err = rr.Exec(ctx, `SELECT updated_at FROM lessons WHERE id=$1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.f.pool.Exec(ctx, `UPDATE lessons SET status='cancelled' WHERE id=$1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	_, err = rr.Exec(ctx, `UPDATE attendance SET status='present' WHERE id=$1`, stalePupil.ID)
	dbConstraint(t, err, "40001", "")
	future := a.lesson(t, time.Hour, 2*time.Hour)
	fj := lessonJournal(t, a.admin, future.ID)
	futurePupil := journalStudent(t, fj, a.firstID)
	_, err = a.f.pool.Exec(ctx, `UPDATE attendance SET status='present' WHERE id=$1`, futurePupil.ID)
	dbConstraint(t, err, "23514", "attendance_lesson_not_started")
	_, err = a.f.pool.Exec(ctx, `UPDATE lessons SET status='completed' WHERE id=$1`, future.ID)
	dbConstraint(t, err, "23514", "lessons_completion_time")
}
func TestAttendanceParallelBulkRequestsDoNotMixOrDuplicate(t *testing.T) {
	a := setupAttendance(t)
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	original := lessonJournal(t, a.admin, l.ID)
	bodies := []map[string]any{marksBody(markValue(a.firstID, "present", "Batch A"), markValue(a.secondID, "late", "Batch A")), marksBody(markValue(a.firstID, "absent", "Batch B"), markValue(a.secondID, "excused", "Batch B"))}
	start := make(chan struct{})
	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			statuses[i], _ = a.own.do(t, "PUT", lessonPath(l.ID)+"/attendance", bodies[i])
		}(i)
	}
	close(start)
	wg.Wait()
	if statuses[0] != 204 || statuses[1] != 204 {
		t.Fatalf("parallel bulk status: %v", statuses)
	}
	result := lessonJournal(t, a.admin, l.ID)
	first := journalStudent(t, result, a.firstID)
	second := journalStudent(t, result, a.secondID)
	if first.ID != journalStudent(t, original, a.firstID).ID || second.ID != journalStudent(t, original, a.secondID).ID || len(result.Items) != 2 {
		t.Fatal("parallel save duplicated identities")
	}
	if !((first.Status == "present" && second.Status == "late" && first.Comment == "Batch A" && second.Comment == "Batch A") || (first.Status == "absent" && second.Status == "excused" && first.Comment == "Batch B" && second.Comment == "Batch B")) {
		t.Fatal("concurrent bulk requests were mixed")
	}
}

func TestAttendanceLargeUnicodeCommentBatch(t *testing.T) {
	a := setupAttendance(t)
	expect(t, a.admin, "PATCH", groupPath(a.groupID), map[string]any{"capacity": 20}, 200)
	for i := 0; i < 16; i++ {
		student := studentCreate(t, a.admin, map[string]any{"name": fmt.Sprintf("Ученик %d", i)})
		expect(t, a.admin, "POST", groupPath(a.groupID)+"/enrollments", map[string]any{"student_id": student.ID, "starts_on": "2000-01-01"}, 201)
	}
	l := a.lesson(t, -3*time.Hour, -2*time.Hour)
	original := lessonJournal(t, a.own, l.ID)
	items := []map[string]any{}
	for _, p := range original.Items {
		items = append(items, markValue(p.StudentID, "present", strings.Repeat("😀", 500)))
	}
	body := marksBody(items...)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= 32<<10 {
		t.Fatal("regression batch must exceed the former decoder limit")
	}
	expect(t, a.own, "PUT", lessonPath(l.ID)+"/attendance", body, 204)
	actual := lessonJournal(t, a.own, l.ID)
	if len(actual.Items) != 18 {
		t.Fatalf("large batch lost pupils: %d", len(actual.Items))
	}
	for _, p := range actual.Items {
		if p.Status != "present" || p.Comment != strings.Repeat("😀", 500) || p.ID != journalStudent(t, original, p.StudentID).ID {
			t.Fatal("large bulk save changed identities or truncated comments")
		}
	}
}
