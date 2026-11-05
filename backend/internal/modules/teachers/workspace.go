package teachers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkspaceHandler struct{ Pool *pgxpool.Pool }

// Personal views always use the authenticated teacher's id, including accounts
// that also have administrative roles. No teacher/user id is accepted from clients.
const personalGroups = `(t.user_id=$1 OR EXISTS(SELECT 1 FROM lessons gl JOIN teachers gt ON gt.id=gl.teacher_id WHERE gl.group_id=g.id AND gt.user_id=$1 AND gl.status<>'cancelled'))`
const personalMembers = `WITH participants AS (
 SELECT DISTINCT s.id,s.name,s.name_search,s.status,g.id group_id,g.name group_name
 FROM lessons l JOIN teachers t ON t.id=l.teacher_id JOIN groups g ON g.id=l.group_id
 JOIN students s ON (CASE WHEN l.roster_initialized_at IS NOT NULL
 THEN EXISTS(SELECT 1 FROM attendance a WHERE a.lesson_id=l.id AND a.student_id=s.id AND a.included)
 ELSE EXISTS(SELECT 1 FROM group_enrollments e WHERE e.group_id=l.group_id AND e.student_id=s.id
 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date
 AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)) END)
 WHERE t.user_id=$1 AND l.status<>'cancelled'
) `

