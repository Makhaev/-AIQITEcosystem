package students

import (
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Handler struct {
	Service  *Service
	Learning *learning.Service
}

func respond(w http.ResponseWriter, r *http.Request, value any, err error, status int) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, status, value)
}

func idFrom(r *http.Request, key string) (int64, error) {
	v, e := strconv.ParseInt(r.PathValue(key), 10, 64)
	if e != nil || v <= 0 {
		return 0, httpx.Invalid(key, "Некорректный идентификатор.")
	}
	return v, nil
}

func filterFrom(r *http.Request) (Filter, error) {
	f := Filter{Page: 1, Size: 20, Search: strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search"))), Status: r.URL.Query().Get("status")}
	for _, p := range []struct {
		key    string
		target *int
		max    int
	}{{"page", &f.Page, 1000000}, {"page_size", &f.Size, 100}} {
		if text := r.URL.Query().Get(p.key); text != "" {
			n, err := strconv.Atoi(text)
			if err != nil || n < 1 || n > p.max {
				return f, httpx.Invalid(p.key, "Некорректный параметр страницы.")
			}
			*p.target = n
		}
	}
	if utf8.RuneCountInString(f.Search) > 200 {
		return f, httpx.Invalid("search", "Поиск: не более 200 символов.")
	}
	if f.Status != "" && f.Status != "active" && f.Status != "archived" {
		return f, httpx.Invalid("status", "Допустимые статусы: active и archived.")
	}
	return f, nil
}

func (h *Handler) Students(w http.ResponseWriter, r *http.Request) {
	f, err := filterFrom(r)
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Students(r.Context(), f)
	respond(w, r, v, err, 200)
}

func (h *Handler) Student(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Student(r.Context(), id)
	respond(w, r, v, err, 200)
}

func (h *Handler) CreateStudent(w http.ResponseWriter, r *http.Request) {
	var in StudentInput
	if err := httpx.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err, 201)
		return
	}
	v, err := h.Service.SaveStudent(r.Context(), 0, in, nil)
	respond(w, r, v, err, 201)
}

func (h *Handler) UpdateStudent(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	var in StudentUpdate
	if err = httpx.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.SaveStudent(r.Context(), id, StudentInput{}, &in)
	respond(w, r, v, err, 200)
}

func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		respond(w, r, nil, err, 201)
		return
	}
	var in LinkInput
	if err = httpx.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err, 201)
		return
	}
	if in.ParentID <= 0 {
		respond(w, r, nil, httpx.Invalid("parent_id", "Выберите родителя."), 201)
		return
	}
	v, err := h.Service.SaveLink(r.Context(), id, in, nil)
	respond(w, r, v, err, 201)
}

func (h *Handler) UpdateLink(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	parentID, err := idFrom(r, "parent_id")
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	var in LinkUpdate
	if err = httpx.Decode(w, r, &in); err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.SaveLink(r.Context(), id, LinkInput{ParentID: parentID}, &in)
	respond(w, r, v, err, 200)
}
