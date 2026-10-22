package tasks

import (
	"context"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

const overdueSQL = `(k.status IN ('todo','in_progress') AND k.due_at<now())`
const columns = `k.id,k.title,k.description,k.creator_id,c.name,k.assignee_id,u.name,k.due_at,k.status,` + overdueSQL + `,
 CASE WHEN k.student_id IS NOT NULL THEN 'student' WHEN k.group_id IS NOT NULL THEN 'group' WHEN k.lesson_id IS NOT NULL THEN 'lesson' END,
 COALESCE(k.student_id,k.group_id,k.lesson_id),COALESCE(s.name,g.name,l.topic),k.created_at,k.updated_at,k.closed_at`
const fromSQL = ` FROM tasks k JOIN users c ON c.id=k.creator_id JOIN users u ON u.id=k.assignee_id
 LEFT JOIN students s ON s.id=k.student_id LEFT JOIN groups g ON g.id=k.group_id LEFT JOIN lessons l ON l.id=k.lesson_id`

func scan(row pgx.Row) (Task, error) {
	var v Task
	err := row.Scan(&v.ID, &v.Title, &v.Description, &v.CreatorID, &v.CreatorName, &v.AssigneeID, &v.AssigneeName, &v.DueAt, &v.Status, &v.Overdue, &v.ObjectType, &v.ObjectID, &v.ObjectName, &v.CreatedAt, &v.UpdatedAt, &v.ClosedAt)
	return v, err
}
func (r *Repository) begin(ctx context.Context, action access.Action) (pgx.Tx, httpx.Actor, error) {
	return access.Begin(ctx, r.Pool, access.Tasks, action, action != access.Read)
}

func load(ctx context.Context, tx pgx.Tx, id int64, actor httpx.Actor, lock bool) (Task, error) {
	query := `SELECT ` + columns + fromSQL + ` WHERE k.id=$1 AND ($2 OR k.assignee_id=$3 OR k.creator_id=$3)`
	if lock {
		query += ` FOR UPDATE OF k`
	}
	return scan(tx.QueryRow(ctx, query, id, access.ScopeFor(actor, access.Tasks, access.Read) == access.All, actor.ID))
}
func (r *Repository) Get(ctx context.Context, id int64) (Task, error) {
	tx, actor, err := r.begin(ctx, access.Read)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)
	v, err := load(ctx, tx, id, actor, false)
	if err != nil {
		return v, err
	}
	if err = projectTask(ctx, tx, actor, &v); err != nil {
		return Task{}, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, f Filter) (Page[Task], error) {
	result := Page[Task]{Items: []Task{}, Page: f.Page, PageSize: f.Size}
	tx, actor, err := r.begin(ctx, access.Read)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	where := ` WHERE ($1 OR k.assignee_id=$2 OR k.creator_id=$2) AND ($3='' OR strpos(k.title_search,$3)>0) AND ($4='' OR k.status=$4) AND ($5::bigint=0 OR k.assignee_id=$5) AND ($6::boolean IS NULL OR ` + overdueSQL + `=$6)`
	args := []any{access.ScopeFor(actor, access.Tasks, access.Read) == access.All, actor.ID, f.Search, f.Status, f.AssigneeID, f.Overdue}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM tasks k`+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+fromSQL+where+` ORDER BY k.due_at,k.id LIMIT $7 OFFSET $8`, append(args, f.Size, (f.Page-1)*f.Size)...)
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
	for i := range result.Items {
		if err = projectTask(ctx, tx, actor, &result.Items[i]); err != nil {
			return Page[Task]{}, err
		}
	}
	return result, tx.Commit(ctx)
}
func validStatus(s string) bool {
	return s == "todo" || s == "in_progress" || s == "done" || s == "cancelled"
}
func normalize(in *Input) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	for _, f := range []struct {
		key, value string
		max        int
	}{{"title", in.Title, 200}, {"description", in.Description, 5000}} {
		if n := utf8.RuneCountInString(f.value); n < 1 || n > f.max || strings.ContainsRune(f.value, 0) {
			return httpx.Invalid(f.key, "Заполните поле и проверьте допустимую длину.")
		}
	}
	if in.AssigneeID <= 0 {
		return httpx.Invalid("assignee_id", "Выберите ответственного.")
	}
	if in.DueAt.IsZero() || in.DueAt.Year() < 1 || in.DueAt.Year() > 9999 {
		return httpx.Invalid("due_at", "Укажите срок с часовым поясом.")
	}
	if !validStatus(in.Status) {
		return httpx.Invalid("status", "Недопустимый статус задачи.")
	}
	if (in.ObjectType == nil) != (in.ObjectID == nil) {
		return httpx.Invalid("object_id", "Выберите тип и номер связанного объекта либо очистите оба поля.")
	}
	if in.ObjectType != nil && (*in.ObjectID <= 0 || (*in.ObjectType != "student" && *in.ObjectType != "group" && *in.ObjectType != "lesson")) {
		return httpx.Invalid("object_type", "Допустима одна связь: ученик, группа или занятие.")
	}
	return nil
}
func merge[T any](field Optional[T], target *T, key string) error {
	if !field.Set {
		return nil
	}
	if field.Value == nil {
		return httpx.Invalid(key, "Поле обязательно.")
	}
	*target = *field.Value
	return nil
}
func (r *Repository) Save(ctx context.Context, id int64, in Input, patch *Update) (Task, error) {
	// Status-only PATCH shares the object permissions and idempotency of /status.
	// Any other field still requires management rights inside this repository.
	if patch != nil && patch.Status.Set && !patch.Title.Set && !patch.Description.Set && !patch.AssigneeID.Set && !patch.DueAt.Set && !patch.ObjectType.Set && !patch.ObjectID.Set {
		if patch.Status.Value == nil {
			return Task{}, httpx.Invalid("status", "Поле обязательно.")
		}
		return r.Status(ctx, id, *patch.Status.Value)
	}
	tx, actor, err := r.begin(ctx, access.Manage)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)
	var old Task
	action := "task.created"
	if patch != nil {
		old, err = load(ctx, tx, id, actor, true)
		if err != nil {
			return Task{}, err
		}
		if !patch.Title.Set && !patch.Description.Set && !patch.AssigneeID.Set && !patch.DueAt.Set && !patch.Status.Set && !patch.ObjectType.Set && !patch.ObjectID.Set {
			return Task{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		in = Input{Title: old.Title, Description: old.Description, AssigneeID: old.AssigneeID, DueAt: old.DueAt, Status: old.Status, ObjectType: old.ObjectType, ObjectID: old.ObjectID}
		if err = merge(patch.Title, &in.Title, "title"); err != nil {
			return Task{}, err
		}
		if err = merge(patch.Description, &in.Description, "description"); err != nil {
			return Task{}, err
		}
		if err = merge(patch.AssigneeID, &in.AssigneeID, "assignee_id"); err != nil {
			return Task{}, err
		}
		if err = merge(patch.DueAt, &in.DueAt, "due_at"); err != nil {
			return Task{}, err
		}
		if err = merge(patch.Status, &in.Status, "status"); err != nil {
			return Task{}, err
		}
		if patch.ObjectType.Set {
			in.ObjectType = patch.ObjectType.Value
		}
		if patch.ObjectID.Set {
			in.ObjectID = patch.ObjectID.Value
		}
		action = "task.updated"
		if in.AssigneeID != old.AssigneeID {
			action = "task.reassigned"
		}
	} else if in.Status == "" {
		in.Status = "todo"
	}
	if err = normalize(&in); err != nil {
		return Task{}, err
	}
	if patch == nil && in.Status != "todo" {
		return Task{}, httpx.Invalid("status", "Новая задача создаётся в статусе todo.")
	}
	if patch != nil {
		if err = transition(old, actor, in.Status); err != nil {
			return Task{}, err
		}
	}
	// All account, teaching, roster and enrollment writers share this transaction's
	// management lock. Recheck the merged assignment after waiting for that lock.
	if patch == nil || patch.AssigneeID.Set || patch.ObjectType.Set || patch.ObjectID.Set {
		if err = validateAssignment(ctx, tx, actor, in); err != nil {
			return Task{}, err
		}
	}
	var studentID, groupID, lessonID *int64
	if in.ObjectType != nil {
		switch *in.ObjectType {
		case "student":
			studentID = in.ObjectID
		case "group":
			groupID = in.ObjectID
		case "lesson":
			lessonID = in.ObjectID
		}
	}
	if patch == nil {
		err = tx.QueryRow(ctx, `INSERT INTO tasks(title,title_search,description,creator_id,assignee_id,due_at,status,student_id,group_id,lesson_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, in.Title, strings.ToLower(in.Title), in.Description, actor.ID, in.AssigneeID, in.DueAt, in.Status, studentID, groupID, lessonID).Scan(&id)
	} else {
		_, err = tx.Exec(ctx, `UPDATE tasks SET title=$2,title_search=$3,description=$4,assignee_id=$5,due_at=$6,status=$7,student_id=$8,group_id=$9,lesson_id=$10,updated_at=clock_timestamp() WHERE id=$1`, id, in.Title, strings.ToLower(in.Title), in.Description, in.AssigneeID, in.DueAt, in.Status, studentID, groupID, lessonID)
	}
	if err != nil {
		return Task{}, err
	}
	result, err := load(ctx, tx, id, actor, false)
	if err != nil {
		return result, err
	}
	fields := database.ChangedFields(old, result, "title", "description", "creator_id", "assignee_id", "due_at", "status", "closed_at", "object_type", "object_id")
	if patch == nil || len(fields) > 0 {
		if err = database.AuditObject(ctx, tx, &actor.ID, action, "task", id, fields...); err != nil {
			return Task{}, err
		}
	}
	return result, tx.Commit(ctx)
}
func (r *Repository) Status(ctx context.Context, id int64, status string) (Task, error) {
	if !validStatus(status) {
		return Task{}, httpx.Invalid("status", "Недопустимый статус задачи.")
	}
	tx, actor, err := r.begin(ctx, access.ChangeStatus)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)
	v, err := load(ctx, tx, id, actor, true)
	if err != nil {
		return v, err
	}
	if err = transition(v, actor, status); err != nil {
		return v, err
	}
	if v.Status != status {
		if _, err = tx.Exec(ctx, `UPDATE tasks SET status=$2,updated_at=clock_timestamp() WHERE id=$1`, id, status); err != nil {
			return v, err
		}
		updated, e := load(ctx, tx, id, actor, false)
		if e != nil {
			return v, e
		}
		if err = database.AuditObject(ctx, tx, &actor.ID, "task.status_changed", "task", id, database.ChangedFields(v, updated, "status", "closed_at")...); err != nil {
			return v, err
		}
	}
	v, err = load(ctx, tx, id, actor, false)
	if err != nil {
		return v, err
	}
	if err = projectTask(ctx, tx, actor, &v); err != nil {
		return Task{}, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Assignees(ctx context.Context, f Filter) (Page[Assignee], error) {
	result := Page[Assignee]{Items: []Assignee{}, Page: f.Page, PageSize: f.Size}
	tx, actor, err := r.begin(ctx, access.Read)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if access.ScopeFor(actor, access.Tasks, access.Manage) != access.All {
		return result, httpx.Forbidden()
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT id,name,blocked FROM users ORDER BY name,id LIMIT $1 OFFSET $2`, f.Size, (f.Page-1)*f.Size)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v Assignee
		if err = rows.Scan(&v.ID, &v.Name, &v.Blocked); err != nil {
			rows.Close()
			return result, err
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
