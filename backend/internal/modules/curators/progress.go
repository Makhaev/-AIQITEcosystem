package curators

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

type Period struct {
	From       string `json:"from"`
	To         string `json:"to"`
	start, end time.Time
}
type Attendance struct {
	Present  int      `json:"present"`
	Late     int      `json:"late"`
	Absent   int      `json:"absent"`
	Excused  int      `json:"excused"`
	Unmarked int      `json:"unmarked"`
	Total    int      `json:"total"`
	Percent  *float64 `json:"percent"`
}
type Assignments struct {
	Total     int      `json:"total"`
	Assigned  int      `json:"assigned"`
	Submitted int      `json:"submitted"`
	Reviewed  int      `json:"reviewed"`
	Overdue   int      `json:"overdue"`
	Percent   *float64 `json:"percent"`
}
type Metrics struct {
	Attendance  Attendance  `json:"attendance"`
	Assignments Assignments `json:"assignments"`
}
type Progress struct {
	Period         Period           `json:"period"`
	PreviousPeriod Period           `json:"previous_period"`
	Current        Metrics          `json:"current"`
	Previous       Metrics          `json:"previous"`
	Monthly        []map[string]any `json:"monthly"`
}

func period(from, to string) (Period, error) {
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return Period{}, err
	}
	today := time.Now().In(zone)
	if from == "" {
		from = today.AddDate(0, 0, -29).Format("2006-01-02")
	}
	if to == "" {
		to = today.Format("2006-01-02")
	}
	start, e1 := time.ParseInLocation("2006-01-02", from, zone)
	end, e2 := time.ParseInLocation("2006-01-02", to, zone)
	if e1 != nil || e2 != nil || end.Before(start) || end.Sub(start) > 365*24*time.Hour || to > today.Format("2006-01-02") {
		return Period{}, httpx.Invalid("period", "Выберите прошедший период до 366 дней: начало не позже окончания.")
	}
	return Period{From: from, To: to, start: start, end: end.AddDate(0, 0, 1)}, nil
}
func requestPeriod(r *http.Request) (Period, error) {
	return period(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
}
func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	value := float64(n) * 100 / float64(d)
	return &value
}
func metrics(ctx context.Context, tx pgx.Tx, id int64, p Period) (Metrics, error) {
	var m Metrics
	err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE a.status='present'),count(*) FILTER(WHERE a.status='late'),count(*) FILTER(WHERE a.status='absent'),count(*) FILTER(WHERE a.status='excused'),count(*) FILTER(WHERE a.status='unmarked') FROM attendance a JOIN lessons l ON l.id=a.lesson_id WHERE a.student_id=$1 AND a.included AND l.status='completed' AND l.starts_at>=$2 AND l.starts_at<$3`, id, p.start, p.end).Scan(&m.Attendance.Present, &m.Attendance.Late, &m.Attendance.Absent, &m.Attendance.Excused, &m.Attendance.Unmarked)
	if err != nil {
		return m, err
	}
	a := &m.Attendance
	a.Total = a.Present + a.Late + a.Absent + a.Excused + a.Unmarked
	a.Percent = ratio(a.Present+a.Late, a.Present+a.Late+a.Absent+a.Excused)
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE a.status='assigned'),count(*) FILTER(WHERE a.status='submitted'),count(*) FILTER(WHERE a.status='reviewed'),count(*) FILTER(WHERE a.status='assigned' AND a.due_at<now()) FROM assignments a JOIN lessons l ON l.id=a.lesson_id WHERE a.student_id=$1 AND l.status<>'cancelled' AND l.starts_at>=$2 AND l.starts_at<$3`, id, p.start, p.end).Scan(&m.Assignments.Total, &m.Assignments.Assigned, &m.Assignments.Submitted, &m.Assignments.Reviewed, &m.Assignments.Overdue)
	m.Assignments.Percent = ratio(m.Assignments.Reviewed, m.Assignments.Total)
	return m, err
}
func progress(ctx context.Context, tx pgx.Tx, id int64, p Period) (Progress, error) {
	duration := p.end.Sub(p.start)
	previous := Period{start: p.start.Add(-duration), end: p.start}
	previous.From = previous.start.Format("2006-01-02")
	previous.To = previous.end.AddDate(0, 0, -1).Format("2006-01-02")
	result := Progress{Period: p, PreviousPeriod: previous, Monthly: []map[string]any{}}
	var err error
	result.Current, err = metrics(ctx, tx, id, p)
	if err == nil {
		result.Previous, err = metrics(ctx, tx, id, previous)
	}
	if err != nil {
		return result, err
	}
	result.Monthly, err = records(ctx, tx, `SELECT jsonb_build_object('month',to_char(l.starts_at AT TIME ZONE 'Europe/Moscow','YYYY-MM'),'present',count(*) FILTER(WHERE a.status IN ('present','late')),'absent',count(*) FILTER(WHERE a.status='absent'),'excused',count(*) FILTER(WHERE a.status='excused'),'unmarked',count(*) FILTER(WHERE a.status='unmarked')) FROM attendance a JOIN lessons l ON l.id=a.lesson_id WHERE a.student_id=$1 AND a.included AND l.status='completed' AND l.starts_at>=$2 AND l.starts_at<$3 GROUP BY to_char(l.starts_at AT TIME ZONE 'Europe/Moscow','YYYY-MM') ORDER BY to_char(l.starts_at AT TIME ZONE 'Europe/Moscow','YYYY-MM')`, id, p.start, p.end)
	return result, err
}
