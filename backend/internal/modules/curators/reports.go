package curators

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const reportScope = `($2::boolean OR ($3::boolean AND curator_has_student($1,s.id)) OR ($4::boolean AND rep.published_at IS NOT NULL AND s.status='active' AND EXISTS(SELECT 1 FROM curator_report_recipients rr JOIN parents p ON p.id=rr.parent_id JOIN student_parents sp ON sp.parent_id=p.id AND sp.student_id=s.id WHERE rr.report_id=rep.id AND p.user_id=$1 AND p.status='active')))`
const reportProjection = `to_jsonb(rep)||jsonb_build_object('files',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',rf.id,'name',rf.name,'size',octet_length(rf.content),'created_at',rf.created_at) ORDER BY rf.id) FROM curator_report_files rf WHERE rf.report_id=rep.id),'[]'::jsonb))||jsonb_build_object('can_feedback',($4::boolean AND rep.published_at IS NOT NULL AND s.status='active' AND EXISTS(SELECT 1 FROM curator_report_recipients fr JOIN parents fp ON fp.id=fr.parent_id JOIN student_parents fs ON fs.parent_id=fp.id AND fs.student_id=s.id WHERE fr.report_id=rep.id AND fp.user_id=$1 AND fp.status='active')),'student_name',s.name,'author_name',author.name,'recipients',CASE WHEN $2::boolean OR ($3::boolean AND curator_has_student($1,s.id)) THEN COALESCE((SELECT jsonb_agg(jsonb_build_object('id',p.id,'name',p.name) ORDER BY p.id) FROM curator_report_recipients rr JOIN parents p ON p.id=rr.parent_id WHERE rr.report_id=rep.id),'[]'::jsonb) ELSE '[]'::jsonb END,'feedback',COALESCE((SELECT jsonb_agg(message ORDER BY fid) FROM (SELECT f.id fid,jsonb_build_object('id',f.id,'parent_name',p.name,'message',f.message,'created_at',f.created_at) message FROM curator_report_feedback f JOIN parents p ON p.id=f.parent_id WHERE f.report_id=rep.id AND ($2::boolean OR ($3::boolean AND curator_has_student($1,s.id)) OR p.user_id=$1) ORDER BY f.id DESC LIMIT 100) messages),'[]'::jsonb))`
const reportFrom = ` FROM curator_reports rep JOIN students s ON s.id=rep.student_id JOIN users author ON author.id=rep.created_by WHERE ` + reportScope

func reportArgs(a httpx.Actor) []any {
	return []any{a.ID, a.Admin() || has(a, "leader"), has(a, "curator"), has(a, "parent")}
}
func writableStudent(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64) (string, error) {
	var status string
	err := tx.QueryRow(ctx, `SELECT s.status FROM students s WHERE s.id=$1 AND ($2 OR curator_has_student($3,s.id)) FOR UPDATE`, id, a.Admin(), a.ID).Scan(&status)
	return status, err
}

type ReportInput struct {
	StudentID       int64  `json:"student_id"`
	From            string `json:"from"`
	To              string `json:"to"`
	Summary         string `json:"summary"`
	Recommendations string `json:"recommendations"`
	Goals           string `json:"goals"`
}

