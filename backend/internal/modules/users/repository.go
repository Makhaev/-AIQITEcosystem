package users

import (
	"context"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

func roles(ctx context.Context, tx pgx.Tx, id int64, values []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id=$1`, id); err != nil {
		return err
	}
	for _, r := range values {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,$2)`, id, r); err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) Create(ctx context.Context, in CreateInput, hash string, bootstrap bool) (User, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8021002)`); err != nil {
		return User{}, err
	}
	if bootstrap {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists); err != nil {
			return User{}, err
		}
		if exists {
			return User{}, httpx.E(409, "already_initialized", "Первый администратор уже создан. Используйте управление пользователями.")
		}
	} else if err = authorizeAdmin(ctx, tx); err != nil {
		return User{}, err
	}
	var id int64
	if err = tx.QueryRow(ctx, `INSERT INTO users(email,name,password_hash,must_change_password) VALUES($1,$2,$3,$4) RETURNING id`, in.Email, in.Name, hash, !bootstrap).Scan(&id); err != nil {
		return User{}, err
	}
	if err = roles(ctx, tx, id, in.Roles); err != nil {
		return User{}, err
	}
	actor := httpx.CurrentActor(ctx).ID
	if bootstrap {
		actor = id
	}
	if err = database.Audit(ctx, tx, &actor, "user.created", id, "email", "name", "roles", "password"); err != nil {
		return User{}, err
	}
	u, err := Scan(tx.QueryRow(ctx, `SELECT `+Columns+` FROM users u WHERE u.id=$1`, id))
	if err != nil {
		return u, err
	}
	return u, tx.Commit(ctx)
}

func authorizeAdmin(ctx context.Context, tx pgx.Tx) error {
	_, err := access.Authorize(ctx, tx, access.Users, access.Manage)
	return err
}
func (r *Repository) Update(ctx context.Context, id int64, in UpdateInput, hash *string) (User, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8021002)`); err != nil {
		return User{}, err
	}
	if err = authorizeAdmin(ctx, tx); err != nil {
		return User{}, err
	}
	old, err := Scan(tx.QueryRow(ctx, `SELECT `+Columns+` FROM users u WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return User{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET name=COALESCE($2,name),blocked=COALESCE($3,blocked),password_hash=COALESCE($4,password_hash),must_change_password=CASE WHEN $4::text IS NOT NULL THEN true ELSE must_change_password END, failed_attempts=CASE WHEN $4::text IS NOT NULL THEN 0 ELSE failed_attempts END,locked_until=CASE WHEN $4::text IS NOT NULL THEN NULL ELSE locked_until END,updated_at=now() WHERE id=$1`, id, in.Name, in.Blocked, hash); err != nil {
		return User{}, err
	}
	if in.Roles != nil {
		if err = roles(ctx, tx, id, in.Roles); err != nil {
			return User{}, err
		}
	}
	var admins int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users u JOIN user_roles r ON r.user_id=u.id WHERE NOT u.blocked AND r.role='admin'`).Scan(&admins); err != nil {
		return User{}, err
	}
	if admins == 0 {
		return User{}, httpx.E(409, "last_admin", "Должен остаться хотя бы один активный администратор.")
	}
	// Revoke on password resets, blocking, and role changes to eliminate stale authorization.
	if hash != nil || (in.Blocked != nil && *in.Blocked) || in.Roles != nil {
		if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
			return User{}, err
		}
	}
	actor := httpx.CurrentActor(ctx).ID
	u, err := Scan(tx.QueryRow(ctx, `SELECT `+Columns+` FROM users u WHERE id=$1`, id))
	if err != nil {
		return u, err
	}
	fields := database.ChangedFields(old, u, "name", "roles", "blocked")
	if hash != nil {
		fields = append(fields, "password")
	}
	if err = database.Audit(ctx, tx, &actor, "user.updated", id, fields...); err != nil {
		return User{}, err
	}
	return u, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, page, size int, role string) (Page, error) {
	result := Page{Items: []User{}, Page: page, PageSize: size}
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = access.AuthorizeRead(ctx, tx, access.Users); err != nil {
		return result, err
	}
	where := ` WHERE ($1='' OR EXISTS(SELECT 1 FROM user_roles WHERE user_id=u.id AND role=$1))`
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM users u`+where, role).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+Columns+` FROM users u`+where+` ORDER BY u.id LIMIT $2 OFFSET $3`, role, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		u, err := Scan(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, u)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
