package family

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

func conflict() error {
	return httpx.E(409, "revision_conflict", "Запись изменилась. Обновите страницу.")
}
func audit(ctx context.Context, tx pgx.Tx, a httpx.Actor, action, kind string, id int64, fields ...string) error {
	return database.AuditObject(ctx, tx, &a.ID, action, kind, id, fields...)
}
func (r *Repository) Links(ctx context.Context, sid int64) ([]LinkAccess, error) {
	out := []LinkAccess{}
	tx, a, err := access.Begin(ctx, r.Pool, access.Students, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if !a.Admin() {
		return out, httpx.Forbidden()
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.name,COALESCE(f.confirmed,false) FROM student_parents sp JOIN parents p ON p.id=sp.parent_id LEFT JOIN family_access f ON f.student_id=sp.student_id AND f.parent_id=sp.parent_id WHERE sp.student_id=$1 ORDER BY p.id`, sid)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v LinkAccess
		if err = rows.Scan(&v.ParentID, &v.Name, &v.Confirmed); err != nil {
			rows.Close()
			return out, err
		}
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) SetLink(ctx context.Context, sid, pid int64, confirmed bool) error {
	tx, a, err := access.Begin(ctx, r.Pool, access.Students, access.Manage, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var ok bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM student_parents sp JOIN parents p ON p.id=sp.parent_id JOIN students s ON s.id=sp.student_id WHERE sp.student_id=$1 AND sp.parent_id=$2 AND (NOT $3 OR (p.status='active' AND s.status='active')))`, sid, pid, confirmed).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return missing()
	}
	_, err = tx.Exec(ctx, `INSERT INTO family_access(student_id,parent_id,confirmed,changed_by) VALUES($1,$2,$3,$4) ON CONFLICT(student_id,parent_id) DO UPDATE SET confirmed=EXCLUDED.confirmed,changed_by=EXCLUDED.changed_by,changed_at=clock_timestamp()`, sid, pid, confirmed, a.ID)
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, a, "family.access_changed", "student", sid, "parent_id", "confirmed"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) Share(ctx context.Context, id int64, in Audience, file bool) error {
	resource, action := access.Students, access.Manage
	if file {
		action = access.Read
	}
	tx, a, err := access.Begin(ctx, r.Pool, resource, action, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	kind := "learning"
	if file {
		v, e := files.ReadTx(ctx, tx, a, id, false)
		if e != nil {
			return e
		}
		if !v.CanManage {
			return httpx.Forbidden()
		}
		_, err = tx.Exec(ctx, `INSERT INTO family_file_access(file_id,students,parents,changed_by) VALUES($1,$2,$3,$4) ON CONFLICT(file_id) DO UPDATE SET students=EXCLUDED.students,parents=EXCLUDED.parents,changed_by=EXCLUDED.changed_by`, id, in.Students, in.Parents, a.ID)
		kind = "file"
	} else {
		var rev int
		var publishable bool
		err = tx.QueryRow(ctx, `SELECT current_revision,kind='observations' AND state='reviewed' AND published FROM learning_records WHERE id=$1 FOR UPDATE`, id).Scan(&rev, &publishable)
		if errors.Is(err, pgx.ErrNoRows) {
			return missing()
		}
		if err != nil {
			return err
		}
		if rev != in.Revision {
			return conflict()
		}
		if (in.Students || in.Parents) && !publishable {
			return httpx.Invalid("publication", "Сначала проверьте и опубликуйте наблюдение.")
		}
		_, err = tx.Exec(ctx, `INSERT INTO family_learning_publications(record_id,revision,students,parents,changed_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(record_id) DO UPDATE SET revision=EXCLUDED.revision,students=EXCLUDED.students,parents=EXCLUDED.parents,changed_by=EXCLUDED.changed_by`, id, rev, in.Students, in.Parents, a.ID)
	}
	if err != nil {
		return err
	}
	if err = audit(ctx, tx, a, "family.publication_changed", kind, id, "students", "parents"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func reportHistory(ctx context.Context, tx pgx.Tx, a httpx.Actor, v Report) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO family_report_history(report_id,revision,content,actor_id) VALUES($1,$2,$3,$4)`, v.ID, v.Revision, raw, a.ID)
	if err != nil {
		return err
	}
	return audit(ctx, tx, a, "family.report_changed", "family_report", v.ID, "revision", "published")
}
func (r *Repository) SaveReport(ctx context.Context, sid, id int64, in ReportInput) (Report, error) {
	var v Report
	tx, a, err := access.Begin(ctx, r.Pool, access.FamilyReports, access.Read, true)
	if err != nil {
		return v, err
	}
	defer tx.Rollback(ctx)
	if id != 0 {
		v, err = readReport(ctx, tx, a, id, true)
		if err != nil {
			return Report{}, err
		}
		sid = v.StudentID
		if v.Revision != in.Revision {
			return Report{}, conflict()
		}
		if v.State != "draft" {
			return Report{}, httpx.E(409, "report_state", "Редактировать можно только черновик. Создайте исправленную версию.")
		}
	}
	if err = staffStudent(ctx, tx, a, sid); err != nil {
		return Report{}, err
	}
	if err = reportReviewer(ctx, tx, in.ReviewerID); err != nil {
		return Report{}, err
	}
	// Preserve a saved snapshot on ordinary edits. An explicit regeneration or
	// period change refreshes it and, like every edit, removes publication.
	if in.From == "" && v.Snapshot != nil {
		in.From = v.Snapshot.From
		in.To = v.Snapshot.To
	}
	if in.From != "" && (v.Snapshot == nil || in.Generate || v.Snapshot.From != in.From || v.Snapshot.To != in.To) {
		v.Snapshot, err = collectReport(ctx, tx, sid, in.From, in.To)
		if err != nil {
			return Report{}, err
		}
	}
	if in.Generate {
		in.Title = "Отчёт семье: " + in.From + " — " + in.To
		in.Body = reportTemplate(v.Snapshot)
	}
	v.NextFocus = in.NextFocus
	v.ReviewerID = in.ReviewerID
	v.StudentID = sid
	v.Title = in.Title
	v.Body = in.Body
	if id == 0 {
		v.Version = 1
		v.State = "draft"
	}
	v.Files = []File{}
	v.Revision++
	for _, fid := range in.FileIDs {
		f, e := files.ReadTx(ctx, tx, a, fid, false)
		if e != nil {
			return Report{}, e
		}
		if f.StudentID != sid {
			return Report{}, missing()
		}
		v.Files = append(v.Files, File{ID: f.ID, Name: f.Name, MediaType: f.MediaType, Size: f.Size})
	}
	if id == 0 {
		err = tx.QueryRow(ctx, `INSERT INTO family_reports(student_id,title,body,created_by,snapshot,next_focus,reviewer_id,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,$4) RETURNING id`, sid, in.Title, in.Body, a.ID, v.Snapshot, v.NextFocus, v.ReviewerID).Scan(&v.ID)
	} else {
		_, err = tx.Exec(ctx, `UPDATE family_reports SET title=$2,body=$3,revision=$4,updated_at=clock_timestamp(),snapshot=$5,next_focus=$6,reviewer_id=$7,updated_by=$8 WHERE id=$1`, id, in.Title, in.Body, v.Revision, v.Snapshot, v.NextFocus, v.ReviewerID, a.ID)
	}
	if err != nil {
		return Report{}, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM family_report_files WHERE report_id=$1`, v.ID); err != nil {
		return Report{}, err
	}
	for _, fid := range in.FileIDs {
		if _, err = tx.Exec(ctx, `INSERT INTO family_report_files(report_id,file_id) VALUES($1,$2)`, v.ID, fid); err != nil {
			return Report{}, err
		}
	}
	v, err = readReport(ctx, tx, a, v.ID, true)
	if err != nil {
		return Report{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO family_report_editors(report_id,version,user_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, v.ID, v.Version, a.ID); err != nil {
		return Report{}, err
	}
	if err = reportHistory(ctx, tx, a, v); err != nil {
		return Report{}, err
	}
	return v, tx.Commit(ctx)
}
