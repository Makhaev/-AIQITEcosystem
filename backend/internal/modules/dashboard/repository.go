package dashboard

import (
	"context"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/attendance"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

func (r *Repository) Get(ctx context.Context, start, end time.Time) (Overview, error) {
	var result Overview
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = access.AuthorizeRead(ctx, tx, access.Dashboard); err != nil {
		return result, err
	}
	// Counters and marks are read from the same snapshot. Today's counters do
	// not depend on the attendance period; calendar days are Moscow days.
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM students WHERE status='active'),
 (SELECT count(*) FROM groups WHERE status='active'),
 (SELECT count(*) FROM teachers WHERE status='active'),
 (SELECT count(*) FROM lessons WHERE status IN ('scheduled','completed')
  AND starts_at>=((now() AT TIME ZONE 'Europe/Moscow')::date::timestamp AT TIME ZONE 'Europe/Moscow')
  AND starts_at<(((now() AT TIME ZONE 'Europe/Moscow')::date+1)::timestamp AT TIME ZONE 'Europe/Moscow')),
 (SELECT count(*) FROM tasks WHERE status IN ('todo','in_progress') AND due_at<now())`).Scan(
		&result.ActiveStudents, &result.ActiveGroups, &result.ActiveTeachers, &result.LessonsToday, &result.OverdueTasks)
	if err != nil {
		return result, err
	}
	var present, late, absent, excused int
	// Aggregate individual marks across every group, never group percentages.
	err = tx.QueryRow(ctx, `SELECT
 count(*) FILTER (WHERE a.status='present'), count(*) FILTER (WHERE a.status='late'),
 count(*) FILTER (WHERE a.status='absent'), count(*) FILTER (WHERE a.status='excused')
 FROM attendance_records a JOIN lessons l ON l.id=a.lesson_id
 WHERE a.included AND l.status='completed' AND l.starts_at>=$1 AND l.starts_at<$2`, start, end).Scan(
		&present, &late, &absent, &excused)
	if err != nil {
		return result, err
	}
	result.AttendancePercent = attendance.Percent(present, late, absent, excused)
	return result, tx.Commit(ctx)
}
