package groups

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Repo *Repository }

func reply(w http.ResponseWriter, r *http.Request, v any, err error, status int) {
	if err != nil {
		httpx.Fail(w, r, translate(err))
		return
	}
	httpx.JSON(w, status, v)
}
func idFrom(r *http.Request, key string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil || id <= 0 {
		return 0, httpx.Invalid(key, "Некорректный идентификатор.")
	}
	return id, nil
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f := Filter{Page: 1, Size: 20, Search: r.URL.Query().Get("search"), Status: r.URL.Query().Get("status")}
	for _, p := range []struct {
		key    string
		target *int
	}{{"page", &f.Page}, {"page_size", &f.Size}} {
		if value := r.URL.Query().Get(p.key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				reply(w, r, nil, httpx.Invalid(p.key, "Некорректный параметр страницы."), 200)
				return
			}
			*p.target = n
		}
	}
	v, err := h.Repo.List(r.Context(), f)
	reply(w, r, v, err, 200)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Repo.Get(r.Context(), id)
	reply(w, r, v, err, 200)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 201)
		return
	}
	v, err := h.Repo.Save(r.Context(), 0, in, nil)
	reply(w, r, v, err, 201)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	var in UpdateInput
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Repo.Save(r.Context(), id, CreateInput{}, &in)
	reply(w, r, v, err, 200)
}
func (h *Handler) Enroll(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		reply(w, r, nil, err, 201)
		return
	}
	var in EnrollInput
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 201)
		return
	}
	v, err := h.Repo.Enroll(r.Context(), id, in)
	reply(w, r, v, err, 201)
}
func (h *Handler) Close(w http.ResponseWriter, r *http.Request) {
	groupID, err := idFrom(r, "id")
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	id, err := idFrom(r, "enrollment_id")
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	var in CloseInput
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Repo.Close(r.Context(), groupID, id, in)
	reply(w, r, v, err, 200)
}
func (h *Handler) Transfer(w http.ResponseWriter, r *http.Request) {
	groupID, err := idFrom(r, "id")
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	id, err := idFrom(r, "enrollment_id")
	if err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	var in TransferInput
	if err = httpx.Decode(w, r, &in); err != nil {
		reply(w, r, nil, err, 200)
		return
	}
	v, err := h.Repo.Transfer(r.Context(), groupID, id, in)
	reply(w, r, v, err, 200)
}
