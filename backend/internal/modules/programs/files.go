package programs

import (
	"context"
	"net/http"
	"time"

	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/uploads"
	"github.com/jackc/pgx/v5"
)

const MaxModuleFileSize = uploads.MaxFileSize

type ModuleFile struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Size      int       `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

func moduleFileIDs(r *http.Request) (int64, int64, error) {
	id, err := idFrom(r, "id")
	if err != nil {
		return 0, 0, err
	}
	module, err := idFrom(r, "module_id")
	return id, module, err
}

func checkFileModule(ctx context.Context, tx pgx.Tx, programID, moduleID int64) error {
	var found bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM program_modules WHERE id=$1 AND program_id=$2)`, moduleID, programID).Scan(&found); err != nil {
		return err
	}
	if !found {
		return pgx.ErrNoRows
	}
	return nil
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	id, module, err := moduleFileIDs(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	items, err := h.Service.Repo.listFiles(r.Context(), id, module)
	reply(w, r, map[string]any{"items": items}, translate(err), 200)
}

func (repo *Repository) listFiles(ctx context.Context, id, module int64) ([]ModuleFile, error) {
	items := []ModuleFile{}
	tx, actor, err := repo.begin(ctx, false)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = scopedProgram(ctx, tx, actor, id); err != nil {
		return nil, err
	}
	if err = checkFileModule(ctx, tx, id, module); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,name,octet_length(content),created_at FROM module_files WHERE module_id=$1 ORDER BY created_at,id`, module)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var file ModuleFile
		if err = rows.Scan(&file.ID, &file.Name, &file.Size, &file.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, file)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return items, tx.Commit(ctx)
}

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	id, module, err := moduleFileIDs(r)
	if err != nil {
		reply(w, r, nil, err, 201)
		return
	}
	name, content, err := uploads.Read(w, r)
	if err != nil {
		reply(w, r, nil, err, 201)
		return
	}
	file, err := h.Service.Repo.saveFile(r.Context(), id, module, name, content)
	reply(w, r, file, translate(err), 201)
}

func (repo *Repository) fileWrite(ctx context.Context, id, module int64) (pgx.Tx, httpx.Actor, error) {
	tx, actor, err := repo.begin(ctx, true)
	if err != nil {
		return nil, actor, err
	}
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM programs WHERE id=$1 FOR UPDATE`, id).Scan(&status)
	if err == nil && status == "archived" {
		err = httpx.E(409, "program_archived", "Для изменения файлов восстановите программу из архива.")
	}
	if err == nil {
		err = checkFileModule(ctx, tx, id, module)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}

func (repo *Repository) saveFile(ctx context.Context, id, module int64, name string, content []byte) (ModuleFile, error) {
	tx, actor, err := repo.fileWrite(ctx, id, module)
	if err != nil {
		return ModuleFile{}, err
	}
	defer tx.Rollback(ctx)
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM module_files WHERE module_id=$1`, module).Scan(&count); err != nil {
		return ModuleFile{}, err
	}
	if count >= 20 {
		return ModuleFile{}, httpx.E(409, "module_file_limit", "В модуле может быть до 20 файлов. Удалите ненужный файл перед загрузкой нового.")
	}
	var file ModuleFile
	err = tx.QueryRow(ctx, `INSERT INTO module_files(module_id,name,content) VALUES($1,$2,$3) RETURNING id,name,octet_length(content),created_at`, module, name, content).Scan(&file.ID, &file.Name, &file.Size, &file.CreatedAt)
	if err != nil {
		return file, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, "module.file_uploaded", "module", module); err != nil {
		return file, err
	}
	return file, tx.Commit(ctx)
}

func (h *Handler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	id, module, err := moduleFileIDs(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	fileID, err := idFrom(r, "file_id")
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	repo := h.Service.Repo
	tx, actor, err := repo.begin(r.Context(), false)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = scopedProgram(r.Context(), tx, actor, id); err == nil {
		err = checkFileModule(r.Context(), tx, id, module)
	}
	var name string
	var content []byte
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT name,content FROM module_files WHERE id=$1 AND module_id=$2`, fileID, module).Scan(&name, &content)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		reply(w, r, nil, translate(err), 200)
		return
	}
	uploads.Download(w, name, content)
}

func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	id, module, err := moduleFileIDs(r)
	if err != nil {
		reply(w, r, nil, err, 204)
		return
	}
	fileID, err := idFrom(r, "file_id")
	if err != nil {
		reply(w, r, nil, err, 204)
		return
	}
	tx, actor, err := h.Service.Repo.fileWrite(r.Context(), id, module)
	if err != nil {
		reply(w, r, nil, translate(err), 204)
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `DELETE FROM module_files WHERE id=$1 AND module_id=$2`, fileID, module)
	if err == nil && tag.RowsAffected() == 0 {
		err = pgx.ErrNoRows
	}
	if err == nil {
		err = database.AuditObject(r.Context(), tx, &actor.ID, "module.file_deleted", "module", module)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	reply(w, r, nil, translate(err), 204)
}
