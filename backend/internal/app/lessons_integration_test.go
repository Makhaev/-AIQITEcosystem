package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"aiq.local/core/internal/modules/lessons"
)

func lessonBody(groupID, teacherID int64, starts, ends string) map[string]any {
	return map[string]any{"group_id": groupID, "teacher_id": teacherID, "topic": "  Думаем вместе  ", "starts_at": starts, "ends_at": ends, "location": "  Класс AIQ  "}
}
func lessonCreate(t *testing.T, b *browser, body map[string]any) lessons.Lesson {
	t.Helper()
	var v lessons.Lesson
	decode(t, expect(t, b, "POST", "/lessons", body, 201), &v)
	return v
}
func lessonPath(id int64) string { return fmt.Sprintf("/lessons/%d", id) }
func lessonPage(t *testing.T, b *browser, path string) lessons.Page {
	t.Helper()
	var v lessons.Page
	decode(t, expect(t, b, "GET", path, nil, 200), &v)
	return v
}
func lessonJournal(t *testing.T, b *browser, id int64) lessons.Journal {
	t.Helper()
	var v lessons.Journal
	decode(t, expect(t, b, "GET", lessonPath(id)+"/attendance", nil, 200), &v)
	return v
}
func TestLessonsLifecycleIntervalsModulesAndMoscowFilters(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	otherProgram := programCreate(t, admin, map[string]any{"name": "Other", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	otherGroup := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher"})
	otherTeacher := teacherCreate(t, admin, map[string]any{"name": "Other teacher"})
	module := moduleCreate(t, admin, p.ID, 1, "Module")
	foreign := moduleCreate(t, admin, otherProgram.ID, 1, "Foreign")
	body := lessonBody(g.ID, teacher.ID, "2026-10-01T10:00:00+03:00", "2026-10-01T11:00:00+03:00")
	body["module_id"] = module.ID
	l := lessonCreate(t, admin, body)
	if l.Status != "scheduled" || l.Topic != "Думаем вместе" || l.Location != "Класс AIQ" || l.ModuleID == nil || *l.ModuleID != module.ID || l.StartsAt.Format(time.RFC3339) != "2026-10-01T07:00:00Z" {
		t.Fatalf("normalization: %+v", l)
	}
	var kind string
	if err := f.pool.QueryRow(ctx, `SELECT pg_typeof(starts_at)::text FROM lessons WHERE id=$1`, l.ID).Scan(&kind); err != nil || kind != "timestamp with time zone" {
		t.Fatalf("timestamp type: %s %v", kind, err)
	}
	expect(t, admin, "POST", "/lessons", lessonBody(otherGroup.ID, teacher.ID, "2026-10-01T10:30:00+03:00", "2026-10-01T11:30:00+03:00"), 409)
	expect(t, admin, "POST", "/lessons", lessonBody(g.ID, otherTeacher.ID, "2026-10-01T10:30:00+03:00", "2026-10-01T11:30:00+03:00"), 409)
	adjacent := lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, "2026-10-01T11:00:00+03:00", "2026-10-01T12:00:00+03:00"))
	cancelledBody := lessonBody(g.ID, teacher.ID, "2026-10-01T10:00:00+03:00", "2026-10-01T11:00:00+03:00")
	cancelledBody["status"] = "cancelled"
	cancelled := lessonCreate(t, admin, cancelledBody)
	expect(t, admin, "PATCH", lessonPath(cancelled.ID), map[string]any{"status": "scheduled"}, 409)
	expect(t, admin, "PATCH", lessonPath(l.ID), map[string]any{"status": "completed"}, 200)
	expect(t, admin, "PATCH", lessonPath(adjacent.ID), map[string]any{"starts_at": "2026-10-01T07:30:00Z"}, 409)
	expect(t, admin, "POST", lessonPath(l.ID)+"/cancel", nil, 200)
	replacement := lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, "2026-10-01T07:00:00Z", "2026-10-01T08:00:00Z"))
	expect(t, admin, "PATCH", lessonPath(l.ID), map[string]any{"status": "scheduled"}, 409)
	expect(t, admin, "PATCH", lessonPath(replacement.ID), map[string]any{"module_id": foreign.ID}, 400)
	expect(t, admin, "PATCH", lessonPath(replacement.ID), map[string]any{"module_id": module.ID, "location": nil}, 200)
	expect(t, admin, "PATCH", lessonPath(replacement.ID), map[string]any{"module_id": nil, "topic": "Название %_ буквально"}, 200)
	if lessonPage(t, admin, "/lessons?search="+url.QueryEscape("%_")).Total != 1 || lessonPage(t, admin, "/lessons?search="+url.QueryEscape("НАЗВАНИЕ")).Total != 1 {
		t.Fatal("literal/case-insensitive search")
	}
	boundary := lessonCreate(t, admin, lessonBody(otherGroup.ID, otherTeacher.ID, "2026-10-01T21:30:00Z", "2026-10-01T22:30:00Z"))
	page := lessonPage(t, admin, fmt.Sprintf("/lessons?date_from=2026-10-02&date_to=2026-10-02&group_id=%d&teacher_id=%d", otherGroup.ID, otherTeacher.ID))
	if page.Total != 1 || page.Items[0].ID != boundary.ID {
		t.Fatalf("Moscow boundary: %+v", page)
	}
	if lessonPage(t, admin, "/lessons?status=cancelled").Total != 2 {
		t.Fatal("cancelled status filter")
	}
	if page = lessonPage(t, admin, "/lessons?page_size=1&page=2"); len(page.Items) != 1 || page.Total != 5 {
		t.Fatalf("pagination: %+v", page)
	}
	for _, patch := range []map[string]any{{}, {"group_id": nil}, {"teacher_id": 0}, {"topic": " "}, {"topic": strings.Repeat("Я", 201)}, {"location": strings.Repeat("Я", 501)}, {"module_id": 0}, {"status": "bad"}, {"status": ""}, {"status": nil}, {"starts_at": nil}, {"ends_at": "2026-10-01T07:00:00Z"}, {"starts_at": "2026-10-01T12:00:00"}, {"unknown": true}} {
		expect(t, admin, "PATCH", lessonPath(replacement.ID), patch, 400)
	}
	for _, path := range []string{"/lessons/0", "/lessons/bad", "/lessons?page=0", "/lessons?page_size=101", "/lessons?group_id=0", "/lessons?teacher_id=x", "/lessons?date_from=2026-02-30", "/lessons?date_from=2026-10-02&date_to=2026-10-01", "/lessons?status=draft"} {
		expect(t, admin, "GET", path, nil, 400)
	}
	expect(t, admin, "GET", "/lessons/99999", nil, 404)
	expect(t, admin, "PATCH", fmt.Sprintf("/teachers/%d", otherTeacher.ID), map[string]any{"status": "archived"}, 200)
	expect(t, admin, "POST", "/lessons", lessonBody(g.ID, otherTeacher.ID, "2026-10-03T07:00:00Z", "2026-10-03T08:00:00Z"), 409)
	expect(t, admin, "PATCH", lessonPath(boundary.ID), map[string]any{"topic": "Исторический педагог"}, 200)
}
func TestLessonJournalMoscowDateSnapshotAndAuditedCorrections(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	target := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher"})
	s := studentCreate(t, admin, map[string]any{"name": "Имя в снимке"})
	boundary := studentCreate(t, admin, map[string]any{"name": "На дату окончания"})
	future := studentCreate(t, admin, map[string]any{"name": "Позже даты занятия"})
	e := enroll(t, admin, g.ID, s.ID)
	end := enroll(t, admin, g.ID, boundary.ID)
	expect(t, admin, "PATCH", enrollmentPath(g.ID, end.ID), map[string]any{"ends_on": "2026-10-02"}, 200)
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": future.ID, "starts_on": "2026-10-03"}, 201)
	l := lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, "2026-10-01T22:00:00Z", "2026-10-01T23:00:00Z"))
	snapshot := lessonJournal(t, admin, l.ID)
	if snapshot.LessonDate != "2026-10-02" || len(snapshot.Items) != 2 || snapshot.InitializedAt == nil {
		t.Fatalf("initial date/composition: %+v", snapshot)
	}
	expect(t, admin, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": []map[string]any{{"student_id": s.ID, "status": "present", "comment": "Не терять"}}}, 204)
	expect(t, admin, "POST", enrollmentPath(g.ID, e.ID)+"/transfer", map[string]any{"target_group_id": target.ID, "transfers_on": "2026-10-01"}, 200)
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", s.ID), map[string]any{"name": "Новое имя"}, 200)
	late := studentCreate(t, admin, map[string]any{"name": "Добавлен позже"})
	lateEnrollment := enroll(t, admin, g.ID, late.ID)
	after := lessonJournal(t, admin, l.ID)
	if len(after.Items) != 2 || !after.InitializedAt.Equal(*snapshot.InitializedAt) {
		t.Fatal("composition reinitialized after group change")
	}
	var marked lessons.Participant
	for _, item := range after.Items {
		if item.StudentID == s.ID {
			marked = item
		}
	}
	if marked.Name != "Имя в снимке" || marked.EnrollmentID != e.ID || marked.Status != "present" || marked.Comment != "Не терять" {
		t.Fatalf("snapshot/mark lost: %+v", marked)
	}
	expect(t, admin, "PATCH", lessonPath(l.ID), map[string]any{"starts_at": "2026-10-02T22:00:00Z", "ends_at": "2026-10-02T23:00:00Z"}, 409)
	expect(t, admin, "PATCH", lessonPath(l.ID), map[string]any{"group_id": target.ID}, 409)
	expect(t, admin, "PATCH", lessonPath(l.ID), map[string]any{"starts_at": "2026-10-01T23:00:00Z", "ends_at": "2026-10-02T00:00:00Z"}, 200)
	expect(t, admin, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": []int64{end.ID, lateEnrollment.ID}}, 400)
	expect(t, admin, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": []int64{end.ID, end.ID}, "reason": "Дубликат"}, 400)
	var corrected lessons.Journal
	decode(t, expect(t, admin, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": []int64{end.ID, lateEnrollment.ID}, "reason": "  Исправление списка  "}, 200), &corrected)
	if len(corrected.Items) != 2 || !corrected.InitializedAt.Equal(*snapshot.InitializedAt) {
		t.Fatal("correction discarded initialization marker")
	}
	var included bool
	var status, comment string
	if err := f.pool.QueryRow(ctx, `SELECT included,status,comment FROM attendance WHERE lesson_id=$1 AND student_id=$2`, l.ID, s.ID).Scan(&included, &status, &comment); err != nil || included || status != "present" || comment != "Не терять" {
		t.Fatalf("excluded row lost marks: %v", err)
	}
	expect(t, admin, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": []int64{e.ID, end.ID, lateEnrollment.ID}, "reason": "Вернули ученика"}, 200)
	readded := lessonJournal(t, admin, l.ID)
	if len(readded.Items) != 3 {
		t.Fatal("readdition failed")
	}
	for _, item := range readded.Items {
		if item.StudentID == s.ID && (item.ID != marked.ID || item.Status != "present" || item.Comment != "Не терять") {
			t.Fatal("readdition recreated attendance history")
		}
	}
	var details json.RawMessage
	if err := f.pool.QueryRow(ctx, `SELECT details FROM audit_logs WHERE action='lesson.roster_corrected' AND object_id=$1 ORDER BY id LIMIT 1`, l.ID).Scan(&details); err != nil {
		t.Fatal(err)
	}
	var audit struct {
		Reason        string `json:"reason"`
		Before, After []int64
	}
	if err := json.Unmarshal(details, &audit); err != nil || audit.Reason != "Исправление списка" || len(audit.Before) != 2 || len(audit.After) != 2 {
		t.Fatalf("correction audit: %s %v", details, err)
	}
	expect(t, admin, "POST", lessonPath(l.ID)+"/cancel", nil, 200)
	if len(lessonJournal(t, admin, l.ID).Items) != 3 {
		t.Fatal("cancellation erased snapshot")
	}
	expect(t, admin, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": []map[string]any{{"student_id": s.ID, "status": "absent"}}}, 409)
}
func TestLessonEmptyAndConcurrentJournalInitialization(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher"})
	l := lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, "2026-10-01T07:00:00Z", "2026-10-01T08:00:00Z"))
	first := lessonJournal(t, admin, l.ID)
	if len(first.Items) != 0 || first.InitializedAt == nil {
		t.Fatal("empty journal not persisted")
	}
	s := studentCreate(t, admin, map[string]any{"name": "First"})
	enroll(t, admin, g.ID, s.ID)
	if later := lessonJournal(t, admin, l.ID); len(later.Items) != 0 || !later.InitializedAt.Equal(*first.InitializedAt) {
		t.Fatal("empty journal changed after enrollment")
	}
	next := lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, "2026-10-01T08:00:00Z", "2026-10-01T09:00:00Z"))
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	other := studentCreate(t, admin, map[string]any{"name": "Second"})
	if _, err = tx.Exec(ctx, `INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES($1,$2,'2026-09-01')`, g.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	type response struct {
		status int
		body   []byte
	}
	done := make(chan response, 1)
	go func() {
		status, body := admin.do(t, "GET", lessonPath(next.ID)+"/attendance", nil)
		done <- response{status, body}
	}()
	select {
	case value := <-done:
		t.Fatalf("snapshot did not wait for enrollment: %d", value.status)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	value := <-done
	if value.status != 200 {
		t.Fatalf("journal: %d %s", value.status, value.body)
	}
	var snapshot lessons.Journal
	decode(t, value.body, &snapshot)
	if len(snapshot.Items) != 2 {
		t.Fatalf("snapshot missed committed composition: %+v", snapshot)
	}
	next = lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, "2026-10-01T09:00:00Z", "2026-10-01T10:00:00Z"))
	var wg sync.WaitGroup
	statuses := make([]int, 2)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], _ = admin.do(t, "GET", lessonPath(next.ID)+"/attendance", nil)
		}(i)
	}
	wg.Wait()
	var count int
	if err = f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='lesson.roster_initialized' AND object_id=$1`, next.ID).Scan(&count); err != nil || count != 1 || statuses[0] != 200 || statuses[1] != 200 {
		t.Fatalf("journal initialized more than once: %d %v %v", count, statuses, err)
	}
}
func TestLessonsJournalObjectPermissionsAndValidation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	tu, tb := provision(t, f, admin, "lesson-own@example.test", "teacher")
	cu, cb := provision(t, f, admin, "lesson-curator@example.test", "curator")
	_, leader := provision(t, f, admin, "lesson-leader@example.test", "leader")
	_, foreignTeacher := provision(t, f, admin, "lesson-foreign@example.test", "teacher")
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	foreignGroup := groupCreate(t, admin, p.ID, 10, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher", "user_id": tu.ID})
	otherTeacher := teacherCreate(t, admin, map[string]any{"name": "Other"})
	s := studentCreate(t, admin, map[string]any{"name": "Assigned", "curator_id": cu.ID})
	other := studentCreate(t, admin, map[string]any{"name": "Other"})
	enroll(t, admin, g.ID, s.ID)
	enroll(t, admin, g.ID, other.ID)
	l := lessonCreate(t, admin, lessonBody(g.ID, teacher.ID, "2026-10-01T07:00:00Z", "2026-10-01T08:00:00Z"))
	foreign := lessonCreate(t, admin, lessonBody(foreignGroup.ID, otherTeacher.ID, "2026-10-01T07:00:00Z", "2026-10-01T08:00:00Z"))
	if len(lessonJournal(t, tb, l.ID).Items) != 2 || len(lessonJournal(t, cb, l.ID).Items) != 1 || len(lessonJournal(t, leader, l.ID).Items) != 2 {
		t.Fatal("journal object/row scopes")
	}
	for _, b := range []*browser{tb, cb} {
		expect(t, b, "GET", lessonPath(foreign.ID), nil, 404)
		expect(t, b, "GET", lessonPath(foreign.ID)+"/attendance", nil, 404)
		if lessonPage(t, b, "/lessons?group_id="+fmt.Sprint(foreignGroup.ID)).Total != 0 {
			t.Fatal("filter expanded object scope")
		}
	}
	expect(t, foreignTeacher, "GET", lessonPath(l.ID)+"/attendance", nil, 404)
	for _, b := range []*browser{tb, cb, leader} {
		expect(t, b, "POST", "/lessons", lessonBody(g.ID, teacher.ID, "2026-10-02T07:00:00Z", "2026-10-02T08:00:00Z"), 403)
		expect(t, b, "PATCH", lessonPath(l.ID), map[string]any{"topic": "Forbidden"}, 403)
		expect(t, b, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": []int64{}, "reason": "Forbidden"}, 403)
		expect(t, b, "POST", lessonPath(l.ID)+"/complete", nil, 403)
	}
	expect(t, tb, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": []map[string]any{{"student_id": s.ID, "status": "present"}}}, 204)
	expect(t, cb, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": []map[string]any{{"student_id": s.ID, "status": "present"}}}, 403)
	expect(t, leader, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": []map[string]any{{"student_id": s.ID, "status": "present"}}}, 403)
	expect(t, admin, "POST", lessonPath(l.ID)+"/complete", nil, 200)
	expect(t, tb, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": []map[string]any{{"student_id": s.ID, "status": "late"}}}, 403)
	expect(t, admin, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": []map[string]any{{"student_id": s.ID, "status": "late"}}}, 204)
	for _, items := range []any{nil, []map[string]any{{"student_id": s.ID, "status": "invalid"}}, []map[string]any{{"student_id": 99999, "status": "present"}}, []map[string]any{{"student_id": s.ID, "status": "present"}, {"student_id": s.ID, "status": "absent"}}} {
		expect(t, admin, "PUT", lessonPath(l.ID)+"/attendance", map[string]any{"items": items}, 400)
	}
	anonymous := f.browser(t)
	expect(t, anonymous, "GET", "/lessons", nil, 401)
	admin.csrf = ""
	expect(t, admin, "PUT", lessonPath(l.ID)+"/roster", map[string]any{"enrollment_ids": []int64{}, "reason": "CSRF"}, 403)
}
func TestLessonsConcurrentDatabaseAndHTTPOverlap(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	g1 := groupCreate(t, admin, p.ID, 10, "active")
	g2 := groupCreate(t, admin, p.ID, 10, "active")
	t1 := teacherCreate(t, admin, map[string]any{"name": "First"})
	t2 := teacherCreate(t, admin, map[string]any{"name": "Second"})
	insert := `INSERT INTO lessons(group_id,teacher_id,topic,starts_at,ends_at) VALUES($1,$2,'Concurrent',$3::timestamptz,$4::timestamptz)`
	run := func(groupIDs, teacherIDs []int64, starts, ends string, constraint string) {
		gate := make(chan struct{})
		results := make([]error, 2)
		var wg sync.WaitGroup
		for i := range results {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-gate
				_, results[i] = f.pool.Exec(ctx, insert, groupIDs[i], teacherIDs[i], starts, ends)
			}(i)
		}
		close(gate)
		wg.Wait()
		successes := 0
		for _, err := range results {
			if err == nil {
				successes++
			} else {
				dbConstraint(t, err, "23P01", constraint)
			}
		}
		if successes != 1 {
			t.Fatalf("parallel overlap: %v", results)
		}
	}
	run([]int64{g1.ID, g1.ID}, []int64{t1.ID, t2.ID}, "2026-10-01T07:00:00Z", "2026-10-01T08:00:00Z", "lessons_group_overlap")
	run([]int64{g1.ID, g2.ID}, []int64{t1.ID, t1.ID}, "2026-10-01T09:00:00Z", "2026-10-01T10:00:00Z", "lessons_teacher_overlap")
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, insert, g1.ID, t1.ID, "2026-10-02T07:00:00Z", "2026-10-02T08:00:00Z"); err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() {
		_, err := f.pool.Exec(ctx, insert, g2.ID, t1.ID, "2026-10-02T07:00:00Z", "2026-10-02T08:00:00Z")
		waiting <- err
	}()
	select {
	case err := <-waiting:
		t.Fatalf("exclusion did not wait: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	dbConstraint(t, <-waiting, "23P01", "lessons_teacher_overlap")
	// Two HTTP requests competing for one teacher must also return 201/409.
	bodies := []map[string]any{lessonBody(g1.ID, t2.ID, "2026-10-03T07:00:00Z", "2026-10-03T08:00:00Z"), lessonBody(g2.ID, t2.ID, "2026-10-03T07:00:00Z", "2026-10-03T08:00:00Z")}
	statuses := make([]int, 2)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Add(1)
		go func(i int) { defer wg.Done(); <-gate; statuses[i], _ = admin.do(t, "POST", "/lessons", bodies[i]) }(i)
	}
	close(gate)
	wg.Wait()
	if !((statuses[0] == 201 && statuses[1] == 409) || (statuses[0] == 409 && statuses[1] == 201)) {
		t.Fatalf("HTTP overlap: %v", statuses)
	}
	first := lessonCreate(t, admin, lessonBody(g1.ID, t1.ID, "2026-10-04T07:00:00Z", "2026-10-04T08:00:00Z"))
	second := lessonCreate(t, admin, lessonBody(g2.ID, t1.ID, "2026-10-04T08:00:00Z", "2026-10-04T09:00:00Z"))
	ids := []int64{first.ID, second.ID}
	results := make([]error, 2)
	gate = make(chan struct{})
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			_, results[i] = f.pool.Exec(ctx, `UPDATE lessons SET starts_at='2026-10-04T09:00:00Z',ends_at='2026-10-04T10:00:00Z' WHERE id=$1`, ids[i])
		}(i)
	}
	close(gate)
	wg.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else {
			dbConstraint(t, err, "23P01", "lessons_teacher_overlap")
		}
	}
	if successes != 1 {
		t.Fatalf("parallel edit overlap: %v", results)
	}
}
