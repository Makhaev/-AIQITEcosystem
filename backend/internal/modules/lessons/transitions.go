package lessons

import (
	"context"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

func complete(ctx context.Context, tx pgx.Tx, actor httpx.Actor, v Lesson) (Lesson, error) {
	if v.Status == "cancelled" {
		return v, httpx.E(409, "lesson_cancelled", "Отменённое занятие нельзя завершить. Сначала восстановите его в расписании.")
	}
	if v.Status == "completed" {
		return v, nil
	}
	if err := initializeJournal(ctx, tx, actor, &v); err != nil {
		return v, err
	}
	// PostgreSQL validates end time and all persisted marks under this lesson lock.
	if _, err := tx.Exec(ctx, `UPDATE lessons SET status='completed',updated_at=clock_timestamp() WHERE id=$1`, v.ID); err != nil {
		return v, err
	}
	return load(ctx, tx, v.ID, false)
}
func (r *Repository) Complete(ctx context.Context, id int64) (Lesson, error) {
	tx, actor, err := r.begin(ctx, access.Attendance, access.Mark, true)
	if err != nil {
		return Lesson{}, err
	}
	defer tx.Rollback(ctx)
	v, err := lockLesson(ctx, tx, actor, id)
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
		if err = database.AuditObject(ctx, tx, &actor.ID, "lesson.completed", "lesson", id); err != nil {
			return v, err
		}
	}
	return v, tx.Commit(ctx)
}
