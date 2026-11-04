package students

import (
	"aiq.local/core/internal/domain/people"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type NoteInput struct {
	Body    string `json:"body"`
	Version int    `json:"version"`
}

func (s *Service) Notes(ctx context.Context, sid, nid int64, in *NoteInput, page int) (map[string]any, error) {
	v, e := s.Repo.Notes(ctx, sid, nid, in, page)
	return v, people.Translate(e)
}
func (r *Repository) Notes(ctx context.Context, sid, nid int64, in *NoteInput, page int) (map[string]any, error) {
	tx, a, e := access.Begin(ctx, r.Pool, access.Students, access.Read, in != nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	scope := access.ScopeFor(a, access.Students, access.ReadFull)
	if scope == access.None {
		return nil, httpx.Forbidden()
	}
	var allowed bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students s WHERE s.id=$1 AND ($2 OR ($3 AND curator_has_student($4,s.id))))`, sid, scope == access.All, scope&access.Assigned != 0, a.ID).Scan(&allowed)
	if e != nil {
		return nil, e
	}
	if !allowed {
		return nil, httpx.E(404, "not_found", "Ученик недоступен.")
	}
	if in != nil {
		in.Body = strings.TrimSpace(in.Body)
		if utf8.RuneCountInString(in.Body) < 1 || utf8.RuneCountInString(in.Body) > 2000 || strings.ContainsRune(in.Body, 0) {
			return nil, httpx.Invalid("body", "Заметка: от 1 до 2000 символов.")
		}
		version := 1
		if nid == 0 {
			e = tx.QueryRow(ctx, `INSERT INTO curator_notes(student_id,author_id,body) VALUES($1,$2,$3) RETURNING id`, sid, a.ID, in.Body).Scan(&nid)
		} else {
			var author int64
			e = tx.QueryRow(ctx, `SELECT author_id,version FROM curator_notes WHERE id=$1 AND student_id=$2 FOR UPDATE`, nid, sid).Scan(&author, &version)
			if e != nil {
				return nil, e
			}
			if author != a.ID && !a.Admin() {
				return nil, httpx.Forbidden()
			}
			if version != in.Version {
				return nil, httpx.E(409, "note_version_conflict", "Заметка уже изменена. Обновите ленту.")
			}
			version++
			_, e = tx.Exec(ctx, `UPDATE curator_notes SET body=$2,version=$3,updated_at=clock_timestamp() WHERE id=$1`, nid, in.Body, version)
		}
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO curator_note_revisions(note_id,version,editor_id,body) VALUES($1,$2,$3,$4)`, nid, version, a.ID, in.Body); e != nil {
			return nil, e
		}
		if e = database.AuditObject(ctx, tx, &a.ID, "curator_note.saved", "curator_note", nid, "body", "version"); e != nil {
			return nil, e
		}
		return map[string]any{"id": nid, "version": version}, tx.Commit(ctx)
	}
	var total int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM curator_notes WHERE student_id=$1`, sid).Scan(&total); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT jsonb_build_object('id',n.id,'author_id',n.author_id,'author_name',u.name,'body',n.body,'version',n.version,'created_at',n.created_at,'updated_at',n.updated_at,'can_edit',(n.author_id=$2 OR $3),'history',(SELECT jsonb_agg(jsonb_build_object('version',v.version,'body',v.body,'editor_name',ed.name,'created_at',v.created_at) ORDER BY v.version DESC) FROM curator_note_revisions v JOIN users ed ON ed.id=v.editor_id WHERE v.note_id=n.id)) FROM curator_notes n JOIN users u ON u.id=n.author_id WHERE n.student_id=$1 ORDER BY n.created_at DESC,n.id DESC LIMIT 20 OFFSET $4`, sid, a.ID, a.Admin(), (page-1)*20)
	if e != nil {
		return nil, e
	}
	items := []map[string]any{}
	for rows.Next() {
		var v map[string]any
		if e = rows.Scan(&v); e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return map[string]any{"items": items, "total": total, "page": page, "page_size": 20}, tx.Commit(ctx)
}
func (h *Handler) Notes(w http.ResponseWriter, r *http.Request) {
	sid, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || sid <= 0 {
		httpx.Fail(w, r, httpx.Invalid("id", "Некорректный ученик."))
		return
	}
	nid := int64(0)
	if raw := r.PathValue("note_id"); raw != "" {
		nid, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || nid <= 0 {
			httpx.Fail(w, r, httpx.Invalid("note_id", "Некорректная заметка."))
			return
		}
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, e = strconv.Atoi(raw)
		if e != nil || page < 1 || page > 1000000 {
			httpx.Fail(w, r, httpx.Invalid("page", "Некорректная страница."))
			return
		}
	}
	var in *NoteInput
	if r.Method != "GET" {
		in = &NoteInput{}
		if e = httpx.Decode(w, r, in); e != nil {
			httpx.Fail(w, r, e)
			return
		}
	}
	v, e := h.Service.Notes(r.Context(), sid, nid, in, page)
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	httpx.JSON(w, status, v)
}
