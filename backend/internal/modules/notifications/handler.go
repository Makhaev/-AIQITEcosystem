package notifications

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func (h *Handler) Deliveries(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	page, size, err := httpx.Pagination(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	out, err := h.Service.Deliveries(r.Context(), page, r.URL.Query().Get("status"), size)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, out)
}
func (h *Handler) Retry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, r, httpx.Invalid("id", "Некорректная отправка."))
		return
	}
	var in RetryInput
	if err == nil {
		err = httpx.Decode(w, r, &in)
	}
	if err == nil {
		err = h.Service.Retry(r.Context(), id, in)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]bool{"queued": true})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	page, size, err := httpx.Pagination(r)
	if err!=nil{httpx.Fail(w,r,err);return}
	unread := r.URL.Query().Get("unread")
	if unread != "" && unread != "true" && unread != "false" {
		httpx.Fail(w, r, httpx.Invalid("filter", "Некорректный фильтр."))
		return
	}
	out, err := h.Service.List(r.Context(), page, unread == "true", size)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, out)
}
func (h *Handler) Read(w http.ResponseWriter, r *http.Request) {
	if err := httpx.DecodeEmpty(w, r); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, r, httpx.Invalid("id", "Некорректное уведомление."))
		return
	}
	if err = h.Service.Read(r.Context(), id); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, map[string]bool{"read": true})
}
