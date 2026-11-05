package curators

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
)

func (h *Handler) MethodistSummary(w http.ResponseWriter, r *http.Request) {
	tx, _, err := h.begin(r.Context(), access.MethodistWorkspace, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var students, programs, teacherReports, curatorReports int
	err = tx.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM students WHERE status='active'),(SELECT count(*) FROM programs WHERE status='active'),(SELECT count(*) FROM learning_reports),(SELECT count(*) FROM curator_reports)`).Scan(&students, &programs, &teacherReports, &curatorReports)
	recent := []map[string]any{}
	if err == nil {
		recent, err = records(r.Context(), tx, `SELECT jsonb_build_object('student_id',student_id,'student_name',s.name,'kind',kind,'summary',summary,'created_at',rep.created_at) FROM (SELECT student_id,'teacher' kind,summary,created_at,id FROM learning_reports UNION ALL SELECT student_id,'curator',summary,created_at,id FROM curator_reports) rep JOIN students s ON s.id=rep.student_id ORDER BY rep.created_at DESC,rep.kind,rep.id DESC LIMIT 10`)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"counts": map[string]int{"students": students, "programs": programs, "teacher_reports": teacherReports, "curator_reports": curatorReports}, "recent_reports": recent})
}

func (h *Handler) MethodistStudents(w http.ResponseWriter, r *http.Request) {
	page, size, err := pagination(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	status := r.URL.Query().Get("status")
	if utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) || status != "" && status != "active" && status != "archived" {
		fail(w, r, httpx.Invalid("search", "Проверьте поиск и статус."))
		return
	}
	tx, _, err := h.begin(r.Context(), access.MethodistWorkspace, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	where := ` FROM students s WHERE ($1='' OR strpos(s.name_search,$1)>0) AND ($2='' OR s.status=$2)`
	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, search, status).Scan(&total)
	items := []map[string]any{}
	if err == nil {
		items, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',s.id,'name',s.name,'status',s.status,'groups',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',g.id,'name',g.name,'program_id',p.id,'program_name',p.name) ORDER BY g.name,g.id) FROM group_enrollments e JOIN groups g ON g.id=e.group_id JOIN programs p ON p.id=g.program_id WHERE e.student_id=s.id AND g.status='active' AND e.ends_on IS NULL AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date),'[]'::jsonb))`+where+` ORDER BY s.name_search,s.id LIMIT $3 OFFSET $4`, search, status, size, (page-1)*size)
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

// MethodistPrograms connects educational content with the teachers who use it.
func (h *Handler) MethodistPrograms(w http.ResponseWriter, r *http.Request) {
	page, size, err := pagination(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	if utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) {
		fail(w, r, httpx.Invalid("search", "Введите до 200 символов."))
		return
	}
	tx, _, err := h.begin(r.Context(), access.MethodistWorkspace, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	where := ` FROM groups g JOIN programs p ON p.id=g.program_id LEFT JOIN teachers t ON t.id=g.teacher_id WHERE g.status='active' AND ($1='' OR strpos(lower(p.name || ' ' || g.name || ' ' || COALESCE(t.name,'')),$1)>0)`
	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, search).Scan(&total)
	items := []map[string]any{}
	if err == nil {
		items, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',g.id,'group_name',g.name,'program_id',p.id,'program_name',p.name,'teacher_name',t.name,'lesson_count',(SELECT count(*) FROM lessons l WHERE l.group_id=g.id AND l.status<>'cancelled'),'completed_lessons',(SELECT count(*) FROM lessons l WHERE l.group_id=g.id AND l.status='completed'),'module_count',(SELECT count(*) FROM program_modules m WHERE m.program_id=p.id),'student_count',(SELECT count(DISTINCT e.student_id) FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.group_id=g.id AND s.status='active' AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(now() AT TIME ZONE 'Europe/Moscow')::date)))`+where+` ORDER BY p.name,g.name,g.id LIMIT $2 OFFSET $3`, search, size, (page-1)*size)
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

func (h *Handler) MethodistTeacherReports(w http.ResponseWriter, r *http.Request) {
	h.methodistReports(w, r, false)
}
func (h *Handler) MethodistCuratorReports(w http.ResponseWriter, r *http.Request) {
	h.methodistReports(w, r, true)
}
func (h *Handler) methodistReports(w http.ResponseWriter, r *http.Request, curator bool) {
	page, size, err := pagination(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	var studentID int64
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
	publication := r.URL.Query().Get("publication")
	if utf8.RuneCountInString(search) > 200 || strings.ContainsRune(search, 0) || publication != "" && publication != "draft" && publication != "published" {
		fail(w, r, httpx.Invalid("search", "Проверьте поиск и статус публикации."))
		return
	}
	if raw := r.URL.Query().Get("student_id"); raw != "" {
		studentID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || studentID <= 0 {
			fail(w, r, httpx.Invalid("student_id", "Некорректный ученик."))
			return
		}
	}
	var from, to any
	if r.URL.Query().Get("from") != "" || r.URL.Query().Get("to") != "" {
		p, e := requestPeriod(r)
		if e != nil {
			fail(w, r, e)
			return
		}
		from, to = p.From, p.To
	}
	tx, _, err := h.begin(r.Context(), access.MethodistWorkspace, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	projection := `jsonb_build_object('id',rep.id,'student_id',s.id,'student_name',s.name,'author_name',t.name,'summary',rep.summary,'published_at',rep.published_at,'created_at',rep.created_at,'lesson_topic',l.topic,'lesson_date',(l.starts_at AT TIME ZONE 'Europe/Moscow')::date,'program_name',pr.name,'program_id',pr.id)`
	where := ` FROM learning_reports rep JOIN students s ON s.id=rep.student_id JOIN lessons l ON l.id=rep.lesson_id JOIN teachers t ON t.id=l.teacher_id JOIN groups g ON g.id=l.group_id JOIN programs pr ON pr.id=g.program_id WHERE ($1::bigint=0 OR s.id=$1) AND ($2::date IS NULL OR (l.starts_at AT TIME ZONE 'Europe/Moscow')::date>=$2::date) AND ($3::date IS NULL OR (l.starts_at AT TIME ZONE 'Europe/Moscow')::date<=$3::date)`
	if curator {
		projection = `jsonb_build_object('id',rep.id,'student_id',s.id,'student_name',s.name,'author_name',u.name,'summary',rep.summary,'recommendations',rep.recommendations,'goals',rep.goals,'period_from',rep.period_from,'period_to',rep.period_to,'published_at',rep.published_at,'created_at',rep.created_at,'snapshot',rep.snapshot)`
		where = ` FROM curator_reports rep JOIN students s ON s.id=rep.student_id JOIN users u ON u.id=rep.created_by WHERE ($1::bigint=0 OR s.id=$1) AND ($2::date IS NULL OR rep.period_to>=$2::date) AND ($3::date IS NULL OR rep.period_from<=$3::date)`
	}
	// Reuse the same typed parameter positions for counts and rows.
	author := "t.name"
	if curator {
		author = "u.name"
	}
	where += ` AND ($4='' OR strpos(lower(s.name || ' ' || ` + author + `),$4)>0) AND ($5='' OR ($5='draft' AND rep.published_at IS NULL) OR ($5='published' AND rep.published_at IS NOT NULL))`
	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, studentID, from, to, search, publication).Scan(&total)
	items := []map[string]any{}
	if err == nil {
		items, err = records(r.Context(), tx, `SELECT `+projection+where+` ORDER BY rep.created_at DESC,rep.id DESC LIMIT $6 OFFSET $7`, studentID, from, to, search, publication, size, (page-1)*size)
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
