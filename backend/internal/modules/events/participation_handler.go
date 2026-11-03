package events

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/httpx"
	"mime"
	"net/http"
	"strconv"
)

func pathID(r *http.Request, key string) (int64, error) {
	v, e := strconv.ParseInt(r.PathValue(key), 10, 64)
	if e != nil || v < 1 {
		return 0, httpx.Invalid(key, "Некорректный идентификатор.")
	}
	return v, nil
}
func pageFrom(r *http.Request) (int, error) {
	if r.URL.Query().Get("page") == "" {
		return 1, nil
	}
	v, e := strconv.Atoi(r.URL.Query().Get("page"))
	if e != nil {
		return 0, httpx.Invalid("page", "Некорректная страница.")
	}
	return v, validPage(v)
}
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	eid, e := idFrom(r)
	var rid int64
	if e == nil && r.PathValue("registration_id") != "" {
		rid, e = pathID(r, "registration_id")
	}
	var in RegistrationInput
	if e == nil {
		e = httpx.Decode(w, r, &in)
	}
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	v, e := h.Service.Register(r.Context(), eid, rid, in)
	code := 200
	if rid == 0 {
		code = 201
	}
	reply(w, r, v, e, code)
}
func (h *Handler) Registrations(w http.ResponseWriter, r *http.Request) {
	eid, e := idFrom(r)
	page, size, pe := httpx.Pagination(r)
	if e == nil {
		e = pe
	}
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	v, e := h.Service.Registrations(r.Context(), eid, page, size)
	reply(w, r, v, e, 200)
}
func (h *Handler) Candidates(w http.ResponseWriter, r *http.Request) {
	eid, e := idFrom(r)
	page, size, pe := httpx.Pagination(r)
	if e == nil {
		e = pe
	}
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	v, e := h.Service.Candidates(r.Context(), eid, Filter{Page: page, Size: size, Search: r.URL.Query().Get("search")})
	reply(w, r, v, e, 200)
}
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpx.Pagination(r)
	if err != nil {
		reply(w, r, nil, err, 400)
		return
	}
	eid, e := idFrom(r)
	rid, re := pathID(r, "registration_id")
	if e == nil {
		e = re
	}
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	if r.URL.Query().Has("page") || r.URL.Query().Has("page_size") {
		v, e := h.Service.HistoryPage(r.Context(), eid, rid, page, size)
		reply(w, r, v, e, 200)
		return
	}
	v, e := h.Service.History(r.Context(), eid, rid)
	reply(w, r, v, e, 200)
}
func (h *Handler) Documents(w http.ResponseWriter, r *http.Request) {
	eid, e := idFrom(r)
	rid, re := pathID(r, "registration_id")
	if e == nil {
		e = re
	}
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	if r.Method == "POST" {
		var in files.Input
		e = httpx.DecodeLimit(w, r, &in, 14<<20)
		if e != nil {
			reply(w, r, nil, e, 400)
			return
		}
		v, e := h.Service.Upload(r.Context(), eid, rid, in)
		reply(w, r, v, e, 201)
		return
	}
	page, size, e := httpx.Pagination(r)
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	v, e := h.Service.Documents(r.Context(), eid, rid, page, size)
	reply(w, r, v, e, 200)
}
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	eid, e := idFrom(r)
	rid, re := pathID(r, "registration_id")
	did, de := pathID(r, "document_id")
	if e == nil {
		e = re
	}
	if e == nil {
		e = de
	}
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	v, e := h.Service.Download(r.Context(), eid, rid, did)
	if e != nil {
		reply(w, r, nil, e, 400)
		return
	}
	w.Header().Set("Content-Type", v.MediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": v.Name}))
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(v.Data)
}
