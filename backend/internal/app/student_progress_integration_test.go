package app_test

import (
	"context"
	"testing"
	"time"
)

func TestStudentGameProgressCountsBeyondListLimitAndExcludesCancelled(t *testing.T) {
	a := setupAttendance(t)
	_, student := invite(t, a.f, a.admin, "student", a.firstID, "game-progress@example.test")
	lesson := a.lesson(t, -3*time.Hour, -2*time.Hour)
	lessonJournal(t, a.own, lesson.ID)
	// More assignments than the learning list returns; HP must still use all rows.
	_, err := a.f.pool.Exec(context.Background(), `INSERT INTO assignments(lesson_id,student_id,title,instructions,created_by)
		SELECT $1,$2,'Game progress assignment','Practice',(SELECT user_id FROM teachers WHERE id=$3) FROM generate_series(1,501)`, lesson.ID, a.firstID, a.teacherID)
	if err != nil {
		t.Fatal(err)
	}
	progress := items(t, student, "/learning/progress")
	if len(progress) != 1 || progress[0]["total"] != float64(501) || progress[0]["assigned"] != float64(501) {
		t.Fatalf("progress truncated at list limit: %+v", progress)
	}
	if _, err = a.f.pool.Exec(context.Background(), `UPDATE lessons SET status='cancelled' WHERE id=$1`, lesson.ID); err != nil {
		t.Fatal(err)
	}
	progress = items(t, student, "/learning/progress")
	if len(progress) != 1 || progress[0]["total"] != float64(0) {
		t.Fatalf("cancelled assignments counted: %+v", progress)
	}
	if _, err = a.f.pool.Exec(context.Background(), `UPDATE students SET status='archived' WHERE id=$1`, a.firstID); err != nil {
		t.Fatal(err)
	}
	if progress = items(t, student, "/learning/progress"); len(progress) != 0 {
		t.Fatal("archived profile still contributes game progress")
	}
}
