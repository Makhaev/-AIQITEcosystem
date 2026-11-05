package pilot

import (
	"net/http"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
)

// StudentProgress always uses the student's own account, including mixed-role users.
// Aggregate in PostgreSQL so the learning list's 500-row limit cannot change HP.
func (h *Handler) StudentProgress(w http.ResponseWriter, r *http.Request) {
	tx, actor, err := h.begin(r.Context(), access.Read)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !has(actor, "student") {
		fail(w, r, httpx.Forbidden())
		return
	}
	sendRows(w, r, tx, `SELECT jsonb_build_object('student_id',s.id,'student_name',s.name,
		'total',count(a.id),'reviewed',count(a.id) FILTER (WHERE a.status='reviewed'),
		'submitted',count(a.id) FILTER (WHERE a.status='submitted'),
		'assigned',count(a.id) FILTER (WHERE a.status='assigned'))
		FROM students s LEFT JOIN assignments a ON a.student_id=s.id
		AND EXISTS(SELECT 1 FROM lessons l WHERE l.id=a.lesson_id AND l.status<>'cancelled')
		WHERE s.user_id=$1 AND s.status='active' GROUP BY s.id ORDER BY s.id`, actor.ID)
}
