package auth

import (
	"context"
	"errors"
	"time"

	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/notify"
	"github.com/jackc/pgx/v5"
)

// Separate, domain-prefixed keys prevent login, email and token limits colliding.
// PostgreSQL serializes each increment across all application instances.
func (r *Repository) AllowReset(ctx context.Context, key string, limit int) (bool, error) {
	var attempts int
	err := r.Pool.QueryRow(ctx, `INSERT INTO password_reset_rate_limits(key_hash,attempts,window_start)
        VALUES($1,1,clock_timestamp()) ON CONFLICT(key_hash) DO UPDATE SET
        attempts=CASE WHEN password_reset_rate_limits.window_start<=clock_timestamp()-interval '30 minutes' THEN 1 ELSE LEAST(password_reset_rate_limits.attempts+1,1000) END,
        window_start=CASE WHEN password_reset_rate_limits.window_start<=clock_timestamp()-interval '30 minutes' THEN clock_timestamp() ELSE password_reset_rate_limits.window_start END
        RETURNING attempts`, Hash(key)).Scan(&attempts)
	return attempts <= limit, err
}

func (r *Repository) IssueReset(ctx context.Context, email string, tokenHash []byte) (bool, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	var blocked bool
	err = tx.QueryRow(ctx, `SELECT id,blocked FROM users WHERE email=$1 FOR UPDATE`, email).Scan(&id, &blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if blocked {
		return false, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO password_reset_tokens(user_id,token_hash,expires_at)
        VALUES($1,$2,clock_timestamp()+interval '30 minutes') ON CONFLICT(user_id) DO UPDATE
        SET token_hash=EXCLUDED.token_hash,expires_at=EXCLUDED.expires_at,created_at=clock_timestamp()`, id, tokenHash)
	if err != nil {
		return false, err
	}
	if err = notify.Account(ctx, tx, "password_reset", id, id, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *Repository) RevokeReset(ctx context.Context, tokenHash []byte) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM password_reset_tokens WHERE token_hash=$1`, tokenHash)
	return err
}

func (r *Repository) ResetPassword(ctx context.Context, tokenHash []byte, newHash string, samePassword func(string) bool) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	// This lookup is deliberately unlocked: user -> token is the global lock order.
	err = tx.QueryRow(ctx, `SELECT user_id FROM password_reset_tokens WHERE token_hash=$1`, tokenHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalidReset()
	}
	if err != nil {
		return err
	}
	var oldHash string
	var blocked bool
	err = tx.QueryRow(ctx, `SELECT password_hash,blocked FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&oldHash, &blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalidReset()
	}
	if err != nil {
		return err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM password_reset_tokens WHERE user_id=$1 AND token_hash=$2 AND expires_at>clock_timestamp())`, id, tokenHash).Scan(&valid)
	if err != nil {
		return err
	}
	if blocked || !valid {
		return invalidReset()
	}
	if samePassword(oldHash) {
		return unchangedPassword()
	}
	// The trigger consumes all links atomically with the new password.
	tag, err := tx.Exec(ctx, `UPDATE users SET password_hash=$2,must_change_password=false,
		failed_attempts=0,failure_window_start=NULL,locked_until=NULL,updated_at=now() WHERE id=$1
		AND EXISTS(SELECT 1 FROM password_reset_tokens WHERE user_id=$1 AND token_hash=$3 AND expires_at>clock_timestamp())`, id, newHash, tokenHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return invalidReset()
	}
	if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		return err
	}
	if err = database.Audit(ctx, tx, &id, "auth.password_reset", id, "password"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
