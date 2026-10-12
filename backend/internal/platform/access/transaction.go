package access

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

// LockManagement serializes administrative mutations with role changes and blocking.
// Acquire this before actor/object row locks to keep the lock order consistent.
func LockManagement(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(8021002)`)
	return err
}

func Authorize(ctx context.Context, tx pgx.Tx, resource Resource, action Action) (httpx.Actor, error) {
	return authorize(ctx, tx, resource, action, true)
}

func AuthorizeRead(ctx context.Context, tx pgx.Tx, resource Resource) (httpx.Actor, error) {
	return authorize(ctx, tx, resource, Read, false)
}

func authorize(ctx context.Context, tx pgx.Tx, resource Resource, action Action, lock bool) (httpx.Actor, error) {
	actor := httpx.CurrentActor(ctx)
	var blocked bool
	query := `SELECT blocked,must_change_password,ARRAY(SELECT role FROM user_roles WHERE user_id=u.id ORDER BY role) FROM users u WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, query, actor.ID).Scan(&blocked, &actor.MustChangePassword, &actor.Roles)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && blocked {
		return actor, httpx.Unauthorized()
	}
	if err != nil {
		return actor, err
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>clock_timestamp())`, actor.SessionHash, actor.ID).Scan(&valid); err != nil {
		return actor, err
	}
	if !valid {
		return actor, httpx.Unauthorized()
	}
	if actor.MustChangePassword {
		return actor, httpx.E(403, "password_change_required", "Сначала смените временный пароль.")
	}
	if ScopeFor(actor, resource, action) == None {
		return actor, httpx.Forbidden()
	}
	return actor, nil
}
