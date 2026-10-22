package lessonstore

import (
	lessonmodel "aiq.local/core/internal/domain/lesson"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
	_ "time/tzdata"
)

type Lesson = lessonmodel.Lesson

const scopeSQL = access.LessonReadScopeSQL

const HistoricalTopic = `CASE WHEN l.status='completed' AND l.snapshot_state='captured' THEN l.snapshot_topic ELSE l.topic END`

const Columns = `l.id,l.group_id,g.name,l.program_id,l.module_id,
 CASE WHEN l.status='completed' AND l.snapshot_state='captured' THEN l.snapshot_module_name ELSE CASE WHEN l.version_module_key<>'' THEN l.plan_snapshot->>'module_name' ELSE m.name END END,` + HistoricalTopic + `,l.teacher_id,t.name,t.user_id,l.starts_at,l.ends_at,l.location,l.status,l.roster_initialized_at,l.created_at,l.updated_at,
 CASE WHEN l.status='completed' AND l.snapshot_state='captured' THEN l.snapshot_program_name ELSE CASE WHEN l.version_module_key<>'' THEN l.plan_snapshot->>'program_name' ELSE p.name END END,l.snapshot_state,l.snapshot_captured_at,l.program_version_id,l.version_module_key,l.template_key,l.plan_snapshot`

const FromSQL = ` FROM lessons l JOIN groups g ON g.id=l.group_id JOIN programs p ON p.id=l.program_id JOIN teachers t ON t.id=l.teacher_id LEFT JOIN program_modules m ON m.id=l.module_id`

func Scan(row pgx.Row) (Lesson, error) {
	var v Lesson
	err := row.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.ProgramID, &v.ModuleID, &v.ModuleName, &v.Topic, &v.TeacherID, &v.TeacherName, &v.TeacherUserID, &v.StartsAt, &v.EndsAt, &v.Location, &v.Status, &v.RosterInitializedAt, &v.CreatedAt, &v.UpdatedAt, &v.ProgramName, &v.SnapshotState, &v.SnapshotCapturedAt, &v.ProgramVersionID, &v.VersionModuleKey, &v.TemplateKey, &v.PlanSnapshot)
	v.StartsAt = v.StartsAt.UTC()
	v.EndsAt = v.EndsAt.UTC()
	return v, err
}

func Flags(actor httpx.Actor) (bool, bool, bool) {
	scope := access.ScopeFor(actor, access.Lessons, access.Read)
	return scope == access.All, scope&access.Own != 0, scope&access.Assigned != 0
}

func Load(ctx context.Context, tx pgx.Tx, id int64, lock bool) (Lesson, error) {
	query := `SELECT ` + Columns + FromSQL + ` WHERE l.id=$1`
	if lock {
		query += ` FOR UPDATE OF l`
	}
	return Scan(tx.QueryRow(ctx, query, id))
}

func Scoped(ctx context.Context, tx pgx.Tx, actor httpx.Actor, id int64, lock bool) (Lesson, error) {
	all, own, assigned := Flags(actor)
	query := `SELECT ` + Columns + FromSQL + ` WHERE ` + scopeSQL + ` AND l.id=$5`
	if lock {
		query += ` FOR UPDATE OF l`
	}
	return Scan(tx.QueryRow(ctx, query, all, own, assigned, actor.ID, id))
}

func LockGroup(ctx context.Context, tx pgx.Tx, id int64) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM groups WHERE id=$1 FOR SHARE`, id).Scan(&status)
	return status, err
}

// Every journal writer locks group before lesson. Enrollment/transfer writers
// lock that same group first, so the first snapshot sees a committed composition.

func LockLesson(ctx context.Context, tx pgx.Tx, actor httpx.Actor, id int64) (Lesson, error) {
	initial, err := Scoped(ctx, tx, actor, id, false)
	if err != nil {
		return initial, err
	}
	if _, err = LockGroup(ctx, tx, initial.GroupID); err != nil {
		return initial, err
	}
	v, err := Scoped(ctx, tx, actor, id, true)
	if err != nil {
		return v, err
	}
	if v.GroupID != initial.GroupID {
		return v, httpx.E(409, "concurrent_change", "Группа занятия изменилась. Повторите операцию.")
	}
	return v, nil
}

var Moscow = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		panic(err)
	}
	return loc
}()

func Translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Занятие не найдено.")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "lessons_version_content":
		return httpx.Invalid("template_key", "Выберите модуль и шаблон закреплённой версии.")
	case "lessons_plan_immutable":
		return httpx.E(409, "plan_immutable", "План начавшегося или исторического занятия изменять нельзя.")
	case "lessons_version_program", "lessons_version_matches_group", "lessons_program_matches_group", "lessons_published_version":
		return httpx.E(409, "lesson_version_conflict", "План занятия должен принадлежать закреплённой версии программы.")
	case "lessons_completion_time":
		return httpx.E(409, "lesson_not_ended", "Завершить занятие можно только после его окончания.")
	case "lessons_completion_unmarked":
		return httpx.E(409, "attendance_unmarked", "Перед завершением сохраните отметки для всех учеников.")
	case "lessons_cancellation_marks":
		return httpx.E(409, "lesson_has_marks", "Нельзя отменить занятие с внесёнными отметками посещаемости.")
	case "attendance_lesson_cancelled":
		return httpx.E(409, "lesson_cancelled", "Отменённое занятие не допускает новых отметок.")
	case "attendance_lesson_not_started":
		return httpx.E(409, "lesson_not_started", "Посещаемость можно отметить после начала занятия.")
	case "lessons_teacher_overlap":
		return httpx.E(409, "teacher_overlap", "У педагога уже есть занятие в это время.")
	case "lessons_group_overlap":
		return httpx.E(409, "group_overlap", "У группы уже есть занятие в это время.")
	case "lessons_roster_frozen", "attendance_lesson_group":
		return httpx.E(409, "journal_frozen", "После открытия журнала группа фиксирована; для смены даты используйте перенос с обновлением состава.")
	case "lessons_ends_at_check":
		return httpx.Invalid("ends_at", "Окончание должно быть позже начала.")
	case "lessons_module_program":
		return httpx.Invalid("module_id", "Выберите модуль программы этой группы.")
	case "lessons_group_program":
		return httpx.Invalid("group_id", "Группа не найдена.")
	case "lessons_teacher_id_fkey":
		return httpx.Invalid("teacher_id", "Педагог не найден.")
	}
	if pg.Code == "40001" || pg.Code == "40P01" {
		return httpx.E(409, "concurrent_change", "Данные изменились одновременно. Повторите операцию.")
	}
	return err
}
