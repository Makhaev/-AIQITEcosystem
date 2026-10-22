package curriculum

import (
	"net/http"
	"strconv"

	"aiq.local/core/internal/platform/httpx"
)

type Handler struct{ Service *Service }

// Full teaching documents and transfer mappings share the proxy's 1 MiB limit.
// Small commands keep the ordinary 32 KiB limit.
const maxContentRequestBytes = 1 << 20

func pathID(r *http.Request, key string) (int64, error) {
	id, e := strconv.ParseInt(r.PathValue(key), 10, 64)
	if e != nil || id <= 0 {
		return 0, httpx.Invalid(key, "Некорректный идентификатор.")
	}
	return id, nil
}
func reply(w http.ResponseWriter, r *http.Request, value any, err error, status int) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, value)
}
func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) {
	p, e := pathID(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	if r.Method == "GET" {
		v, e := h.Service.List(r.Context(), p)
		reply(w, r, v, e, 200)
		return
	}
	var in CreateInput
	if e = httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 201)
		return
	}
	v, e := h.Service.Create(r.Context(), p, in)
	reply(w, r, v, e, 201)
}
func (h *Handler) Version(w http.ResponseWriter, r *http.Request) {
	p, e := pathID(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	id, e := pathID(r, "version_id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	if r.Method == "GET" {
		v, e := h.Service.Get(r.Context(), p, id)
		reply(w, r, v, e, 200)
		return
	}
	var in SaveInput
	if e = httpx.DecodeLimit(w, r, &in, maxContentRequestBytes); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Save(r.Context(), p, id, in)
	reply(w, r, v, e, 200)
}
func (h *Handler) Action(w http.ResponseWriter, r *http.Request) {
	p, e := pathID(r, "id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	id, e := pathID(r, "version_id")
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var in ActionInput
	if e = httpx.Decode(w, r, &in); e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Action(r.Context(), p, id, in)
	reply(w, r, v, e, 200)
}
