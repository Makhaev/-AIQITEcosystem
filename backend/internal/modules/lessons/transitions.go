package lessons

import (
	"aiq.local/core/internal/modules/attendance"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/lessonstore"
	"context"
	"github.com/jackc/pgx/v5"
)

// Timing, persisted marks and cancellation guards are enforced in PostgreSQL
// under the lesson lock. This state rule is shared by PATCH and /complete.
func transition(from, to string) error {
	if !validStatus(to) || from != "" && !validStatus(from) {
		return httpx.Invalid("status", "Недопустимый статус занятия.")
	}
	if from == "cancelled" && to == "completed" {
		return httpx.E(409, "lesson_cancelled", "Отменённое занятие нельзя завершить. Сначала восстановите его в расписании.")
	}
	return nil
}

func complete(ctx context.Context, tx pgx.Tx, actor httpx.Actor, v Lesson) (Lesson, error) {
	if err := transition(v.Status, "completed"); err != nil {
		return v, err
	}
	if v.Status == "completed" {
		return v, nil
	}
	if err := attendance.InitializeJournal(ctx, tx, actor, &v); err != nil {
		return v, err
	}
	// PostgreSQL validates end time and all persisted marks under this lesson lock.
	if _, err := tx.Exec(ctx, `UPDATE lessons SET status='completed',updated_at=clock_timestamp(),
 snapshot_program_name=CASE WHEN snapshot_state='not_captured' THEN CASE WHEN version_module_key<>'' THEN plan_snapshot->>'program_name' ELSE (SELECT name FROM programs WHERE id=lessons.program_id) END ELSE snapshot_program_name END,
 snapshot_module_name=CASE WHEN snapshot_state='not_captured' THEN CASE WHEN version_module_key<>'' THEN plan_snapshot->>'module_name' ELSE (SELECT name FROM program_modules WHERE id=lessons.module_id) END ELSE snapshot_module_name END,
 snapshot_topic=CASE WHEN snapshot_state='not_captured' THEN topic ELSE snapshot_topic END,
 snapshot_captured_at=CASE WHEN snapshot_state='not_captured' THEN clock_timestamp() ELSE snapshot_captured_at END,
 snapshot_state=CASE WHEN snapshot_state='not_captured' THEN 'captured' ELSE snapshot_state END WHERE id=$1`, v.ID); err != nil {
		return v, err
	}
	return lessonstore.Load(ctx, tx, v.ID, false)
}

func (r *Repository) Complete(ctx context.Context, id int64) (Lesson, error) {
	tx, actor, err := r.begin(ctx, access.Attendance, access.Mark, true)
	if err != nil {
		return Lesson{}, err
	}
	defer tx.Rollback(ctx)
	v, err := lessonstore.LockLesson(ctx, tx, actor, id)
	if err != nil {
		return v, err
	}
	if !access.LessonAllowed(actor, v.TeacherUserID, true) {
		return v, httpx.Forbidden()
	}
	previousStatus := v.Status
	v, err = complete(ctx, tx, actor, v)
	if err != nil {
		return v, err
	}
	if previousStatus != "completed" {
		if err = database.AuditObject(ctx, tx, &actor.ID, "lesson.completed", "lesson", id, "status"); err != nil {
			return v, err
		}
	}
	return v, tx.Commit(ctx)
}
