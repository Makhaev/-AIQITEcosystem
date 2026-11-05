package app_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCuratorProgressPublicationRecipientsAndFeedback(t *testing.T) {
	a := setupAttendance(t)
	ctx := context.Background()
	ownUser, own := provision(t, a.f, a.admin, "own-curator-workspace@example.test", "curator")
	foreignUser, foreign := provision(t, a.f, a.admin, "other-curator-workspace@example.test", "curator")
	setGroupCurator(t, a.admin, a.groupID, ownUser.ID)
	curatedTestGroup(t, a.admin, a.secondID, foreignUser.ID)
	firstParent := parentCreate(t, a.admin, map[string]any{"name": "Получатель", "email": "recipient-curator@example.test"})
	secondParent := parentCreate(t, a.admin, map[string]any{"name": "Другой родитель", "email": "not-recipient-curator@example.test"})
	noAccount := parentCreate(t, a.admin, map[string]any{"name": "Без кабинета", "phone": "+79000000000"})
	for _, id := range []int64{firstParent.ID, secondParent.ID} {
		expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/parents", a.firstID), map[string]any{"parent_id": id, "relationship": "Родитель", "is_primary": false}, 201)
	}
	expect(t, a.admin, "POST", fmt.Sprintf("/students/%d/parents", a.secondID), map[string]any{"parent_id": firstParent.ID, "relationship": "Родитель", "is_primary": false}, 201)
	_, parent := invite(t, a.f, a.admin, "parent", firstParent.ID, "recipient-curator@example.test")
	_, notRecipient := invite(t, a.f, a.admin, "parent", secondParent.ID, "not-recipient-curator@example.test")
	current := a.lesson(t, -3*time.Hour, -2*time.Hour)
	previous := a.lesson(t, -40*24*time.Hour-3*time.Hour, -40*24*time.Hour-2*time.Hour)
	for _, l := range []struct {
		id     int64
		status string
	}{{current.ID, "present"}, {previous.ID, "absent"}} {
		lessonJournal(t, a.own, l.id)
		expect(t, a.own, "PUT", lessonPath(l.id)+"/attendance", marksBody(markValue(a.firstID, l.status, "Внутренний комментарий педагога"), markValue(a.secondID, "present", "")), 204)
		expect(t, a.own, "POST", lessonPath(l.id)+"/complete", nil, 200)
	}
	var teacherUser int64
	if err := a.f.pool.QueryRow(ctx, `SELECT user_id FROM teachers WHERE id=$1`, a.teacherID).Scan(&teacherUser); err != nil {
		t.Fatal(err)
	}
	for _, task := range []struct {
		lesson int64
		status string
	}{{current.ID, "reviewed"}, {current.ID, "assigned"}, {previous.ID, "assigned"}} {
		if _, err := a.f.pool.Exec(ctx, `INSERT INTO assignments(lesson_id,student_id,title,instructions,status,answer,feedback,created_by) VALUES($1,$2,'Проверяемая работа','Задание',$3,'Ответ ученика','Комментарий педагога',$4)`, task.lesson, a.firstID, task.status, teacherUser); err != nil {
			t.Fatal(err)
		}
	}
	list, total := personalPage(t, own, "/curator/students?page_size=1")
	if total != 2 || len(list) != 1 || int64(list[0]["id"].(float64)) != a.firstID {
		t.Fatal("assigned student scope leaked")
	}
	if len(list[0]["parents"].([]any)) != 2 {
		t.Fatal("linked family missing")
	}
	_, total = personalPage(t, own, "/curator/students?search=%25")
	if total != 0 {
		t.Fatal("wildcard search broadened list")
	}
	path := fmt.Sprintf("/curator/students/%d", a.firstID)
	var detail struct {
		Progress struct {
			Current struct {
				Attendance struct {
					Percent *float64 `json:"percent"`
				} `json:"attendance"`
				Assignments struct {
					Percent *float64 `json:"percent"`
				} `json:"assignments"`
			} `json:"current"`
			Previous struct {
				Attendance struct {
					Percent *float64 `json:"percent"`
				} `json:"attendance"`
			} `json:"previous"`
		} `json:"progress"`
	}
	decode(t, expect(t, own, "GET", path, nil, 200), &detail)
	if detail.Progress.Current.Attendance.Percent == nil || *detail.Progress.Current.Attendance.Percent != 100 || detail.Progress.Previous.Attendance.Percent == nil || *detail.Progress.Previous.Attendance.Percent != 0 || detail.Progress.Current.Assignments.Percent == nil || *detail.Progress.Current.Assignments.Percent != 50 {
		t.Fatalf("incorrect progress: %+v", detail)
	}
	expect(t, foreign, "GET", path, nil, 404)
	expect(t, own, "GET", fmt.Sprintf("/curator/students/%d", a.secondID), nil, 200)
	zone, _ := time.LoadLocation("Europe/Moscow")
	today := a.base.In(zone)
	body := map[string]any{"student_id": a.firstID, "from": today.AddDate(0, 0, -29).Format("2006-01-02"), "to": today.Format("2006-01-02"), "summary": "Ученик стал регулярно посещать занятия.", "recommendations": "Обсуждать задания дома.", "goals": "Завершить оставшуюся работу."}
	expect(t, foreign, "POST", "/curator-reports", body, 404)
	reportID := resultID(t, expect(t, own, "POST", "/curator-reports", body, 201))
	reportPath := fmt.Sprintf("/curator-reports/%d", reportID)
	expect(t, parent, "GET", reportPath, nil, 404)
	expect(t, notRecipient, "GET", reportPath, nil, 404)
	expect(t, foreign, "GET", reportPath, nil, 404)
	expect(t, foreign, "PATCH", reportPath, body, 404)
	if len(items(t, parent, "/curator-reports")) != 0 {
		t.Fatal("draft is visible to parent")
	}
	body["summary"] = "Обновлённый итог для семьи."
	expect(t, own, "PATCH", reportPath, body, 200)
	expect(t, own, "POST", reportPath+"/publish", map[string]any{"parent_ids": []int64{noAccount.ID}}, 400)
	expect(t, own, "POST", reportPath+"/publish", map[string]any{"parent_ids": []int64{firstParent.ID, firstParent.ID}}, 400)
	expect(t, foreign, "POST", reportPath+"/publish", map[string]any{"parent_ids": []int64{firstParent.ID}}, 404)
	expect(t, own, "POST", reportPath+"/publish", map[string]any{"parent_ids": []int64{firstParent.ID}}, 200)
	expect(t, own, "POST", reportPath+"/publish", map[string]any{"parent_ids": []int64{firstParent.ID}}, 409)
	expect(t, own, "PATCH", reportPath, body, 409)
	visible := expect(t, parent, "GET", reportPath, nil, 200)
	if strings.Contains(string(visible), "Внутренний комментарий") || strings.Contains(string(visible), "Другой родитель") || !strings.Contains(string(visible), `"can_feedback":true`) {
		t.Fatal("parent response leaked staff/family data or cannot reply")
	}
	expect(t, notRecipient, "GET", reportPath, nil, 404)
	expect(t, notRecipient, "POST", reportPath+"/feedback", map[string]string{"message": "Чужой получатель"}, 404)
	expect(t, parent, "POST", reportPath+"/feedback", map[string]string{"message": "Обсудим оставшееся задание дома."}, 201)
	expect(t, own, "POST", reportPath+"/feedback", map[string]string{"message": "Не родитель"}, 403)
	if !strings.Contains(string(expect(t, own, "GET", "/curator/workspace", nil, 200)), "Обсудим оставшееся задание дома.") {
		t.Fatal("parent feedback did not reach curator")
	}
	// Live progress can change; published evidence must remain at 50 percent.
	if _, err := a.f.pool.Exec(ctx, `UPDATE assignments SET status='reviewed' WHERE lesson_id=$1 AND student_id=$2`, current.ID, a.firstID); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Snapshot struct {
			Current struct {
				Assignments struct {
					Percent float64 `json:"percent"`
				} `json:"assignments"`
			} `json:"current"`
		} `json:"snapshot"`
	}
	decode(t, expect(t, parent, "GET", reportPath, nil, 200), &report)
	if report.Snapshot.Current.Assignments.Percent != 50 {
		t.Fatal("published metrics changed")
	}
	// Reassignment removes the old curator's access immediately, preserving family access.
	setGroupCurator(t, a.admin, a.groupID, foreignUser.ID)
	expect(t, own, "GET", reportPath, nil, 404)
	expect(t, own, "GET", path, nil, 404)
	expect(t, foreign, "GET", reportPath, nil, 200)
	expect(t, parent, "GET", reportPath, nil, 200)
	if _, err := a.f.pool.Exec(ctx, `DELETE FROM student_parents WHERE student_id=$1 AND parent_id=$2`, a.firstID, firstParent.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, parent, "GET", reportPath, nil, 404)
	expect(t, parent, "POST", reportPath+"/feedback", map[string]string{"message": "Связь удалена"}, 404)
}

