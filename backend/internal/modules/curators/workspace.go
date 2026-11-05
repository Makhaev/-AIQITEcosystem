package curators

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Handler struct{ Pool *pgxpool.Pool }

func has(a httpx.Actor, role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}
func fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		err = httpx.E(404, "not_found", "Запись не найдена или недоступна.")
	}
	httpx.Fail(w, r, err)
}
func (h *Handler) begin(ctx context.Context, resource access.Resource, action access.Action, write bool) (pgx.Tx, httpx.Actor, error) {
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if write {
		options = pgx.TxOptions{}
	}
	tx, err := h.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, httpx.Actor{}, err
	}
	var a httpx.Actor
	if write {
		err = access.LockManagement(ctx, tx)
		if err == nil {
			a, err = access.Authorize(ctx, tx, resource, action)
		}
	} else {
		a, err = access.AuthorizeRead(ctx, tx, resource)
	}
	if err != nil {
		tx.Rollback(ctx)
		return nil, a, err
	}
	return tx, a, nil
}
func records(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var value map[string]any
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		items = append(items, value)
	}
	return items, rows.Err()
}
func idFrom(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.Invalid("id", "Некорректный номер.")
	}
	return id, nil
}
func pagination(r *http.Request) (int, int, error) {
	page, size := 1, 20
	for _, v := range []struct {
		key   string
		value *int
		max   int
	}{{"page", &page, 1000000}, {"page_size", &size, 100}} {
		if raw := r.URL.Query().Get(v.key); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > v.max {
				return 0, 0, httpx.Invalid(v.key, "Некорректная страница.")
			}
			*v.value = n
		}
	}
	return page, size, nil
}
func personal(ctx context.Context, tx pgx.Tx, a httpx.Actor, id int64, lock bool) (map[string]any, error) {
	query := `SELECT jsonb_build_object('id',s.id,'name',s.name,'birth_date',s.birth_date,'status',s.status) FROM students s WHERE s.id=$1 AND curator_has_student($2,s.id)`
	if lock {
		query += ` FOR UPDATE`
	}
	var value map[string]any
	err := tx.QueryRow(ctx, query, id, a.ID).Scan(&value)
	return value, err
}
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	tx, a, err := h.begin(r.Context(), access.CuratorWorkspace, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var groups int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM groups WHERE curator_id=$1`, a.ID).Scan(&groups); err != nil {
		fail(w, r, err)
		return
	}
	var students, parents, missing, drafts, feedback int
	err = tx.QueryRow(r.Context(), `SELECT count(*),count(*) FILTER(WHERE NOT EXISTS(SELECT 1 FROM student_parents sp JOIN parents p ON p.id=sp.parent_id WHERE sp.student_id=s.id AND p.status='active')) FROM students s WHERE curator_has_student($1,s.id) AND s.status='active'`, a.ID).Scan(&students, &missing)
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT count(DISTINCT p.id) FROM students s JOIN student_parents sp ON sp.student_id=s.id JOIN parents p ON p.id=sp.parent_id WHERE curator_has_student($1,s.id) AND s.status='active' AND p.status='active'`, a.ID).Scan(&parents)
	}
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM curator_reports rep JOIN students s ON s.id=rep.student_id WHERE curator_has_student($1,s.id) AND rep.published_at IS NULL`, a.ID).Scan(&drafts)
	}
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM curator_report_feedback f JOIN curator_reports rep ON rep.id=f.report_id JOIN students s ON s.id=rep.student_id WHERE curator_has_student($1,s.id)`, a.ID).Scan(&feedback)
	}
	messages := []map[string]any{}
	if err == nil {
		messages, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',f.id,'report_id',rep.id,'student_name',s.name,'parent_name',p.name,'message',f.message,'created_at',f.created_at) FROM curator_report_feedback f JOIN curator_reports rep ON rep.id=f.report_id JOIN students s ON s.id=rep.student_id JOIN parents p ON p.id=f.parent_id WHERE curator_has_student($1,s.id) ORDER BY f.id DESC LIMIT 10`, a.ID)
	}
	attention := []map[string]any{}
	var attentionCount int
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM students s WHERE curator_has_student($1,s.id) AND s.status='active' AND `+attentionCondition, a.ID).Scan(&attentionCount)
	}
	if err == nil {
		attention, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',s.id,'name',s.name,'attention',`+attentionJSON+`) FROM students s WHERE curator_has_student($1,s.id) AND s.status='active' AND `+attentionCondition+` ORDER BY s.name_search,s.id LIMIT 10`, a.ID)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"counts": map[string]int{"groups": groups, "students": students, "parents": parents, "missing_family": missing, "draft_reports": drafts, "parent_messages": feedback, "attention": attentionCount}, "messages": messages, "attention": attention})
}
func (h *Handler) Students(w http.ResponseWriter, r *http.Request) {
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
	attention := r.URL.Query().Get("attention")
	if attention != "" && attention != "true" && attention != "false" {
		fail(w, r, httpx.Invalid("attention", "Некорректный фильтр внимания."))
		return
	}
	var groupID int64
	if raw := r.URL.Query().Get("group_id"); raw != "" {
		groupID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || groupID <= 0 {
			fail(w, r, httpx.Invalid("group_id", "Некорректная группа."))
			return
		}
	}
	tx, a, err := h.begin(r.Context(), access.CuratorWorkspace, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var group map[string]any
	if groupID != 0 {
		if err = tx.QueryRow(r.Context(), `SELECT jsonb_build_object('id',id,'name',name) FROM groups WHERE id=$1 AND curator_id=$2`, groupID, a.ID).Scan(&group); err != nil {
			fail(w, r, err)
			return
		}
	}
	where := ` WHERE curator_has_student($1,s.id) AND ($2='' OR strpos(s.name_search,$2)>0) AND ($3='' OR s.status=$3) AND ($4::bigint=0 OR EXISTS(SELECT 1 FROM group_enrollments e WHERE e.student_id=s.id AND e.group_id=$4 AND EXISTS(SELECT 1 FROM groups selected WHERE selected.id=e.group_id AND selected.status='active') AND e.ends_on IS NULL AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date))`
	if attention == "true" {
		where += " AND s.status='active' AND " + attentionCondition
	}

	var total int
	err = tx.QueryRow(r.Context(), `SELECT count(*) FROM students s`+where, a.ID, search, status, groupID).Scan(&total)
	items := []map[string]any{}
	if err == nil {
		items, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',s.id,'name',s.name,'status',s.status,'birth_date',s.birth_date,'attention',`+attentionJSON+`,'groups',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',g.id,'name',g.name) ORDER BY g.name,g.id) FROM group_enrollments e JOIN groups g ON g.id=e.group_id WHERE e.student_id=s.id AND g.curator_id=$1 AND g.status='active' AND e.ends_on IS NULL AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date),'[]'::jsonb),'parents',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',p.id,'name',p.name,'phone',p.phone,'email',p.email,'status',p.status,'relationship',sp.relationship,'is_primary',sp.is_primary) ORDER BY sp.is_primary DESC,p.name,p.id) FROM student_parents sp JOIN parents p ON p.id=sp.parent_id WHERE sp.student_id=s.id),'[]'::jsonb)) FROM students s`+where+` ORDER BY s.name_search,s.id LIMIT $5 OFFSET $6`, a.ID, search, status, groupID, size, (page-1)*size)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "group": group})
}
func (h *Handler) Student(w http.ResponseWriter, r *http.Request)          { h.student(w, r, false) }
func (h *Handler) MethodistStudent(w http.ResponseWriter, r *http.Request) { h.student(w, r, true) }
func (h *Handler) student(w http.ResponseWriter, r *http.Request, methodist bool) {
	id, err := idFrom(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	p, err := requestPeriod(r)
	if err != nil {
		fail(w, r, err)
		return
	}
	resource := access.CuratorWorkspace
	if methodist {
		resource = access.MethodistWorkspace
	}
	tx, a, err := h.begin(r.Context(), resource, access.Read, false)
	if err != nil {
		fail(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var student map[string]any
	if methodist {
		err = tx.QueryRow(r.Context(), `SELECT jsonb_build_object('id',s.id,'name',s.name,'status',s.status) FROM students s WHERE s.id=$1`, id).Scan(&student)
	} else {
		student, err = personal(r.Context(), tx, a, id, false)
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	family := []map[string]any{}
	if !methodist {
		family, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',p.id,'name',p.name,'phone',p.phone,'email',p.email,'status',p.status,'relationship',sp.relationship,'is_primary',sp.is_primary,'has_account',p.user_id IS NOT NULL) FROM student_parents sp JOIN parents p ON p.id=sp.parent_id WHERE sp.student_id=$1 ORDER BY sp.is_primary DESC,p.name,p.id`, id)
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	stats, err := progress(r.Context(), tx, id, p)
	if err != nil {
		fail(w, r, err)
		return
	}
	groups, err := records(r.Context(), tx, `SELECT jsonb_build_object('id',g.id,'name',g.name,'program_name',pr.name,'program_id',pr.id,'teacher_name',t.name,'starts_on',e.starts_on,'ends_on',e.ends_on) FROM group_enrollments e JOIN groups g ON g.id=e.group_id JOIN programs pr ON pr.id=g.program_id LEFT JOIN teachers t ON t.id=g.teacher_id WHERE e.student_id=$1 ORDER BY e.starts_on DESC,e.id DESC`, id)
	if err != nil {
		fail(w, r, err)
		return
	}
	assignments, err := records(r.Context(), tx, `SELECT jsonb_build_object('id',a.id,'title',a.title,'status',a.status,'answer',a.answer,'feedback',a.feedback,'due_at',a.due_at,'lesson_topic',l.topic,'teacher_name',t.name) FROM assignments a JOIN lessons l ON l.id=a.lesson_id JOIN teachers t ON t.id=l.teacher_id WHERE a.student_id=$1 AND l.status<>'cancelled' AND l.starts_at>=$2 AND l.starts_at<$3 ORDER BY a.created_at DESC,a.id DESC LIMIT 100`, id, p.start, p.end)
	if err != nil {
		fail(w, r, err)
		return
	}
	teacherReports, err := records(r.Context(), tx, `SELECT jsonb_build_object('id',rep.id,'summary',rep.summary,'published_at',rep.published_at,'lesson_topic',l.topic,'teacher_name',t.name) FROM learning_reports rep JOIN lessons l ON l.id=rep.lesson_id JOIN teachers t ON t.id=l.teacher_id WHERE rep.student_id=$1 AND l.starts_at>=$2 AND l.starts_at<$3 ORDER BY rep.created_at DESC,rep.id DESC LIMIT 100`, id, p.start, p.end)
	schedule := []map[string]any{}
	attendance := []map[string]any{}
	if err == nil {
		schedule, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',l.id,'topic',l.topic,'group_name',g.name,'teacher_name',t.name,'starts_at',l.starts_at,'ends_at',l.ends_at,'location',l.location) FROM lessons l JOIN groups g ON g.id=l.group_id JOIN teachers t ON t.id=l.teacher_id WHERE l.status='scheduled' AND l.starts_at>=now() AND l.starts_at<now()+interval '30 days' AND EXISTS(SELECT 1 FROM group_enrollments e WHERE e.student_id=$1 AND e.group_id=l.group_id AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)) ORDER BY l.starts_at,l.id LIMIT 100`, id)
	}
	if err == nil {
		attendance, err = records(r.Context(), tx, `SELECT jsonb_build_object('id',att.id,'lesson_topic',l.topic,'starts_at',l.starts_at,'group_name',g.name,'status',att.status,'comment',att.comment) FROM attendance att JOIN lessons l ON l.id=att.lesson_id JOIN groups g ON g.id=l.group_id WHERE att.student_id=$1 AND att.included AND l.status<>'cancelled' AND l.starts_at>=$2 AND l.starts_at<$3 ORDER BY l.starts_at DESC,l.id DESC LIMIT 100`, id, p.start, p.end)
	}

	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"student": student, "parents": family, "groups": groups, "progress": stats, "assignments": assignments, "teacher_reports": teacherReports, "schedule": schedule, "attendance": attendance})
}
