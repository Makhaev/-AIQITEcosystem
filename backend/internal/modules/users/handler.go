package users

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	u, err := h.Service.Create(r.Context(), in)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 201, u)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Fail(w, r, httpx.Invalid("id", "Некорректный идентификатор."))
		return
	}
	var in UpdateInput
	if err = httpx.Decode(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	u, err := h.Service.Update(r.Context(), id, in)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, u)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, size := 1, 20
	var err error
	if value := r.URL.Query().Get("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 || page > 1000000 {
			httpx.Fail(w, r, httpx.Invalid("page", "Допустимое значение: 1–1000000."))
			return
		}
	}
	if value := r.URL.Query().Get("page_size"); value != "" {
		size, err = strconv.Atoi(value)
		if err != nil || size < 1 || size > 100 {
			httpx.Fail(w, r, httpx.Invalid("page_size", "Допустимое значение: 1–100."))
			return
		}
	}
	result, err := h.Service.List(r.Context(), page, size, r.URL.Query().Get("role"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, result)
}
