package app_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

// Compare the public contracts directly, including error payloads. Request IDs
// identify separate requests and are the only response fields allowed to differ.
func equalStudentOverviewRoutes(t *testing.T, b *browser, id, period string, status int) map[string]any {
	t.Helper()
	previousRedirect := b.client.CheckRedirect
	b.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	defer func() { b.client.CheckRedirect = previousRedirect }()
	var primary, alias map[string]any
	for _, route := range []struct {
		name string
		body *map[string]any
	}{{"overview", &primary}, {"360", &alias}} {
		decode(t, expect(t, b, "GET", fmt.Sprintf("/students/%s/%s%s", id, route.name, period), nil, status), route.body)
		if status != 200 {
			if requestID, ok := (*route.body)["request_id"].(string); !ok || requestID == "" {
				t.Fatalf("%s error has no request_id: %+v", route.name, *route.body)
			}
			delete(*route.body, "request_id")
		}
	}
	if !reflect.DeepEqual(primary, alias) {
		t.Fatalf("Student 360 routes differ: overview=%+v alias=%+v", primary, alias)
	}
	return primary
}

func TestStudentOverviewAliasCompatibility(t *testing.T) {
	a := setupAttendance(t)
	curator, ownCurator := provision(t, a.f, a.admin, "alias-curator@example.test", "curator")
	_, otherCurator := provision(t, a.f, a.admin, "alias-other@example.test", "curator")
	leaderUser, leader := provision(t, a.f, a.admin, "alias-leader@example.test", "leader")
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": curator.ID, "birth_date": "2013-01-01"}, 200)
	parent := parentCreate(t, a.admin, map[string]any{"name": "Alias parent", "email": "alias-parent@example.test"})
	linkParent(t, a.admin, a.firstID, parent.ID, true)
	// The middle two lessons belong to June 11 in Moscow. Both adjacent
	// calendar days must stay outside an inclusive single-day filter.
	for i, start := range []string{"2024-06-10T20:00:00Z", "2024-06-10T21:00:00Z", "2024-06-11T20:00:00Z", "2024-06-11T21:00:00Z"} {
		at, err := time.Parse(time.RFC3339, start)
		if err != nil {
			t.Fatal(err)
		}
		lesson := lessonCreate(t, a.admin, lessonBody(a.groupID, a.teacherID, start, at.Add(30*time.Minute).Format(time.RFC3339)))
		finishOverviewLesson(t, a, lesson, []string{"present", "late", "absent", "excused"}[i])
	}
	a.lesson(t, time.Hour, 2*time.Hour)
	id := fmt.Sprint(a.firstID)
	for _, actor := range []struct {
		name string
		b    *browser
		full bool
	}{{"admin", a.admin, true}, {"leader", leader, true}, {"assigned_curator", ownCurator, true}, {"own_teacher", a.own, false}} {
		t.Run(actor.name, func(t *testing.T) {
			for _, period := range []string{"", "?from=&to=", "?from=2000-01-01", "?to=2099-12-31", "?from=2024-06-11&to=2024-06-11"} {
				body := equalStudentOverviewRoutes(t, actor.b, id, period, 200)
				if body["full_access"] != actor.full {
					t.Fatalf("unexpected projection: %+v", body)
				}
				if !actor.full {
					for _, private := range []string{"age", "birth_date", "curator_id", "curator_name", "parents", "history", "created_at", "updated_at"} {
						if _, exists := body[private]; exists {
							t.Fatalf("teacher received private field %s", private)
						}
					}
				}
				if period == "?from=2024-06-11&to=2024-06-11" {
					attendance := body["attendance"].(map[string]any)
					if attendance["from"] != "2024-06-11" || attendance["to"] != "2024-06-11" || attendance["percent"] != float64(50) || len(attendance["items"].([]any)) != 2 {
						t.Fatalf("from/to no longer respect inclusive Moscow dates: %+v", attendance)
					}
				}
			}
		})
	}
	for _, tc := range []struct {
		name, id, period string
		b                *browser
		status           int
	}{
		{"anonymous", id, "", a.f.browser(t), 401},
		{"foreign_curator", id, "", otherCurator, 404},
		{"foreign_teacher", id, "", a.foreign, 404},
		{"missing_student", "999999", "", a.admin, 404},
		{"zero_id", "0", "", a.admin, 400},
		{"invalid_id", "bad", "", a.admin, 400},
		{"invalid_from", id, "?from=2024-02-30", a.admin, 400},
		{"invalid_to", id, "?to=bad", a.admin, 400},
		{"reversed_period", id, "?from=2024-06-12&to=2024-06-11", a.admin, 400},
	} {
		t.Run(tc.name, func(t *testing.T) { equalStudentOverviewRoutes(t, tc.b, tc.id, tc.period, tc.status) })
	}
	var teacherUserID int64
	if err := a.f.pool.QueryRow(context.Background(), `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) VALUES($1,'curator')`, teacherUserID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.secondID), map[string]any{"curator_id": teacherUserID}, 200)
	if body := equalStudentOverviewRoutes(t, a.own, id, "", 200); body["full_access"] != false {
		t.Fatal("curating another pupil expanded this teaching card")
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": teacherUserID}, 200)
	if body := equalStudentOverviewRoutes(t, a.own, id, "", 200); body["full_access"] != true {
		t.Fatal("assigned teacher/curator lost the full projection")
	}
	pending := create(t, a.admin, "alias-pending@example.test", "curator")
	pendingBrowser := a.f.browser(t)
	pendingBrowser.login(t, pending.Email, userPassword)
	equalStudentOverviewRoutes(t, pendingBrowser, id, "", 403)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"curator_id": nil}, 200)
	equalStudentOverviewRoutes(t, ownCurator, id, "", 404)
	expect(t, a.admin, "PATCH", fmt.Sprintf("/users/%d", leaderUser.ID), map[string]any{"blocked": true}, 200)
	equalStudentOverviewRoutes(t, leader, id, "", 401)
	if _, err := a.f.pool.Exec(context.Background(), `DELETE FROM sessions WHERE user_id=(SELECT user_id FROM teachers WHERE id=$1)`, a.teacherID); err != nil {
		t.Fatal(err)
	}
	equalStudentOverviewRoutes(t, a.own, id, "", 401)
}
