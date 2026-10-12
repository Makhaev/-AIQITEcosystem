package teachers

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func idFrom(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.Invalid("id", "Некорректный идентификатор.")
	}
	return id, nil
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := httpx.Decode(w, r, &input); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	value, err := h.Service.Create(r.Context(), input)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, value)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var input UpdateInput
	if err = httpx.Decode(w, r, &input); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	value, err := h.Service.Update(r.Context(), id, input)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, value)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	value, err := h.Service.Get(r.Context(), id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, value)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	filter := Filter{Page: 1, Size: 20, Search: r.URL.Query().Get("search"), Status: r.URL.Query().Get("status")}
	for _, parameter := range []struct {
		name   string
		target *int
		max    int
	}{{"page", &filter.Page, 1000000}, {"page_size", &filter.Size, 100}} {
		if value := r.URL.Query().Get(parameter.name); value != "" {
			number, err := strconv.Atoi(value)
			if err != nil || number < 1 || number > parameter.max {
				httpx.Fail(w, r, httpx.Invalid(parameter.name, "Некорректный параметр страницы."))
				return
			}
			*parameter.target = number
		}
	}
	value, err := h.Service.List(r.Context(), filter)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, value)
}
