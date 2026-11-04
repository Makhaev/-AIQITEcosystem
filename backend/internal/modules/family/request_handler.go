package family

import (
	"aiq.local/core/internal/platform/httpx"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

func requestStaff(r *http.Request) bool { return !strings.HasPrefix(r.URL.Path, "/api/v1/family/") }
func (h *Handler) RequestList(w http.ResponseWriter, r *http.Request) {
	f, e := filter(r)
	var v Page[Request]
	if e == nil {
		v, e = h.Service.Requests(r.Context(), 0, f, requestStaff(r))
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) RequestCard(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var v Request
	if e == nil {
		v, e = h.Service.RequestCard(r.Context(), id, requestStaff(r))
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) ChangeRequest(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var in RequestChange
	var v Request
	if e == nil {
		e = httpx.DecodeUpload(w, r, &in)
	}
	if e == nil {
		v, e = h.Service.ChangeRequest(r.Context(), id, in, requestStaff(r))
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) RequestDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id, e := pathID(r, "id")
	var fid int64
	var v File
	if e == nil {
		fid, e = pathID(r, "file_id")
	}
	if e == nil {
		v, e = h.Service.RequestDownload(r.Context(), id, fid, requestStaff(r))
	}
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	w.Header().Set("Content-Type", v.MediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": v.Name}))
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(v.Data)))
	w.WriteHeader(200)
	_, _ = w.Write(v.Data)
}
