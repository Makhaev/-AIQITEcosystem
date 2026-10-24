package learning

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
	"time"
)

type Handler struct{ Service *Service }

func respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, v)
}
func id(r *http.Request) (int64, error) {
	n, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || n < 1 {
		return 0, httpx.Invalid("id", "Некорректный идентификатор.")
	}
	return n, nil
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	n, e := id(r)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	var in Input
	if e = httpx.Decode(w, r, &in); e != nil {
		respond(w, r, nil, e)
		return
	}
	v, e := h.Service.Create(r.Context(), n, in)
	respond(w, r, v, e)
}
func (h *Handler) Card(w http.ResponseWriter, r *http.Request) {
	n, e := id(r)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	if r.Method == "PUT" {
		var in Input
		if e = httpx.Decode(w, r, &in); e != nil {
			respond(w, r, nil, e)
			return
		}
		v, e := h.Service.Correct(r.Context(), n, in)
		respond(w, r, v, e)
		return
	}
	rev := 0
	if raw := r.URL.Query().Get("revision"); raw != "" {
		rev, e = strconv.Atoi(raw)
		if e != nil || rev < 1 {
			respond(w, r, nil, httpx.Invalid("revision", "Некорректная редакция."))
			return
		}
	}
	v, e := h.Service.Get(r.Context(), n, rev)
	respond(w, r, v, e)
}
func (h *Handler) Action(w http.ResponseWriter, r *http.Request) {
	n, e := id(r)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	var in ActionInput
	if e = httpx.Decode(w, r, &in); e != nil {
		respond(w, r, nil, e)
		return
	}
	v, e := h.Service.Action(r.Context(), n, in)
	respond(w, r, v, e)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	n, e := id(r)
	if e != nil {
		respond(w, r, nil, e)
		return
	}
	q := r.URL.Query()
	zone := time.FixedZone("Europe/Moscow", 10800)
	now := time.Now().In(zone)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone)
	end := start.AddDate(0, 1, 0)
	if q.Has("education_from") || q.Has("education_to") {
		var e1, e2 error
		start, e1 = time.ParseInLocation("2006-01-02", q.Get("education_from"), zone)
		end, e2 = time.ParseInLocation("2006-01-02", q.Get("education_to"), zone)
		if e1 != nil || e2 != nil || start.Year() < 1 || end.Year() < 1 || end.Before(start) || len(q["education_from"]) != 1 || len(q["education_to"]) != 1 {
			respond(w, r, nil, httpx.Invalid("education_period", "Укажите обе даты в правильном порядке."))
			return
		}
		end = end.AddDate(0, 0, 1)
	}
	page := 1
	if q.Has("page") {
		page, e = strconv.Atoi(q.Get("page"))
		if e != nil || page < 1 || page > 1000000 {
			respond(w, r, nil, httpx.Invalid("page", "Некорректная страница."))
			return
		}
	}
	if q.Has("current") && q.Get("current") != "true" && q.Get("current") != "false" {
		respond(w, r, nil, httpx.Invalid("current", "Ожидается true или false."))
		return
	}
	if q.Has("view") && q.Get("view") != "work" && q.Get("view") != "published" {
		respond(w, r, nil, httpx.Invalid("view", "Допустимы published и work."))
		return
	}
	v, e := h.Service.List(r.Context(), n, Filter{Kind: r.PathValue("kind"), Start: start, End: end, Page: page, Current: q.Get("current") == "true", Work: q.Get("view") == "work", Status: q.Get("status"), ModelVersion: q.Get("model_version"), SkillKey: q.Get("skill_key")})
	respond(w, r, v, e)
}
