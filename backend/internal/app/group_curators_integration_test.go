package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGroupCuratorsMembershipTransferAndLegacyIsolation(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	first, one := provision(t, f, admin, "group-curator-one@example.test", "curator")
	second, two := provision(t, f, admin, "group-curator-two@example.test", "curator")
	teacher, _ := provision(t, f, admin, "group-curator-teacher@example.test", "teacher")
	p := programCreate(t, admin, map[string]any{"name": "Group supervision", "status": "active"})
	g := groupCreate(t, admin, p.ID, 10, "active")
	target := groupCreate(t, admin, p.ID, 10, "active")
	expect(t, admin, "PATCH", groupPath(g.ID), map[string]any{"curator_id": teacher.ID}, 400)
	setGroupCurator(t, admin, g.ID, first.ID)
	setGroupCurator(t, admin, target.ID, second.ID)
	// Empty groups are visible before any student is enrolled.
	_, total := personalPage(t, one, "/curator/groups")
	if total != 1 {
		t.Fatal("empty assigned group missing")
	}
	expect(t, one, "GET", fmt.Sprintf("/curator/students?group_id=%d", target.ID), nil, 404)
	child := studentCreate(t, admin, map[string]any{"name": "Group pupil"})
	expect(t, admin, "PATCH", fmt.Sprintf("/students/%d", child.ID), map[string]any{"curator_id": first.ID}, 400)
	// Legacy assignments alone never grant access.
	if _, err := f.pool.Exec(context.Background(), `UPDATE students SET curator_id=$1 WHERE id=$2`, first.ID, child.ID); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/curator/students/%d", child.ID)
	expect(t, one, "GET", path, nil, 404)
	zone, _ := time.LoadLocation("Europe/Moscow")
	today := time.Now().In(zone).Format("2006-01-02")
	future := time.Now().In(zone).AddDate(0, 0, 1).Format("2006-01-02")
	var pending struct {
		ID int64 `json:"id"`
	}
	decode(t, expect(t, admin, "POST", groupPath(g.ID)+"/enrollments", map[string]any{"student_id": child.ID, "starts_on": future}, 201), &pending)
	expect(t, one, "GET", path, nil, 404)
	expect(t, admin, "PATCH", enrollmentPath(g.ID, pending.ID), map[string]any{"ends_on": future}, 200)
	e := enroll(t, admin, g.ID, child.ID)
	expect(t, one, "GET", path, nil, 200)
	expect(t, two, "GET", path, nil, 404)
	// Transfer revokes the previous curator on the same day, without erasing history.
	expect(t, admin, "POST", enrollmentPath(g.ID, e.ID)+"/transfer", map[string]any{"target_group_id": target.ID, "transfers_on": today}, 200)
	expect(t, one, "GET", path, nil, 404)
	expect(t, two, "GET", path, nil, 200)
	expect(t, one, "GET", fmt.Sprintf("/students/%d", child.ID), nil, 404)
	// Multiple current groups can grant independent access; duplicates do not inflate totals.
	enroll(t, admin, g.ID, child.ID)
	expect(t, one, "GET", path, nil, 200)
	extra := groupCreate(t, admin, p.ID, 10, "active")
	setGroupCurator(t, admin, extra.ID, first.ID)
	enroll(t, admin, extra.ID, child.ID)
	_, total = personalPage(t, one, "/curator/students")
	if total != 1 {
		t.Fatal("student counted more than once")
	}
	setGroupCurator(t, admin, g.ID, 0)
	expect(t, one, "GET", path, nil, 200)
	expect(t, admin, "PATCH", groupPath(extra.ID), map[string]any{"status": "archived"}, 200)
	expect(t, one, "GET", path, nil, 404)
	expect(t, two, "GET", path, nil, 200)
	var derived struct {
		Curators []struct {
			ID int64 `json:"id"`
		} `json:"curators"`
	}
	decode(t, expect(t, admin, "GET", fmt.Sprintf("/students/%d", child.ID), nil, 200), &derived)
	if len(derived.Curators) != 1 || derived.Curators[0].ID != second.ID {
		t.Fatal("student curators must derive from current groups")
	}
	setGroupCurator(t, admin, target.ID, 0)
	expect(t, two, "GET", path, nil, 404)
}
