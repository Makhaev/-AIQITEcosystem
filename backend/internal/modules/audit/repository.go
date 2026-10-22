package audit

import (
	"context"

	"aiq.local/core/internal/platform/access"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

const columns = `a.id,a.occurred_at,a.actor_id,u.name,a.action,a.object_type,a.object_id,a.changed_fields,a.request_id`
const fromSQL = ` FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_id`

func scan(row pgx.Row) (Entry, error) {
	var v Entry
	err := row.Scan(&v.ID, &v.OccurredAt, &v.ActorID, &v.ActorName, &v.Action, &v.ObjectType, &v.ObjectID, &v.ChangedFields, &v.RequestID)
	return v, err
}

func (r *Repository) begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	if _, err = access.AuthorizeRead(ctx, tx, access.Audit); err != nil {
		tx.Rollback(ctx)
		return nil, err
	}
	return tx, nil
}

func (r *Repository) List(ctx context.Context, f Filter) (Page, error) {
	result := Page{Items: []Entry{}, Page: f.Page, PageSize: f.Size}
	tx, err := r.begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	where := ` WHERE ($1::timestamptz IS NULL OR a.occurred_at>=$1) AND ($2::timestamptz IS NULL OR a.occurred_at<$2)
 AND ($3::bigint=0 OR a.actor_id=$3) AND ($4='' OR a.action=$4)
 AND ($5='' OR a.object_type=$5) AND ($6::bigint=0 OR a.object_id=$6) AND ($7='' OR a.request_id=$7)`
	args := []any{f.From, f.To, f.ActorID, f.Action, f.ObjectType, f.ObjectID, f.RequestID}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs a`+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+columns+fromSQL+where+` ORDER BY a.occurred_at DESC,a.id DESC LIMIT $8 OFFSET $9`, append(args, f.Size, (f.Page-1)*f.Size)...)
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

func (r *Repository) Get(ctx context.Context, id int64) (Entry, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback(ctx)
	v, err := scan(tx.QueryRow(ctx, `SELECT `+columns+fromSQL+` WHERE a.id=$1`, id))
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
