package attendance

import (
	"aiq.local/core/internal/platform/httpx"

	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func reply(w http.ResponseWriter, r *http.Request, v any, err error, status int) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, v)
}

func idFrom(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.Invalid("id", "Некорректный идентификатор.")
	}
	return id, nil
}

func (h *Handler) Journal(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Journal(r.Context(), id)
	reply(w, r, v, err, 200)
}

func (h *Handler) CorrectRoster(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	var in RosterInput
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.CorrectRoster(r.Context(), id, in)
	reply(w, r, v, err, 200)
}

func (h *Handler) Mark(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 204)
		return
	}
	var in MarkInput
	if err = httpx.DecodeLimit(w, r, &in, 1<<20); err != nil {
		reply(w, r, nil, err, 204)
		return
	}
	err = h.Service.Mark(r.Context(), id, in)
	reply(w, r, nil, err, 204)
}
