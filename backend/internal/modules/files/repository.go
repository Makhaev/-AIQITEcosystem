package files

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

func missing() error { return httpx.E(404, "not_found", "Файл не найден.") }
func student(ctx context.Context, tx pgx.Tx, actor httpx.Actor, id int64) error {
	ok, err := access.ObjectReadable(ctx, tx, actor, "student", id)
	if err != nil {
		return err
	}
	if !ok {
		return missing()
	}
	return nil
}
func (r *Repository) Create(ctx context.Context, in Input, kind, hash string) (File, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Students, access.Read, true)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback(ctx)
	if err = student(ctx, tx, a, in.StudentID); err != nil {
		return File{}, err
	}
	var f File
	err = tx.QueryRow(ctx, `INSERT INTO private_files(student_id,name,media_type,data,sha256,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id,created_at`, in.StudentID, in.Name, kind, in.Data, hash, a.ID).Scan(&f.ID, &f.CreatedAt)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO private_file_grants(file_id,user_id,can_manage) VALUES($1,$2,true)`, f.ID, a.ID)
	}
	if err == nil {
		err = database.AuditObject(ctx, tx, &a.ID, "file.created", "private_file", f.ID, "content")
	}
	if err != nil {
		return File{}, err
	}
	f.StudentID = in.StudentID
	f.Name = in.Name
	f.MediaType = kind
	f.Size = len(in.Data)
	f.SHA256 = hash
	f.CanManage = true
	return f, tx.Commit(ctx)
}

// ReadTx is shared by source modules when projecting attachments. Every file has
// its own live grant, even when its parent source is readable. Never emit a URL
// or metadata before this check. A blocked/revoked session is checked by Begin.
func ReadTx(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64, content bool) (File, error) {
	var f File
	dataColumn := "NULL::bytea"
	if content {
		dataColumn = "f.data"
	}
	err := tx.QueryRow(ctx, `SELECT f.id,f.student_id,f.name,f.media_type,octet_length(f.data),f.sha256,f.created_at,g.can_manage,`+dataColumn+` FROM private_files f JOIN private_file_grants g ON g.file_id=f.id AND g.user_id=$2 WHERE f.id=$1 AND f.revoked_at IS NULL`, id, a.ID).Scan(&f.ID, &f.StudentID, &f.Name, &f.MediaType, &f.Size, &f.SHA256, &f.CreatedAt, &f.CanManage, &f.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, missing()
	}
	if err != nil {
		return File{}, err
	}
	if err = student(ctx, tx, a, f.StudentID); err != nil {
		return File{}, err
	}
	return f, nil
}
func (r *Repository) Get(ctx context.Context, id int64) (File, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Students, access.Read, false)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback(ctx)
	f, err := ReadTx(ctx, tx, a, id, true)
	if err != nil {
		return File{}, err
	}
	return f, tx.Commit(ctx)
}
func (r *Repository) List(ctx context.Context, id int64, page int) (Page, error) {
	out := Page{Items: []File{}, Page: page, PageSize: 20}
	tx, a, err := access.Begin(ctx, r.Pool, access.Students, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = student(ctx, tx, a, id); err != nil {
		return out, err
	}
	predicate := ` FROM private_files f JOIN private_file_grants g ON g.file_id=f.id AND g.user_id=$2 WHERE f.student_id=$1 AND f.revoked_at IS NULL`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+predicate, id, a.ID).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT f.id,f.student_id,f.name,f.media_type,octet_length(f.data),f.sha256,f.created_at,g.can_manage`+predicate+` ORDER BY f.id DESC LIMIT 20 OFFSET $3`, id, a.ID, (page-1)*20)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var f File
		if err = rows.Scan(&f.ID, &f.StudentID, &f.Name, &f.MediaType, &f.Size, &f.SHA256, &f.CreatedAt, &f.CanManage); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Grant(ctx context.Context, id int64, in Grant) error {
	tx, a, err := access.Begin(ctx, r.Pool, access.Students, access.Read, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	f, err := ReadTx(ctx, tx, a, id, false)
	if err != nil {
		return err
	}
	if !f.CanManage {
		return httpx.Forbidden()
	}
	if in.UserID == a.ID {
		return httpx.Invalid("user_id", "Владелец отзывает файл целиком.")
	}
	if in.Allow {
		target := httpx.Actor{ID: in.UserID}
		var blocked bool
		err = tx.QueryRow(ctx, `SELECT blocked,ARRAY(SELECT role FROM user_roles WHERE user_id=u.id) FROM users u WHERE id=$1`, in.UserID).Scan(&blocked, &target.Roles)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && blocked {
			return httpx.Invalid("user_id", "Сотрудник недоступен.")
		}
		if err != nil {
			return err
		}
		if err = student(ctx, tx, target, f.StudentID); err != nil {
			return httpx.Invalid("user_id", "У сотрудника нет доступа к ученику.")
		}
		_, err = tx.Exec(ctx, `INSERT INTO private_file_grants(file_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, id, in.UserID)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM private_file_grants WHERE file_id=$1 AND user_id=$2 AND NOT can_manage`, id, in.UserID)
	}
	if err == nil {
		err = database.AuditObject(ctx, tx, &a.ID, "file.access_changed", "private_file", id, "grants")
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) Revoke(ctx context.Context, id int64) error {
	tx, a, err := access.Begin(ctx, r.Pool, access.Students, access.Read, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	f, err := ReadTx(ctx, tx, a, id, false)
	if err != nil {
		return err
	}
	if !f.CanManage {
		return httpx.Forbidden()
	}
	_, err = tx.Exec(ctx, `UPDATE private_files SET revoked_at=clock_timestamp() WHERE id=$1`, id)
	if err == nil {
		err = database.AuditObject(ctx, tx, &a.ID, "file.revoked", "private_file", id, "revoked_at")
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
