package families

import (
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/spreadsheet"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type registerStudent struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	BirthDate    string `json:"birth_date"`
	RegisteredOn string `json:"registered_on"`
	Groups       string `json:"groups"`
	Curators     string `json:"curators"`
}

func (h *Handler) MonthlyRegister(w http.ResponseWriter, r *http.Request) {
	location := time.FixedZone("Europe/Moscow", 3*60*60)
	now := time.Now().In(location)
	raw := r.URL.Query().Get("month")
	if raw == "" {
		raw = now.Format("2006-01")
	}
	month, err := time.ParseInLocation("2006-01", raw, location)
	if err != nil || raw != month.Format("2006-01") || month.Year() < 1900 || month.After(now) {
		respond(w, r, nil, httpx.Invalid("month", "Выберите месяц не позднее текущего."), 200)
		return
	}
	page := 1
	if text := r.URL.Query().Get("page"); text != "" {
		page, err = strconv.Atoi(text)
		if err != nil || page < 1 || page > 1000000 {
			respond(w, r, nil, httpx.Invalid("page", "Некорректная страница."), 200)
			return
		}
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "xlsx" {
		respond(w, r, nil, httpx.Invalid("format", "Допустимый формат: xlsx."), 200)
		return
	}
	cutoff := month.AddDate(0, 1, 0)
	asOf := cutoff.AddDate(0, 0, -1).Format("2006-01-02")
	if now.Before(cutoff) {
		cutoff = now
		asOf = now.Format("2006-01-02")
	}
	tx, _, err := h.Repo.begin(r.Context(), access.StudentRegister, false)
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	defer tx.Rollback(r.Context())
	where := ` FROM students s WHERE s.created_at<$1 AND (SELECT e.status FROM student_status_events e WHERE e.student_id=s.id AND e.changed_at<$1 ORDER BY e.changed_at DESC,e.id DESC LIMIT 1)='active'`
	var total int
	if err = tx.QueryRow(r.Context(), `SELECT count(*)`+where, cutoff).Scan(&total); err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	query := `SELECT s.id,s.name,COALESCE(s.birth_date::text,''),(s.created_at AT TIME ZONE 'Europe/Moscow')::date::text,
 COALESCE((SELECT string_agg(DISTINCT g.name,', ' ORDER BY g.name) FROM group_enrollments e JOIN groups g ON g.id=e.group_id WHERE e.student_id=s.id AND e.starts_on<=$2::date AND (e.ends_on IS NULL OR e.ends_on>=$2::date)),''),
 COALESCE((SELECT string_agg(DISTINCT u.name,', ' ORDER BY u.name) FROM group_enrollments e JOIN groups g ON g.id=e.group_id JOIN users u ON u.id=g.curator_id WHERE e.student_id=s.id AND e.starts_on<=$2::date AND (e.ends_on IS NULL OR e.ends_on>=$2::date)),'')` + where + ` ORDER BY s.name_search,s.id`
	args := []any{cutoff, asOf}
	if format != "xlsx" {
		query += ` LIMIT 20 OFFSET $3`
		args = append(args, (page-1)*20)
	}
	rows, err := tx.Query(r.Context(), query, args...)
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	items := []registerStudent{}
	for rows.Next() {
		var item registerStudent
		if err = rows.Scan(&item.ID, &item.Name, &item.BirthDate, &item.RegisteredOn, &item.Groups, &item.Curators); err != nil {
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
		respond(w, r, nil, err, 200)
		return
	}
	if format == "xlsx" {
		values := make([][]string, 0, len(items))
		for i, item := range items {
			values = append(values, []string{strconv.Itoa(i + 1), item.Name, item.BirthDate, item.RegisteredOn, item.Groups, item.Curators, raw})
		}
		contents, e := spreadsheet.XLSX([]string{"№", "ФИО ученика", "Дата рождения", "Дата добавления", "Группы", "Кураторы", "Месяц"}, values)
		if e != nil {
			respond(w, r, nil, e, 200)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="students-%s.xlsx"`, raw))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		_, _ = w.Write(contents)
		return
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "total": total, "month": raw, "as_of": asOf, "page": page, "page_size": 20})
}
