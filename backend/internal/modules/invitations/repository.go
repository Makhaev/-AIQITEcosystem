package invitations

import (
	"aiq.local/core/internal/platform/notify"
	"context"
	"errors"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

const columns = `id,email,role,COALESCE(parent_id,student_id),created_by,created_at,expires_at,used_at,revoked_at,CASE WHEN used_at IS NOT NULL THEN 'used' WHEN revoked_at IS NOT NULL THEN 'revoked' WHEN expires_at<=clock_timestamp() THEN 'expired' ELSE 'pending' END`

func scan(row pgx.Row) (Invitation, error) {
	var i Invitation
	err := row.Scan(&i.ID, &i.Email, &i.Role, &i.CardID, &i.CreatedBy, &i.CreatedAt, &i.ExpiresAt, &i.UsedAt, &i.RevokedAt, &i.Status)
	return i, err
}
func card(ctx context.Context, tx pgx.Tx, role string, id int64) (string, error) {
	query := `SELECT name FROM students WHERE id=$1 AND status='active' FOR UPDATE`
	if role == "parent" {
		query = `SELECT name FROM parents WHERE id=$1 AND status='active' FOR UPDATE`
	}
	var name string
	err := tx.QueryRow(ctx, query, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpx.E(409, "invitation_card_unavailable", "Карточка отсутствует или находится в архиве.")
	}
	return name, err
}
func bindingAvailable(ctx context.Context, tx pgx.Tx, role string, cardID, userID int64) error {
	query := `SELECT EXISTS(SELECT 1 FROM student_accounts WHERE (student_id=$1 AND user_id<>$2) OR (user_id=$2 AND student_id<>$1))`
	if role == "parent" {
		query = `SELECT EXISTS(SELECT 1 FROM parent_accounts WHERE (parent_id=$1 AND user_id<>$2) OR (user_id=$2 AND parent_id<>$1))`
	}
	var conflict bool
	if err := tx.QueryRow(ctx, query, cardID, userID).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return httpx.E(409, "account_link_conflict", "Карточка или аккаунт уже имеет другую привязку этой роли.")
	}
	return nil
}
func (r *Repository) Issue(ctx context.Context, in Input, resendID int64, hash []byte) (Invitation, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Users, access.Manage, true)
	if err != nil {
		return Invitation{}, err
	}
	defer tx.Rollback(ctx)
	if resendID != 0 {
		old, e := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM invitations WHERE id=$1 FOR UPDATE`, resendID))
		if errors.Is(e, pgx.ErrNoRows) {
			return Invitation{}, httpx.E(404, "not_found", "Приглашение не найдено.")
		}
		if e != nil {
			return Invitation{}, e
		}
		if old.UsedAt != nil {
			return Invitation{}, httpx.E(409, "invitation_used", "Использованное приглашение нельзя отправить повторно.")
		}
		in = Input{Email: old.Email, Role: old.Role, CardID: old.CardID}
	}
	if _, err = card(ctx, tx, in.Role, in.CardID); err != nil {
		return Invitation{}, err
	}
	var existing int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT id FROM users WHERE email=$1),0)`, in.Email).Scan(&existing); err != nil {
		return Invitation{}, err
	}
	if err = bindingAvailable(ctx, tx, in.Role, in.CardID, existing); err != nil {
		return Invitation{}, err
	}
	// Shared management lock serializes issue/resend/revoke/accept, even when
	// concurrent requests refer to the same email via different cards.
	rows, err := tx.Query(ctx, `UPDATE invitations SET revoked_at=clock_timestamp() WHERE used_at IS NULL AND revoked_at IS NULL AND role=$1 AND (email=$2 OR COALESCE(parent_id,student_id)=$3) RETURNING id`, in.Role, in.Email, in.CardID)
	if err != nil {
		return Invitation{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return Invitation{}, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Invitation{}, err
	}
	for _, id := range ids {
		if err = database.AuditObject(ctx, tx, &a.ID, "invitation.superseded", "invitation", id, "revoked_at"); err != nil {
			return Invitation{}, err
		}
	}
	i, err := scan(tx.QueryRow(ctx, `WITH stamp AS (SELECT clock_timestamp() AS t) INSERT INTO invitations(email,role,parent_id,student_id,token_hash,created_by,created_at,expires_at) SELECT $1,$2,CASE WHEN $2='parent' THEN $3::bigint END,CASE WHEN $2='student' THEN $3::bigint END,$4,$5,t,t+interval '48 hours' FROM stamp RETURNING `+columns, in.Email, in.Role, in.CardID, hash, a.ID))
	if err != nil {
		return i, err
	}
	if err = database.AuditObject(ctx, tx, &a.ID, "invitation.created", "invitation", i.ID, "recipient", "role", "card", "expires_at"); err != nil {
		return i, err
	}
	if existing > 0 {
		if err = notify.Account(ctx, tx, "invitation", i.ID, existing, "issued"); err != nil {
			return i, err
		}
	}
	return i, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, page int, sizes ...int) (Page, error) {
	out := Page{Items: []Invitation{}, Page: page, PageSize: httpx.PageSize(sizes...)}
	tx, _, err := access.Begin(ctx, r.Pool, access.Users, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM invitations`).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+` FROM invitations ORDER BY id DESC LIMIT $2 OFFSET $1`, (page-1)*out.PageSize, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		i, e := scan(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Revoke(ctx context.Context, id int64) error {
	tx, a, err := access.Begin(ctx, r.Pool, access.Users, access.Manage, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	i, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM invitations WHERE id=$1 FOR UPDATE`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Приглашение не найдено.")
	}
	if err != nil {
		return err
	}
	if i.UsedAt != nil {
		return httpx.E(409, "invitation_used", "Связь уже активирована; отзыв приглашения не отменяет аккаунт.")
	}
	if i.RevokedAt == nil {
		if _, err = tx.Exec(ctx, `UPDATE invitations SET revoked_at=clock_timestamp() WHERE id=$1`, id); err != nil {
			return err
		}
		if err = database.AuditObject(ctx, tx, &a.ID, "invitation.revoked", "invitation", id, "revoked_at"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (r *Repository) DeliveryFailed(_ context.Context, id int64) error {
	// Request cancellation must not leave an emailed but unconfirmed token live.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = access.LockManagement(ctx, tx); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE invitations SET revoked_at=clock_timestamp() WHERE id=$1 AND used_at IS NULL AND revoked_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if err = database.AuditObject(ctx, tx, nil, "invitation.delivery_failed", "invitation", id, "revoked_at"); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (r *Repository) Inspect(ctx context.Context, hash []byte) (Preview, error) {
	var p Preview
	err := r.Pool.QueryRow(ctx, `SELECT i.email,i.role,i.expires_at,EXISTS(SELECT 1 FROM users u WHERE u.email=i.email) FROM invitations i WHERE token_hash=$1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at>clock_timestamp()`, hash).Scan(&p.Email, &p.Role, &p.ExpiresAt, &p.RequiresLogin)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, invalid()
	}
	return p, err
}
func (r *Repository) Accept(ctx context.Context, hash []byte, passwordHash string) (Result, error) {
	out := Result{}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = access.LockManagement(ctx, tx); err != nil {
		return out, err
	}
	a := httpx.CurrentActor(ctx)
	if a.ID != 0 {
		a, err = access.Authorize(ctx, tx, access.Account, access.Read)
		if err != nil {
			return out, err
		}
	}
	i, err := scan(tx.QueryRow(ctx, `SELECT `+columns+` FROM invitations WHERE token_hash=$1 FOR UPDATE`, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return out, invalid()
	}
	if err != nil {
		return out, err
	}
	if i.Status != "pending" {
		return out, invalid()
	}
	var userID int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT id FROM users WHERE email=$1),0)`, i.Email).Scan(&userID); err != nil {
		return out, err
	}
	if a.ID != 0 && a.ID != userID {
		return out, httpx.E(403, "invitation_account_mismatch", "Выйдите и войдите в аккаунт с email приглашения.")
	}
	if userID != 0 && a.ID == 0 {
		return out, httpx.E(401, "invitation_login_required", "Войдите в существующий аккаунт с email приглашения.")
	}
	name, err := card(ctx, tx, i.Role, i.CardID)
	if err != nil {
		return out, err
	}
	if err = bindingAvailable(ctx, tx, i.Role, i.CardID, userID); err != nil {
		return out, err
	}
	if userID == 0 {
		if passwordHash == "" {
			return out, httpx.Invalid("password", "Задайте пароль нового аккаунта.")
		}
		if err = tx.QueryRow(ctx, `INSERT INTO users(email,name,password_hash,must_change_password) VALUES($1,$2,$3,false) RETURNING id`, i.Email, name, passwordHash).Scan(&userID); err != nil {
			return out, err
		}
		out.CreatedAccount = true
		if err = database.Audit(ctx, tx, &userID, "user.created_by_invitation", userID, "email", "name", "password"); err != nil {
			return out, err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_roles(user_id,role) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, i.Role); err != nil {
		return out, err
	}
	query := `INSERT INTO student_accounts(student_id,user_id,invitation_id) VALUES($1,$2,$3) ON CONFLICT (student_id) DO NOTHING`
	if i.Role == "parent" {
		query = `INSERT INTO parent_accounts(parent_id,user_id,invitation_id) VALUES($1,$2,$3) ON CONFLICT (parent_id) DO NOTHING`
	}
	if _, err = tx.Exec(ctx, query, i.CardID, userID, i.ID); err != nil {
		return out, err
	}
	// Recheck the deadline after all row locks and writes. Roll back the account,
	// role and binding if time expired while waiting for a concurrent mutation.
	tag, err := tx.Exec(ctx, `UPDATE invitations SET used_at=clock_timestamp(),used_by=$2 WHERE id=$1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at>clock_timestamp()`, i.ID, userID)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() != 1 {
		return out, invalid()
	}
	if err = database.AuditObject(ctx, tx, &userID, "invitation.accepted", "invitation", i.ID, "used_at", "account_link", "role"); err != nil {
		return out, err
	}
	out.Role = i.Role
	out.CardID = i.CardID
	if err = notify.Account(ctx, tx, "invitation", i.ID, userID, "issued"); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Links(ctx context.Context) ([]Link, error) {
	out := []Link{}
	tx, a, err := access.Begin(ctx, r.Pool, access.Account, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT 'parent',p.id,p.name FROM parent_accounts l JOIN parents p ON p.id=l.parent_id WHERE l.user_id=$1 AND EXISTS(SELECT 1 FROM user_roles WHERE user_id=$1 AND role='parent') UNION ALL SELECT 'student',s.id,s.name FROM student_accounts l JOIN students s ON s.id=l.student_id WHERE l.user_id=$1 AND EXISTS(SELECT 1 FROM user_roles WHERE user_id=$1 AND role='student')`, a.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var l Link
		if err = rows.Scan(&l.Role, &l.CardID, &l.Name); err != nil {
			rows.Close()
			return out, err
		}
		out = append(out, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
