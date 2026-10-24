package progress

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"strconv"
)

type Handler struct{ Service *Service }

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 1 {
		return 0, httpx.Invalid("id", "Некорректный идентификатор.")
	}
	return id, nil
}
func respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, 200, v)
}
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if r.Method == "GET" {
		v, e := h.Service.Report(r.Context(), id)
		respond(w, r, v, e)
		return
	}
	var in ReportInput
	if err = httpx.Decode(w, r, &in); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, err := h.Service.SaveReport(r.Context(), id, in)
	respond(w, r, v, err)
}
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	var assignment int64
	if raw := r.URL.Query().Get("assignment_id"); err == nil && raw != "" {
		assignment, err = parseID(raw)
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	v, err := h.Service.Overview(r.Context(), id, assignment)
	respond(w, r, v, err)
}
