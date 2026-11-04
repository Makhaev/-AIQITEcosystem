package dashboard

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"math"
	"net/http"
	"strconv"
	"time"
)

// Snapshot: current capacity and task state. Period: completed lesson marks and
// task closures in [Moscow start, day after inclusive end).
func (r *Repository) Operations(ctx context.Context, start, end time.Time, branch int64, threshold float64) (map[string]any, error) {
	tx, _, e := access.Begin(ctx, r.Pool, access.Dashboard, access.Read, false)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if branch != 0 {
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM branches WHERE id=$1)`, branch).Scan(&exists); e != nil {
			return nil, e
		}
		if !exists {
			return nil, httpx.E(404, "not_found", "Филиал не найден.")
		}
	}
	// The same branch scope applies to groups and task links. Unlinked tasks are
	// included in the organisation view only, never attributed to an arbitrary branch.
	taskScope := `($3::bigint=0 OR EXISTS(SELECT 1 FROM groups g WHERE g.branch_id=$3 AND (k.group_id=g.id OR EXISTS(SELECT 1 FROM lessons l WHERE l.id=k.lesson_id AND l.group_id=g.id) OR EXISTS(SELECT 1 FROM group_enrollments e WHERE e.group_id=g.id AND e.student_id=k.student_id AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(now() AT TIME ZONE 'Europe/Moscow')::date)))))`
	var working, closed int
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE k.status='in_progress'),count(*) FILTER(WHERE k.status='done' AND k.closed_at>=$1 AND k.closed_at<$2) FROM tasks k WHERE `+taskScope, start, end, branch).Scan(&working, &closed)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT jsonb_build_object('id',g.id,'name',g.name,'branch_id',g.branch_id,'branch_name',b.name,'capacity',g.capacity,
 'students',(SELECT count(DISTINCT e.student_id) FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.group_id=g.id AND s.status='active' AND e.starts_on<=(now() AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(now() AT TIME ZONE 'Europe/Moscow')::date)),
 'attendance_percent',(SELECT round(100.0*count(*) FILTER(WHERE a.status IN ('present','late'))/NULLIF(count(*) FILTER(WHERE a.status IN ('present','late','absent','excused')),0),2) FROM attendance_records a JOIN lessons l ON l.id=a.lesson_id WHERE l.group_id=g.id AND l.status='completed' AND a.included AND l.starts_at>=$1 AND l.starts_at<$2))
 FROM groups g LEFT JOIN branches b ON b.id=g.branch_id WHERE g.status='active' AND ($3::bigint=0 OR g.branch_id=$3) ORDER BY b.name NULLS LAST,g.name,g.id`, start, end, branch)
	if e != nil {
		return nil, e
	}
	groups := []map[string]any{}
	capacity, enrolled, low := 0, 0, 0
	for rows.Next() {
		var item map[string]any
		if e = rows.Scan(&item); e != nil {
			rows.Close()
			return nil, e
		}
		c := int(item["capacity"].(float64))
		n := int(item["students"].(float64))
		capacity += c
		enrolled += n
		item["occupancy_percent"] = math.Round(float64(n)*10000/float64(c)) / 100
		item["low_attendance"] = false
		if p, ok := item["attendance_percent"].(float64); ok && p < threshold {
			item["low_attendance"] = true
			low++
		}
		groups = append(groups, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	var occupancy *float64
	if capacity > 0 {
		v := math.Round(float64(enrolled)*10000/float64(capacity)) / 100
		occupancy = &v
	}
	return map[string]any{"groups": groups, "branch_id": branch, "low_attendance_threshold": threshold, "low_attendance_groups": low, "students": enrolled, "capacity": capacity, "occupancy_percent": occupancy, "tasks_in_progress": working, "tasks_closed": closed, "date_from": start.Format("2006-01-02"), "date_to": end.AddDate(0, 0, -1).Format("2006-01-02")}, tx.Commit(ctx)
}
func (h *Handler) Operations(w http.ResponseWriter, r *http.Request) {
	start, end, e := periodFrom(r.URL.Query(), time.Now())
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	branch := int64(0)
	if raw := r.URL.Query().Get("branch_id"); raw != "" {
		branch, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || branch <= 0 {
			httpx.Fail(w, r, httpx.Invalid("branch_id", "Некорректный филиал."))
			return
		}
	}
	threshold := 70.0
	if raw := r.URL.Query().Get("threshold"); raw != "" {
		threshold, e = strconv.ParseFloat(raw, 64)
		if e != nil || math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 100 {
			httpx.Fail(w, r, httpx.Invalid("threshold", "Порог: от 0 до 100%."))
			return
		}
	}
	v, e := h.Service.Repo.Operations(r.Context(), start, end, branch, threshold)
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	httpx.JSON(w, 200, v)
}
