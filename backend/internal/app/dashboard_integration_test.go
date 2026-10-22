package app_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"aiq.local/core/internal/modules/auth"
	"aiq.local/core/internal/modules/dashboard"
	"aiq.local/core/internal/platform/httpx"
)

func dashboardGet(t *testing.T, b *browser, query string) dashboard.Overview {
	t.Helper()
	var v dashboard.Overview
	decode(t, expect(t, b, "GET", "/dashboard"+query, nil, 200), &v)
	return v
}

func TestDashboardWeightsEveryMarkAndMatchesStudent360(t *testing.T) {
	a := setupAttendance(t)
	_, leader := provision(t, a.f, a.admin, "dashboard-leader@example.test", "leader")
	first := a.lesson(t, -10*24*time.Hour, -10*24*time.Hour+time.Hour)
	expect(t, a.admin, "PUT", lessonPath(first.ID)+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "late", "")), 204)
	expect(t, a.admin, "POST", lessonPath(first.ID)+"/complete", nil, 200)
	second := a.lesson(t, -9*24*time.Hour, -9*24*time.Hour+time.Hour)
	expect(t, a.admin, "PUT", lessonPath(second.ID)+"/attendance", marksBody(markValue(a.firstID, "absent", ""), markValue(a.secondID, "excused", "")), 204)
	expect(t, a.admin, "POST", lessonPath(second.ID)+"/complete", nil, 200)
	// A smaller second group has 0%; averaging group percentages would give
	// 25%, whereas the five individual marks give 40% (2 / 5).
	p := programCreate(t, a.admin, map[string]any{"name": "Small group program", "status": "active"})
	g := groupCreate(t, a.admin, p.ID, 10, "active")
	teacher := teacherCreate(t, a.admin, map[string]any{"name": "Small group teacher"})
	expect(t, a.admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": a.firstID, "starts_on": "2000-01-01"}, 201)
	third := lessonCreate(t, a.admin, lessonBody(g.ID, teacher.ID, a.base.Add(-8*24*time.Hour).Format(time.RFC3339Nano), a.base.Add(-8*24*time.Hour+time.Hour).Format(time.RFC3339Nano)))
	expect(t, a.admin, "PUT", lessonPath(third.ID)+"/attendance", marksBody(markValue(a.firstID, "absent", "")), 204)
	expect(t, a.admin, "POST", lessonPath(third.ID)+"/complete", nil, 200)
	// Neither scheduled marks, cleared marks nor excluded rows count.
	scheduled := a.lesson(t, -7*24*time.Hour, -7*24*time.Hour+time.Hour)
	expect(t, a.admin, "PUT", lessonPath(scheduled.ID)+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "present", "")), 204)
	excluded := a.lesson(t, -6*24*time.Hour, -6*24*time.Hour+time.Hour)
	finishOverviewLesson(t, a, excluded, "present")
	expect(t, a.admin, "PUT", lessonPath(excluded.ID)+"/roster", map[string]any{"enrollment_ids": []int64{}, "reason": "Исключены из расчёта"}, 200)
	unmarked := a.lesson(t, -5*24*time.Hour, -5*24*time.Hour+time.Hour)
	finishOverviewLesson(t, a, unmarked, "present")
	expect(t, a.admin, "PUT", lessonPath(unmarked.ID)+"/attendance", marksBody(markValue(a.firstID, "unmarked", ""), markValue(a.secondID, "unmarked", "")), 204)
	cancelled := a.lesson(t, -4*24*time.Hour, -4*24*time.Hour+time.Hour)
	expect(t, a.admin, "POST", lessonPath(cancelled.ID)+"/cancel", nil, 200)
	period := "?date_from=2000-01-01&date_to=2099-12-31"
	for _, b := range []*browser{a.admin, leader} {
		v := dashboardGet(t, b, period)
		if v.AttendancePercent == nil || math.Abs(*v.AttendancePercent-40) > 0.00001 {
			t.Fatalf("expected 40%% of all marks, got %+v", v)
		}
		// Combine Student 360 counts as an independent HTTP cross-check.
		var present, late, absent, excused int
		for _, id := range []int64{a.firstID, a.secondID} {
			summary := overview(t, b, id, "?from=2000-01-01&to=2099-12-31").Attendance
			present += summary.Present
			late += summary.Late
			absent += summary.Absent
			excused += summary.Excused
		}
		want := float64(present+late) / float64(present+late+absent+excused) * 100
		if *v.AttendancePercent != want {
			t.Fatalf("Dashboard and Student 360 differ: %v != %v", *v.AttendancePercent, want)
		}
	}
	// Archiving does not erase historical attendance. Active counters are
	// entity counts, even when a pupil belongs to several groups.
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"status": "archived"}, 200)
	expect(t, a.admin, "PATCH", groupPath(a.groupID), map[string]any{"status": "archived"}, 200)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/teachers/%d", a.teacherID), map[string]any{"status": "archived"}, 200)
	v := dashboardGet(t, leader, period)
	if v.ActiveStudents != 1 || v.ActiveGroups != 1 || v.ActiveTeachers != 1 || v.AttendancePercent == nil || *v.AttendancePercent != 40 {
		t.Fatalf("active counters or archived history incorrect: %+v", v)
	}
}

