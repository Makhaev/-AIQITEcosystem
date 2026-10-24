package learning

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Repository struct{ Pool *pgxpool.Pool }

func missing() error {
	return httpx.E(404, "not_found", "Образовательная запись не найдена.")
}
func has(a httpx.Actor, role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}
func supervisor(a httpx.Actor) bool { return has(a, "admin") || has(a, "leader") }
func student(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64) error {
	ok, err := access.ObjectReadable(ctx, tx, a, "student", id)
	if err != nil {
		return err
	}
	if !ok {
		return missing()
	}
	return nil
}
func curator(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students WHERE id=$1 AND curator_id=$2)`, id, a.ID).Scan(&ok)
	return ok && has(a, "curator"), err
}

// Object scope is evaluated separately from the Core pupil projection. Authorship
// is never a read permission: teacher/curator replacement affects the next read.
const scopeSQL = `(
 $2 OR ($3 AND r.scope='curator' AND s.curator_id=$1)
 OR ($4 AND r.scope='lesson' AND EXISTS(
  SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id
  WHERE l.id=r.lesson_id AND l.status<>'cancelled' AND t.user_id=$1 AND (
   (l.roster_initialized_at IS NOT NULL AND EXISTS(
    SELECT 1 FROM lesson_participants p WHERE p.lesson_id=l.id AND p.student_id=r.student_id AND p.included
   )) OR (l.roster_initialized_at IS NULL AND EXISTS(
    SELECT 1 FROM group_enrollments e WHERE e.group_id=l.group_id AND e.student_id=r.student_id
    AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date
    AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)
   ))
  )
 ))
 OR (r.kind<>'goals' AND r.reviewer_id=$1 AND ($3 OR $4))
 OR EXISTS(SELECT 1 FROM learning_grants g WHERE g.record_id=r.id AND g.user_id=$1)
)`
const columns = `r.id,r.student_id,r.kind,r.current_revision,r.state,r.published,r.created_at,r.reviewer_id,COALESCE((SELECT d.actor_id FROM learning_decisions d WHERE d.record_id=r.id AND d.action IN ('created','corrected') ORDER BY d.revision DESC,d.id DESC LIMIT 1),r.created_by),r.parent_id,r.parent_revision,r.lesson_id,r.scope,v.content`
const recordFrom = ` FROM learning_records r JOIN students s ON s.id=r.student_id JOIN learning_revisions v ON v.record_id=r.id AND v.revision=r.current_revision`

func args(a httpx.Actor) []any {
	return []any{a.ID, supervisor(a), has(a, "curator"), has(a, "teacher")}
}
func scan(row pgx.Row) (Record, error) {
	var r Record
	var raw []byte
	err := row.Scan(&r.ID, &r.StudentID, &r.Kind, &r.Revision, &r.State, &r.Published, &r.CreatedAt, &r.ReviewerID, &r.AuthorID, &r.ParentID, &r.ParentRevision, &r.LessonID, &r.Scope, &raw)
	if err == nil {
		err = json.Unmarshal(raw, &r.Content)
	}
	r.Sources = []Source{}
	r.Files = []files.File{}
	r.Actions = []string{}
	return r, err
}
func load(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64) (Record, error) {
	r, err := scan(tx.QueryRow(ctx, `SELECT `+columns+recordFrom+` WHERE r.id=$5 AND `+scopeSQL, append(args(a), id)...))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, missing()
	}
	if err != nil {
		return r, err
	}
	return r, student(ctx, tx, a, r.StudentID)
}
func source(r Record, start, end time.Time) Source {
	date := r.Content.OccurredOn
	if date == "" {
		date = r.CreatedAt.In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02")
	}
	outside := !start.IsZero() && (date < start.Format("2006-01-02") || date >= end.Format("2006-01-02"))
	return Source{ID: r.ID, Kind: r.Kind, Revision: r.Revision, Title: r.Content.Title, Date: date, URL: fmt.Sprintf("#/learning/%d?revision=%d", r.ID, r.Revision), OutsidePeriod: outside}
}
func snapshot(ctx context.Context, tx pgx.Tx, r *Record, revision int) error {
	if revision == r.Revision {
		return nil
	}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT content,COALESCE((SELECT d.actor_id FROM learning_decisions d WHERE d.record_id=$1 AND d.revision<=$2 AND d.action IN ('created','corrected') ORDER BY d.revision DESC,d.id DESC LIMIT 1),v.author_id) FROM learning_revisions v WHERE record_id=$1 AND revision=$2`, r.ID, revision).Scan(&raw, &r.AuthorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return missing()
	}
	if err != nil {
		return err
	}
	r.Revision = revision
	return json.Unmarshal(raw, &r.Content)
}
func projected(ctx context.Context, tx pgx.Tx, a httpx.Actor, r Record, start, end time.Time) (Record, error) {
	r.Source = source(r, start, end)
	rows, err := tx.Query(ctx, `SELECT source_id,source_revision FROM learning_sources WHERE record_id=$1 AND revision=$2 ORDER BY source_id,source_revision`, r.ID, r.Revision)
	if err != nil {
		return r, err
	}
	refs := []Reference{}
	for rows.Next() {
		var ref Reference
		if err = rows.Scan(&ref.ID, &ref.Revision); err != nil {
			rows.Close()
			return r, err
		}
		refs = append(refs, ref)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	for _, ref := range refs {
		base, e := load(ctx, tx, a, ref.ID)
		if e != nil {
			var denied *httpx.Error
			if errors.As(e, &denied) && denied.Status == 404 {
				r.BasisUnavailable = true
				continue
			}
			return r, e
		}
		if base.Revision != ref.Revision || !evidence(base) {
			r.NeedsReview = true
		}
		if e = snapshot(ctx, tx, &base, ref.Revision); e != nil {
			return r, e
		}
		r.Sources = append(r.Sources, source(base, start, end))
	}
	rows, err = tx.Query(ctx, `SELECT file_id FROM learning_files WHERE record_id=$1 AND revision=$2 ORDER BY file_id`, r.ID, r.Revision)
	if err != nil {
		return r, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return r, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return r, err
	}
	for _, id := range ids {
		f, e := files.ReadTx(ctx, tx, a, id, false)
		if e != nil {
			var denied *httpx.Error
			if errors.As(e, &denied) && denied.Status == 404 {
				continue
			}
			return r, e
		}
		r.Files = append(r.Files, f)
	}
	if r.ParentID != nil && r.ParentRevision != nil {
		parent, e := load(ctx, tx, a, *r.ParentID)
		if e == nil {
			if e = snapshot(ctx, tx, &parent, *r.ParentRevision); e != nil {
				return r, e
			}
			s := source(parent, start, end)
			r.Parent = &s
		} else {
			var denied *httpx.Error
			if !errors.As(e, &denied) || denied.Status != 404 {
				return r, e
			}
			r.BasisUnavailable = true
		}
	}
	if r.Kind == "goals" {
		var nextID int64
		e := tx.QueryRow(ctx, `SELECT id FROM learning_records WHERE parent_id=$1 AND kind='goals' ORDER BY id DESC LIMIT 1`, r.ID).Scan(&nextID)
		if e == nil {
			next, e := load(ctx, tx, a, nextID)
			if e == nil {
				s := source(next, start, end)
				r.Successor = &s
			} else {
				var denied *httpx.Error
				if !errors.As(e, &denied) || denied.Status != 404 {
					return r, e
				}
			}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return r, e
		}
	}
	r.Actions, err = actions(ctx, tx, a, r)
	return r, err
}
func (repo *Repository) Get(ctx context.Context, id int64, revision int) (Card, error) {
	tx, a, err := access.Begin(ctx, repo.Pool, access.Students, access.Read, false)
	if err != nil {
		return Card{}, err
	}
	defer tx.Rollback(ctx)
	r, err := load(ctx, tx, a, id)
	if err != nil {
		return Card{}, err
	}
	current := r.Revision
	if revision > 0 {
		if err = snapshot(ctx, tx, &r, revision); err != nil {
			return Card{}, err
		}
	}
	r, err = projected(ctx, tx, a, r, time.Time{}, time.Time{})
	if err != nil {
		return Card{}, err
	}
	if r.Revision != current {
		r.Actions = []string{}
	}
	out := Card{Record: r, History: []Decision{}}
	rows, err := tx.Query(ctx, `SELECT id,revision,action,reason,actor_id,created_at FROM learning_decisions WHERE record_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var d Decision
		if err = rows.Scan(&d.ID, &d.Revision, &d.Action, &d.Reason, &d.ActorID, &d.CreatedAt); err != nil {
			rows.Close()
			return out, err
		}
		out.History = append(out.History, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func evidence(r Record) bool {
	return r.Kind == "observations" && r.State == "reviewed" && r.Published || r.Kind == "attempts" && r.State == "accepted" || r.Kind == "projects" && r.State == "completed"
}
func saveEvidence(ctx context.Context, tx pgx.Tx, a httpx.Actor, student, id int64, revision int, refs []Reference, filesIDs []int64) error {
	for _, ref := range refs {
		r, e := load(ctx, tx, a, ref.ID)
		if e != nil {
			return e
		}
		if r.StudentID != student || r.Revision != ref.Revision || !evidence(r) {
			return httpx.Invalid("sources", "Нужны действующие подтверждённые редакции этого ученика.")
		}
		if _, e = tx.Exec(ctx, `INSERT INTO learning_sources VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, id, revision, ref.ID, ref.Revision); e != nil {
			return e
		}
	}
	for _, fid := range filesIDs {
		f, e := files.ReadTx(ctx, tx, a, fid, false)
		if e != nil {
			return e
		}
		if f.StudentID != student {
			return httpx.Invalid("file_ids", "Файл относится к другому ученику.")
		}
		if _, e = tx.Exec(ctx, `INSERT INTO learning_files VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, revision, fid); e != nil {
			return e
		}
	}
	return nil
}
func event(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64, revision int, action, reason string) error {
	_, err := tx.Exec(ctx, `INSERT INTO learning_decisions(record_id,revision,action,reason,actor_id) VALUES($1,$2,$3,$4,$5)`, id, revision, action, reason, a.ID)
	if err == nil {
		err = database.AuditObject(ctx, tx, &a.ID, "learning."+action, "learning_record", id, "state", "revision")
	}
	return err
}
