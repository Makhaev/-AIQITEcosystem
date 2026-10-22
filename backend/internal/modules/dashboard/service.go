package dashboard

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"net/url"
	"time"
)

type Service struct{ Repo *Repository }

func periodFrom(q url.Values, now time.Time) (time.Time, time.Time, error) {
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	today := now.In(zone)
	from, to := today.Format("2006-01-")+"01", time.Date(today.Year(), today.Month()+1, 0, 0, 0, 0, 0, zone).Format("2006-01-02")
	if value := q.Get("date_from"); value != "" {
		from = value
	}
	if value := q.Get("date_to"); value != "" {
		to = value
	}
	start, e1 := time.ParseInLocation("2006-01-02", from, zone)
	end, e2 := time.ParseInLocation("2006-01-02", to, zone)
	if e1 != nil || start.Year() < 1 {
		return start, end, httpx.Invalid("date_from", "Укажите корректную дату начала периода в формате YYYY-MM-DD.")
	}
	if e2 != nil || end.Year() < 1 {
		return start, end, httpx.Invalid("date_to", "Укажите корректную дату окончания периода в формате YYYY-MM-DD.")
	}
	if end.Before(start) {
		return start, end, httpx.Invalid("period", "Начало периода не может быть позже окончания.")
	}
	return start, end.AddDate(0, 0, 1), nil
}

func (s *Service) Get(ctx context.Context, q url.Values) (Overview, error) {
	start, end, e := periodFrom(q, time.Now())
	if e != nil {
		return Overview{}, e
	}
	return s.Repo.Get(ctx, start, end)
}
