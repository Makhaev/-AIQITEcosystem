package family

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
)

func (h *Handler) ReportQueue(w http.ResponseWriter, r *http.Request) {
	f, e := filter(r)
	var v Page[Report]
	if e == nil {
		v, e = h.Service.ReportQueue(r.Context(), f)
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) StaffReport(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var v Report
	if e == nil {
		v, e = h.Service.StaffReport(r.Context(), id)
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) ReportAction(w http.ResponseWriter, r *http.Request) {
	id, e := pathID(r, "id")
	var in ReportAction
	var v Report
	if e == nil {
		e = httpx.Decode(w, r, &in)
	}
	if e == nil {
		v, e = h.Service.ReportAction(r.Context(), id, in)
	}
	reply(w, r, v, e, 200)
}
func (h *Handler) ReportNotifications(w http.ResponseWriter, r *http.Request) {
	page, size, err := httpx.Pagination(r)
	if err != nil {
		reply(w, r, nil, err, 400)
		return
	}
	id, e := pathID(r, "id")
	if e == nil && (r.URL.Query().Has("page") || r.URL.Query().Has("page_size")) {
		v, e := h.Service.ReportNotificationsPage(r.Context(), id, page, size)
		reply(w, r, v, e, 200)
		return
	}
	var v []ReportNotification
	if e == nil {
		v, e = h.Service.ReportNotifications(r.Context(), id)
	}
	reply(w, r, v, e, 200)
}
