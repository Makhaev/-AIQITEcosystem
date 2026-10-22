package audit

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"aiq.local/core/internal/platform/httpx"
)

type Handler struct{ Service *Service }

func filterFrom(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{Page: 1, Size: 20, Action: q.Get("action"), ObjectType: q.Get("object_type"), RequestID: q.Get("request_id")}
	for _, v := range []struct {
		key    string
		target *int
		max    int
	}{{"page", &f.Page, 1000000}, {"page_size", &f.Size, 100}} {
		if value := q.Get(v.key); value != "" {
			n, e := strconv.Atoi(value)
			if e != nil || n < 1 || n > v.max {
				return f, httpx.Invalid(v.key, "Некорректная страница.")
			}
			*v.target = n
		}
	}
	for _, v := range []struct {
		key    string
		target *int64
	}{{"actor_id", &f.ActorID}, {"object_id", &f.ObjectID}} {
		if value := q.Get(v.key); value != "" {
			n, e := strconv.ParseInt(value, 10, 64)
			if e != nil || n <= 0 {
				return f, httpx.Invalid(v.key, "Укажите положительный ID.")
			}
			*v.target = n
		}
	}
	for _, v := range []struct{ key, value string }{{"action", f.Action}, {"object_type", f.ObjectType}, {"request_id", f.RequestID}} {
		if utf8.RuneCountInString(v.value) > 128 || strings.ContainsRune(v.value, 0) {
			return f, httpx.Invalid(v.key, "Не более 128 символов.")
		}
	}
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return f, err
	}
	for _, v := range []struct {
		key    string
		target **time.Time
	}{{"date_from", &f.From}, {"date_to", &f.To}} {
		if value := q.Get(v.key); value != "" {
			date, e := time.ParseInLocation("2006-01-02", value, zone)
			if e != nil || date.Year() < 1 {
				return f, httpx.Invalid(v.key, "Укажите дату в формате YYYY-MM-DD.")
			}
			*v.target = &date
		}
	}
	if f.From != nil && f.To != nil && f.To.Before(*f.From) {
		return f, httpx.Invalid("date_to", "Начало периода не может быть позже окончания.")
	}
	if f.To != nil {
		end := f.To.AddDate(0, 0, 1)
		f.To = &end
	}
	return f, nil
}

func reply(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f, err := filterFrom(r)
	if err != nil {
		reply(w, r, nil, err)
		return
	}
	v, err := h.Service.List(r.Context(), f)
	reply(w, r, v, err)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		reply(w, r, nil, httpx.Invalid("id", "Некорректный ID записи журнала."))
		return
	}
	v, err := h.Service.Get(r.Context(), id)
	reply(w, r, v, err)
}

func (h *Handler) ReadOnly(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "GET, HEAD")
	httpx.Fail(w, r, httpx.E(http.StatusMethodNotAllowed, "audit_read_only", "Журнал доступен только для чтения."))
}