func (h *WorkspaceHandler) begin(ctx context.Context) (pgx.Tx, httpx.Actor, error) {
	tx, err := h.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, httpx.Actor{}, err
	}
	actor, err := access.AuthorizeRead(ctx, tx, access.TeacherWorkspace)
	if err != nil {
		tx.Rollback(ctx)
		return nil, actor, err
	}
	return tx, actor, nil
}
func workspaceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		err = httpx.E(404, "not_found", "Группа не найдена или не относится к вашим занятиям.")
	}
	httpx.Fail(w, r, err)
}
func jsonRecords(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]map[string]any, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]any{}
	for rows.Next() {
		var item map[string]any
		if err = rows.Scan(&item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

type personalFilter struct {
	Page, Size     int
	Search, Status string
	GroupID        int64
}

func workspaceFilter(r *http.Request, groups bool) (personalFilter, error) {
	f := personalFilter{Page: 1, Size: 20, Search: strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search"))), Status: r.URL.Query().Get("status")}
	for _, p := range []struct {
		name   string
		target *int
		max    int
	}{{"page", &f.Page, 1000000}, {"page_size", &f.Size, 100}} {
		if value := r.URL.Query().Get(p.name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > p.max {
				return f, httpx.Invalid(p.name, "Некорректный параметр страницы.")
			}
			*p.target = n
		}
	}
	if utf8.RuneCountInString(f.Search) > 200 || strings.ContainsRune(f.Search, 0) {
		return f, httpx.Invalid("search", "Поиск: до 200 символов без нулевых символов.")
	}
	if f.Status != "" && f.Status != "active" && f.Status != "archived" && !(groups && f.Status == "draft") {
		return f, httpx.Invalid("status", "Некорректный статус.")
	}
	if raw := r.URL.Query().Get("group_id"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return f, httpx.Invalid("group_id", "Некорректная группа.")
		}
		f.GroupID = n
	}
	for _, name := range []string{"teacher_id", "user_id"} {
		if r.URL.Query().Has(name) {
			return f, httpx.Invalid(name, "Кабинет показывает данные только текущего педагога.")
		}
	}
	return f, nil
}
func (h *WorkspaceHandler) Summary(w http.ResponseWriter, r *http.Request) {
	if _, err := workspaceFilter(r, false); err != nil {
		workspaceError(w, r, err)
		return
	}
	tx, a, err := h.begin(r.Context())
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var profile map[string]any
	err = tx.QueryRow(r.Context(), `SELECT jsonb_build_object('id',id,'name',name,'specialization',specialization,'status',status) FROM teachers WHERE user_id=$1`, a.ID).Scan(&profile)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	var groups, students, today, pending, drafts int
	err = tx.QueryRow(r.Context(), `SELECT count(*) FROM groups g LEFT JOIN teachers t ON t.id=g.teacher_id WHERE `+personalGroups, a.ID).Scan(&groups)
	if err == nil {
		err = tx.QueryRow(r.Context(), personalMembers+`SELECT count(DISTINCT id) FROM participants`, a.ID).Scan(&students)
	}
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM lessons l JOIN teachers t ON t.id=l.teacher_id WHERE t.user_id=$1 AND l.status<>'cancelled' AND (l.starts_at AT TIME ZONE 'Europe/Moscow')::date=(now() AT TIME ZONE 'Europe/Moscow')::date`, a.ID).Scan(&today)
	}
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM assignments a JOIN lessons l ON l.id=a.lesson_id JOIN teachers t ON t.id=l.teacher_id WHERE t.user_id=$1 AND l.status<>'cancelled' AND a.status='submitted'`, a.ID).Scan(&pending)
	}
	if err == nil {
		err = tx.QueryRow(r.Context(), `SELECT count(*) FROM learning_reports rep JOIN lessons l ON l.id=rep.lesson_id JOIN teachers t ON t.id=l.teacher_id WHERE t.user_id=$1 AND l.status<>'cancelled' AND rep.published_at IS NULL`, a.ID).Scan(&drafts)
	}
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	lessons, err := jsonRecords(r.Context(), tx, `SELECT jsonb_build_object('id',l.id,'topic',l.topic,'group_id',g.id,'group_name',g.name,'starts_at',l.starts_at,'ends_at',l.ends_at,'location',l.location,'program_name',p.name,'module_name',m.name) FROM lessons l JOIN teachers t ON t.id=l.teacher_id JOIN groups g ON g.id=l.group_id JOIN programs p ON p.id=l.program_id LEFT JOIN program_modules m ON m.id=l.module_id WHERE t.user_id=$1 AND l.status='scheduled' AND l.ends_at>now() ORDER BY l.starts_at,l.id LIMIT 5`, a.ID)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"profile": profile, "counts": map[string]int{"groups": groups, "students": students, "lessons_today": today, "pending_reviews": pending, "draft_reports": drafts}, "upcoming_lessons": lessons})
}
func (h *WorkspaceHandler) Groups(w http.ResponseWriter, r *http.Request) {
	f, err := workspaceFilter(r, true)
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	tx, a, err := h.begin(r.Context())
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	from := ` FROM groups g JOIN programs p ON p.id=g.program_id LEFT JOIN teachers t ON t.id=g.teacher_id WHERE ` + personalGroups + ` AND ($2='' OR strpos(g.name_search,$2)>0) AND ($3='' OR g.status=$3)`
	var total int
	if err = tx.QueryRow(r.Context(), `SELECT count(*)`+from, a.ID, f.Search, f.Status).Scan(&total); err != nil {
		workspaceError(w, r, err)
		return
	}
	items, err := jsonRecords(r.Context(), tx, `SELECT jsonb_build_object('id',g.id,'name',g.name,'program_name',p.name,'status',g.status,'primary_teacher',COALESCE(t.user_id=$1,false),'student_count',(SELECT count(DISTINCT e.student_id) FROM group_enrollments e WHERE e.group_id=g.id AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(now() AT TIME ZONE 'Europe/Moscow')::date)))`+from+` ORDER BY g.name_search,g.id LIMIT $4 OFFSET $5`, a.ID, f.Search, f.Status, f.Size, (f.Page-1)*f.Size)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "total": total, "page": f.Page, "page_size": f.Size})
}
func (h *WorkspaceHandler) Students(w http.ResponseWriter, r *http.Request) {
	f, err := workspaceFilter(r, false)
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	tx, a, err := h.begin(r.Context())
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var group map[string]any
	if f.GroupID != 0 {
		err = tx.QueryRow(r.Context(), `SELECT jsonb_build_object('id',g.id,'name',g.name) FROM groups g LEFT JOIN teachers t ON t.id=g.teacher_id WHERE `+personalGroups+` AND g.id=$2`, a.ID, f.GroupID).Scan(&group)
		if err != nil {
			workspaceError(w, r, err)
			return
		}
	}
	where := ` WHERE ($2::bigint=0 OR group_id=$2) AND ($3='' OR strpos(name_search,$3)>0) AND ($4='' OR status=$4)`
	var total int
	if err = tx.QueryRow(r.Context(), personalMembers+`SELECT count(DISTINCT id) FROM participants`+where, a.ID, f.GroupID, f.Search, f.Status).Scan(&total); err != nil {
		workspaceError(w, r, err)
		return
	}
	items, err := jsonRecords(r.Context(), tx, personalMembers+`SELECT jsonb_build_object('id',id,'name',name,'status',status,'groups',jsonb_agg(jsonb_build_object('id',group_id,'name',group_name) ORDER BY group_name,group_id)) FROM participants`+where+` GROUP BY id,name,name_search,status ORDER BY name_search,id LIMIT $5 OFFSET $6`, a.ID, f.GroupID, f.Search, f.Status, f.Size, (f.Page-1)*f.Size)
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		workspaceError(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "total": total, "page": f.Page, "page_size": f.Size, "group": group})
}
