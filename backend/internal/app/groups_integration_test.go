package app_test

import (
	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/groups"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func groupCreate(t *testing.T, b *browser, programID int64, capacity int, status string) groups.Group {
	t.Helper()
	var v groups.Group
	decode(t, expect(t, b, "POST", "/groups", map[string]any{"name": "  Группа развития  ", "program_id": programID, "capacity": capacity, "status": status}, 201), &v)
	return v
}
func groupPath(id int64) string { return fmt.Sprintf("/groups/%d", id) }
func enrollmentPath(groupID, id int64) string {
	return fmt.Sprintf("/groups/%d/enrollments/%d", groupID, id)
}
func enroll(t *testing.T, b *browser, groupID, studentID int64) groups.Enrollment {
	t.Helper()
	var v groups.Enrollment
	decode(t, expect(t, b, "POST", groupPath(groupID)+"/enrollments", map[string]any{"student_id": studentID, "starts_on": "2026-09-01"}, 201), &v)
	return v
}
func groupCard(t *testing.T, b *browser, id int64) groups.Card {
	t.Helper()
	var v groups.Card
	decode(t, expect(t, b, "GET", groupPath(id), nil, 200), &v)
	return v
}
func groupPage(t *testing.T, b *browser, path string) groups.Page {
	t.Helper()
	var v groups.Page
	decode(t, expect(t, b, "GET", path, nil, 200), &v)
	return v
}
func TestGroupsLifecycleHistoryAndValidation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Активная программа", "status": "active"})
	draft := programCreate(t, admin, map[string]any{"name": "Черновик"})
	g := groupCreate(t, admin, p.ID, 2, "")
	other := groupCreate(t, admin, p.ID, 2, "active")
	if g.Name != "Группа развития" || g.Status != "draft" || g.ProgramName != p.Name || g.TeacherID != nil {
		t.Fatalf("defaults: %+v", g)
	}
	s := studentCreate(t, admin, map[string]any{"name": "Первый"})
	second := studentCreate(t, admin, map[string]any{"name": "Второй"})
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": s.ID, "starts_on": "2026-09-01"}, 409)
	teacher := teacherCreate(t, admin, map[string]any{"name": "Педагог"})
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"status": "active", "teacher_id": teacher.ID}, 200)
	e := enroll(t, admin, g.ID, s.ID)
	enroll(t, admin, g.ID, second.ID)
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": s.ID, "starts_on": "2026-09-02"}, 409)
	third := studentCreate(t, admin, map[string]any{"name": "Третий"})
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": third.ID, "starts_on": "2026-09-01"}, 409)
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"capacity": 1}, 409)
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"capacity": 3, "teacher_id": nil}, 200)
	expect(t, admin, "PATCH", enrollmentPath(other.ID, e.ID), map[string]any{"ends_on": "2026-10-01"}, 404)
	expect(t, admin, "PATCH", enrollmentPath(g.ID, e.ID), map[string]any{"ends_on": "2026-08-31"}, 400)
	expect(t, admin, "PATCH", enrollmentPath(g.ID, e.ID), map[string]any{"ends_on": "2026-10-01"}, 200)
	expect(t, admin, "PATCH", enrollmentPath(g.ID, e.ID), map[string]any{"ends_on": "2026-10-02"}, 409)
	again := enroll(t, admin, g.ID, s.ID)
	if again.ID == e.ID {
		t.Fatal("reenrollment reused historical identity")
	}
	card := groupCard(t, admin, g.ID)
	if len(card.Enrollments) != 3 || card.ActiveCount != 2 || card.TeacherID != nil {
		t.Fatalf("history/card: %+v", card)
	}
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"status": "archived"}, 200)
	if card = groupCard(t, admin, g.ID); len(card.Enrollments) != 3 || card.Status != "archived" {
		t.Fatal("archive removed history")
	}
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": third.ID, "starts_on": "2026-10-01"}, 409)
	expect(t, admin, "PATCH", enrollmentPath(g.ID, again.ID), map[string]any{"ends_on": "2026-10-02"}, 200)
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"status": "active"}, 200)
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", third.ID), map[string]any{"status": "archived"}, 200)
	expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": third.ID, "starts_on": "2026-10-01"}, 409)
	q := programCreate(t, admin, map[string]any{"name": "Другая", "status": "active"})
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"program_id": q.ID}, 409)
	// M2 requires an explicit preview/transfer even when the group has no history.
	expect(t, admin, "PATCH", groupPath(other.ID), map[string]any{"program_id": q.ID}, 409)
	expect(t, admin, "PATCH", groupPath(other.ID), map[string]any{"program_id": draft.ID}, 409)
	expect(t, admin, "PATCH", fmt.Sprintf("/teachers/%d", teacher.ID), map[string]any{"status": "archived"}, 200)
	expect(t, admin, "PATCH", groupPath(other.ID), map[string]any{"teacher_id": teacher.ID}, 409)
	if page := groupPage(t, admin, "/groups?search="+url.QueryEscape("ГРУППА РАЗ")+"&page_size=1&page=2"); page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != other.ID {
		t.Fatalf("search/page: %+v", page)
	}
	expect(t, admin, "PATCH", groupPath(other.ID), map[string]any{"name": "Название %_"}, 200)
	if groupPage(t, admin, "/groups?search="+url.QueryEscape("%_")).Total != 1 {
		t.Fatal("search interpreted wildcard")
	}
	for _, body := range []map[string]any{{}, {"name": " "}, {"name": strings.Repeat("Я", 201)}, {"program_id": nil}, {"teacher_id": 0}, {"capacity": nil}, {"capacity": 0}, {"capacity": 1.5}, {"capacity": 2147483648}, {"status": nil}, {"status": ""}, {"status": "deleted"}} {
		expect(t, admin, "PATCH", groupPath(other.ID), body, 400)
	}
	for _, on := range []string{"", "2026-02-30", "2026-9-01", "0000-01-01"} {
		expect(t, admin, "POST", groupPath(other.ID)+"/enrollments", map[string]any{"student_id": s.ID, "starts_on": on}, 400)
	}
	for _, path := range []string{"/groups/0", "/groups/bad", "/groups?page=0", "/groups?page_size=101", "/groups?status=deleted"} {
		expect(t, admin, "GET", path, nil, 400)
	}
	expect(t, admin, "GET", "/groups/99999", nil, 404)
	expect(t, admin, "POST", groupPath(other.ID)+"/enrollments", map[string]any{"student_id": 99999, "starts_on": "2026-10-01"}, 400)
}
func TestGroupTransferRollbackAndAttendanceHistory(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	source := groupCreate(t, admin, p.ID, 2, "active")
	target := groupCreate(t, admin, p.ID, 1, "active")
	s := studentCreate(t, admin, map[string]any{"name": "Pupil"})
	other := studentCreate(t, admin, map[string]any{"name": "Other"})
	original := enroll(t, admin, source.ID, s.ID)
	occupied := enroll(t, admin, target.ID, other.ID)
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher"})
	var lessonID, attendanceID int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO lessons(group_id,teacher_id,topic,starts_at,ends_at) VALUES($1,$2,'Lesson','2026-09-10T08:00:00Z','2026-09-10T09:00:00Z') RETURNING id`, source.ID, teacher.ID).Scan(&lessonID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id) VALUES($1,$2,$3,$4) RETURNING id`, lessonID, source.ID, original.ID, s.ID).Scan(&attendanceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE attendance SET status='present',comment='Историческая отметка' WHERE id=$1`, attendanceID); err != nil {
		t.Fatal(err)
	}
	path := enrollmentPath(source.ID, original.ID) + "/transfer"
	body := map[string]any{"target_group_id": target.ID, "transfers_on": "2026-10-01"}
	expect(t, admin, "POST", path, body, 409)
	if card := groupCard(t, admin, source.ID); card.ActiveCount != 1 || card.Enrollments[0].EndsOn != nil {
		t.Fatal("failed transfer closed source")
	}
	expect(t, admin, "PATCH", enrollmentPath(target.ID, occupied.ID), map[string]any{"ends_on": "2026-10-01"}, 200)
	expect(t, admin, "POST", path, map[string]any{"target_group_id": source.ID, "transfers_on": "2026-10-01"}, 400)
	expect(t, admin, "POST", path, map[string]any{"target_group_id": target.ID, "transfers_on": "2026-08-01"}, 400)
	expect(t, admin, "POST", path, map[string]any{"target_group_id": 99999, "transfers_on": "2026-10-01"}, 404)
	duplicate := enroll(t, admin, target.ID, s.ID)
	expect(t, admin, "POST", path, body, 409)
	if groupCard(t, admin, source.ID).Enrollments[0].EndsOn != nil {
		t.Fatal("duplicate target failed to roll back")
	}
	expect(t, admin, "PATCH", enrollmentPath(target.ID, duplicate.ID), map[string]any{"ends_on": "2026-10-01"}, 200)
	expect(t, admin, "PATCH", groupPath(target.ID), map[string]any{"status": "archived"}, 200)
	expect(t, admin, "POST", path, body, 409)
	expect(t, admin, "PATCH", groupPath(target.ID), map[string]any{"status": "active"}, 200)
	var result groups.TransferResult
	decode(t, expect(t, admin, "POST", path, body, 200), &result)
	if result.Previous.ID != original.ID || result.Previous.EndsOn == nil || *result.Previous.EndsOn != "2026-10-01" || result.Enrollment.ID == original.ID || result.Enrollment.GroupID != target.ID || result.Enrollment.StartsOn != "2026-10-01" {
		t.Fatalf("transfer: %+v", result)
	}
	if groupCard(t, admin, source.ID).ActiveCount != 0 || groupCard(t, admin, target.ID).ActiveCount != 1 {
		t.Fatal("transfer counts")
	}
	var storedGroup, storedEnrollment int64
	var status, comment string
	if err := f.pool.QueryRow(ctx, `SELECT group_id,enrollment_id,status,comment FROM attendance_records WHERE id=$1`, attendanceID).Scan(&storedGroup, &storedEnrollment, &status, &comment); err != nil || storedGroup != source.ID || storedEnrollment != original.ID || status != "present" || comment != "Историческая отметка" {
		t.Fatalf("attendance history changed: %v", err)
	}
	for _, sql := range []string{`DELETE FROM group_enrollments WHERE id=$1`, `UPDATE group_enrollments SET group_id=999 WHERE id=$1`, `UPDATE group_enrollments SET ends_on=NULL WHERE id=$1`} {
		_, err := f.pool.Exec(ctx, sql, original.ID)
		dbConstraint(t, err, "23514", "enrollments_history_immutable")
	}
	expect(t, admin, "POST", path, body, 409)
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action IN ('enrollment.transferred_in','enrollment.transferred_out')`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("transfer audit rollback: %d %v", count, err)
	}
}
func TestGroupsObjectScopesAndSessionSecurity(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	ctx := context.Background()
	tu, tb := provision(t, f, admin, "groups-teacher@example.test", "teacher")
	cu, cb := provision(t, f, admin, "groups-curator@example.test", "curator")
	_, leader := provision(t, f, admin, "groups-leader@example.test", "leader")
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	own := groupCreate(t, admin, p.ID, 3, "active")
	other := groupCreate(t, admin, p.ID, 3, "active")
	teacher := teacherCreate(t, admin, map[string]any{"name": "Teacher", "user_id": tu.ID})
	expect(t, admin, "PATCH", groupPath(own.ID), map[string]any{"teacher_id": teacher.ID}, 200)
	s := studentCreate(t, admin, map[string]any{"name": "Own pupil", "curator_id": cu.ID})
	stranger := studentCreate(t, admin, map[string]any{"name": "Other pupil"})
	e := enroll(t, admin, own.ID, s.ID)
	enroll(t, admin, own.ID, stranger.ID)
	if len(groupCard(t, tb, own.ID).Enrollments) != 0 {
		t.Fatal("primary teacher must not see pupils without their own lesson")
	}
	lessonCreate(t, admin, lessonBody(own.ID, teacher.ID, "2026-10-01T10:00:00+03:00", "2026-10-01T11:00:00+03:00"))
	if len(groupCard(t, tb, own.ID).Enrollments) != 2 || len(groupCard(t, cb, own.ID).Enrollments) != 1 {
		t.Fatal("roster scoping")
	}
	for _, b := range []*browser{tb, cb} {
		if groupPage(t, b, "/groups").Total != 1 {
			t.Fatal("scoped directory")
		}
		expect(t, b, "GET", groupPath(other.ID), nil, 404)
	}
	if groupPage(t, leader, "/groups").Total != 2 {
		t.Fatal("leader scope")
	}
	for _, b := range []*browser{tb, cb, leader} {
		expect(t, b, "POST", "/groups", map[string]any{"name": "Forbidden", "program_id": p.ID, "capacity": 1}, 403)
		expect(t, b, "PATCH", groupPath(own.ID), map[string]any{"status": "archived"}, 403)
		expect(t, b, "POST", groupPath(own.ID)+"/enrollments", map[string]any{"student_id": s.ID, "starts_on": "2026-09-01"}, 403)
		expect(t, b, "PATCH", enrollmentPath(own.ID, e.ID), map[string]any{"ends_on": "2026-10-01"}, 403)
		expect(t, b, "POST", enrollmentPath(own.ID, e.ID)+"/transfer", map[string]any{"target_group_id": other.ID, "transfers_on": "2026-10-01"}, 403)
	}
	// Mixed teacher/curator roles must not reveal another curator's pupils in a group they do not teach.
	if _, err := f.pool.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,'teacher')`, cu.ID); err != nil {
		t.Fatal(err)
	}
	if len(groupCard(t, cb, own.ID).Enrollments) != 1 {
		t.Fatal("mixed roles expanded roster without teaching relationship")
	}
	anonymous := f.browser(t)
	expect(t, anonymous, "GET", "/groups", nil, 401)
	actor := httpx.Actor{ID: f.admin.ID, Roles: []string{"admin"}, SessionHash: auth.Hash(admin.sessionToken(t))}
	admin.csrf = ""
	expect(t, admin, "PATCH", enrollmentPath(own.ID, e.ID), map[string]any{"ends_on": "2026-10-01"}, 403)
	expect(t, admin, "POST", enrollmentPath(own.ID, e.ID)+"/transfer", map[string]any{"target_group_id": other.ID, "transfers_on": "2026-10-01"}, 403)
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	decode(t, expect(t, admin, "GET", "/auth/csrf", nil, 200), &csrf)
	admin.csrf = csrf.Token
	expect(t, admin, "POST", "/auth/logout", nil, 204)
	repo := groups.Repository{Pool: f.pool}
	actorCtx := httpx.WithActor(ctx, actor)
	_, err := repo.Enroll(actorCtx, own.ID, groups.EnrollInput{StudentID: s.ID, StartsOn: "2026-10-01"})
	var api *httpx.Error
	if !errors.As(err, &api) || api.Status != 401 {
		t.Fatalf("revoked session: %v", err)
	}
}
func TestGroupsParallelHTTPEnrollmentsAndTransfers(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	first := studentCreate(t, admin, map[string]any{"name": "First"})
	second := studentCreate(t, admin, map[string]any{"name": "Second"})
	target := groupCreate(t, admin, p.ID, 1, "active")
	race := func(paths []string, bodies []map[string]any) []int {
		start := make(chan struct{})
		statuses := make([]int, len(paths))
		var wg sync.WaitGroup
		for i := range paths {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				statuses[i], _ = admin.do(t, "POST", paths[i], bodies[i])
			}(i)
		}
		close(start)
		wg.Wait()
		return statuses
	}
	path := groupPath(target.ID) + "/enrollments"
	statuses := race([]string{path, path}, []map[string]any{{"student_id": first.ID, "starts_on": "2026-09-01"}, {"student_id": second.ID, "starts_on": "2026-09-01"}})
	if !((statuses[0] == 201 && statuses[1] == 409) || (statuses[0] == 409 && statuses[1] == 201)) {
		t.Fatalf("parallel enrollment statuses: %v", statuses)
	}
	card := groupCard(t, admin, target.ID)
	expect(t, admin, "PATCH", enrollmentPath(target.ID, card.Enrollments[0].ID), map[string]any{"ends_on": "2026-10-01"}, 200)
	source1 := groupCreate(t, admin, p.ID, 1, "active")
	source2 := groupCreate(t, admin, p.ID, 1, "active")
	e1 := enroll(t, admin, source1.ID, first.ID)
	e2 := enroll(t, admin, source2.ID, second.ID)
	body := map[string]any{"target_group_id": target.ID, "transfers_on": "2026-10-01"}
	statuses = race([]string{enrollmentPath(source1.ID, e1.ID) + "/transfer", enrollmentPath(source2.ID, e2.ID) + "/transfer"}, []map[string]any{body, body})
	if !((statuses[0] == 200 && statuses[1] == 409) || (statuses[0] == 409 && statuses[1] == 200)) {
		t.Fatalf("parallel transfer statuses: %v", statuses)
	}
	if groupCard(t, admin, target.ID).ActiveCount != 1 || groupCard(t, admin, source1.ID).ActiveCount+groupCard(t, admin, source2.ID).ActiveCount != 1 {
		t.Fatal("failed parallel transfer lost pupil or overbooked target")
	}
}

func TestGroupsConcurrentDatabaseCapacityAndUniqueness(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	p := programCreate(t, admin, map[string]any{"name": "Program", "status": "active"})
	g := groupCreate(t, admin, p.ID, 1, "active")
	first := studentCreate(t, admin, map[string]any{"name": "First"})
	second := studentCreate(t, admin, map[string]any{"name": "Second"})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	race := func(studentIDs []int64) []error {
		start := make(chan struct{})
		results := make([]error, len(studentIDs))
		var wg sync.WaitGroup
		for i, id := range studentIDs {
			wg.Add(1)
			go func(i int, id int64) {
				defer wg.Done()
				<-start
				_, results[i] = f.pool.Exec(ctx, `INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES($1,$2,'2026-09-01')`, g.ID, id)
			}(i, id)
		}
		close(start)
		wg.Wait()
		return results
	}
	results := race([]int64{first.ID, second.ID})
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		} else {
			dbConstraint(t, err, "23514", "groups_capacity_exceeded")
		}
	}
	if successes != 1 {
		t.Fatalf("last place race: %v", results)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE group_enrollments SET ends_on='2026-10-01' WHERE group_id=$1 AND ends_on IS NULL`, g.ID); err != nil {
		t.Fatal(err)
	}
	results = race([]int64{first.ID, first.ID})
	successes = 0
	for _, err := range results {
		if err == nil {
			successes++
		} else {
			dbConstraint(t, err, "23505", "group_enrollments_current_unique")
		}
	}
	if successes != 1 {
		t.Fatalf("duplicate race: %v", results)
	}
	// A shrinking capacity writer waits for enrollment and then sees its commit.
	if _, err := f.pool.Exec(ctx, `UPDATE groups SET capacity=2 WHERE id=$1`, g.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES($1,$2,'2026-09-01')`, g.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	shrinking := make(chan error, 1)
	go func() { _, err := f.pool.Exec(ctx, `UPDATE groups SET capacity=1 WHERE id=$1`, g.ID); shrinking <- err }()
	select {
	case err := <-shrinking:
		t.Fatalf("shrink did not wait: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	dbConstraint(t, <-shrinking, "23514", "groups_capacity_exceeded")
	// A stale snapshot cannot overbook even when a caller uses REPEATABLE READ.
	repeatable, err := f.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer repeatable.Rollback(ctx)
	if _, err = repeatable.Exec(ctx, `SELECT count(*) FROM group_enrollments`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `UPDATE groups SET capacity=3 WHERE id=$1`, g.ID); err != nil {
		t.Fatal(err)
	}
	_, err = repeatable.Exec(ctx, `INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES($1,$2,'2026-09-01')`, g.ID, second.ID)
	dbConstraint(t, err, "40001", "")
}
