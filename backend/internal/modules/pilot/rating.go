package pilot

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// One successful test, accepted homework or attended completed lesson earns one mark.
// Rank all active students before applying search or pagination.
const studentRatingSQL = `WITH homework AS (
 SELECT a.student_id,
 count(*) FILTER(WHERE a.status='reviewed') AS accepted,
 count(*) FILTER(WHERE a.status='submitted') AS submitted,
 count(*) FILTER(WHERE a.status='assigned') AS assigned,
 max(GREATEST(a.submitted_at,a.reviewed_at)) AS last_activity
 FROM assignments a JOIN lessons l ON l.id=a.lesson_id WHERE l.status<>'cancelled' GROUP BY a.student_id
), tests AS (
 SELECT h.student_id,count(*) FILTER(WHERE h.completed_at IS NOT NULL) AS completed,
 count(*) FILTER(WHERE h.attempts>0 AND h.completed_at IS NULL) AS failed,
 count(*) FILTER(WHERE h.attempts=0) AS assigned,max(h.last_attempt_at) AS last_activity
 FROM homework_test_assignments h GROUP BY h.student_id
), lessons_done AS (
 SELECT att.student_id,count(*) AS completed,max(l.ends_at) AS last_activity
 FROM attendance att JOIN lessons l ON l.id=att.lesson_id
 WHERE att.included AND att.status IN ('present','late') AND l.status='completed' GROUP BY att.student_id
), scores AS (
 SELECT s.id,s.name,s.name_search,
 COALESCE(h.accepted,0) AS accepted_assignments,COALESCE(h.submitted,0) AS submitted_assignments,
 COALESCE(h.assigned,0)+COALESCE(t.assigned,0) AS pending,
 COALESCE(t.completed,0) AS completed_tests,COALESCE(t.failed,0) AS failed_tests,
 COALESCE(l.completed,0) AS completed_lessons,
 COALESCE(h.accepted,0)+COALESCE(t.completed,0)+COALESCE(l.completed,0) AS progress_marks,
 GREATEST(h.last_activity,t.last_activity,l.last_activity) AS last_activity
 FROM students s LEFT JOIN homework h ON h.student_id=s.id LEFT JOIN tests t ON t.student_id=s.id
 LEFT JOIN lessons_done l ON l.student_id=s.id WHERE s.status='active'
), ranked AS (
 SELECT *,dense_rank() OVER(ORDER BY progress_marks DESC) AS rank FROM scores
) `

func (h *Handler) StudentRating(w http.ResponseWriter, r *http.Request) {
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000000 {
			fail(w, r, httpx.Invalid("page", "Некорректная страница."))
			return
		}
		page = n
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	if utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
		fail(w, r, httpx.Invalid("search", "Введите до 200 символов."))
		return
	}
	tx, err := h.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = access.AuthorizeRead(r.Context(), tx, access.StudentRating); err != nil {
		fail(w, r, err)
		return
	}
	var total int
	err = tx.QueryRow(r.Context(), studentRatingSQL+`SELECT count(*) FROM ranked WHERE ($1='' OR strpos(name_search,$1)>0)`, search).Scan(&total)
	if err != nil {
		fail(w, r, err)
		return
	}
	rows, err := tx.Query(r.Context(), studentRatingSQL+`SELECT to_jsonb(r)-'name_search' FROM ranked r WHERE ($1='' OR strpos(name_search,$1)>0) ORDER BY rank,name_search,id LIMIT 20 OFFSET $2`, search, (page-1)*20)
	if err != nil {
		fail(w, r, err)
		return
	}
	items := []map[string]any{}
	for rows.Next() {
		var item map[string]any
		if err = rows.Scan(&item); err != nil {
			break
		}
		items = append(items, item)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": 20})
}
