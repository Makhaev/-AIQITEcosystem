package app_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHomeworkTestsCreationCompletionProgressAndIsolation(t *testing.T) {
	a := setupAttendance(t)
	u, curator := provision(t, a.f, a.admin, "quiz-curator@example.test", "curator")
	_, methodist := provision(t, a.f, a.admin, "quiz-methodist@example.test", "methodist")
	_, foreignCurator := provision(t, a.f, a.admin, "quiz-foreign-curator@example.test", "curator")
	_, leader := provision(t, a.f, a.admin, "quiz-leader@example.test", "leader")
	setGroupCurator(t, a.admin, a.groupID, u.ID)
	_, student := invite(t, a.f, a.admin, "student", a.firstID, "quiz-student@example.test")
	_, otherStudent := invite(t, a.f, a.admin, "student", a.secondID, "quiz-other-student@example.test")
	body := map[string]any{
		"title": "Тест на логику", "instructions": "Выберите правильные ответы.", "student_ids": []int64{a.firstID},
		"questions": []map[string]any{{"prompt": "Сколько будет 2 + 2?", "options": []string{"3", "4"}, "correct": 1}},
	}
	for _, viewer := range []*browser{student, a.own, leader} {
		expect(t, viewer, "POST", "/homework-tests", body, 403)
	}
	expect(t, foreignCurator, "POST", "/homework-tests", body, 400)
	expect(t, a.f.browser(t), "GET", "/homework-tests", nil, 401)
	if got := items(t, methodist, "/homework-tests"); len(got) != 0 {
		t.Fatal("foreign assignment must roll back")
	}
	if got := items(t, curator, "/homework-tests/students"); len(got) != 2 {
		t.Fatal("curator student choices missing")
	}
	if got := items(t, foreignCurator, "/homework-tests/students"); len(got) != 0 {
		t.Fatal("foreign curator can select students")
	}
	expect(t, student, "GET", "/homework-tests/students", nil, 403)
	expect(t, curator, "POST", "/homework-tests", body, 201)
	rows := items(t, student, "/homework-tests")
	if len(rows) != 1 {
		t.Fatalf("want one test, got %+v", rows)
	}
	id := int64(rows[0]["id"].(float64))
	path := fmt.Sprintf("/homework-tests/%d/submit", id)
	raw := expect(t, student, "GET", "/homework-tests", nil, 200)
	if strings.Contains(string(raw), `"correct":`) {
		t.Fatal("answer key exposed to student")
	}
	if got := items(t, otherStudent, "/homework-tests"); len(got) != 0 {
		t.Fatal("test leaked to another student")
	}
	expect(t, otherStudent, "POST", path, map[string]any{"answers": []int{1}}, 404)
	expect(t, curator, "POST", path, map[string]any{"answers": []int{1}}, 403)
	expect(t, student, "POST", path, map[string]any{"answers": []int{}}, 400)
	expect(t, student, "POST", path, map[string]any{"answers": []int{2}}, 400)
	expect(t, student, "POST", path, map[string]any{"answers": []int{0}}, 200)
	progress := items(t, student, "/learning/progress")
	if progress[0]["completed_tests"] != float64(0) || progress[0]["reviewed"] != float64(0) || progress[0]["total"] != float64(1) {
		t.Fatalf("failed test awarded progress: %+v", progress)
	}
	expect(t, student, "POST", path, map[string]any{"answers": []int{1}}, 409)
	failed := items(t, student, "/homework-tests")[0]
	if failed["can_submit"] != false || failed["attempts"] != float64(1) || failed["answers"].([]any)[0] != float64(0) {
		t.Fatal("failed attempt was editable or changed")
	}
	_, err := a.f.pool.Exec(context.Background(), `UPDATE homework_test_assignments SET correct_count=1 WHERE id=$1`, id)
	dbConstraint(t, err, "23514", "homework_test_attempt_final")
	// A distinct test has its own single attempt and may earn one mark.
	expect(t, curator, "POST", "/homework-tests", body, 201)
	rows = items(t, student, "/homework-tests")
	path = fmt.Sprintf("/homework-tests/%d/submit", int64(rows[0]["id"].(float64)))
	// Concurrent successful submissions can award the same homework only once.
	var wg sync.WaitGroup
	results := make(chan []byte, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- expect(t, student, "POST", path, map[string]any{"answers": []int{1}}, 200)
		}()
	}
	wg.Wait()
	close(results)
	awarded := 0
	for raw := range results {
		var result struct {
			Awarded bool `json:"awarded"`
		}
		decode(t, raw, &result)
		if result.Awarded {
			awarded++
		}
	}
	if awarded != 1 {
		t.Fatalf("want exactly one award, got %d", awarded)
	}
	progress = items(t, student, "/learning/progress")
	if progress[0]["completed_tests"] != float64(1) || progress[0]["reviewed"] != float64(1) || progress[0]["assigned"] != float64(0) {
		t.Fatalf("missing completed progress: %+v", progress)
	}
	expect(t, student, "POST", path, map[string]any{"answers": []int{0}}, 200)
	if got := items(t, student, "/learning/progress"); got[0]["completed_tests"] != float64(1) {
		t.Fatal("replay changed progress")
	}
	lesson := a.lesson(t, -3*time.Hour, -2*time.Hour)
	lessonJournal(t, a.own, lesson.ID)
	expect(t, a.own, "PUT", lessonPath(lesson.ID)+"/attendance", marksBody(markValue(a.firstID, "present", ""), markValue(a.secondID, "absent", "")), 204)
	expect(t, a.own, "POST", lessonPath(lesson.ID)+"/complete", nil, 200)
	_, err = a.f.pool.Exec(context.Background(), `INSERT INTO assignments(lesson_id,student_id,title,instructions,created_by,status,answer,feedback) VALUES($1,$2,'Accepted homework','Practice',$3,'reviewed','Done','Accepted')`, lesson.ID, a.firstID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		p := items(t, student, "/learning/progress")[0]
		if p["progress_marks"] != float64(3) || p["completed_tests"] != float64(1) || p["accepted_assignments"] != float64(1) || p["completed_lessons"] != float64(1) || p["failed_tests"] != float64(1) {
			t.Fatalf("want one mark per successful activity, got %+v", p)
		}
	}
	if p := items(t, otherStudent, "/learning/progress")[0]; p["progress_marks"] != float64(0) {
		t.Fatal("absent pupil received a lesson mark")
	}
	// Administrative rating matches each student's own marks without exposing answers.
	rating := items(t, a.admin, "/learning/rating")
	if len(rating) != 2 || rating[0]["id"] != float64(a.firstID) || rating[0]["rank"] != float64(1) || rating[0]["progress_marks"] != float64(3) || rating[0]["failed_tests"] != float64(1) || rating[0]["last_activity"] == nil || rating[1]["progress_marks"] != float64(0) || rating[1]["rank"] != float64(2) {
		t.Fatalf("unexpected student rating: %+v", rating)
	}
	filtered := items(t, leader, "/learning/rating?search=second")
	if len(filtered) != 1 || filtered[0]["rank"] != float64(2) {
		t.Fatal("search changed global rank")
	}
	expect(t, a.admin, "PATCH", fmt.Sprintf("/students/%d", a.firstID), map[string]any{"name": "Алиса"}, 200)
	if found := items(t, a.admin, "/learning/rating?search="+url.QueryEscape("АЛИСА")); len(found) != 1 || found[0]["id"] != float64(a.firstID) {
		t.Fatal("Cyrillic name search failed")
	}
	if got := items(t, a.admin, "/learning/rating?page=2"); len(got) != 0 {
		t.Fatal("rating pagination failed")
	}
	for _, viewer := range []*browser{student, curator, methodist, a.own} {
		expect(t, viewer, "GET", "/learning/rating", nil, 403)
	}
	expect(t, a.admin, "GET", "/learning/rating?page=0", nil, 400)
	if _, err = a.f.pool.Exec(context.Background(), `UPDATE students SET status='archived' WHERE id=$1`, a.secondID); err != nil {
		t.Fatal(err)
	}
	if got := items(t, a.admin, "/learning/rating"); len(got) != 1 {
		t.Fatal("archived student included in rating")
	}
	if _, err = a.f.pool.Exec(context.Background(), `UPDATE students SET status='active' WHERE id=$1`, a.secondID); err != nil {
		t.Fatal(err)
	}
	// Methodists can assign outside a curator's groups; creators see their own results.
	body["student_ids"] = []int64{a.firstID, a.secondID}
	expect(t, methodist, "POST", "/homework-tests", body, 201)
	if got := items(t, methodist, "/homework-tests"); len(got) != 2 {
		t.Fatal("methodist assignments missing")
	}
	if got := items(t, curator, "/homework-tests"); len(got) != 2 {
		t.Fatal("curator saw another author's tests")
	}
	expect(t, student, "GET", "/homework-tests?page=0", nil, 400)
	body["student_ids"] = []int64{a.firstID, a.firstID}
	expect(t, methodist, "POST", "/homework-tests", body, 400)
	body["student_ids"] = []int64{a.firstID}
	body["questions"] = []map[string]any{{"prompt": "Invalid", "options": []string{"same", "same"}, "correct": 0}}
	expect(t, methodist, "POST", "/homework-tests", body, 400)
	// Transferring a group revokes the previous curator's access to pupils' answers.
	setGroupCurator(t, a.admin, a.groupID, 0)
	if got := items(t, curator, "/homework-tests"); len(got) != 0 {
		t.Fatal("former curator retained access")
	}
}
