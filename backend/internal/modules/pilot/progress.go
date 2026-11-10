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
	sendRows(w, r, tx, `WITH work AS (
        SELECT a.student_id,a.id,a.status,false AS quiz FROM assignments a JOIN lessons l ON l.id=a.lesson_id WHERE l.status<>'cancelled'
        UNION ALL SELECT h.student_id,h.id,CASE WHEN h.completed_at IS NOT NULL THEN 'reviewed' WHEN h.attempts>0 THEN 'failed' ELSE 'assigned' END,true FROM homework_test_assignments h
    ), lesson_points AS (
        SELECT att.student_id,count(*) AS points FROM attendance att JOIN lessons l ON l.id=att.lesson_id
        WHERE att.included AND att.status IN ('present','late') AND l.status='completed' GROUP BY att.student_id
    ) SELECT jsonb_build_object('student_id',s.id,'student_name',s.name,
		'total',count(a.id),'reviewed',count(a.id) FILTER (WHERE a.status='reviewed'),
		'submitted',count(a.id) FILTER (WHERE a.status='submitted'),
		'assigned',count(a.id) FILTER (WHERE a.status='assigned'),
        'completed_tests',count(a.id) FILTER (WHERE a.quiz AND a.status='reviewed'),
        'failed_tests',count(a.id) FILTER (WHERE a.status='failed'),
        'accepted_assignments',count(a.id) FILTER (WHERE NOT a.quiz AND a.status='reviewed'),
        'completed_lessons',COALESCE(max(lp.points),0),
        'progress_marks',count(a.id) FILTER (WHERE a.status='reviewed')+COALESCE(max(lp.points),0))
        FROM students s LEFT JOIN work a ON a.student_id=s.id LEFT JOIN lesson_points lp ON lp.student_id=s.id
		WHERE s.user_id=$1 AND s.status='active' GROUP BY s.id ORDER BY s.id`, actor.ID)
}
