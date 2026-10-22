package lessons

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

func (h *Handler) Reschedule(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	var in RescheduleInput
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Reschedule(r.Context(), id, in)
	reply(w, r, v, err, 200)
}

func (h *Handler) CorrectSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	var in SnapshotInput
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.CorrectSnapshot(r.Context(), id, in)
	reply(w, r, v, err, 200)
}

func (h *Handler) SnapshotHistory(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	page, size := 1, 20
	for _, p := range []struct {
		key    string
		target *int
		max    int
	}{{"page", &page, 1000000}, {"page_size", &size, 100}} {
		if value := r.URL.Query().Get(p.key); value != "" {
			n, e := strconv.Atoi(value)
			if e != nil || n < 1 || n > p.max {
				reply(w, r, nil, httpx.Invalid(p.key, "Некорректная страница."), 200)
				return
			}
			*p.target = n
		}
	}
	v, err := h.Service.SnapshotHistory(r.Context(), id, page, size)
	reply(w, r, v, err, 200)
}
