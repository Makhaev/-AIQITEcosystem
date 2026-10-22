package access

import (
	"context"

	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Begin keeps fresh authorization, the management lock and data writes in one
// transaction. Read-only operations use a consistent repeatable-read snapshot.
func Begin(ctx context.Context, pool *pgxpool.Pool, resource Resource, action Action, write bool) (pgx.Tx, httpx.Actor, error) {
	opts := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		opts = pgx.TxOptions{}
	}
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, httpx.Actor{}, err
	}
	var actor httpx.Actor
	if write {
		err = LockManagement(ctx, tx)
		if err == nil {
			actor, err = Authorize(ctx, tx, resource, action)
		}
	} else {
		actor, err = AuthorizeRead(ctx, tx, resource)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}
