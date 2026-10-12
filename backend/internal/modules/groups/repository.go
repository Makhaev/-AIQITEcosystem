package groups

import (
	"aiq.local/core/internal/modules/programs"
	"aiq.local/core/internal/modules/teachers"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"unicode/utf8"
)

type Repository struct{ Pool *pgxpool.Pool }

const scopeSQL = `($1 OR ($2 AND (t.user_id=$4 OR EXISTS(SELECT 1 FROM lessons l JOIN teachers lt ON lt.id=l.teacher_id WHERE l.group_id=g.id AND lt.user_id=$4))) OR ($3 AND EXISTS(SELECT 1 FROM group_enrollments ge JOIN students s ON s.id=ge.student_id WHERE ge.group_id=g.id AND s.curator_id=$4)))`

func flags(actor httpx.Actor) (bool, bool, bool) {
	scope := access.ScopeFor(actor, access.Groups, access.Read)
	return scope == access.All, scope&access.Own != 0, scope&access.Assigned != 0
}
func (r *Repository) begin(ctx context.Context, write bool) (pgx.Tx, httpx.Actor, error) {
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
			actor, err = access.Authorize(ctx, tx, access.Groups, access.Manage)
		}
	} else {
		actor, err = access.AuthorizeRead(ctx, tx, access.Groups)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}
