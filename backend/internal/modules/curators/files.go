package curators

import (
	"context"
	"net/http"
	"strconv"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/uploads"
	"github.com/jackc/pgx/v5"
)

func reportFileID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("file_id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.Invalid("file_id", "Некорректный файл.")
	}
	return id, nil
}

// Lock in the same order as publication: student, then report. Publication and
// attachment edits cannot race; changes to curator assignments are rechecked.
func (h *Handler) writableReportFiles(ctx context.Context, id int64) (pgx.Tx, httpx.Actor, error) {
	tx, a, err := h.begin(ctx, access.CuratorReports, access.Manage, true)
	if err != nil {
		return nil, a, err
	}
	var studentID int64
	err = tx.QueryRow(ctx, `SELECT student_id FROM curator_reports WHERE id=$1`, id).Scan(&studentID)
	if err == nil {
		var status string
		status, err = writableStudent(ctx, tx, a, studentID)
		if err == nil && status != "active" {
			err = httpx.E(409, "student_archived", "Для изменения вложений нужна активная карточка ученика.")
		}
	}
	if err == nil {
		var published bool
		err = tx.QueryRow(ctx, `SELECT published_at IS NOT NULL FROM curator_reports WHERE id=$1 FOR UPDATE`, id).Scan(&published)
		if err == nil && published {
			err = httpx.E(409, "report_published", "Вложения опубликованного отчёта нельзя изменять. Создайте новый отчёт.")
		}
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, a, err
	}
	return tx, a, nil
}

func (h *Handler) UploadReportFile(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	name, content, err := uploads.Read(w, r)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, a, err := h.writableReportFiles(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var count int
	err = tx.QueryRow(r.Context(), `SELECT count(*) FROM curator_report_files WHERE report_id=$1`, id).Scan(&count)
	if err == nil && count >= 20 {
		err = httpx.E(409, "report_file_limit", "В отчёте может быть до 20 файлов. Удалите ненужное вложение.")
	}
	var file uploads.Info
	if err == nil {
		err = tx.QueryRow(r.Context(), `INSERT INTO curator_report_files(report_id,name,content) VALUES($1,$2,$3) RETURNING id,name,octet_length(content),created_at`, id, name, content).Scan(&file.ID, &file.Name, &file.Size, &file.CreatedAt)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE curator_reports SET updated_at=now() WHERE id=$1`, id)
	}
	if err == nil {
		err = database.AuditObject(r.Context(), tx, &a.ID, "curator_report.file_uploaded", "curator_report", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, file)
}

func (h *Handler) DownloadReportFile(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	fileID, err := reportFileID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, a, err := h.begin(r.Context(), access.CuratorReports, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Reuse the exact report visibility rule, including current family links and
	// selected recipients. Knowing a file ID alone never grants download access.
	var visible int64
	err = tx.QueryRow(r.Context(), `SELECT rep.id`+reportFrom+` AND rep.id=$5`, append(reportArgs(a), id)...).Scan(&visible)
	var name string
	var content []byte
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT name,content FROM curator_report_files WHERE report_id=$1 AND id=$2`, id, fileID).Scan(&name, &content)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	uploads.Download(w, name, content)
}

func (h *Handler) DeleteReportFile(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	fileID, err := reportFileID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, a, err := h.writableReportFiles(r.Context(), id)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `DELETE FROM curator_report_files WHERE report_id=$1 AND id=$2`, id, fileID)
	if err == nil && tag.RowsAffected() == 0 {
		err = pgx.ErrNoRows
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `UPDATE curator_reports SET updated_at=now() WHERE id=$1`, id)
	}
	if err == nil {
		err = database.AuditObject(r.Context(), tx, &a.ID, "curator_report.file_deleted", "curator_report", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
