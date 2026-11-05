package curators

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (h *Handler) Groups(w http.ResponseWriter, r *http.Request) {
	page, size, err := pagination(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	if utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
		fail(w, r, httpx.Invalid("search", "Поиск: до 200 символов."))
		return
	}
	tx, a, err := h.begin(r.Context(), access.CuratorWorkspace, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	where := ` FROM groups g JOIN programs p ON p.id=g.program_id LEFT JOIN teachers t ON t.id=g.teacher_id WHERE g.curator_id=$1 AND ($2='' OR strpos(g.name_search,$2)>0)`
	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, a.ID, search).Scan(&total)
	items := []map[string]any{}
	if err == nil {
		items, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',g.id,'name',g.name,'program_name',p.name,'teacher_name',t.name,'status',g.status,'student_count',CASE WHEN g.status='active' THEN (SELECT count(DISTINCT e.student_id) FROM group_enrollments e WHERE e.group_id=g.id AND e.ends_on IS NULL AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date) ELSE 0 END)`+where+` ORDER BY g.name_search,g.id LIMIT $3 OFFSET $4`, a.ID, search, size, (page-1)*size)
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
