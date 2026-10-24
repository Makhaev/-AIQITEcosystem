package students

import (
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/platform/httpx"
	"errors"
	"log/slog"
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
	educationStart, educationEnd, err := educationPeriod(r.URL.Query(), today)
	if err != nil {
		respond(w, r, nil, err, 200)
		return
	}
	v, err := h.Service.Student360(r.Context(), id, start, end.AddDate(0, 0, 1))
	if err == nil {
		v.Education = &EducationOverview{From: educationStart.Format("2006-01-02"), To: educationEnd.AddDate(0, 0, -1).Format("2006-01-02")}
		blocks := []struct {
			kind    string
			current bool
			target  *learning.Block
		}{{"goals", false, &v.Education.Goals}, {"observations", false, &v.Education.Observations}, {"skills", false, &v.Education.Skills}, {"assignments", false, &v.Education.Assignments}, {"attempts", false, &v.Education.Attempts}, {"projects", false, &v.Education.Projects}, {"achievements", false, &v.Education.Achievements}, {"goals", true, &v.Education.CurrentGoals}, {"assignments", true, &v.Education.CurrentAssignments}}
		for _, block := range blocks {
			*block.target, err = h.Learning.List(r.Context(), id, learning.Filter{Kind: block.kind, Start: educationStart, End: educationEnd, Page: 1, Current: block.current})
			if err != nil {
				var failure *httpx.Error
				if errors.As(err, &failure) && failure.Status < 500 {
					break
				}
				slog.ErrorContext(r.Context(), "Student 360 education block failed", "kind", block.kind, "error", err)
				*block.target = learning.Block{State: "error"}
				err = nil
			}
		}
	}
	respond(w, r, v, err, 200)
}