func TestCuratorEmptyDataDatesRolesAndConcurrentPublish(t *testing.T) {
	f := setup(t)
	admin := f.adminBrowser(t)
	user, curator := provision(t, f, admin, "period-curator@example.test", "curator")
	child := studentCreate(t, admin, map[string]any{"name": "Без результатов"})
	curatedTestGroup(t, admin, child.ID, user.ID)
	parentCard := parentCreate(t, admin, map[string]any{"name": "Получатель", "email": "period-parent@example.test"})
	expect(t, admin, "POST", fmt.Sprintf("/students/%d/parents", child.ID), map[string]any{"parent_id": parentCard.ID, "relationship": "Родитель", "is_primary": true}, 201)
	_, parent := invite(t, f, admin, "parent", parentCard.ID, "period-parent@example.test")
	detail := expect(t, curator, "GET", fmt.Sprintf("/curator/students/%d", child.ID), nil, 200)
	if !strings.Contains(string(detail), `"percent":null`) {
		t.Fatal("empty progress fabricated a percentage")
	}
	for _, q := range []string{"from=bad", "from=2026-11-04&to=2026-01-01", "from=2020-01-01&to=2026-01-01", "to=2999-01-01"} {
		expect(t, curator, "GET", fmt.Sprintf("/curator/students/%d?%s", child.ID, q), nil, 400)
	}
	expect(t, curator, "GET", "/curator/students?page_size=101", nil, 400)
	for _, role := range []string{"teacher", "student", "leader"} {
		_, b := provision(t, f, admin, "curator-denied-"+role+"@example.test", role)
		expect(t, b, "GET", "/curator/workspace", nil, 403)
		if role != "leader" {
			expect(t, b, "GET", "/curator-reports", nil, 403)
		}
	}
	expect(t, parent, "GET", "/curator/workspace", nil, 403)
	expect(t, admin, "GET", "/curator/workspace", nil, 403)
	expect(t, f.browser(t), "GET", "/curator-reports", nil, 401)
	reportID := resultID(t, expect(t, curator, "POST", "/curator-reports", map[string]any{"student_id": child.ID, "summary": "Пока недостаточно данных, наблюдаем."}, 201))
	path := fmt.Sprintf("/curator-reports/%d/publish", reportID)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := curator.do(t, "POST", path, map[string]any{"parent_ids": []int64{parentCard.ID}})
			codes <- code
		}()
	}
	wg.Wait()
	close(codes)
	success, rejected := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		}
		if code == 409 {
			rejected++
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("concurrent publish: %d %d", success, rejected)
	}
	// Dual roles do not expand the personal student workspace.
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO user_roles(user_id,role) VALUES($1,'admin')`, user.ID); err != nil {
		t.Fatal(err)
	}
	studentCreate(t, admin, map[string]any{"name": "Не закреплён"})
	_, total := personalPage(t, curator, "/curator/students")
	if total != 1 {
		t.Fatal("personal curator view widened through admin role")
	}
	expect(t, admin, "PATCH", fmt.Sprintf("/users/%d", user.ID), map[string]any{"blocked": true}, 200)
	expect(t, curator, "GET", "/curator/workspace", nil, 401)
}
