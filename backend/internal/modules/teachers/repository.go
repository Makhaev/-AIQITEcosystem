package teachers

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

type Repository struct{ Pool *pgxpool.Pool }

func linkedUser(ctx context.Context, tx pgx.Tx, id *int64) error {
	if id == nil {
		return nil
	}
	var valid bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN user_roles r ON r.user_id=u.id WHERE u.id=$1 AND NOT u.blocked AND r.role='teacher')`, *id).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return httpx.Invalid("user_id", "Выберите активного пользователя с ролью педагога.")
	}
	return nil
}
func (r *Repository) Create(ctx context.Context, in CreateInput) (Teacher, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return Teacher{}, err
	}
	defer tx.Rollback(ctx)
	if err = access.LockManagement(ctx, tx); err != nil {
		return Teacher{}, err
	}
	actor, err := access.Authorize(ctx, tx, access.Teachers, access.Manage)
	if err != nil {
		return Teacher{}, err
	}
	if err = linkedUser(ctx, tx, in.UserID); err != nil {
		return Teacher{}, err
	}
	value, err := scan(tx.QueryRow(ctx, `INSERT INTO teachers(name,name_search,phone,email,specialization,status,user_id) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id,name,phone,email,specialization,status,user_id,created_at,updated_at`, in.Name, strings.ToLower(in.Name), in.Phone, in.Email, in.Specialization, in.Status, in.UserID))
	if err != nil {
		return Teacher{}, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, "teacher.created", "teacher", value.ID, "name", "phone", "email", "specialization", "status", "user_id"); err != nil {
		return Teacher{}, err
	}
	return value, tx.Commit(ctx)
}
func (r *Repository) Update(ctx context.Context, id int64, input UpdateInput) (Teacher, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return Teacher{}, err
	}
	defer tx.Rollback(ctx)
	if err = access.LockManagement(ctx, tx); err != nil {
		return Teacher{}, err
	}
	actor, err := access.Authorize(ctx, tx, access.Teachers, access.Manage)
	if err != nil {
		return Teacher{}, err
	}
	current, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM teachers t WHERE t.id=$1 FOR UPDATE`, id))
	if err != nil {
		return Teacher{}, err
	}
	in := CreateInput{Name: current.Name, Phone: current.Phone, Email: current.Email, Specialization: current.Specialization, Status: current.Status, UserID: current.UserID}
	for _, field := range []struct {
		input  Optional[string]
		target *string
	}{{input.Name, &in.Name}, {input.Phone, &in.Phone}, {input.Email, &in.Email}, {input.Specialization, &in.Specialization}, {input.Status, &in.Status}} {
		if field.input.Set {
			if field.input.Value == nil {
				*field.target = ""
			} else {
				*field.target = *field.input.Value
			}
		}
	}
	if input.UserID.Set {
		in.UserID = input.UserID.Value
	}
	in, err = normalize(in)
	if err != nil {
		return Teacher{}, err
	}
	// Existing links stay in history even if the account is later blocked/demoted.
	if input.UserID.Set && (in.UserID != nil && (current.UserID == nil || *current.UserID != *in.UserID)) {
		if err = linkedUser(ctx, tx, in.UserID); err != nil {
			return Teacher{}, err
		}
	}
	value, err := scan(tx.QueryRow(ctx, `UPDATE teachers SET name=$2,name_search=$3,phone=$4,email=$5,specialization=$6,status=$7,user_id=$8,updated_at=now() WHERE id=$1 RETURNING id,name,phone,email,specialization,status,user_id,created_at,updated_at`, id, in.Name, strings.ToLower(in.Name), in.Phone, in.Email, in.Specialization, in.Status, in.UserID))
	if err != nil {
		return Teacher{}, err
	}
	action := "teacher.updated"
	if current.Status != value.Status {
		if value.Status == "archived" {
			action = "teacher.archived"
		} else {
			action = "teacher.restored"
		}
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "teacher", id, database.ChangedFields(current, value, "name", "phone", "email", "specialization", "status", "user_id")...); err != nil {
		return Teacher{}, err
	}
	return value, tx.Commit(ctx)
}
func (r *Repository) Get(ctx context.Context, id int64) (Teacher, error) {
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Teacher{}, err
	}
	defer tx.Rollback(ctx)
	actor, err := access.AuthorizeRead(ctx, tx, access.Teachers)
	if err != nil {
		return Teacher{}, err
	}
	value, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM teachers t WHERE t.id=$1 AND ($2 OR t.user_id=$3)`, id, access.ScopeFor(actor, access.Teachers, access.Read) == access.All, actor.ID))
	if err != nil {
		return Teacher{}, err
	}
	return value, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, filter Filter) (Page, error) {
	result := Page{Items: []Teacher{}, Page: filter.Page, PageSize: filter.Size}
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	actor, err := access.AuthorizeRead(ctx, tx, access.Teachers)
	if err != nil {
		return result, err
	}
	where := ` WHERE ($1 OR t.user_id=$2) AND ($3='' OR strpos(t.name_search,$3)>0) AND ($4='' OR t.status=$4)`
	all := access.ScopeFor(actor, access.Teachers, access.Read) == access.All
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM teachers t`+where, all, actor.ID, filter.Search, filter.Status).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM teachers t`+where+` ORDER BY t.name_search,t.id LIMIT $5 OFFSET $6`, all, actor.ID, filter.Search, filter.Status, filter.Size, (filter.Page-1)*filter.Size)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		value, scanErr := scan(rows)
		if scanErr != nil {
			rows.Close()
			return result, scanErr
		}
		result.Items = append(result.Items, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

// RequireActive must be called inside the transaction that assigns a teacher to
// a NEW lesson/group. Its shared row lock serializes assignment with archival.
// Historical lessons must retain their existing teacher foreign key unchanged.
func RequireActive(ctx context.Context, tx pgx.Tx, id int64) (Teacher, error) {
	value, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM teachers t WHERE t.id=$1 FOR SHARE`, id))
	if err != nil {
		return Teacher{}, translate(err)
	}
	if value.Status != "active" {
		return Teacher{}, httpx.E(409, "teacher_archived", "Архивного педагога нельзя назначить на новое занятие или группу.")
	}
	return value, nil
}
