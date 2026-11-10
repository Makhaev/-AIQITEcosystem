package app_test

import (
	"context"
	"fmt"
	"testing"
)

func TestCuratorsDirectoryActivityAndPermissions(t *testing.T) {
	a := setupAttendance(t)
	u, curator := provision(t, a.f, a.admin, "directory-curator@example.test", "curator")
	_, leader := provision(t, a.f, a.admin, "directory-leader@example.test", "leader")
	other, _ := provision(t, a.f, a.admin, "directory-empty@example.test", "curator")
	setGroupCurator(t, a.admin, a.groupID, u.ID)
	reportID := resultID(t, expect(t, curator, "POST", "/curator-reports", map[string]any{"student_id": a.firstID, "summary": "Directory activity"}, 201))
	path := fmt.Sprintf("/curators/%d", u.ID)
	var card struct {
		ID             int64 `json:"id"`
		GroupsCount    int   `json:"groups_count"`
		StudentsCount  int   `json:"students_count"`
		ReportsCount   int   `json:"reports_count"`
		PublishedCount int   `json:"published_count"`
		Recent         []struct {
			ID int64 `json:"id"`
		} `json:"recent_reports"`
	}
	decode(t, expect(t, leader, "GET", path, nil, 200), &card)
	if card.ID != u.ID || card.GroupsCount != 1 || card.StudentsCount != 2 || card.ReportsCount != 1 || card.PublishedCount != 0 || len(card.Recent) != 1 || card.Recent[0].ID != reportID {
		t.Fatalf("unexpected activity: %+v", card)
	}
	_, total := personalPage(t, a.admin, "/curators")
	if total != 2 {
		t.Fatalf("want both curators including one without activity, got %d", total)
	}
	_, total = personalPage(t, leader, "/curators?search=directory-empty")
	if total != 1 {
		t.Fatal("search failed")
	}
	for _, b := range []*browser{curator, a.own} {
		expect(t, b, "GET", "/curators", nil, 403)
		expect(t, b, "GET", path, nil, 403)
	}
	expect(t, a.f.browser(t), "GET", path, nil, 401)
	expect(t, a.admin, "GET", fmt.Sprintf("/curators/%d", a.f.admin.ID), nil, 404)
	expect(t, a.admin, "GET", "/curators/invalid", nil, 400)
	expect(t, a.admin, "GET", "/curators?status=invalid", nil, 400)
	expect(t, a.admin, "GET", "/curators?page=0", nil, 400)
	if _, err := a.f.pool.Exec(context.Background(), `UPDATE users SET blocked=true WHERE id=$1`, other.ID); err != nil {
		t.Fatal(err)
	}
	_, total = personalPage(t, leader, "/curators?status=blocked")
	if total != 1 {
		t.Fatal("blocked filter failed")
	}
	if _, err := a.f.pool.Exec(context.Background(), `UPDATE users SET blocked=false WHERE id=$1`, other.ID); err != nil {
		t.Fatal(err)
	}
	setGroupCurator(t, a.admin, a.groupID, other.ID)
	decode(t, expect(t, a.admin, "GET", path, nil, 200), &card)
	if card.GroupsCount != 0 || card.StudentsCount != 0 || card.ReportsCount != 1 {
		t.Fatal("assignment transfer must preserve authored activity")
	}
}
