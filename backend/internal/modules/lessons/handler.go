package lessons

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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filter{Page: 1, Size: 20, Search: q.Get("search"), Status: q.Get("status"), DateFrom: q.Get("date_from"), DateTo: q.Get("date_to")}
	for _, p := range []struct {
		key    string
		target *int
	}{{"page", &f.Page}, {"page_size", &f.Size}} {
		if text := q.Get(p.key); text != "" {
			value, err := strconv.Atoi(text)
			if err != nil {
				reply(w, r, nil, httpx.Invalid(p.key, "Некорректная страница."), 200)
				return
			}
			*p.target = value
		}
	}
	for _, p := range []struct {
		key    string
		target *int64
	}{{"group_id", &f.GroupID}, {"teacher_id", &f.TeacherID}} {
		if text := q.Get(p.key); text != "" {
			value, err := strconv.ParseInt(text, 10, 64)
			if err != nil || value <= 0 {
				reply(w, r, nil, httpx.Invalid(p.key, "Некорректный идентификатор."), 200)
				return
			}
			*p.target = value
		}
	}
	v, err := h.Service.List(r.Context(), f)
	reply(w, r, v, err, 200)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Get(r.Context(), id)
	reply(w, r, v, err, 200)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 201)
		return
	}
	v, err := h.Service.Save(r.Context(), 0, in, nil)
	reply(w, r, v, err, 201)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	var in Update
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Save(r.Context(), id, Input{}, &in)
	reply(w, r, v, err, 200)
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Complete(r.Context(), id)
	reply(w, r, v, err, 200)
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r)
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	status := "cancelled"
	v, err := h.Service.Save(r.Context(), id, Input{}, &Update{Status: Optional[string]{Set: true, Value: &status}})
	reply(w, r, v, err, 200)
}
