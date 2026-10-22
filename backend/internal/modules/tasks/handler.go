package tasks

import (
	"aiq.local/core/internal/platform/httpx"

	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Handler struct{ Service *Service }

func reply(w http.ResponseWriter, r *http.Request, v any, err error, status int) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, v)
}
func idFrom(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.Invalid("id", "Некорректный идентификатор задачи.")
	}
	return id, nil
}
func filterFrom(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{Page: 1, Size: 20, Search: strings.ToLower(strings.TrimSpace(q.Get("search"))), Status: q.Get("status")}
	for _, v := range []struct {
		key    string
		target *int
		max    int
	}{{"page", &f.Page, 1000000}, {"page_size", &f.Size, 100}} {
		if q.Get(v.key) != "" {
			n, e := strconv.Atoi(q.Get(v.key))
			if e != nil || n < 1 || n > v.max {
				return f, httpx.Invalid(v.key, "Некорректная страница.")
			}
			*v.target = n
		}
	}
	if q.Get("assignee_id") != "" {
		n, e := strconv.ParseInt(q.Get("assignee_id"), 10, 64)
		if e != nil || n <= 0 {
			return f, httpx.Invalid("assignee_id", "Некорректный ответственный.")
		}
		f.AssigneeID = n
	}
	if value := q.Get("overdue"); value != "" {
		if value != "true" && value != "false" {
			return f, httpx.Invalid("overdue", "Ожидается true или false.")
		}
		b := value == "true"
		f.Overdue = &b
	}
	if f.Status != "" && !validStatus(f.Status) {
		return f, httpx.Invalid("status", "Недопустимый статус.")
	}
	if utf8.RuneCountInString(f.Search) > 200 || strings.ContainsRune(f.Search, 0) {
		return f, httpx.Invalid("search", "Поиск: не более 200 символов.")
	}
	return f, nil
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f, e := filterFrom(r)
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.List(r.Context(), f)
	reply(w, r, v, e, 200)
}
func (h *Handler) Assignees(w http.ResponseWriter, r *http.Request) {
	f, e := filterFrom(r)
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Assignees(r.Context(), f)
	reply(w, r, v, e, 200)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r)
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Get(r.Context(), id)
	reply(w, r, v, e, 200)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if e := httpx.DecodeLimit(w, r, &in, 64<<10); e != nil {
		reply(w, r, nil, e, 201)
		return
	}
	v, e := h.Service.Save(r.Context(), 0, in, nil)
	reply(w, r, v, e, 201)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r)
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var in Update
	if e = httpx.DecodeLimit(w, r, &in, 64<<10); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Save(r.Context(), id, Input{}, &in)
	reply(w, r, v, e, 200)
}
func (h *Handler) Status(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r)
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if e = httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Status(r.Context(), id, in.Status)
	reply(w, r, v, e, 200)
}
