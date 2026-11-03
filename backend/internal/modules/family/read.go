package family

import (
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// $1 pupil, $2 own pupil, $3 confirmed parent. Publication is revision-specific.
const visibleRecord = `r.student_id=$1 AND (
 (r.kind='goals' AND r.state IN ('planned','active','achieved','cancelled','revised'))
 OR (r.kind='assignments' AND r.state IN ('assigned','completed'))
 OR ($2 AND r.kind='projects' AND r.state IN ('active','submitted','completed'))
 OR (r.kind='attempts' AND r.state IN ('submitted','accepted','revision'))
 OR (r.kind='observations' AND r.state='reviewed' AND r.published AND EXISTS(
 SELECT 1 FROM family_learning_publications p WHERE p.record_id=r.id AND p.revision=r.current_revision
 AND (($2 AND p.students) OR ($3 AND p.parents)))))`
const recordFrom = ` FROM learning_records r JOIN learning_revisions v ON v.record_id=r.id AND v.revision=r.current_revision`

func safeContent(kind string, raw []byte) (map[string]any, error) {
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	keys := map[string][]string{
		"goals":        {"title", "due_on", "skill_name"},
		"assignments":  {"title", "instruction", "criteria", "due_on", "format", "allow_late"},
		"attempts":     {"title", "answer", "format"},
		"projects":     {"title", "starts_on", "ends_on", "contribution", "criteria"},
		"observations": {"title", "fact", "occurred_on", "skill_name"},
	}
	out := map[string]any{}
	for _, k := range keys[kind] {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}
func readRecord(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64) (Record, error) {
	var v Record
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT student_id FROM learning_records WHERE id=$1`, id).Scan(&v.StudentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, missing()
	}
	if err != nil {
		return v, err
	}
	own, parent, err := audience(ctx, tx, a, v.StudentID)
	if err != nil {
		return v, err
	}
	err = tx.QueryRow(ctx, `SELECT r.id,r.kind,r.state,r.current_revision,v.content,r.created_at`+recordFrom+` WHERE r.id=$4 AND `+visibleRecord, v.StudentID, own, parent, id).Scan(&v.ID, &v.Kind, &v.State, &v.Revision, &raw, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, missing()
	}
	if err != nil {
		return v, err
	}
	v.Content, err = safeContent(v.Kind, raw)
	if err != nil {
		return v, err
	}
	if v.Kind == "assignments" && own {
		var c learning.Content
		if err = json.Unmarshal(raw, &c); err != nil {
			return v, err
		}
		e := learning.AttemptAvailability(learning.Record{Kind: v.Kind, State: v.State, Content: c})
		v.CanSubmit = e == nil
		if e != nil {
			v.SubmissionIssue = "Задание закрыто или срок сдачи истёк."
		}
	}
	if v.Kind == "attempts" {
		if own {
			err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT reason FROM learning_decisions WHERE record_id=$1 AND revision=$2 AND action IN ('return','approve') ORDER BY id DESC LIMIT 1),'')`, id, v.Revision).Scan(&v.Feedback)
			if err != nil {
				return v, err
			}
		}
		var p Source
		err = tx.QueryRow(ctx, `SELECT r.id,r.kind,COALESCE(v.content->>'title','')`+recordFrom+` WHERE `+visibleRecord+` AND r.id=(SELECT parent_id FROM learning_records WHERE id=$4)`, v.StudentID, own, parent, id).Scan(&p.ID, &p.Kind, &p.Title)
		if err == nil {
			v.Parent = &p
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return v, err
		}
	}
	v.Sources = []Source{}
	v.Files = []File{}
	rows, err := tx.Query(ctx, `SELECT r.id,r.kind,COALESCE(v.content->>'title','')`+recordFrom+` WHERE `+visibleRecord+` AND EXISTS(SELECT 1 FROM learning_sources s WHERE s.record_id=$4 AND s.revision=$5 AND s.source_id=r.id AND s.source_revision=r.current_revision) ORDER BY r.id`, v.StudentID, own, parent, id, v.Revision)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var s Source
		if err = rows.Scan(&s.ID, &s.Kind, &s.Title); err != nil {
			rows.Close()
			return v, err
		}
		v.Sources = append(v.Sources, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	v.Files, err = recordFiles(ctx, tx, v.StudentID, id, v.Revision, own, parent, false)
	return v, err
}
func (r *Repository) Record(ctx context.Context, id int64) (Record, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return Record{}, err
	}
	defer tx.Rollback(ctx)
	v, err := readRecord(ctx, tx, a, id)
	if err != nil {
		return v, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Records(ctx context.Context, sid int64, f Filter) (Page[Record], error) {
	out := Page[Record]{Items: []Record{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	own, parent, err := audience(ctx, tx, a, sid)
	if err != nil {
		return out, err
	}
	// Search only a public field, never internal notes or hidden records.
	where := recordFrom + ` WHERE ` + visibleRecord + ` AND ($4='' OR r.kind=$4) AND strpos(lower(COALESCE(v.content->>'title','')),lower($5))>0 AND ($6::bigint=0 OR r.parent_id=$6)`
	args := []any{sid, own, parent, f.Kind, f.Search, f.AssignmentID}
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT r.id`+where+` ORDER BY r.id DESC LIMIT $8 OFFSET $7`, append(args, (f.Page-1)*out.PageSize, out.PageSize)...)
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
		v, e := readRecord(ctx, tx, a, id)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Progress(ctx context.Context, sid int64) (map[string]int, error) {
	out := map[string]int{"goals": 0, "achieved_goals": 0, "assignments": 0, "completed_assignments": 0}
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	own, parent, err := audience(ctx, tx, a, sid)
	if err != nil {
		return out, err
	}
	var g, ag, w, cw int
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE r.kind='goals'),count(*) FILTER(WHERE r.kind='goals' AND r.state='achieved'),count(*) FILTER(WHERE r.kind='assignments'),count(*) FILTER(WHERE r.kind='assignments' AND r.state='completed') FROM learning_records r WHERE `+visibleRecord, sid, own, parent).Scan(&g, &ag, &w, &cw)
	if err != nil {
		return out, err
	}
	out["goals"] = g
	out["achieved_goals"] = ag
	out["assignments"] = w
	out["completed_assignments"] = cw
	return out, tx.Commit(ctx)
}
func recordFiles(ctx context.Context, tx pgx.Tx, sid, id int64, revision int, own, parent, report bool) ([]File, error) {
	where := `EXISTS(SELECT 1 FROM learning_files x WHERE x.file_id=f.id AND x.record_id=$4 AND x.revision=$5)`
	route := "learning"
	if report {
		where = `EXISTS(SELECT 1 FROM family_report_edition_files x WHERE x.file_id=f.id AND x.report_id=$4 AND x.version=$5)`
		route = "reports"
	}
	rows, err := tx.Query(ctx, `SELECT f.id,f.name,f.media_type,octet_length(f.data) FROM private_files f JOIN family_file_access p ON p.file_id=f.id WHERE f.student_id=$1 AND f.revoked_at IS NULL AND (($2 AND p.students) OR ($3 AND p.parents)) AND `+where+` ORDER BY f.id`, sid, own, parent, id, revision)
	out := []File{}
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v File
		if err = rows.Scan(&v.ID, &v.Name, &v.MediaType, &v.Size); err != nil {
			return out, err
		}
		v.URL = fmt.Sprintf("/api/v1/family/%s/%d/files/%d", route, id, v.ID)
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *Repository) Report(ctx context.Context, id int64) (Report, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback(ctx)
	v, err := readReport(ctx, tx, a, id, false)
	if err != nil {
		return Report{}, err
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Reports(ctx context.Context, sid int64, f Filter, staff bool) (Page[Report], error) {
	out := Page[Report]{Items: []Report{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	resource := access.Family
	if staff {
		resource = access.FamilyReports
	}
	tx, a, err := access.Begin(ctx, r.Pool, resource, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if staff {
		err = staffStudent(ctx, tx, a, sid)
	} else {
		_, _, err = audience(ctx, tx, a, sid)
	}
	if err != nil {
		return out, err
	}
	where := ` FROM family_reports WHERE student_id=$1 AND ($2 OR published) AND strpos(lower(CASE WHEN $2 THEN title ELSE (SELECT content->>'title' FROM family_report_editions e WHERE e.report_id=family_reports.id AND e.version=family_reports.published_version) END),lower($3))>0`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, sid, staff, f.Search).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id`+where+` ORDER BY id DESC LIMIT $5 OFFSET $4`, sid, staff, f.Search, (f.Page-1)*out.PageSize, out.PageSize)
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
		v, e := readReport(ctx, tx, a, id, staff)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Download(ctx context.Context, id, fid int64, report bool) (File, error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return File{}, err
	}
	defer tx.Rollback(ctx)
	var list []File
	if report {
		v, e := readReport(ctx, tx, a, id, false)
		err = e
		list = v.Files
	} else {
		v, e := readRecord(ctx, tx, a, id)
		err = e
		list = v.Files
	}
	if err != nil {
		return File{}, err
	}
	for _, v := range list {
		if v.ID == fid {
			if err = tx.QueryRow(ctx, `SELECT data FROM private_files WHERE id=$1 AND revoked_at IS NULL`, fid).Scan(&v.Data); err != nil {
				return File{}, err
			}
			return v, tx.Commit(ctx)
		}
	}
	return File{}, missing()
}
