package family

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/notify"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
	"unicode/utf8"
)

func (s *Service) ReportAction(ctx context.Context, id int64, in ReportAction) (Report, error) {
	if in.Version < 1 || in.Revision < 1 {
		return Report{}, httpx.Invalid("version", "Укажите версию и текущую редакцию.")
	}
	switch in.Action {
	case "submit", "return", "approve", "publish", "withdraw", "correct":
	default:
		return Report{}, httpx.Invalid("action", "Неизвестное действие.")
	}
	if utf8.RuneCountInString(in.Reason) > 5000 || ((in.Action == "return" || in.Action == "withdraw") && !validText(in.Reason, 5000)) {
		return Report{}, httpx.Invalid("reason", "Укажите замечание или причину (до 5000 символов).")
	}
	return s.Repo.ReportAction(ctx, id, in)
}
func (r *Repository) ReportAction(ctx context.Context, id int64, in ReportAction) (Report, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.FamilyReports, access.Read, true)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback(ctx)
	view, err := readReport(ctx, tx, a, id, true)
	if err != nil {
		return Report{}, err
	}
	v, err := reportHead(ctx, tx, id)
	if err != nil {
		return Report{}, err
	}
	// Idempotence is checked against a concrete, still-current publication, never
	// against an arbitrary old revision that could republish superseded content.
	if in.Action == "publish" && in.Version == v.Version && v.State == "published" && v.PublishedVersion != nil && *v.PublishedVersion == in.Version {
		if !reportReviewActor(a, v) {
			return Report{}, httpx.Forbidden()
		}
		return view, tx.Commit(ctx)
	}
	if in.Revision != v.Revision {
		return Report{}, conflict()
	}
	if in.Action == "withdraw" {
		if v.PublishedVersion == nil || *v.PublishedVersion != in.Version {
			return Report{}, conflict()
		}
	} else if in.Version != v.Version {
		return Report{}, conflict()
	}
	allowed, err := reportActions(ctx, tx, a, v)
	if err != nil {
		return Report{}, err
	}
	ok := false
	for _, action := range allowed {
		if action == in.Action {
			ok = true
		}
	}
	if !ok {
		return Report{}, httpx.E(403, "report_action_denied", "Действие недоступно для вашей роли, версии или состояния отчёта.")
	}
	switch in.Action {
	case "submit":
		if v.ReviewerID == nil || (v.Snapshot != nil && !validText(v.NextFocus, 5000)) {
			return Report{}, httpx.Invalid("report", "Назначьте проверяющего и заполните следующий образовательный фокус.")
		}
		if err = reportReviewer(ctx, tx, v.ReviewerID); err != nil {
			return Report{}, err
		}
		var own bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM family_report_editors WHERE report_id=$1 AND version=$2 AND user_id=$3)`, id, v.Version, *v.ReviewerID).Scan(&own); err != nil {
			return Report{}, err
		}
		if own {
			return Report{}, httpx.Invalid("reviewer_id", "Автор и редакторы версии не могут её утверждать. Назначьте другого проверяющего.")
		}
		_, err = tx.Exec(ctx, `UPDATE family_reports SET state='review' WHERE id=$1`, id)
	case "return":
		_, err = tx.Exec(ctx, `UPDATE family_reports SET state='draft' WHERE id=$1`, id)
	case "approve":
		_, err = tx.Exec(ctx, `UPDATE family_reports SET state='approved',approved_by=$2,approved_at=clock_timestamp() WHERE id=$1`, id, a.ID)
	case "correct":
		_, err = tx.Exec(ctx, `UPDATE family_reports SET version=version+1,state='draft',created_by=$2,created_at=clock_timestamp(),approved_by=NULL,approved_at=NULL WHERE id=$1`, id, a.ID)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO family_report_editors(report_id,version,user_id) VALUES($1,$2,$3)`, id, v.Version+1, a.ID)
		}
	case "publish":
		_, err = tx.Exec(ctx, `INSERT INTO family_report_publications(report_id,version,published_at,published_by) VALUES($1,$2,clock_timestamp(),$3)`, id, v.Version, a.ID)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE family_reports SET state='published',published=true,published_version=version,published_at=clock_timestamp() WHERE id=$1`, id)
		}
		if err == nil {
			err = notifyReport(ctx, tx, id, v.Version, v.StudentID)
		}
	case "withdraw":
		_, err = tx.Exec(ctx, `UPDATE family_report_publications SET withdrawn_at=clock_timestamp() WHERE report_id=$1 AND version=$2`, id, in.Version)
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE family_reports SET published=false,published_version=NULL,withdrawn_at=clock_timestamp(),state=CASE WHEN version=$2 AND state='published' THEN 'withdrawn' ELSE state END WHERE id=$1`, id, in.Version)
		}
	}
	if err != nil {
		return Report{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE family_reports SET revision=revision+1,updated_by=$2,updated_at=clock_timestamp() WHERE id=$1`, id, a.ID); err != nil {
		return Report{}, err
	}
	head, err := reportHead(ctx, tx, id)
	if err != nil {
		return Report{}, err
	}
	if in.Action == "approve" {
		frozen := head
		frozen.Files = []File{}
		frozen.Actions = nil
		frozen.Decisions = nil
		frozen.Published = false
		frozen.PublishedVersion = nil
		frozen.PublishedAt = nil
		frozen.WithdrawnAt = nil
		raw, e := json.Marshal(frozen)
		if e != nil {
			return Report{}, e
		}
		if _, err = tx.Exec(ctx, `INSERT INTO family_report_editions(report_id,version,content) VALUES($1,$2,$3)`, id, head.Version, raw); err != nil {
			return Report{}, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO family_report_edition_files SELECT report_id,$2,file_id FROM family_report_files WHERE report_id=$1`, id, head.Version); err != nil {
			return Report{}, err
		}
	}
	decisionVersion := in.Version
	if in.Action == "correct" {
		decisionVersion = head.Version
	}
	if _, err = tx.Exec(ctx, `INSERT INTO family_report_decisions(report_id,version,action,reason,actor_id) VALUES($1,$2,$3,$4,$5)`, id, decisionVersion, in.Action, in.Reason, a.ID); err != nil {
		return Report{}, err
	}
	if err = reportHistory(ctx, tx, a, head); err != nil {
		return Report{}, err
	}
	if err = audit(ctx, tx, a, "family.report_"+in.Action, "family_report", id, "version", "state", "published_version"); err != nil {
		return Report{}, err
	}
	result, err := readReport(ctx, tx, a, id, true)
	if err != nil {
		// Returning a version to its curator or withdrawing the final publication
		// can revoke the reviewer's read access. Do not expose the working head.
		var denied *httpx.Error
		if !errors.As(err, &denied) || denied.Status != 404 {
			return Report{}, err
		}
		result = view
		result.Revision = head.Revision
		result.Actions = nil
		result.Decisions = nil
		if in.Action == "return" {
			result.State = "draft"
		}
		if in.Action == "withdraw" {
			result.State = "withdrawn"
			result.Published = false
			result.PublishedVersion = nil
		}
	}
	return result, tx.Commit(ctx)
}

