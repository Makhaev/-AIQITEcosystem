package pilot

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

func has(a httpx.Actor, role string) bool {
	for _, v := range a.Roles {
		if v == role {
			return true
		}
	}
	return false
}
func (h *Handler) begin(ctx context.Context, action access.Action) (pgx.Tx, httpx.Actor, error) {
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		return nil, httpx.Actor{}, err
	}
	if action != access.Read {
		if err = access.LockManagement(ctx, tx); err != nil {
			tx.Rollback(ctx)
			return nil, httpx.Actor{}, err
		}
	}
	var actor httpx.Actor
	if action == access.Read {
		actor, err = access.AuthorizeRead(ctx, tx, access.Learning)
	} else {
		actor, err = access.Authorize(ctx, tx, access.Learning, action)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}

// Parameters $1..$6 are actor id, all-staff, teacher, student, parent, curator.
func scopeArgs(a httpx.Actor) []any {
	return []any{a.ID, a.Admin() || has(a, "leader"), has(a, "teacher"), has(a, "student"), has(a, "parent"), has(a, "curator")}
}

const childScope = `(s.status='active' AND (($4::boolean AND s.user_id=$1) OR ($5::boolean AND EXISTS(SELECT 1 FROM student_parents sp JOIN parents p ON p.id=sp.parent_id WHERE sp.student_id=s.id AND p.user_id=$1 AND p.status='active'))))`
const memberScope = `(CASE WHEN l.roster_initialized_at IS NOT NULL THEN EXISTS(SELECT 1 FROM attendance att WHERE att.lesson_id=l.id AND att.student_id=s.id AND att.included) ELSE EXISTS(SELECT 1 FROM group_enrollments e WHERE e.group_id=l.group_id AND e.student_id=s.id AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)) END)`
const studentScope = `($2::boolean OR ($6::boolean AND curator_has_student($1,s.id)) OR ` + childScope + ` OR ($3::boolean AND EXISTS(SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id WHERE t.user_id=$1 AND ` + memberScope + `)))`
const lessonScope = `($2::boolean OR ($3::boolean AND t.user_id=$1) OR ($6::boolean AND curator_has_student($1,s.id)) OR ` + childScope + `)`
const assignmentScope = `($2::boolean OR ($3::boolean AND t.user_id=$1) OR ($5::boolean AND curator_has_student($1,s.id)) OR ($4::boolean AND s.user_id=$1 AND s.status='active'))`

