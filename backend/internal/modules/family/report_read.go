package family

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

func reportHead(ctx context.Context, tx pgx.Tx, id int64) (Report, error) {
	var v Report
	err := tx.QueryRow(ctx, `SELECT r.id,r.student_id,r.title,r.body,r.revision,r.published,r.snapshot,r.next_focus,r.reviewer_id,COALESCE(rv.name,''),r.created_by,u.name,COALESCE(r.updated_by,r.created_by),r.created_at,r.updated_at,r.published_at,r.withdrawn_at,r.version,r.state,r.published_version,r.approved_by,r.approved_at
 FROM family_reports r JOIN users u ON u.id=r.created_by LEFT JOIN users rv ON rv.id=r.reviewer_id WHERE r.id=$1`, id).Scan(&v.ID, &v.StudentID, &v.Title, &v.Body, &v.Revision, &v.Published, &v.Snapshot, &v.NextFocus, &v.ReviewerID, &v.ReviewerName, &v.AuthorID, &v.AuthorName, &v.UpdatedBy, &v.CreatedAt, &v.UpdatedAt, &v.PublishedAt, &v.WithdrawnAt, &v.Version, &v.State, &v.PublishedVersion, &v.ApprovedBy, &v.ApprovedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = missing()
	}
	return v, err
}
func reportEditor(ctx context.Context, tx pgx.Tx, a httpx.Actor, sid int64) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students WHERE id=$1 AND ($2 OR ($3 AND curator_has_student($4,id))))`, sid, a.Admin(), has(a, "curator"), a.ID).Scan(&ok)
	return ok, err
}
func reportReviewActor(a httpx.Actor, v Report) bool {
	return a.Admin() || has(a, "education_leader") && v.ReviewerID != nil && *v.ReviewerID == a.ID
}
func reportEdition(ctx context.Context, tx pgx.Tx, id int64, version int) (Report, error) {
	var raw []byte
	var v Report
	err := tx.QueryRow(ctx, `SELECT content FROM family_report_editions WHERE report_id=$1 AND version=$2`, id, version).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, missing()
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(raw, &v)
	return v, err
}
func reportWithdrawAllowed(ctx context.Context, tx pgx.Tx, a httpx.Actor, v Report) (bool, error) {
	if v.PublishedVersion == nil {
		return false, nil
	}
	if a.Admin() {
		return true, nil
	}
	p, err := reportEdition(ctx, tx, v.ID, *v.PublishedVersion)
	return err == nil && reportReviewActor(a, p), err
}
func reportActions(ctx context.Context, tx pgx.Tx, a httpx.Actor, v Report) ([]string, error) {
	out := []string{}
	edit, err := reportEditor(ctx, tx, a, v.StudentID)
	if err != nil {
		return out, err
	}
	if edit {
		if v.State == "draft" {
			out = append(out, "edit", "submit")
		}
		if v.State == "approved" || v.State == "published" || v.State == "withdrawn" {
			out = append(out, "correct")
		}
	}
	if reportReviewActor(a, v) {
		if v.State == "review" {
			out = append(out, "return")
			var own bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM family_report_editors WHERE report_id=$1 AND version=$2 AND user_id=$3)`, v.ID, v.Version, a.ID).Scan(&own); err != nil {
				return out, err
			}
			if !own {
				out = append(out, "approve")
			}
		}
		if v.State == "approved" {
			out = append(out, "publish")
		}
	}
	withdraw, err := reportWithdrawAllowed(ctx, tx, a, v)
	if err != nil {
		return out, err
	}
	if withdraw {
		out = append(out, "withdraw")
	}
	return out, nil
}
func readReport(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64, staff bool) (Report, error) {
	head, err := reportHead(ctx, tx, id)
	if err != nil {
		return Report{}, err
	}
	v := head
	if !staff {
		own, parent, e := audience(ctx, tx, a, head.StudentID)
		if e != nil {
			return Report{}, e
		}
		if head.PublishedVersion == nil || !head.Published {
			return Report{}, missing()
		}
		v, err = reportEdition(ctx, tx, id, *head.PublishedVersion)
		if err != nil {
			return Report{}, err
		}
		v.State = "published"
		v.Published = true
		v.PublishedVersion = head.PublishedVersion
		v.Actions = nil
		v.Decisions = nil
		err = tx.QueryRow(ctx, `SELECT published_at FROM family_report_publications WHERE report_id=$1 AND version=$2 AND withdrawn_at IS NULL`, id, v.Version).Scan(&v.PublishedAt)
		if err != nil {
			return Report{}, err
		}
		v.Files, err = recordFiles(ctx, tx, v.StudentID, id, v.Version, own, parent, true)
		return v, err
	}
	edit, err := reportEditor(ctx, tx, a, head.StudentID)
	if err != nil {
		return Report{}, err
	}
	frozen := false
	if !edit && !(reportReviewActor(a, head) && head.State != "draft") {
		if head.PublishedVersion == nil {
			return Report{}, missing()
		}
		v, err = reportEdition(ctx, tx, id, *head.PublishedVersion)
		if err != nil {
			return Report{}, err
		}
		if !reportReviewActor(a, v) {
			return Report{}, missing()
		}
		frozen = true
		v.State = "published"
		v.Published = true
		v.PublishedVersion = head.PublishedVersion
		v.Revision = head.Revision
		v.Actions = []string{"withdraw"}
		if err = tx.QueryRow(ctx, `SELECT published_at FROM family_report_publications WHERE report_id=$1 AND version=$2 AND withdrawn_at IS NULL`, id, v.Version).Scan(&v.PublishedAt); err != nil {
			return Report{}, err
		}
	} else {
		v.Actions, err = reportActions(ctx, tx, a, v)
		if err != nil {
			return Report{}, err
		}
	}
	v.Files = []File{}
	// A report assignment never bypasses the independent live pupil + file ACL.
	query := `SELECT file_id FROM family_report_files WHERE report_id=$1`
	args := []any{id}
	if frozen {
		query = `SELECT file_id FROM family_report_edition_files WHERE report_id=$1 AND version=$2`
		args = append(args, v.Version)
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return Report{}, err
	}
	ids := []int64{}
	for rows.Next() {
		var fid int64
		if err = rows.Scan(&fid); err != nil {
			rows.Close()
			return Report{}, err
		}
		ids = append(ids, fid)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Report{}, err
	}
	for _, fid := range ids {
		f, e := files.ReadTx(ctx, tx, a, fid, false)
		if e != nil {
			var denied *httpx.Error
			if errors.As(e, &denied) && denied.Status == 404 {
				continue
			}
			return Report{}, e
		}
		v.Files = append(v.Files, File{ID: f.ID, Name: f.Name, MediaType: f.MediaType, Size: f.Size, URL: fmt.Sprintf("/api/v1/files/%d/content", f.ID)})
	}
	v.Decisions = []ReportDecision{}
	rows, err = tx.Query(ctx, `SELECT d.version,d.action,d.reason,d.actor_id,u.name,d.created_at FROM family_report_decisions d JOIN users u ON u.id=d.actor_id WHERE d.report_id=$1 AND ($2 OR d.version=$3) ORDER BY d.id`, id, edit, v.Version)
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var d ReportDecision
		if err = rows.Scan(&d.Version, &d.Action, &d.Reason, &d.ActorID, &d.ActorName, &d.CreatedAt); err != nil {
			return Report{}, err
		}
		v.Decisions = append(v.Decisions, d)
	}
	return v, rows.Err()
}
func (r *Repository) StaffReport(ctx context.Context, id int64) (Report, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.FamilyReports, access.Read, false)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback(ctx)
	v, err := readReport(ctx, tx, a, id, true)
	if err != nil {
		return Report{}, err
	}
	return v, tx.Commit(ctx)
}

// Queue is scoped independently from Students: reviewers gain no directory access.
func (r *Repository) ReportQueue(ctx context.Context, f Filter) (Page[Report], error) {
	out := Page[Report]{Items: []Report{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.FamilyReports, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	where := ` FROM family_reports r JOIN students s ON s.id=r.student_id WHERE
 ($1 OR ($2 AND curator_has_student($4,s.id)) OR ($3 AND ((r.reviewer_id=$4 AND r.state<>'draft') OR EXISTS(SELECT 1 FROM family_report_editions e WHERE e.report_id=r.id AND e.version=r.published_version AND (e.content->>'reviewer_id')::bigint=$4))))`
	args := []any{a.Admin(), has(a, "curator"), has(a, "education_leader"), a.ID}
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT r.id`+where+` ORDER BY r.id DESC LIMIT $6 OFFSET $5`, append(args, (f.Page-1)*out.PageSize, out.PageSize)...)
	if err != nil {
		return out, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		v, e := readReport(ctx, tx, a, id, true)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, tx.Commit(ctx)
}
