package curriculum

import (
	"context"
	"encoding/json"
	"strings"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

const versionColumns = `v.id,v.program_id,v.number,v.status,v.document,v.revision,v.created_by,v.submitted_by,v.approved_by,v.source_version_id,v.legacy_import,v.created_at,v.updated_at,v.published_at`

func scanVersion(row pgx.Row) (Version, error) {
	var v Version
	var raw []byte
	e := row.Scan(&v.ID, &v.ProgramID, &v.Number, &v.Status, &raw, &v.Revision, &v.CreatedBy, &v.SubmittedBy, &v.ApprovedBy, &v.SourceVersionID, &v.LegacyImport, &v.CreatedAt, &v.UpdatedAt, &v.PublishedAt)
	if e == nil {
		e = json.Unmarshal(raw, &v.Document)
	}
	return v, e
}
func (r *Repository) begin(ctx context.Context, resource access.Resource, action access.Action) (pgx.Tx, httpx.Actor, error) {
	write := action != access.Read
	opts := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		opts = pgx.TxOptions{}
	}
	tx, e := r.Pool.BeginTx(ctx, opts)
	if e != nil {
		return nil, httpx.Actor{}, e
	}
	var actor httpx.Actor
	if write {
		e = access.LockManagement(ctx, tx)
		if e == nil {
			actor, e = access.Authorize(ctx, tx, resource, action)
		}
	} else {
		actor, e = access.AuthorizeRead(ctx, tx, resource)
	}
	if e != nil {
		tx.Rollback(ctx)
		return nil, actor, e
	}
	return tx, actor, nil
}

// Group- and lesson-based membership is evaluated in the same snapshot as the
// version. Knowing a program ID never grants access to its unpublished drafts.
var versionScope = `($3 OR (v.status IN ('published','archived') AND (
 EXISTS(SELECT 1 FROM groups g LEFT JOIN teachers t ON t.id=g.teacher_id
  WHERE g.program_version_id=v.id AND
   (($4 AND (t.user_id=$6 OR EXISTS(SELECT 1 FROM lessons l JOIN teachers lt ON lt.id=l.teacher_id WHERE l.group_id=g.id AND lt.user_id=$6))) OR
    ($5 AND EXISTS(SELECT 1 FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.group_id=g.id AND s.curator_id=$6))))
 OR EXISTS(SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id WHERE l.program_version_id=v.id AND
 ` + strings.NewReplacer("$1", "$3", "$2", "$4", "$3", "$5", "$4", "$6").Replace(access.LessonReadScopeSQL) + `)
)))`

