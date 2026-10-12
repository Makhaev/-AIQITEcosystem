package programs

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func idFrom(r *http.Request, key string) (int64, error) {
	id, e := strconv.ParseInt(r.PathValue(key), 10, 64)
	if e != nil || id <= 0 {
		return 0, httpx.Invalid(key, "Некорректный идентификатор.")
	}
	return id, nil
}
func reply(w http.ResponseWriter, r *http.Request, v any, e error, status int) {
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	httpx.JSON(w, status, v)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f := Filter{Page: 1, Size: 20, Search: r.URL.Query().Get("search"), Status: r.URL.Query().Get("status")}
	for _, p := range []struct {
		key    string
		target *int
	}{{"page", &f.Page}, {"page_size", &f.Size}} {
		if text := r.URL.Query().Get(p.key); text != "" {
			n, e := strconv.Atoi(text)
			if e != nil {
				reply(w, r, nil, httpx.Invalid(p.key, "Некорректный параметр страницы."), 200)
				return
			}
			*p.target = n
		}
	}
	v, e := h.Service.List(r.Context(), f)
	reply(w, r, v, e, 200)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Get(r.Context(), id)
	reply(w, r, v, e, 200)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if e := httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 201)
		return
	}
	v, e := h.Service.Create(r.Context(), in)
	reply(w, r, v, e, 201)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var in UpdateInput
	if e = httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Update(r.Context(), id, in)
	reply(w, r, v, e, 200)
}
func (h *Handler) CreateModule(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r, "id")
	if e != nil {
		reply(w, r, nil, e, 201)
		return
	}
	var in ModuleInput
	if e = httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 201)
		return
	}
	v, e := h.Service.CreateModule(r.Context(), id, in)
	reply(w, r, v, e, 201)
}
func (h *Handler) GetModule(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	moduleID, e := idFrom(r, "module_id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.GetModule(r.Context(), id, moduleID)
	reply(w, r, v, e, 200)
}
func (h *Handler) UpdateModule(w http.ResponseWriter, r *http.Request) {
	id, e := idFrom(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	moduleID, e := idFrom(r, "module_id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var in ModuleUpdate
	if e = httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.UpdateModule(r.Context(), id, moduleID, in)
	reply(w, r, v, e, 200)
}
