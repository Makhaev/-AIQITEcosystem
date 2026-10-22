package auth

import (
	"context"
	"errors"
	"time"

	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }
type Account struct {
	ID                  int64
	Hash                string
	Blocked             bool
	Attempts            int
	Window, LockedUntil *time.Time
}
type LoginTx struct{ tx pgx.Tx }

func (t *LoginTx) LockedFailure(ctx context.Context, id int64) error {
	_, err := t.tx.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,object_id,request_id) VALUES($1,'auth.login_failed',$1,$2)`, id, httpx.RequestID(ctx))
	return err
}

// A row lock serializes logins with blocking, password changes and session revocation.
// Authentication failure is an outcome, not a transaction error: attempts must commit.
func (r *Repository) LoginTransaction(ctx context.Context, email string, fn func(Account, *LoginTx) error) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var a Account
	err = tx.QueryRow(ctx, `SELECT id,password_hash,blocked,failed_attempts,failure_window_start,locked_until FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&a.ID, &a.Hash, &a.Blocked, &a.Attempts, &a.Window, &a.LockedUntil)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err = fn(a, &LoginTx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (t *LoginTx) Failure(ctx context.Context, a Account, now time.Time) error {
	var id *int64
	if a.ID != 0 {
		id = &a.ID
		if a.Window == nil || now.Sub(*a.Window) >= 15*time.Minute {
			a.Attempts = 0
			a.Window = &now
		}
		a.Attempts++
		if a.Attempts >= 5 {
			until := now.Add(15 * time.Minute)
			a.LockedUntil = &until
		}
		if _, err := t.tx.Exec(ctx, `UPDATE users SET failed_attempts=$2,failure_window_start=$3,locked_until=$4 WHERE id=$1`, a.ID, a.Attempts, a.Window, a.LockedUntil); err != nil {
			return err
		}
	}
	_, err := t.tx.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,object_id,request_id) VALUES($1,'auth.login_failed',$1,$2)`, id, httpx.RequestID(ctx))
	return err
}
func (t *LoginTx) Success(ctx context.Context, a Account, tokenHash, csrfHash, previousHash []byte, expires time.Time) (users.User, error) {
	if _, err := t.tx.Exec(ctx, `UPDATE users SET failed_attempts=0,failure_window_start=NULL,locked_until=NULL WHERE id=$1`, a.ID); err != nil {
		return users.User{}, err
	}
	if _, err := t.tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1 AND expires_at<=now()`, a.ID); err != nil {
		return users.User{}, err
	}
	// Replacing a browser session and creating the new one are one transaction.
	if previousHash != nil {
		if _, err := t.tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, previousHash); err != nil {
			return users.User{}, err
		}
	}
	if _, err := t.tx.Exec(ctx, `INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,$4)`, tokenHash, csrfHash, a.ID, expires); err != nil {
		return users.User{}, err
	}
	if err := database.Audit(ctx, t.tx, &a.ID, "auth.login", a.ID); err != nil {
		return users.User{}, err
	}
	return users.Scan(t.tx.QueryRow(ctx, `SELECT `+users.Columns+` FROM users u WHERE id=$1`, a.ID))
}

type Session struct {
	User     users.User
	CSRFHash []byte
	Expires  time.Time
}

func (r *Repository) Current(ctx context.Context, hash []byte) (Session, error) {
	var s Session
	err := r.Pool.QueryRow(ctx, `SELECT `+users.Columns+`,s.csrf_hash,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND NOT u.blocked`, hash).Scan(&s.User.ID, &s.User.Email, &s.User.Name, &s.User.Roles, &s.User.Blocked, &s.User.MustChangePassword, &s.User.CreatedAt, &s.User.UpdatedAt, &s.CSRFHash, &s.Expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, httpx.Unauthorized()
	}
	return s, err
}
func (r *Repository) RotateCSRF(ctx context.Context, session, csrf []byte) error {
	tag, err := r.Pool.Exec(ctx, `UPDATE sessions SET csrf_hash=$2 WHERE token_hash=$1 AND expires_at>now()`, session, csrf)
	if err == nil && tag.RowsAffected() == 0 {
		return httpx.Unauthorized()
	}
	return err
}
func (r *Repository) Logout(ctx context.Context, hash []byte, id int64) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Keep the same user -> session lock order as authenticated mutations.
	var userID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&userID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1 AND user_id=$2`, hash, id); err != nil {
		return err
	}
	if err = database.Audit(ctx, tx, &id, "auth.logout", id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) ChangePassword(ctx context.Context, id int64, session []byte, newHash string, check func(string) bool) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var oldHash string
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT password_hash,blocked FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&oldHash, &blocked); err != nil {
		return err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>clock_timestamp())`, session, id).Scan(&valid); err != nil {
		return err
	}
	if blocked || !valid {
		return httpx.Unauthorized()
	}
	if !check(oldHash) {
		return httpx.Invalid("old_password", "Неверный текущий пароль.")
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,must_change_password=false,failed_attempts=0,failure_window_start=NULL,locked_until=NULL,updated_at=now() WHERE id=$1`, id, newHash); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		return err
	}
	if err = database.Audit(ctx, tx, &id, "auth.password_changed", id, "password"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) AllowIP(ctx context.Context, hash []byte) (bool, error) {
	var attempts int
	err := r.Pool.QueryRow(ctx, `INSERT INTO login_rate_limits(key_hash,attempts,window_start) VALUES($1,1,now()) ON CONFLICT(key_hash) DO UPDATE SET attempts=CASE WHEN login_rate_limits.window_start<=now()-interval '15 minutes' THEN 1 ELSE login_rate_limits.attempts+1 END,window_start=CASE WHEN login_rate_limits.window_start<=now()-interval '15 minutes' THEN now() ELSE login_rate_limits.window_start END RETURNING attempts`, hash).Scan(&attempts)
	return attempts <= 30, err
}
func (r *Repository) Cleanup(ctx context.Context) error {
	if _, err := r.Pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at<=now()`); err != nil {
		return err
	}
	_, err := r.Pool.Exec(ctx, `DELETE FROM login_rate_limits WHERE window_start<now()-interval '30 minutes'`)
	return err
}
