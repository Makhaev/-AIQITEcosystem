package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCuratorOverviewAttentionScheduleAndSavedAttendance(t *testing.T) {
	a := setupAttendance(t)
	u, curator := provision(t, a.f, a.admin, "overview-curator@example.test", "curator")
	_, foreign := provision(t, a.f, a.admin, "overview-foreign@example.test", "curator")
	setGroupCurator(t, a.admin, a.groupID, u.ID)
	parent := parentCreate(t, a.admin, map[string]any{"name": "Family contact", "phone": "+79001112233"})
	expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/parents", a.firstID), map[string]any{"parent_id": parent.ID, "relationship": "Parent"}, 201)
	lesson := a.lesson(t, -3*time.Hour, -2*time.Hour)
	lessonJournal(t, a.own, lesson.ID)
	expect(t, a.own, "PUT", lessonPath(lesson.ID)+"/attendance", marksBody(markValue(a.firstID, "absent", "Follow up"), markValue(a.secondID, "present", "")), 204)
	expect(t, a.own, "POST", lessonPath(lesson.ID)+"/complete", nil, 200)
	future := a.lesson(t, 24*time.Hour, 25*time.Hour)
	if _, err := a.f.pool.Exec(context.Background(), `INSERT INTO assignments(lesson_id,student_id,title,instructions,created_by,due_at) VALUES($1,$2,'Late task','Instructions',$3,now()-interval '1 day')`, lesson.ID, a.firstID, a.f.admin.ID); err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Counts struct {
			Attention int `json:"attention"`
		} `json:"counts"`
		Attention []struct {
			ID      int64 `json:"id"`
			Signals struct {
				Missing           bool `json:"missing_family"`
				Absences, Overdue int
			} `json:"attention"`
		} `json:"attention"`
	}
	decode(t, expect(t, curator, "GET", "/curator/workspace", nil, 200), &summary)
	if summary.Counts.Attention != 2 || len(summary.Attention) != 2 || summary.Attention[0].ID != a.firstID || summary.Attention[0].Signals.Missing || summary.Attention[0].Signals.Absences != 1 || summary.Attention[0].Signals.Overdue != 1 {
		t.Fatalf("incorrect factual signals: %+v", summary)
	}
	_, total := personalPage(t, curator, "/curator/students?attention=true")
	if total != 2 {
		t.Fatal("attention filter inconsistent")
	}
	_, total = personalPage(t, foreign, "/curator/students?attention=true")
	if total != 0 {
		t.Fatal("foreign signals leaked")
	}
	expect(t, curator, "GET", "/curator/students?attention=invalid", nil, 400)
	var card struct {
		Schedule []struct {
			ID int64 `json:"id"`
		} `json:"schedule"`
		Attendance []struct{ Status, Comment string } `json:"attendance"`
	}
	decode(t, expect(t, curator, "GET", fmt.Sprintf("/curator/students/%d", a.firstID), nil, 200), &card)
	if len(card.Schedule) != 1 || card.Schedule[0].ID != future.ID || len(card.Attendance) != 1 || card.Attendance[0].Status != "absent" || card.Attendance[0].Comment != "Follow up" {
		t.Fatalf("student learning data missing: %+v", card)
	}
	// Resolve both factual signals. The first pupil must leave the attention list.
	if _, err := a.f.pool.Exec(context.Background(), `UPDATE assignments SET status='reviewed',answer='Completed work',feedback='Accepted' WHERE student_id=$1`, a.firstID); err != nil {
		t.Fatal(err)
	}
	expect(t, a.admin, "PUT", lessonPath(lesson.ID)+"/attendance", marksBody(markValue(a.firstID, "excused", "Resolved")), 204)
	rows, total := personalPage(t, curator, "/curator/students?attention=true")
	if total != 1 || int64(rows[0]["id"].(float64)) != a.secondID {
		t.Fatal("resolved signals stayed in attention list")
	}
	setGroupCurator(t, a.admin, a.groupID, 0)
	expect(t, curator, "GET", fmt.Sprintf("/curator/students/%d", a.firstID), nil, 404)
}