func load(ctx context.Context, tx pgx.Tx, id int64, lock bool) (Group, error) {
	query := `SELECT ` + columns + fromSQL + ` WHERE g.id=$1`
	if lock {
		query += ` FOR UPDATE OF g`
	}
	return scan(tx.QueryRow(ctx, query, id))
}
func loadEnrollment(ctx context.Context, tx pgx.Tx, groupID, id int64, lock bool) (Enrollment, error) {
	query := `SELECT ` + enrollmentColumns + ` FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.group_id=$1 AND e.id=$2`
	if lock {
		query += ` FOR UPDATE OF e`
	}
	return scanEnrollment(tx.QueryRow(ctx, query, groupID, id))
}
func (r *Repository) List(ctx context.Context, f Filter) (Page, error) {
	result := Page{Items: []Group{}, Page: f.Page, PageSize: f.Size}
	f.Search = strings.ToLower(strings.TrimSpace(f.Search))
	if f.Page < 1 || f.Page > 1000000 || f.Size < 1 || f.Size > 100 {
		return result, httpx.E(400, "invalid_pagination", "Проверьте параметры страницы.")
	}
	if utf8.RuneCountInString(f.Search) > 200 || strings.ContainsRune(f.Search, 0) {
		return result, httpx.Invalid("search", "Поиск: не более 200 символов без нулевых символов.")
	}
	if f.Status != "" && f.Status != "draft" && f.Status != "active" && f.Status != "archived" {
		return result, httpx.Invalid("status", "Допустимые статусы: draft, active и archived.")
	}
	tx, actor, err := r.begin(ctx, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all, own, assigned := flags(actor)
	where := ` WHERE ` + scopeSQL + ` AND ($5='' OR strpos(g.name_search,$5)>0) AND ($6='' OR g.status=$6)`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+fromSQL+where, all, own, assigned, actor.ID, f.Search, f.Status).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+fromSQL+where+` ORDER BY g.name_search,g.id LIMIT $7 OFFSET $8`, all, own, assigned, actor.ID, f.Search, f.Status, f.Size, (f.Page-1)*f.Size)
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
func (r *Repository) Get(ctx context.Context, id int64) (Card, error) {
	result := Card{Enrollments: []Enrollment{}}
	tx, actor, err := r.begin(ctx, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	all, own, assigned := flags(actor)
	result.Group, err = scan(tx.QueryRow(ctx, `SELECT `+columns+fromSQL+` WHERE `+scopeSQL+` AND g.id=$5`, all, own, assigned, actor.ID, id))
	if err != nil {
		return result, err
	}
	// Curators see only their pupils even when a group is shared with others.
	var teachesGroup bool
	if own {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups g LEFT JOIN teachers t ON t.id=g.teacher_id WHERE g.id=$1 AND (t.user_id=$2 OR EXISTS(SELECT 1 FROM lessons l JOIN teachers lt ON lt.id=l.teacher_id WHERE l.group_id=g.id AND lt.user_id=$2)))`, id, actor.ID).Scan(&teachesGroup)
		if err != nil {
			return result, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT `+enrollmentColumns+` FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.group_id=$1 AND ($2 OR s.curator_id=$3) ORDER BY e.starts_on DESC,e.id DESC`, id, all || teachesGroup, actor.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		v, e := scanEnrollment(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Enrollments = append(result.Enrollments, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (r *Repository) Save(ctx context.Context, id int64, in CreateInput, patch *UpdateInput) (Group, error) {
	tx, actor, err := r.begin(ctx, true)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback(ctx)
	action := "group.created"
	var old Group
	if patch != nil {
		old, err = load(ctx, tx, id, true)
		if err != nil {
			return Group{}, err
		}
		if !patch.Name.Set && !patch.ProgramID.Set && !patch.TeacherID.Set && !patch.Capacity.Set && !patch.Status.Set {
			return Group{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		in = CreateInput{Name: old.Name, ProgramID: old.ProgramID, TeacherID: old.TeacherID, Capacity: old.Capacity, Status: old.Status}
		if err = patchRequired(patch.Name, &in.Name, "name"); err != nil {
			return Group{}, err
		}
		if err = patchRequired(patch.ProgramID, &in.ProgramID, "program_id"); err != nil {
			return Group{}, err
		}
		if err = patchRequired(patch.Capacity, &in.Capacity, "capacity"); err != nil {
			return Group{}, err
		}
		if err = patchRequired(patch.Status, &in.Status, "status"); err != nil {
			return Group{}, err
		}
		if patch.Status.Set && in.Status == "" {
			return Group{}, httpx.Invalid("status", "Статус обязателен.")
		}
		if patch.TeacherID.Set {
			in.TeacherID = patch.TeacherID.Value
		}
		action = "group.updated"
		if in.Status == "archived" && old.Status != "archived" {
			action = "group.archived"
		} else if old.Status == "archived" && in.Status != "archived" {
			action = "group.restored"
		}
	}
	if err = normalize(&in); err != nil {
		return Group{}, err
	}
	if patch == nil || old.ProgramID != in.ProgramID {
		if _, err = programs.RequireActive(ctx, tx, in.ProgramID); err != nil {
			return Group{}, err
		}
	}
	if in.TeacherID != nil && (old.TeacherID == nil || *old.TeacherID != *in.TeacherID) {
		if _, err = teachers.RequireActive(ctx, tx, *in.TeacherID); err != nil {
			return Group{}, err
		}
	}
	if patch == nil {
		err = tx.QueryRow(ctx, `INSERT INTO groups(name,program_id,teacher_id,capacity,status,name_search) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, in.Name, in.ProgramID, in.TeacherID, in.Capacity, in.Status, strings.ToLower(in.Name)).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE groups SET name=$2,program_id=$3,teacher_id=$4,capacity=$5,status=$6,name_search=$7,updated_at=clock_timestamp() WHERE id=$1`, id, in.Name, in.ProgramID, in.TeacherID, in.Capacity, in.Status, strings.ToLower(in.Name))
	}
	if err != nil {
		return Group{}, err
	}
	v, err := load(ctx, tx, id, false)
	if err != nil {
		return Group{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "group", id); err != nil {
		return Group{}, err
	}
	return v, tx.Commit(ctx)
}
func insertEnrollment(ctx context.Context, tx pgx.Tx, groupID, studentID int64, startsOn string) (Enrollment, error) {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO group_enrollments(group_id,student_id,starts_on) VALUES($1,$2,$3::date) RETURNING id`, groupID, studentID, startsOn).Scan(&id)
	if err != nil {
		return Enrollment{}, err
	}
	return loadEnrollment(ctx, tx, groupID, id, false)
}
func (r *Repository) Enroll(ctx context.Context, id int64, in EnrollInput) (Enrollment, error) {
	if in.StudentID <= 0 {
		return Enrollment{}, httpx.Invalid("student_id", "Выберите ученика.")
	}
	if err := validateDate(in.StartsOn, "starts_on"); err != nil {
		return Enrollment{}, err
	}
	tx, actor, err := r.begin(ctx, true)
	if err != nil {
		return Enrollment{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = load(ctx, tx, id, true); err != nil {
		return Enrollment{}, err
	}
	v, err := insertEnrollment(ctx, tx, id, in.StudentID, in.StartsOn)
	if err != nil {
		return Enrollment{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, "enrollment.created", "enrollment", v.ID); err != nil {
		return Enrollment{}, err
	}
	return v, tx.Commit(ctx)
}
func closeEnrollment(ctx context.Context, tx pgx.Tx, groupID, id int64, endsOn string) (Enrollment, error) {
	v, err := loadEnrollment(ctx, tx, groupID, id, true)
	if err != nil {
		return v, err
	}
	if v.EndsOn != nil {
		return v, httpx.E(409, "enrollment_closed", "Зачисление уже завершено.")
	}
	if endsOn < v.StartsOn {
		return v, httpx.Invalid("ends_on", "Окончание не может быть раньше начала зачисления.")
	}
	if _, err = tx.Exec(ctx, `UPDATE group_enrollments SET ends_on=$3::date WHERE group_id=$1 AND id=$2`, groupID, id, endsOn); err != nil {
		return v, err
	}
	return loadEnrollment(ctx, tx, groupID, id, false)
}
func (r *Repository) Close(ctx context.Context, groupID, id int64, in CloseInput) (Enrollment, error) {
	if err := validateDate(in.EndsOn, "ends_on"); err != nil {
		return Enrollment{}, err
	}
	tx, actor, err := r.begin(ctx, true)
	if err != nil {
		return Enrollment{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = load(ctx, tx, groupID, true); err != nil {
		return Enrollment{}, err
	}
	v, err := closeEnrollment(ctx, tx, groupID, id, in.EndsOn)
	if err != nil {
		return v, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, "enrollment.closed", "enrollment", id); err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Transfer(ctx context.Context, groupID, id int64, in TransferInput) (TransferResult, error) {
	result := TransferResult{}
	if in.TargetGroupID <= 0 || in.TargetGroupID == groupID {
		return result, httpx.Invalid("target_group_id", "Выберите другую группу.")
	}
	if err := validateDate(in.TransfersOn, "transfers_on"); err != nil {
		return result, err
	}
	tx, actor, err := r.begin(ctx, true)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	// Lock both groups in ID order, including for opposite-direction transfers.
	first, second := groupID, in.TargetGroupID
	if first > second {
		first, second = second, first
	}
	if _, err = load(ctx, tx, first, true); err != nil {
		return result, err
	}
	if _, err = load(ctx, tx, second, true); err != nil {
		return result, err
	}
	result.Previous, err = closeEnrollment(ctx, tx, groupID, id, in.TransfersOn)
	if err != nil {
		return result, err
	}
	result.Enrollment, err = insertEnrollment(ctx, tx, in.TargetGroupID, result.Previous.StudentID, in.TransfersOn)
	if err != nil {
		return result, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, "enrollment.transferred_out", "enrollment", id); err != nil {
		return result, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, "enrollment.transferred_in", "enrollment", result.Enrollment.ID); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
