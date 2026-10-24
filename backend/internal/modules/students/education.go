package students

import (
	"aiq.local/core/internal/modules/learning"
	"aiq.local/core/internal/platform/httpx"
	"net/url"
	"time"
)

type EducationOverview struct {
	From               string         `json:"from"`
	To                 string         `json:"to"`
	Goals              learning.Block `json:"goals"`
	Observations       learning.Block `json:"observations"`
	Skills             learning.Block `json:"skills"`
	Assignments        learning.Block `json:"assignments"`
	Attempts           learning.Block `json:"attempts"`
	Projects           learning.Block `json:"projects"`
	Achievements       learning.Block `json:"achievements"`
	CurrentGoals       learning.Block `json:"current_goals"`
	CurrentAssignments learning.Block `json:"current_assignments"`
}

func educationPeriod(query url.Values, now time.Time) (time.Time, time.Time, error) {
	zone := time.FixedZone("Europe/Moscow", 3*60*60)
	now = now.In(zone)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone)
	end := start.AddDate(0, 1, 0)
	if !query.Has("education_from") && !query.Has("education_to") {
		return start, end, nil
	}
	invalid := httpx.Invalid("education_period", "Укажите обе существующие даты образовательного периода: начало не позже окончания.")
	if len(query["education_from"]) != 1 || len(query["education_to"]) != 1 {
		return start, end, invalid
	}
	from, e1 := time.ParseInLocation("2006-01-02", query.Get("education_from"), zone)
	to, e2 := time.ParseInLocation("2006-01-02", query.Get("education_to"), zone)
	if e1 != nil || e2 != nil || from.Year() < 1 || to.Year() < 1 || to.Before(from) {
		return start, end, invalid
	}
	return from, to.AddDate(0, 0, 1), nil
}