// Publication and recipient notifications are committed together. There is one
// publication per edition and one in-app notification per recipient/edition.
func notifyReport(ctx context.Context, tx pgx.Tx, id int64, version int, sid int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO family_report_notifications(report_id,version,user_id)
 SELECT $1,$2,u.id FROM users u WHERE NOT u.blocked AND EXISTS(SELECT 1 FROM students s WHERE s.id=$3 AND s.status='active') AND (
 EXISTS(SELECT 1 FROM student_accounts sa JOIN user_roles ur ON ur.user_id=sa.user_id AND ur.role='student' WHERE sa.user_id=u.id AND sa.student_id=$3)
 OR EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id AND p.status='active' JOIN student_parents sp ON sp.parent_id=p.id AND sp.student_id=$3 JOIN family_access fa ON fa.student_id=sp.student_id AND fa.parent_id=sp.parent_id AND fa.confirmed JOIN user_roles ur ON ur.user_id=pa.user_id AND ur.role='parent' WHERE pa.user_id=u.id))
 ON CONFLICT DO NOTHING`, id, version, sid)
	if err != nil {
		return err
	}
	return notify.Family(ctx, tx, "report_published", id, strconv.Itoa(version), sid)
}
func (r *Repository) ReportNotifications(ctx context.Context, sid int64) ([]ReportNotification, error) {
	out, err := r.ReportNotificationsPage(ctx, sid, 1, 20)
	return out.Items, err
}
func (r *Repository) ReportNotificationsPage(ctx context.Context, sid int64, page, size int) (Page[ReportNotification], error) {
	out := Page[ReportNotification]{Items: []ReportNotification{}, Page: page, PageSize: httpx.PageSize(size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = audience(ctx, tx, a, sid); err != nil {
		return out, err
	}
	from := ` FROM family_report_notifications n JOIN family_reports r ON r.id=n.report_id AND r.published AND r.published_version=n.version JOIN family_report_editions e ON e.report_id=n.report_id AND e.version=n.version WHERE n.user_id=$1 AND r.student_id=$2`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+from, a.ID, sid).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT n.report_id,n.version,e.content->>'title',n.created_at`+from+` ORDER BY n.created_at DESC,n.report_id DESC LIMIT $3 OFFSET $4`, a.ID, sid, out.PageSize, (page-1)*out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var n ReportNotification
		if err = rows.Scan(&n.ReportID, &n.Version, &n.Title, &n.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