func readFlags(actor httpx.Actor) (bool, bool, bool) {
	s := access.ScopeFor(actor, access.Curriculum, access.Read)
	return s == access.All, s&access.Own != 0, s&access.Assigned != 0
}
func loadVersion(ctx context.Context, tx pgx.Tx, programID, id int64, lock bool) (Version, error) {
	q := `SELECT ` + versionColumns + ` FROM program_versions v WHERE v.program_id=$1 AND v.id=$2`
	if lock {
		q += ` FOR UPDATE`
	}
	return scanVersion(tx.QueryRow(ctx, q, programID, id))
}
func record(ctx context.Context, tx pgx.Tx, actor httpx.Actor, id int64, action, comment string) error {
	if _, e := tx.Exec(ctx, `INSERT INTO program_version_events(version_id,actor_id,action,comment) VALUES($1,$2,$3,$4)`, id, actor.ID, action, comment); e != nil {
		return e
	}
	return database.AuditObject(ctx, tx, &actor.ID, "program_version."+action, "program_version", id, "status", "document")
}
func (r *Repository) List(ctx context.Context, programID int64) (VersionPage, error) {
	out := VersionPage{Items: []Version{}}
	tx, actor, e := r.begin(ctx, access.Curriculum, access.Read)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	all, own, assigned := readFlags(actor)
	rows, e := tx.Query(ctx, `SELECT `+versionColumns+` FROM program_versions v WHERE v.program_id=$1 AND ($2::bigint=0) AND `+versionScope+` ORDER BY v.number DESC`, programID, 0, all, own, assigned, actor.ID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out.Total = len(out.Items)
	return out, tx.Commit(ctx)
}
func (r *Repository) Get(ctx context.Context, programID, id int64) (Card, error) {
	out := Card{Events: []Event{}}
	tx, actor, e := r.begin(ctx, access.Curriculum, access.Read)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	all, own, assigned := readFlags(actor)
	out.Version, e = scanVersion(tx.QueryRow(ctx, `SELECT `+versionColumns+` FROM program_versions v WHERE v.program_id=$1 AND v.id=$2 AND `+versionScope, programID, id, all, own, assigned, actor.ID))
	if e != nil {
		return out, e
	}
	// Review comments are internal editorial information.
	if all {
		rows, err := tx.Query(ctx, `SELECT id,actor_id,action,comment,created_at FROM program_version_events WHERE version_id=$1 ORDER BY id`, id)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var ev Event
			if err = rows.Scan(&ev.ID, &ev.ActorID, &ev.Action, &ev.Comment, &ev.CreatedAt); err != nil {
				rows.Close()
				return out, err
			}
			out.Events = append(out.Events, ev)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, e
		}
	}
	return out, tx.Commit(ctx)
}
func (r *Repository) Create(ctx context.Context, programID int64, in CreateInput) (Version, error) {
	tx, actor, e := r.begin(ctx, access.Curriculum, access.Manage)
	if e != nil {
		return Version{}, e
	}
	defer tx.Rollback(ctx)
	var name, description string
	e = tx.QueryRow(ctx, `SELECT name,description FROM programs WHERE id=$1 FOR UPDATE`, programID).Scan(&name, &description)
	if e != nil {
		return Version{}, e
	}
	d := Document{Name: name, Description: description, Sections: []Section{}}
	if in.SourceVersionID != nil {
		source, err := loadVersion(ctx, tx, programID, *in.SourceVersionID, false)
		if err != nil {
			return Version{}, err
		}
		if source.Status != "published" && source.Status != "archived" {
			return Version{}, httpx.E(409, "source_not_published", "Копировать можно опубликованную или архивную версию.")
		}
		d = source.Document
	}
	raw, e := json.Marshal(d)
	if e != nil {
		return Version{}, e
	}
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO program_versions(program_id,number,document,created_by,source_version_id)
 VALUES($1,(SELECT COALESCE(max(number),0)+1 FROM program_versions WHERE program_id=$1),$2,$3,$4) RETURNING id`, programID, raw, actor.ID, in.SourceVersionID).Scan(&id)
	if e != nil {
		return Version{}, e
	}
	if e = record(ctx, tx, actor, id, "created", ""); e != nil {
		return Version{}, e
	}
	v, e := loadVersion(ctx, tx, programID, id, false)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func stale(v Version, revision int) error {
	if v.Revision != revision {
		return httpx.E(409, "stale_revision", "Версия уже изменена. Обновите документ перед сохранением.")
	}
	return nil
}
func (r *Repository) Save(ctx context.Context, programID, id int64, in SaveInput) (Version, error) {
	tx, actor, e := r.begin(ctx, access.Curriculum, access.Manage)
	if e != nil {
		return Version{}, e
	}
	defer tx.Rollback(ctx)
	v, e := loadVersion(ctx, tx, programID, id, true)
	if e != nil {
		return v, e
	}
	if e = stale(v, in.Revision); e != nil {
		return v, e
	}
	if v.Status != "draft" {
		return v, httpx.E(409, "version_not_draft", "Изменяется только черновик. Для опубликованной программы создайте новую версию.")
	}
	raw, e := json.Marshal(in.Document)
	if e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, `UPDATE program_versions SET document=$2 WHERE id=$1`, id, raw); e != nil {
		return v, e
	}
	if e = record(ctx, tx, actor, id, "edited", ""); e != nil {
		return v, e
	}
	v, e = loadVersion(ctx, tx, programID, id, false)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
func (r *Repository) Action(ctx context.Context, programID, id int64, in ActionInput) (Version, error) {
	action := access.Publish
	if in.Action == "submit" {
		action = access.Manage
	}
	tx, actor, e := r.begin(ctx, access.Curriculum, action)
	if e != nil {
		return Version{}, e
	}
	defer tx.Rollback(ctx)
	v, e := loadVersion(ctx, tx, programID, id, true)
	if e != nil {
		return v, e
	}
	if e = stale(v, in.Revision); e != nil {
		return v, e
	}
	next := ""
	switch {
	case in.Action == "submit" && v.Status == "draft":
		next = "review"
	case in.Action == "return" && v.Status == "review":
		next = "draft"
	case in.Action == "publish" && v.Status == "review":
		next = "published"
	case in.Action == "archive" && v.Status == "published":
		next = "archived"
	default:
		return v, httpx.E(409, "invalid_transition", "Действие недоступно в текущем статусе.")
	}
	if next == "review" || next == "published" {
		if e = validateDocument(&v.Document, true); e != nil {
			return v, e
		}
	}
	if next == "published" && (v.CreatedBy != nil && *v.CreatedBy == actor.ID || v.SubmittedBy != nil && *v.SubmittedBy == actor.ID) {
		return v, httpx.E(403, "self_approval", "Автор и отправитель не могут утверждать собственную версию.")
	}
	// Also forbid approval by a contributor who edited another author's draft.
	if next == "published" {
		var contributor bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM program_version_events WHERE version_id=$1 AND actor_id=$2 AND action IN ('created','edited','submit'))`, id, actor.ID).Scan(&contributor); e != nil {
			return v, e
		}
		if contributor {
			return v, httpx.E(403, "self_approval", "Участник подготовки не может утверждать эту версию.")
		}
	}
	_, e = tx.Exec(ctx, `UPDATE program_versions SET status=$2,
 submitted_by=CASE WHEN $2='review' THEN $3 ELSE submitted_by END,
 approved_by=CASE WHEN $2='published' THEN $3 ELSE approved_by END,
 published_at=CASE WHEN $2='published' THEN clock_timestamp() ELSE published_at END WHERE id=$1`, id, next, actor.ID)
	if e != nil {
		return v, e
	}
	if e = record(ctx, tx, actor, id, in.Action, in.Comment); e != nil {
		return v, e
	}
	v, e = loadVersion(ctx, tx, programID, id, false)
	if e != nil {
		return v, e
	}
	return v, tx.Commit(ctx)
}
