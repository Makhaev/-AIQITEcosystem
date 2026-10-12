package lessons

import (
	"context"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/modules/programs"
	"aiq.local/core/internal/modules/teachers"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

const scopeSQL = `($1 OR ($2 AND t.user_id=$4) OR ($3 AND (
 (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.group_id=l.group_id AND s.curator_id=$4 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)))
 OR (l.roster_initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM attendance a JOIN students s ON s.id=a.student_id WHERE a.lesson_id=l.id AND a.included AND s.curator_id=$4))
)))`

func flags(actor httpx.Actor) (bool, bool, bool) {
	scope := access.ScopeFor(actor, access.Lessons, access.Read)
	return scope == access.All, scope&access.Own != 0, scope&access.Assigned != 0
}
func (r *Repository) begin(ctx context.Context, resource access.Resource, action access.Action, write bool) (pgx.Tx, httpx.Actor, error) {
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		options = pgx.TxOptions{}
	}
	tx, err := r.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, httpx.Actor{}, err
	}
	var actor httpx.Actor
	if write {
		err = access.LockManagement(ctx, tx)
		if err == nil {
			actor, err = access.Authorize(ctx, tx, resource, action)
		}
	} else {
		actor, err = access.AuthorizeRead(ctx, tx, resource)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}
func load(ctx context.Context, tx pgx.Tx, id int64, lock bool) (Lesson, error) {
	query := `SELECT ` + columns + fromSQL + ` WHERE l.id=$1`
	if lock {
		query += ` FOR UPDATE OF l`
	}
	return scan(tx.QueryRow(ctx, query, id))
}
func scoped(ctx context.Context, tx pgx.Tx, actor httpx.Actor, id int64, lock bool) (Lesson, error) {
	all, own, assigned := flags(actor)
	query := `SELECT ` + columns + fromSQL + ` WHERE ` + scopeSQL + ` AND l.id=$5`
	if lock {
		query += ` FOR UPDATE OF l`
	}
	return scan(tx.QueryRow(ctx, query, all, own, assigned, actor.ID, id))
}
func lockGroup(ctx context.Context, tx pgx.Tx, id int64) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT status FROM groups WHERE id=$1 FOR SHARE`, id).Scan(&status)
	return status, err
}

// Every journal writer locks group before lesson. Enrollment/transfer writers
// lock that same group first, so the first snapshot sees a committed composition.
func lockLesson(ctx context.Context, tx pgx.Tx, actor httpx.Actor, id int64) (Lesson, error) {
	initial, err := scoped(ctx, tx, actor, id, false)
	if err != nil {
		return initial, err
	}
	if _, err = lockGroup(ctx, tx, initial.GroupID); err != nil {
		return initial, err
	}
	v, err := scoped(ctx, tx, actor, id, true)
	if err != nil {
		return v, err
	}
	if v.GroupID != initial.GroupID {
		return v, httpx.E(409, "concurrent_change", "Группа занятия изменилась. Повторите операцию.")
	}
	return v, nil
}
func (r *Repository) Get(ctx context.Context, id int64) (Lesson, error) {
	tx, actor, err := r.begin(ctx, access.Lessons, access.Read, false)
	if err != nil {
		return Lesson{}, err
	}
	defer tx.Rollback(ctx)
	v, err := scoped(ctx, tx, actor, id, false)
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, f Filter) (Page, error) {
	result := Page{Items: []Lesson{}, Page: f.Page, PageSize: f.Size}
	f.Search = strings.ToLower(strings.TrimSpace(f.Search))
	if f.Page < 1 || f.Page > 1000000 || f.Size < 1 || f.Size > 100 {
		return result, httpx.E(400, "invalid_pagination", "Проверьте параметры страницы.")
	}
	if utf8.RuneCountInString(f.Search) > 200 || strings.ContainsRune(f.Search, 0) {
		return result, httpx.Invalid("search", "Поиск: не более 200 символов без нулевых символов.")
	}
	if f.Status != "" && !validStatus(f.Status) {
		return result, httpx.Invalid("status", "Допустимые статусы: scheduled, completed и cancelled.")
	}
	if f.GroupID < 0 || f.TeacherID < 0 {
		return result, httpx.E(400, "invalid_filter", "Идентификаторы должны быть положительными.")
	}
	var from, to any
	if f.DateFrom != "" {
		v, err := parseDate(f.DateFrom, "date_from")
		if err != nil {
			return result, err
		}
		from = v
	}
	if f.DateTo != "" {
		v, err := parseDate(f.DateTo, "date_to")
		if err != nil {
			return result, err
		}
		to = v.AddDate(0, 0, 1)
	}
	if f.DateFrom != "" && f.DateTo != "" && f.DateFrom > f.DateTo {
		return result, httpx.Invalid("date_to", "Окончание периода не может быть раньше начала.")
	}
	tx, actor, err := r.begin(ctx, access.Lessons, access.Read, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all, own, assigned := flags(actor)
	where := ` WHERE ` + scopeSQL + ` AND ($5='' OR strpos(l.topic_search,$5)>0) AND ($6='' OR l.status=$6) AND ($7::timestamptz IS NULL OR l.starts_at>=$7) AND ($8::timestamptz IS NULL OR l.starts_at<$8) AND ($9::bigint=0 OR l.group_id=$9) AND ($10::bigint=0 OR l.teacher_id=$10)`
	args := []any{all, own, assigned, actor.ID, f.Search, f.Status, from, to, f.GroupID, f.TeacherID}
	if err = tx.QueryRow(ctx, `SELECT count(*)`+fromSQL+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	args = append(args, f.Size, (f.Page-1)*f.Size)
	rows, err := tx.Query(ctx, `SELECT `+columns+fromSQL+where+` ORDER BY l.starts_at,l.id LIMIT $11 OFFSET $12`, args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v, e := scan(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (r *Repository) Save(ctx context.Context, id int64, in Input, patch *Update) (Lesson, error) {
	tx, actor, err := r.begin(ctx, access.Lessons, access.Manage, true)
	if err != nil {
		return Lesson{}, err
	}
	defer tx.Rollback(ctx)
	var old Lesson
	if patch != nil {
		old, err = load(ctx, tx, id, false)
		if err != nil {
			return Lesson{}, err
		}
		in = Input{GroupID: old.GroupID, ModuleID: old.ModuleID, Topic: old.Topic, TeacherID: old.TeacherID, StartsAt: old.StartsAt, EndsAt: old.EndsAt, Location: old.Location, Status: old.Status}
		if err = apply(&in, *patch); err != nil {
			return Lesson{}, err
		}
	}
	if err = normalize(&in); err != nil {
		return Lesson{}, err
	}
	if patch != nil && old.GroupID != in.GroupID {
		first, second := old.GroupID, in.GroupID
		if first > second {
			first, second = second, first
		}
		if _, err = lockGroup(ctx, tx, first); err != nil {
			return Lesson{}, err
		}
		if _, err = lockGroup(ctx, tx, second); err != nil {
			return Lesson{}, err
		}
	}
	groupStatus, err := lockGroup(ctx, tx, in.GroupID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Lesson{}, httpx.Invalid("group_id", "Группа не найдена.")
		}
		return Lesson{}, err
	}
	if (patch == nil || old.GroupID != in.GroupID) && groupStatus != "active" {
		return Lesson{}, httpx.E(409, "group_not_active", "Новое занятие можно назначить только активной группе.")
	}
	if patch != nil {
		current, e := load(ctx, tx, id, true)
		if e != nil {
			return Lesson{}, e
		}
		if !current.UpdatedAt.Equal(old.UpdatedAt) || current.GroupID != old.GroupID {
			return Lesson{}, httpx.E(409, "concurrent_change", "Занятие изменилось одновременно. Повторите операцию.")
		}
	}
	if patch == nil || old.TeacherID != in.TeacherID {
		if _, err = teachers.RequireActive(ctx, tx, in.TeacherID); err != nil {
			return Lesson{}, err
		}
	}
	if err = programs.RequireGroupModule(ctx, tx, in.GroupID, in.ModuleID); err != nil {
		return Lesson{}, err
	}
	action := "lesson.created"
	storedStatus := in.Status
	if in.Status == "completed" && (patch == nil || old.Status != "completed") {
		if old.Status == "cancelled" {
			return Lesson{}, httpx.E(409, "lesson_cancelled", "Отменённое занятие нельзя завершить. Сначала восстановите его в расписании.")
		}
		storedStatus = "scheduled"
	}
	if patch == nil {
		err = tx.QueryRow(ctx, `INSERT INTO lessons(group_id,module_id,topic,topic_search,teacher_id,starts_at,ends_at,location,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, in.GroupID, in.ModuleID, in.Topic, strings.ToLower(in.Topic), in.TeacherID, in.StartsAt, in.EndsAt, in.Location, storedStatus).Scan(&id)
	} else {
		action = "lesson.updated"
		if in.Status != old.Status {
			action = "lesson." + in.Status
		}
		// Derive the new program from the group; the caller cannot override it.
		_, err = tx.Exec(ctx, `UPDATE lessons SET group_id=$2,program_id=(SELECT program_id FROM groups WHERE id=$2),module_id=$3,topic=$4,topic_search=$5,teacher_id=$6,starts_at=$7,ends_at=$8,location=$9,status=$10,updated_at=clock_timestamp() WHERE id=$1`, id, in.GroupID, in.ModuleID, in.Topic, strings.ToLower(in.Topic), in.TeacherID, in.StartsAt, in.EndsAt, in.Location, storedStatus)
	}
	if err != nil {
		return Lesson{}, err
	}
	v, err := load(ctx, tx, id, false)
	if err != nil {
		return v, err
	}
	if in.Status == "completed" && storedStatus != "completed" {
		v, err = complete(ctx, tx, actor, v)
		if err != nil {
			return v, err
		}
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "lesson", id); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
