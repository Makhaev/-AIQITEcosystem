package files

import (
	"aiq.local/core/internal/platform/httpx"
	"fmt"
	"mime"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func id(r *http.Request) (int64, error) {
	n, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || n < 1 {
		return 0, httpx.Invalid("id", "Некорректный идентификатор.")
	}
	return n, nil
}
func reply(w http.ResponseWriter, r *http.Request, v any, err error, status int) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, v)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.DecodeLimit(w, r, &in, 14<<20); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, err := h.Service.Create(r.Context(), in)
	reply(w, r, v, err, 201)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	student, err := id(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
	}
	if err != nil || page < 1 || page > 1000000 {
		httpx.Fail(w, r, httpx.Invalid("page", "Некорректная страница."))
		return
	}
	v, err := h.Service.List(r.Context(), student, page)
	reply(w, r, v, err, 200)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, err := h.Service.Get(r.Context(), n)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", v.MediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": v.Name}))
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", fmt.Sprint(len(v.Data)))
	w.WriteHeader(200)
	_, _ = w.Write(v.Data)
}
func (h *Handler) Grant(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var in Grant
	if err = httpx.Decode(w, r, &in); err == nil {
		err = h.Service.Grant(r.Context(), n, in)
	}
	reply(w, r, nil, err, 204)
}
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err == nil {
		err = h.Service.Revoke(r.Context(), n)
	}
	reply(w, r, nil, err, 204)
}
