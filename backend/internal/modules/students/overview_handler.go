package students

import (
	"aiq.local/core/internal/platform/httpx"
	"net/http"
	"time"
)

func (h *Handler) Student360(w http.ResponseWriter, r *http.Request) {
	id, err := idFrom(r, "id")
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	today := time.Now().In(zone)
	from, to := today.AddDate(0, 0, -29).Format("2006-01-02"), today.Format("2006-01-02")
	if value := r.URL.Query().Get("from"); value != "" {
		from = value
	}
	if value := r.URL.Query().Get("to"); value != "" {
		to = value
	}
	start, e1 := time.ParseInLocation("2006-01-02", from, zone)
	end, e2 := time.ParseInLocation("2006-01-02", to, zone)
	if e1 != nil || e2 != nil || end.Before(start) {
		respond(w, r, nil, httpx.Invalid("period", "Укажите корректный период: начало не позже окончания."), 200)
		return
	}
	v, err := h.Service.Student360(r.Context(), id, start, end.AddDate(0, 0, 1))
	respond(w, r, v, err, 200)
}
