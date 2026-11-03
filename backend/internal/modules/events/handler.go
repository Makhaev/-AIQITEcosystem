package events

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func reply(w http.ResponseWriter, r *http.Request, v any, err error, status int) {
	w.Header().Set("Cache-Control", "private, no-store")
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, v)
}
func idFrom(r *http.Request) (int64, error) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		return 0, httpx.Invalid("id", "Некорректный идентификатор события.")
	}
	return id, nil
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, size, pageErr := httpx.Pagination(r)
	if pageErr != nil {
		reply(w, r, nil, pageErr, 400)
		return
	}
	f := Filter{Page: page, Size: size, Search: r.URL.Query().Get("search"), Status: r.URL.Query().Get("status")}
	if r.URL.Path == "/api/v1/events/staff" {
		v, e := h.Service.Staff(r.Context(), f)
		reply(w, r, v, e, 200)
		return
	}
	v, e := h.Service.List(r.Context(), f)
	reply(w, r, v, e, 200)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r)
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	v, e := h.Service.Get(r.Context(), id)
	reply(w, r, v, e, 200)
}
func (h *Handler) Save(w http.ResponseWriter, r *http.Request) {
	var id int64
	var err error
	code := 201
	if r.Method == "PUT" {
		id, err = idFrom(r)
		code = 200
	}
	var in Input
	if err == nil {
		err = httpx.Decode(w, r, &in)
	}
	if err != nil {
		reply(w, r, nil, err, code)
		return
	}
	v, err := h.Service.Save(r.Context(), id, in)
	reply(w, r, v, err, code)
}
func (h *Handler) Action(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	var in ActionInput
	if err == nil {
		err = httpx.Decode(w, r, &in)
	}
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Action(r.Context(), id, in)
	reply(w, r, v, err, 200)
}