// Return typed database records via JSON without duplicating each projection's Go model.
func sendRows(w http.ResponseWriter, r *http.Request, tx pgx.Tx, query string, args ...any) {
	rows, err := tx.Query(r.Context(), query, args...)
	if err != nil {
		fail(w, r, err)
		return
	}
	items := []any{}
	for rows.Next() {
		var value map[string]any
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			fail(w, r, err)
			return
		}
		items = append(items, value)
	}
	err = rows.Err()
	rows.Close()
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) Students(w http.ResponseWriter, r *http.Request) {
	tx, a, err := h.begin(r.Context(), access.Read)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	sendRows(w, r, tx, `SELECT jsonb_build_object('id',s.id,'name',s.name,'status',s.status) FROM students s WHERE `+studentScope+` ORDER BY s.name,s.id LIMIT 500`, scopeArgs(a)...)
}
func (h *Handler) Lessons(w http.ResponseWriter, r *http.Request) {
	tx, a, err := h.begin(r.Context(), access.Read)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	sendRows(w, r, tx, `SELECT jsonb_build_object('id',l.id,'student_id',s.id,'student_name',s.name,'topic',l.topic,'starts_at',l.starts_at,'ends_at',l.ends_at,'status',l.status,'group_name',g.name,'teacher_name',t.name,'attendance',CASE WHEN l.status='completed' THEN (SELECT att.status FROM attendance att WHERE att.lesson_id=l.id AND att.student_id=s.id AND att.included) ELSE NULL END) FROM lessons l JOIN groups g ON g.id=l.group_id JOIN teachers t ON t.id=l.teacher_id JOIN students s ON `+memberScope+` WHERE `+lessonScope+` ORDER BY l.starts_at DESC,s.id LIMIT 500`, scopeArgs(a)...)
}
func (h *Handler) Assignments(w http.ResponseWriter, r *http.Request) {
	tx, a, err := h.begin(r.Context(), access.Read)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Parents see published reports, not answers and teacher review drafts.
	sendRows(w, r, tx, `SELECT to_jsonb(a)-'created_by' || jsonb_build_object('student_name',s.name,'lesson_topic',l.topic) FROM assignments a JOIN students s ON s.id=a.student_id JOIN lessons l ON l.id=a.lesson_id JOIN teachers t ON t.id=l.teacher_id WHERE `+assignmentScope+` ORDER BY a.created_at DESC,a.id DESC LIMIT 500`, a.ID, a.Admin() || has(a, "leader"), has(a, "teacher"), has(a, "student"), has(a, "curator"))
}
func (h *Handler) Reports(w http.ResponseWriter, r *http.Request) {
	tx, a, err := h.begin(r.Context(), access.Read)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	sendRows(w, r, tx, `SELECT to_jsonb(rep)-'created_by' || jsonb_build_object('student_name',s.name,'lesson_topic',l.topic,'lesson_starts_at',l.starts_at) FROM learning_reports rep JOIN students s ON s.id=rep.student_id JOIN lessons l ON l.id=rep.lesson_id JOIN teachers t ON t.id=l.teacher_id WHERE ($2::boolean OR ($3::boolean AND t.user_id=$1) OR ($6::boolean AND curator_has_student($1,s.id)) OR (rep.published_at IS NOT NULL AND `+childScope+`)) ORDER BY rep.created_at DESC,rep.id DESC LIMIT 500`, scopeArgs(a)...)
}
func lessonWriter(ctx context.Context, tx pgx.Tx, a httpx.Actor, lessonID, studentID int64) (string, error) {
	var teacher *int64
	var status string
	var active bool
	err := tx.QueryRow(ctx, `SELECT t.user_id,l.status,s.status='active' AND t.status='active' AND EXISTS(SELECT 1 FROM attendance att WHERE att.lesson_id=l.id AND att.student_id=s.id AND att.included) FROM lessons l JOIN teachers t ON t.id=l.teacher_id JOIN students s ON s.id=$2 WHERE l.id=$1 FOR UPDATE OF l`, lessonID, studentID).Scan(&teacher, &status, &active)
	if err != nil {
		return status, err
	}
	if !a.Admin() && !(has(a, "teacher") && teacher != nil && *teacher == a.ID) {
		return status, httpx.Forbidden()
	}
	if status == "cancelled" || !active {
		return status, httpx.E(409, "lesson_unavailable", "Откройте журнал занятия и проверьте активный состав. Отменённое занятие недоступно.")
	}
	return status, nil
}
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.Invalid("id", "Некорректный номер записи.")
	}
	return id, nil
}
func (h *Handler) CreateAssignment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LessonID     int64      `json:"lesson_id"`
		StudentID    int64      `json:"student_id"`
		Title        string     `json:"title"`
		Instructions string     `json:"instructions"`
		DueAt        *time.Time `json:"due_at"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !textValid(in.Title, 200) || !textValid(in.Instructions, 10000) {
		fail(w, r, httpx.Invalid("instructions", "Укажите название до 200 и задание до 10000 символов."))
		return
	}
	tx, a, err := h.begin(r.Context(), access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = lessonWriter(r.Context(), tx, a, in.LessonID, in.StudentID); err != nil {
		fail(w, r, err)
		return
	}
	var published bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM learning_reports WHERE lesson_id=$1 AND student_id=$2 AND published_at IS NOT NULL)`, in.LessonID, in.StudentID).Scan(&published)
	if err != nil {
		fail(w, r, err)
		return
	}
	if published {
		fail(w, r, httpx.E(409, "report_published", "После публикации отчёта задания занятия закрыты для изменения."))
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(), `INSERT INTO assignments(lesson_id,student_id,title,instructions,due_at,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, in.LessonID, in.StudentID, strings.TrimSpace(in.Title), strings.TrimSpace(in.Instructions), in.DueAt, a.ID).Scan(&id)
	if err == nil {
		err = audit(r.Context(), tx, a.ID, "assignment.created", "assignment", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]int64{"id": id})
}
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var in struct {
		Answer string `json:"answer"`
	}
	if err = httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !textValid(in.Answer, 10000) {
		fail(w, r, httpx.Invalid("answer", "Ответ: от 1 до 10000 символов."))
		return
	}
	tx, a, err := h.begin(r.Context(), access.Mark)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Lock the lesson before the assignment, same as review and report publication.
	var lessonID int64
	if err = tx.QueryRow(r.Context(), `SELECT lesson_id FROM assignments WHERE id=$1`, id).Scan(&lessonID); err != nil {
		fail(w, r, err)
		return
	}
	var lessonStatus string
	if err = tx.QueryRow(r.Context(), `SELECT status FROM lessons WHERE id=$1 FOR UPDATE`, lessonID).Scan(&lessonStatus); err != nil {
		fail(w, r, err)
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE assignments a SET answer=$3,status='submitted',submitted_at=now(),reviewed_at=NULL,updated_at=now() FROM students s WHERE a.id=$1 AND s.id=a.student_id AND s.user_id=$2 AND s.status='active' AND a.status='assigned' AND $4::text<>'cancelled'`, id, a.ID, strings.TrimSpace(in.Answer), lessonStatus)
	if err != nil {
		fail(w, r, err)
		return
	}
	if tag.RowsAffected() != 1 {
		fail(w, r, httpx.E(409, "submission_unavailable", "Задание недоступно или уже отправлено."))
		return
	}
	if err = audit(r.Context(), tx, a.ID, "assignment.submitted", "assignment", id); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) Review(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var in struct {
		Feedback string `json:"feedback"`
		Accepted bool   `json:"accepted"`
	}
	if err = httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !textValid(in.Feedback, 5000) {
		fail(w, r, httpx.Invalid("feedback", "Комментарий: от 1 до 5000 символов."))
		return
	}
	tx, a, err := h.begin(r.Context(), access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var lessonID, studentID int64
	if err = tx.QueryRow(r.Context(), `SELECT lesson_id,student_id FROM assignments WHERE id=$1`, id).Scan(&lessonID, &studentID); err != nil {
		fail(w, r, err)
		return
	}
	if _, err = lessonWriter(r.Context(), tx, a, lessonID, studentID); err != nil {
		fail(w, r, err)
		return
	}
	next := "assigned"
	if in.Accepted {
		next = "reviewed"
	}
	tag, err := tx.Exec(r.Context(), `UPDATE assignments SET status=$2,feedback=$3,reviewed_at=CASE WHEN $2='reviewed' THEN now() ELSE NULL END,updated_at=now() WHERE id=$1 AND status='submitted'`, id, next, strings.TrimSpace(in.Feedback))
	if err != nil {
		fail(w, r, err)
		return
	}
	if tag.RowsAffected() != 1 {
		fail(w, r, httpx.E(409, "not_submitted", "Проверить можно только отправленную работу."))
		return
	}
	if err = audit(r.Context(), tx, a.ID, "assignment.reviewed", "assignment", id); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) CreateReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LessonID  int64  `json:"lesson_id"`
		StudentID int64  `json:"student_id"`
		Summary   string `json:"summary"`
	}
	if err := httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	if !textValid(in.Summary, 10000) {
		fail(w, r, httpx.Invalid("summary", "Отчёт: от 1 до 10000 символов."))
		return
	}
	tx, a, err := h.begin(r.Context(), access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = lessonWriter(r.Context(), tx, a, in.LessonID, in.StudentID); err != nil {
		fail(w, r, err)
		return
	}
	var id int64
	err = tx.QueryRow(r.Context(), `INSERT INTO learning_reports(lesson_id,student_id,summary,created_by) VALUES($1,$2,$3,$4) ON CONFLICT(lesson_id,student_id) DO UPDATE SET summary=EXCLUDED.summary WHERE learning_reports.published_at IS NULL RETURNING id`, in.LessonID, in.StudentID, strings.TrimSpace(in.Summary), a.ID).Scan(&id)
	if err == nil {
		err = audit(r.Context(), tx, a.ID, "report.saved", "report", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]int64{"id": id})
}
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, a, err := h.begin(r.Context(), access.Manage)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var lessonID, studentID int64
	if err = tx.QueryRow(r.Context(), `SELECT lesson_id,student_id FROM learning_reports WHERE id=$1`, id).Scan(&lessonID, &studentID); err != nil {
		fail(w, r, err)
		return
	}
	status, err := lessonWriter(r.Context(), tx, a, lessonID, studentID)
	if err != nil {
		fail(w, r, err)
		return
	}
	var ready bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM assignments WHERE lesson_id=$1 AND student_id=$2 AND status='reviewed') AND NOT EXISTS(SELECT 1 FROM assignments WHERE lesson_id=$1 AND student_id=$2 AND status<>'reviewed')`, lessonID, studentID).Scan(&ready)
	if err != nil {
		fail(w, r, err)
		return
	}
	if status != "completed" || !ready {
		fail(w, r, httpx.E(409, "report_not_ready", "Завершите занятие и проверьте все задания ученика перед публикацией."))
		return
	}
	tag, err := tx.Exec(r.Context(), `UPDATE learning_reports SET published_at=clock_timestamp() WHERE id=$1 AND published_at IS NULL`, id)
	if err != nil {
		fail(w, r, err)
		return
	}
	if tag.RowsAffected() != 1 {
		fail(w, r, httpx.E(409, "already_published", "Отчёт уже опубликован."))
		return
	}
	if err = audit(r.Context(), tx, a.ID, "report.published", "report", id); err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	w.WriteHeader(204)
}
