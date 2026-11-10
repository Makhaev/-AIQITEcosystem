package curators

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
)

// Workload follows current group assignments; reports remain attributed to their author.
const directoryProjection = `jsonb_build_object('id',u.id,'name',u.name,'email',u.email,'blocked',u.blocked,
 'groups_count',(SELECT count(*) FROM groups g WHERE g.curator_id=u.id AND g.status='active'),
 'students_count',(SELECT count(*) FROM students s WHERE s.status='active' AND curator_has_student(u.id,s.id)),
 'reports_count',(SELECT count(*) FROM curator_reports r WHERE r.created_by=u.id),
 'published_count',(SELECT count(*) FROM curator_reports r WHERE r.created_by=u.id AND r.published_at IS NOT NULL),
 'last_activity',(SELECT max(r.updated_at) FROM curator_reports r WHERE r.created_by=u.id))`
const directoryFrom = ` FROM users u WHERE EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=u.id AND ur.role='curator')`

func (h *Handler) Directory(w http.ResponseWriter, r *http.Request) {
	page, size, err := pagination(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	status := r.URL.Query().Get("status")
	if utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) || (status != "" && status != "active" && status != "blocked") {
		fail(w, r, httpx.Invalid("search", "Проверьте поиск и статус."))
		return
	}
	tx, _, err := h.begin(r.Context(), access.Curators, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	where := directoryFrom + ` AND ($1='' OR strpos(lower(u.name || ' ' || u.email),$1)>0) AND ($2='' OR ($2='blocked')=u.blocked)`
	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, search, status).Scan(&total)
	items := []map[string]any{}
	if err == nil {
		items, err = records(r.Context(), tx, `SELECT `+directoryProjection+where+` ORDER BY lower(u.name),u.id LIMIT $3 OFFSET $4`, search, status, size, (page-1)*size)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}

func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, _, err := h.begin(r.Context(), access.Curators, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var item map[string]any
	err = tx.QueryRow(r.Context(), `SELECT `+directoryProjection+directoryFrom+` AND u.id=$1`, id).Scan(&item)
	if err == nil {
		item["groups"], err = records(r.Context(), tx, `SELECT jsonb_build_object('id',g.id,'name',g.name,'status',g.status) FROM groups g WHERE g.curator_id=$1 ORDER BY g.name,g.id`, id)
	}
	if err == nil {
		item["recent_reports"], err = records(r.Context(), tx, `SELECT jsonb_build_object('id',rep.id,'student_name',s.name,'summary',rep.summary,'updated_at',rep.updated_at,'published_at',rep.published_at) FROM curator_reports rep JOIN students s ON s.id=rep.student_id WHERE rep.created_by=$1 ORDER BY rep.updated_at DESC,rep.id DESC LIMIT 20`, id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, item)
}
