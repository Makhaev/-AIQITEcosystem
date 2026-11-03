package invitations

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func finish(w http.ResponseWriter, r *http.Request, value any, err error, status int) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, value)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpx.Pagination(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, e := h.Service.List(r.Context(), page, size)
	finish(w, r, v, e, 200)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if e := httpx.Decode(w, r, &in); e != nil {
		httpx.Fail(w, r, e)
		return
	}
	v, e := h.Service.Issue(r.Context(), in, 0)
	finish(w, r, v, e, 201)
}
func (h *Handler) Resend(w http.ResponseWriter, r *http.Request) {
	if err := httpx.DecodeEmpty(w, r); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		httpx.Fail(w, r, httpx.Invalid("id", "Некорректный номер приглашения."))
		return
	}
	v, e := h.Service.Issue(r.Context(), Input{}, id)
	finish(w, r, v, e, 201)
}
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	if err := httpx.DecodeEmpty(w, r); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		httpx.Fail(w, r, httpx.Invalid("id", "Некорректный номер приглашения."))
		return
	}
	finish(w, r, nil, h.Service.Revoke(r.Context(), id), 204)
}
func (h *Handler) Inspect(w http.ResponseWriter, r *http.Request) {
	var in TokenInput
	if e := httpx.Decode(w, r, &in); e != nil {
		httpx.Fail(w, r, e)
		return
	}
	v, e := h.Service.Inspect(r.Context(), in.Token)
	finish(w, r, v, e, 200)
}
func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	var in AcceptInput
	if e := httpx.Decode(w, r, &in); e != nil {
		httpx.Fail(w, r, e)
		return
	}
	v, e := h.Service.Accept(r.Context(), in)
	finish(w, r, v, e, 200)
}
func (h *Handler) Links(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpx.Pagination(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, e := h.Service.Links(r.Context())
	total := len(v)
	start := (page - 1) * size
	end := start + size
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	finish(w, r, map[string]any{"items": v[start:end], "total": total, "page": page, "page_size": size}, e, 200)
}