func validateReport(in *ReportInput) (Period, error) {
	in.Summary = strings.TrimSpace(in.Summary)
	in.Recommendations = strings.TrimSpace(in.Recommendations)
	in.Goals = strings.TrimSpace(in.Goals)
	if utf8.RuneCountInString(in.Summary) < 1 || utf8.RuneCountInString(in.Summary) > 10000 || utf8.RuneCountInString(in.Recommendations) > 5000 || utf8.RuneCountInString(in.Goals) > 5000 {
		return Period{}, httpx.Invalid("summary", "Укажите итог до 10000 символов; рекомендации и цели — до 5000 символов каждое.")
	}
	return period(in.From, in.To)
}
func (h *Handler) Reports(w http.ResponseWriter, r *http.Request) {
	page, size, err := pagination(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var studentID int64
	if raw := r.URL.Query().Get("student_id"); raw != "" {
		studentID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || studentID <= 0 {
			fail(w, r, httpx.Invalid("student_id", "Некорректный ученик."))
			return
		}
	}
	tx, a, err := h.begin(r.Context(), access.CuratorReports, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	args := append(reportArgs(a), studentID)
	where := reportFrom + ` AND ($5::bigint=0 OR rep.student_id=$5)`
	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, args...).Scan(&total)
	result := []map[string]any{}
	if err == nil {
		result, err = records(r.Context(), tx, `SELECT `+reportProjection+where+` ORDER BY rep.created_at DESC,rep.id DESC LIMIT $6 OFFSET $7`, append(args, size, (page-1)*size)...)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": result, "total": total, "page": page, "page_size": size})
}
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, a, err := h.begin(r.Context(), access.CuratorReports, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var result map[string]any
	err = tx.QueryRow(r.Context(), `SELECT `+reportProjection+reportFrom+` AND rep.id=$5`, append(reportArgs(a), id)...).Scan(&result)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, result)
}
func (h *Handler) SaveReport(w http.ResponseWriter, r *http.Request) {
	var id int64
	var err error
	if r.Method == "PATCH" {
		id, err = idFrom(r)
		if err != nil {
			fail(w, r, err)
			return
		}
	}
	var in ReportInput
	if err = httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	p, err := validateReport(&in)
	if err != nil {
		fail(w, r, err)
		return
	}
	tx, a, err := h.begin(r.Context(), access.CuratorReports, access.Manage, true)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if id != 0 {
		var original int64
		if err = tx.QueryRow(r.Context(), `SELECT student_id FROM curator_reports WHERE id=$1`, id).Scan(&original); err != nil {
			fail(w, r, err)
			return
		}
		if in.StudentID != original {
			fail(w, r, httpx.Invalid("student_id", "Ученик отчёта не может быть изменён."))
			return
		}
	}
	status, err := writableStudent(r.Context(), tx, a, in.StudentID)
	if err != nil {
		fail(w, r, err)
		return
	}
	if status != "active" {
		fail(w, r, httpx.E(409, "student_archived", "Для отчёта нужна активная карточка ученика."))
		return
	}
	if id == 0 {
		err = tx.QueryRow(r.Context(), `INSERT INTO curator_reports(student_id,created_by,period_from,period_to,summary,recommendations,goals) VALUES($1,$2,$3::date,$4::date,$5,$6,$7) RETURNING id`, in.StudentID, a.ID, p.From, p.To, in.Summary, in.Recommendations, in.Goals).Scan(&id)
	} else {
		var tag interface{ RowsAffected() int64 }
		tag, err = tx.Exec(r.Context(), `UPDATE curator_reports SET period_from=$2::date,period_to=$3::date,summary=$4,recommendations=$5,goals=$6,updated_at=now() WHERE id=$1 AND published_at IS NULL`, id, p.From, p.To, in.Summary, in.Recommendations, in.Goals)
		if err == nil && tag.RowsAffected() != 1 {
			err = httpx.E(409, "report_published", "Опубликованный отчёт нельзя изменять. Создайте новый.")
		}
	}
	if err == nil {
		err = database.AuditObject(r.Context(), tx, &a.ID, "curator_report.saved", "curator_report", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	code := 201
	if r.Method == "PATCH" {
		code = 200
	}
	httpx.JSON(w, code, map[string]int64{"id": id})
}
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var in struct {
		ParentIDs []int64 `json:"parent_ids"`
	}
	if err = httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	seen := map[int64]bool{}
	if len(in.ParentIDs) == 0 || len(in.ParentIDs) > 20 {
		fail(w, r, httpx.Invalid("parent_ids", "Выберите от 1 до 20 родителей-получателей."))
		return
	}
	for _, value := range in.ParentIDs {
		if value <= 0 || seen[value] {
			fail(w, r, httpx.Invalid("parent_ids", "Получатели не должны повторяться."))
			return
		}
		seen[value] = true
	}
	tx, a, err := h.begin(r.Context(), access.CuratorReports, access.Manage, true)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var studentID int64
	var from, to string
	err = tx.QueryRow(r.Context(), `SELECT student_id,period_from::text,period_to::text FROM curator_reports WHERE id=$1`, id).Scan(&studentID, &from, &to)
	if err != nil {
		fail(w, r, err)
		return
	}
	status, err := writableStudent(r.Context(), tx, a, studentID)
	if err != nil {
		fail(w, r, err)
		return
	}
	if status != "active" {
		fail(w, r, httpx.E(409, "student_archived", "Ученик находится в архиве."))
		return
	}
	var published bool
	err = tx.QueryRow(r.Context(), `SELECT published_at IS NOT NULL FROM curator_reports WHERE id=$1 FOR UPDATE`, id).Scan(&published)
	if err != nil {
		fail(w, r, err)
		return
	}
	if published {
		fail(w, r, httpx.E(409, "report_published", "Отчёт уже опубликован."))
		return
	}
	for _, parentID := range in.ParentIDs {
		var allowed bool
		err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM student_parents sp JOIN parents p ON p.id=sp.parent_id JOIN users u ON u.id=p.user_id JOIN user_roles ur ON ur.user_id=u.id AND ur.role='parent' WHERE sp.student_id=$1 AND p.id=$2 AND p.status='active' AND NOT u.blocked)`, studentID, parentID).Scan(&allowed)
		if err != nil {
			fail(w, r, err)
			return
		}
		if !allowed {
			fail(w, r, httpx.Invalid("parent_ids", "Выберите связанных активных родителей с доступом к семейному кабинету."))
			return
		}
	}
	p, err := period(from, to)
	if err != nil {
		fail(w, r, err)
		return
	}
	snapshot, err := progress(r.Context(), tx, studentID, p)
	if err != nil {
		fail(w, r, err)
		return
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		fail(w, r, err)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE curator_reports SET snapshot=$2,published_at=clock_timestamp(),updated_at=now() WHERE id=$1`, id, data)
	for _, parentID := range in.ParentIDs {
		if err != nil {
			break
		}
		_, err = tx.Exec(r.Context(), `INSERT INTO curator_report_recipients(report_id,parent_id) VALUES($1,$2)`, id, parentID)
	}
	if err == nil {
		err = database.AuditObject(r.Context(), tx, &a.ID, "curator_report.published", "curator_report", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"id": id, "recipient_count": len(in.ParentIDs), "path": "/#/family-report/" + strconv.FormatInt(id, 10)})
}
func (h *Handler) Feedback(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var in struct {
		Message string `json:"message"`
	}
	if err = httpx.Decode(w, r, &in); err != nil {
		fail(w, r, err)
		return
	}
	in.Message = strings.TrimSpace(in.Message)
	if n := utf8.RuneCountInString(in.Message); n < 1 || n > 3000 {
		fail(w, r, httpx.Invalid("message", "Сообщение: от 1 до 3000 символов."))
		return
	}
	tx, a, err := h.begin(r.Context(), access.CuratorReports, access.Read, true)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	if !has(a, "parent") {
		fail(w, r, httpx.Forbidden())
		return
	}
	var parentID int64
	err = tx.QueryRow(r.Context(), `SELECT p.id FROM curator_reports rep JOIN students s ON s.id=rep.student_id JOIN curator_report_recipients rr ON rr.report_id=rep.id JOIN parents p ON p.id=rr.parent_id JOIN student_parents sp ON sp.student_id=s.id AND sp.parent_id=p.id WHERE rep.id=$1 AND rep.published_at IS NOT NULL AND s.status='active' AND p.status='active' AND p.user_id=$2`, id, a.ID).Scan(&parentID)
	if err != nil {
		fail(w, r, err)
		return
	}
	var feedbackID int64
	err = tx.QueryRow(r.Context(), `INSERT INTO curator_report_feedback(report_id,parent_id,message) VALUES($1,$2,$3) RETURNING id`, id, parentID, in.Message).Scan(&feedbackID)
	if err == nil {
		err = database.AuditObject(r.Context(), tx, &a.ID, "curator_report.parent_feedback", "curator_report", id)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, map[string]int64{"id": feedbackID})
}
