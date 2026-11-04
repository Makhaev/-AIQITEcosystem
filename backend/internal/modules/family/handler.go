package family

import (
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/platform/httpx"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handler struct{ Service *Service }

func pathID(r *http.Request, key string) (int64, error) {
	n, e := strconv.ParseInt(r.PathValue(key), 10, 64)
	if e != nil || n < 1 {
		return 0, httpx.Invalid(key, "Некорректный ID.")
	}
	return n, nil
}
func filter(r *http.Request) (Filter, error) {
	page, size, pageErr := httpx.Pagination(r)
	if pageErr != nil {
		return Filter{}, pageErr
	}
	f := Filter{Page: page, Size: size, Search: r.URL.Query().Get("search"), Kind: r.URL.Query().Get("kind"), From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to")}
	f.Status = r.URL.Query().Get("status")
	if f.Status != "" && f.Status != "new" && f.Status != "assigned" && f.Status != "in_progress" && f.Status != "answered" && f.Status != "closed" {
		return f, httpx.Invalid("status", "Неизвестный статус обращения.")
	}
	for _, date := range []string{f.From, f.To} {
		if date != "" {
			if _, err := time.Parse("2006-01-02", date); err != nil {
				return f, httpx.Invalid("period", "Укажите даты в формате ГГГГ-ММ-ДД.")
			}
		}
	}
	if (f.From == "") != (f.To == "") || f.From > f.To {
		return f, httpx.Invalid("period", "Укажите начало и конец периода в правильном порядке.")
	}
	var e error
	if v := r.URL.Query().Get("assignment_id"); v != "" {
		f.AssignmentID, e = strconv.ParseInt(v, 10, 64)
		if e != nil || f.AssignmentID < 1 {
			return f, httpx.Invalid("assignment_id", "Некорректное задание.")
		}
	}
	if e != nil || f.Page < 1 || f.Page > 1000000 || len(f.Search) > 400 {
		return f, httpx.Invalid("filter", "Некорректный фильтр.")
	}
	return f, nil
}
func reply(w http.ResponseWriter, r *http.Request, v any, e error, status int) {
	w.Header().Set("Cache-Control", "private, no-store")
	if e != nil {
		httpx.Fail(w, r, e)
		return
	}
	httpx.JSON(w, status, v)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	f, e := filter(r)
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	var v any
	var id int64
	if r.PathValue("id") != "" {
		id, e = pathID(r, "id")
		if e != nil {
			reply(w, r, nil, e, 200)
			return
		}
	}
	staff := !strings.HasPrefix(r.URL.Path, "/api/v1/family/")
	switch {
	case strings.HasSuffix(r.URL.Path, "/parent-home"):
		v, e = h.Service.ParentHome(r.Context(), id, f)
	case strings.HasSuffix(r.URL.Path, "/events"):
		v, e = h.Service.Events(r.Context(), id, f)
	case strings.HasSuffix(r.URL.Path, "/home"):
		v, e = h.Service.Home(r.Context(), id)
	case strings.HasSuffix(r.URL.Path, "/students"):
		v, e = h.Service.Students(r.Context(), f)
	case strings.HasSuffix(r.URL.Path, "/lessons"):
		v, e = h.Service.Lessons(r.Context(), id, f)
	case strings.HasSuffix(r.URL.Path, "/learning"):
		v, e = h.Service.Records(r.Context(), id, f)
	case strings.HasSuffix(r.URL.Path, "/progress"):
		v, e = h.Service.Progress(r.Context(), id)
	case strings.HasSuffix(r.URL.Path, "/reports") || strings.HasSuffix(r.URL.Path, "/family-reports"):
		v, e = h.Service.Reports(r.Context(), id, f, staff)
	case strings.HasSuffix(r.URL.Path, "/requests") || strings.HasSuffix(r.URL.Path, "/family-requests"):
		v, e = h.Service.Requests(r.Context(), id, f, staff)
	case strings.HasSuffix(r.URL.Path, "/family-access"):
		v, e = h.Service.Links(r.Context(), id)
	default:
		e = httpx.E(404, "not_found", "Раздел не найден.")
	}
	reply(w, r, v, e, 200)
}

func (h *Handler) SubmitAttempt(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var in learning.StudentAttempt
	if e == nil {
		e = httpx.DecodeUpload(w, r, &in)
	}
	var created int64
	if e == nil {
		repo := learning.Repository{Pool: h.Service.Repo.Pool}
		created, e = repo.SubmitStudent(r.Context(), id, in)
	}
	reply(w, r, map[string]int64{"id": created}, e, 201)
}
func (h *Handler) Card(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var v any
	if e == nil {
		if strings.Contains(r.URL.Path, "/reports/") {
			v, e = h.Service.Report(r.Context(), id)
		} else {
			v, e = h.Service.Record(r.Context(), id)
		}
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	id, e := pathID(r, "id")
	var fid int64
	if e == nil {
		fid, e = pathID(r, "file_id")
	}
	if e != nil {
		reply(w, r, nil, e, 200)
		return
	}
	v, e := h.Service.Download(r.Context(), id, fid, strings.Contains(r.URL.Path, "/reports/"))
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
func (h *Handler) SetLink(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var pid int64
	var in struct {
		Confirmed *bool `json:"confirmed"`
	}
	if e == nil {
		pid, e = pathID(r, "parent_id")
	}
	if e == nil {
		e = httpx.Decode(w, r, &in)
	}
	if e == nil && in.Confirmed == nil {
		e = httpx.Invalid("confirmed", "Укажите подтверждение или отзыв.")
	}
	if e == nil {
		e = h.Service.SetLink(r.Context(), id, pid, *in.Confirmed)
	}
	reply(w, r, nil, e, 204)
}
func (h *Handler) Share(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var in Audience
	if e == nil {
		e = httpx.Decode(w, r, &in)
	}
	if e == nil {
		e = h.Service.Share(r.Context(), id, in, strings.Contains(r.URL.Path, "/files/"))
	}
	reply(w, r, nil, e, 204)
}
func (h *Handler) SaveReport(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var in ReportInput
	var v Report
	status := 200
	if e == nil {
		e = httpx.Decode(w, r, &in)
	}
	if e == nil {
		if r.Method == "POST" {
			v, e = h.Service.SaveReport(r.Context(), id, 0, in)
			status = 201
		} else {
			v, e = h.Service.SaveReport(r.Context(), 0, id, in)
		}
	}
	reply(w, r, v, e, status)
}
func (h *Handler) PublishReport(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var in Publication
	var v Report
	if e == nil {
		e = httpx.Decode(w, r, &in)
	}
	if e == nil {
		v, e = h.Service.PublishReport(r.Context(), id, in)
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) CreateRequest(w http.ResponseWriter, r *http.Request) {
	var id int64
	var e error
	if r.PathValue("id") != "" {
		id, e = pathID(r, "id")
	}
	var in RequestInput
	var v Request
	if e == nil {
		e = httpx.DecodeUpload(w, r, &in)
	}
	if e == nil {
		v, e = h.Service.CreateRequest(r.Context(), id, in)
	}
	reply(w, r, v, e, 201)
}
func (h *Handler) Reply(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var in ReplyInput
	if e == nil {
		e = httpx.Decode(w, r, &in)
	}
	if e == nil {
		e = h.Service.Reply(r.Context(), id, in)
	}
	reply(w, r, nil, e, 204)
}