func TestDashboardMoscowTodayPeriodBoundariesAndOverdueTasks(t *testing.T) {
	a := setupAttendance(t)
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	for _, offset := range []time.Duration{-time.Hour, 0, 23 * time.Hour, 24 * time.Hour} {
		start := today.Add(offset)
		lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339)))
	}
	cancelled := lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, today.Add(12*time.Hour).Format(time.RFC3339), today.Add(13*time.Hour).Format(time.RFC3339)))
	expect(t, a.admin, "POST", lessonPath(cancelled.ID)+"/cancel", nil, 200)
	for _, status := range []string{"todo", "in_progress", "done", "cancelled"} {
		body := taskBody(a.foreignUserID, a.base.Add(-time.Hour))
		taskWithStatus(t, a.admin, body, status)
	}
	taskCreate(t, a.admin, taskBody(a.foreignUserID, a.base.Add(time.Hour)))
	// 21:00 UTC belongs to the next Moscow day, including both period endpoints.
	l := lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, "2024-06-10T21:00:00Z", "2024-06-10T22:00:00Z"))
	finishOverviewLesson(t, a, l, "present")
	before := dashboardGet(t, a.admin, "?date_from=2024-06-10&date_to=2024-06-10")
	inside := dashboardGet(t, a.admin, "?date_from=2024-06-11&date_to=2024-06-11")
	for _, v := range []dashboard.Overview{before, inside, dashboardGet(t, a.admin, "")} {
		if v.LessonsToday != 2 || v.OverdueTasks != 2 || v.ActiveStudents != 2 || v.ActiveGroups != 1 || v.ActiveTeachers != 1 {
			t.Fatalf("period changed current counters, or Moscow/overdue boundaries wrong: %+v", v)
		}
	}
	if before.AttendancePercent != nil || inside.AttendancePercent == nil || *inside.AttendancePercent != 100 {
		t.Fatalf("inclusive Moscow period or no-data value incorrect: before=%+v inside=%+v", before, inside)
	}
	expect(t, a.admin, "PUT", lessonPath(l.ID)+"/attendance", marksBody(markValue(a.firstID, "absent", ""), markValue(a.secondID, "excused", "")), 204)
	zero := dashboardGet(t, a.admin, "?date_from=2024-06-11&date_to=2024-06-11")
	if zero.AttendancePercent == nil || *zero.AttendancePercent != 0 {
		t.Fatal("0% was confused with no data")
	}
	for _, query := range []string{"?date_from=bad", "?date_to=2026-02-30", "?date_from=2026-10-12&date_to=2026-10-11", "?date_from=0000-01-01"} {
		expect(t, a.admin, "GET", "/dashboard"+query, nil, 400)
	}
}

func TestDashboardRolesAndRepositoryReauthorization(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	u, leader := provision(t, f, admin, "dashboard-access-leader@example.test", "leader")
	_, teacher := provision(t, f, admin, "dashboard-access-teacher@example.test", "teacher")
	_, curator := provision(t, f, admin, "dashboard-access-curator@example.test", "curator")
	expect(t, f.browser(t), "GET", "/dashboard", nil, 401)
	for _, b := range []*browser{teacher, curator} {
		expect(t, b, "GET", "/dashboard", nil, 403)
	}
	for _, b := range []*browser{admin, leader} {
		v := dashboardGet(t, b, "")
		if v != (dashboard.Overview{}) {
			t.Fatalf("empty database dashboard: %+v", v)
		}
	}
	repo := &dashboard.Repository{Pool: f.pool}
	actor := httpx.Actor{ID: u.ID, Roles: []string{"leader"}, SessionHash: auth.Hash(leader.sessionToken(t))}
	ctx := httpx.WithActor(context.Background(), actor)
	if _, err := f.pool.Exec(context.Background(), `UPDATE user_roles SET role='teacher' WHERE user_id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	_, err := repo.Get(ctx, time.Now().AddDate(0, 0, -30), time.Now())
	var appErr *httpx.Error
	if !errors.As(err, &appErr) || appErr.Status != 403 {
		t.Fatalf("stale leader role accepted: %v", err)
	}
	expect(t, leader, "GET", "/dashboard", nil, 403)
	if _, err = f.pool.Exec(context.Background(), `UPDATE user_roles SET role='leader' WHERE user_id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, leader, "POST", "/auth/logout", nil, 204)
	_, err = repo.Get(ctx, time.Now().AddDate(0, 0, -30), time.Now())
	if !errors.As(err, &appErr) || appErr.Status != 401 {
		t.Fatalf("revoked session accepted: %v", err)
	}
}
