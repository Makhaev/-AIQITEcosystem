package events

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) participationRead(ctx context.Context, eid, rid int64, write bool) (pgx.Tx, int64, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Events, access.Read, write)
	if err != nil {
		return nil, 0, err
	}
	fail := func(err error) (pgx.Tx, int64, error) { tx.Rollback(ctx); return nil, 0, err }
	e, err := load(ctx, tx, eid, write)
	if err != nil {
		return fail(err)
	}
	if e.Status == "draft" {
		return fail(pgx.ErrNoRows)
	}
	manage, err := manager(ctx, tx, a, eid)
	if err != nil {
		return fail(err)
	}
	v, err := registration(ctx, tx, eid, rid)
	if err != nil {
		return fail(err)
	}
	if err = participant(ctx, tx, a, v.StudentID, manage); err != nil {
		return fail(err)
	}
	return tx, a.ID, nil
}
func (r *Repository) History(ctx context.Context, eid, rid int64) ([]History, error) {
	out, err := r.history(ctx, eid, rid, 1, 0)
	return out.Items, err
}
func (r *Repository) HistoryPage(ctx context.Context, eid, rid int64, page, size int) (Page[History], error) {
	return r.history(ctx, eid, rid, page, httpx.PageSize(size))
}
func (r *Repository) history(ctx context.Context, eid, rid int64, page, size int) (Page[History], error) {
	out := Page[History]{Items: []History{}, Page: page, PageSize: size}
	tx, _, err := r.participationRead(ctx, eid, rid, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM event_registration_history WHERE registration_id=$1`, rid).Scan(&out.Total); err != nil {
		return out, err
	}
	query := `SELECT status,revision,created_at FROM event_registration_history WHERE registration_id=$1 ORDER BY revision,id`
	args := []any{rid}
	if size > 0 {
		query += ` LIMIT $2 OFFSET $3`
		args = append(args, size, (page-1)*size)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v History
		if err = rows.Scan(&v.Status, &v.Revision, &v.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Documents(ctx context.Context, eid, rid int64, page int, sizes ...int) (Page[files.File], error) {
	out := Page[files.File]{Items: []files.File{}, Page: page, PageSize: httpx.PageSize(sizes...)}
	tx, _, err := r.participationRead(ctx, eid, rid, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM event_documents WHERE registration_id=$1`, rid).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id,name,media_type,octet_length(data),created_at FROM event_documents WHERE registration_id=$1 ORDER BY id DESC LIMIT $3 OFFSET $2`, rid, (page-1)*out.PageSize, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v files.File
		if err = rows.Scan(&v.ID, &v.Name, &v.MediaType, &v.Size, &v.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Upload(ctx context.Context, eid, rid int64, in files.Input, media string) (files.File, error) {
	var v files.File
	tx, uid, err := r.participationRead(ctx, eid, rid, true)
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `INSERT INTO event_documents(registration_id,name,media_type,data,created_by) VALUES($1,$2,$3,$4,$5) RETURNING id,created_at`, rid, in.Name, media, in.Data, uid).Scan(&v.ID, &v.CreatedAt)
	if err != nil {
		return v, err
	}
	if err = database.AuditObject(ctx, tx, &uid, "event.document_added", "event_registration", rid, "documents"); err != nil {
		return v, err
	}
	v.Name = in.Name
	v.MediaType = media
	v.Size = len(in.Data)
	return v, tx.Commit(ctx)
}
func (r *Repository) Download(ctx context.Context, eid, rid, did int64) (files.File, error) {
	var v files.File
	tx, _, err := r.participationRead(ctx, eid, rid, false)
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	err = tx.QueryRow(ctx, `SELECT id,name,media_type,data FROM event_documents WHERE id=$1 AND registration_id=$2`, did, rid).Scan(&v.ID, &v.Name, &v.MediaType, &v.Data)
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
