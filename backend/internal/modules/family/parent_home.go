package family

import (
	"context"
	"time"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

type AttendanceSummary struct {
	From    string   `json:"from"`
	To      string   `json:"to"`
	Present int      `json:"present"`
	Late    int      `json:"late"`
	Absent  int      `json:"absent"`
	Excused int      `json:"excused"`
	Total   int      `json:"total"`
	Percent *float64 `json:"percent"`
}

type Event struct {
	ID       int64      `json:"id"`
	Kind     string     `json:"kind"`
	Title    string     `json:"title"`
	StartsAt *time.Time `json:"starts_at,omitempty"`
	EndsAt   *time.Time `json:"ends_at,omitempty"`
	DueOn    string     `json:"due_on,omitempty"`
}

type ParentSummary struct {
	Attendance AttendanceSummary `json:"attendance"`
	Lessons    []Lesson          `json:"lessons"`
	Reports    []Report          `json:"reports"`
	Requests   []Request         `json:"requests"`
	Events     []Event           `json:"events"`
	Totals     map[string]int    `json:"totals"`
}

// A deadline is an all-day Moscow date; it must not be shifted by the browser's timezone.
const eventSource = ` FROM (
 SELECT l.id,'lesson'::text kind,l.topic title,l.starts_at,l.ends_at,''::text due_on,l.starts_at sort_at
 FROM lessons l WHERE ` + lessonMember + ` AND l.status='scheduled' AND l.ends_at>clock_timestamp()
 UNION ALL
 SELECT r.id,'assignment',COALESCE(v.content->>'title',''),NULL::timestamptz,NULL::timestamptz,v.content->>'due_on',
 ((v.content->>'due_on')::date::timestamp AT TIME ZONE 'Europe/Moscow')
 ` + recordFrom + ` WHERE r.student_id=$1 AND r.kind='assignments' AND r.state='assigned'
 AND v.content->>'due_on'>=to_char(clock_timestamp() AT TIME ZONE 'Europe/Moscow','YYYY-MM-DD')
) events WHERE strpos(lower(title),lower($2))>0`

func eventsTx(ctx context.Context, tx pgx.Tx, sid int64, f Filter, limit int) (Page[Event], error) {
	out := Page[Event]{Items: []Event{}, Page: f.Page, PageSize: limit}
	if err := tx.QueryRow(ctx, `SELECT count(*)`+eventSource, sid, f.Search).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id,kind,title,starts_at,ends_at,due_on`+eventSource+` ORDER BY sort_at,kind,id LIMIT $3 OFFSET $4`, sid, f.Search, limit, (f.Page-1)*limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		if err = rows.Scan(&e.ID, &e.Kind, &e.Title, &e.StartsAt, &e.EndsAt, &e.DueOn); err != nil {
			return out, err
		}
		out.Items = append(out.Items, e)
	}
	return out, rows.Err()
}

func (r *Repository) Events(ctx context.Context, sid int64, f Filter) (Page[Event], error) {
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return Page[Event]{}, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = audience(ctx, tx, a, sid); err != nil {
		return Page[Event]{}, err
	}
	out, err := eventsTx(ctx, tx, sid, f, httpx.PageSize(f.Size))
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func parentSummary(ctx context.Context, tx pgx.Tx, a httpx.Actor, sid int64, f Filter) (*ParentSummary, error) {
	out := &ParentSummary{Lessons: []Lesson{}, Reports: []Report{}, Requests: []Request{}, Events: []Event{}, Totals: map[string]int{}}
	at := &out.Attendance
	at.From, at.To = f.From, f.To
	if at.From == "" {
		if err := tx.QueryRow(ctx, `SELECT to_char(date_trunc('month',clock_timestamp() AT TIME ZONE 'Europe/Moscow'),'YYYY-MM-DD'),to_char(date_trunc('month',clock_timestamp() AT TIME ZONE 'Europe/Moscow')+interval '1 month - 1 day','YYYY-MM-DD')`).Scan(&at.From, &at.To); err != nil {
			return nil, err
		}
	}
	err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE a.status='present'),count(*) FILTER(WHERE a.status='late'),count(*) FILTER(WHERE a.status='absent'),count(*) FILTER(WHERE a.status='excused')
 FROM lessons l JOIN lesson_participants p ON p.lesson_id=l.id JOIN attendance a ON a.id=p.id
 WHERE p.student_id=$1 AND p.included AND l.status='completed'
 AND l.starts_at>=($2::date::timestamp AT TIME ZONE 'Europe/Moscow')
 AND l.starts_at<(($3::date+1)::timestamp AT TIME ZONE 'Europe/Moscow')`, sid, at.From, at.To).Scan(&at.Present, &at.Late, &at.Absent, &at.Excused)
	if err != nil {
		return nil, err
	}
	at.Total = at.Present + at.Late + at.Absent + at.Excused
	if at.Total > 0 {
		percent := float64(at.Present+at.Late) * 100 / float64(at.Total)
		at.Percent = &percent
	}
	where := ` FROM lessons l WHERE ` + lessonMember + ` AND l.status='scheduled' AND l.ends_at>clock_timestamp()`
	var total int
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, sid).Scan(&total); err != nil {
		return nil, err
	}
	out.Totals["lessons"] = total
	rows, err := tx.Query(ctx, `SELECT l.id,l.topic,l.starts_at,l.ends_at,l.status`+where+` ORDER BY l.starts_at,l.id LIMIT 5`, sid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l Lesson
		if err = rows.Scan(&l.ID, &l.Topic, &l.StartsAt, &l.EndsAt, &l.Status); err != nil {
			rows.Close()
			return nil, err
		}
		out.Lessons = append(out.Lessons, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM family_reports WHERE student_id=$1 AND published`, sid).Scan(&total); err != nil {
		return nil, err
	}
	out.Totals["reports"] = total
	rows, err = tx.Query(ctx, `SELECT id FROM family_reports WHERE student_id=$1 AND published ORDER BY id DESC LIMIT 5`, sid)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		report, e := readReport(ctx, tx, a, id, false)
		if e != nil {
			return nil, e
		}
		out.Reports = append(out.Reports, report)
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM family_requests WHERE student_id=$1 AND created_by=$2`, sid, a.ID).Scan(&total); err != nil {
		return nil, err
	}
	out.Totals["requests"] = total
	rows, err = tx.Query(ctx, `SELECT `+requestColumns+requestJoins+` WHERE q.student_id=$1 AND q.created_by=$2 ORDER BY q.id DESC LIMIT 5`, sid, a.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		v, scanErr := scanRequest(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		out.Requests = append(out.Requests, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	events, err := eventsTx(ctx, tx, sid, Filter{Page: 1}, 5)
	if err != nil {
		return nil, err
	}
	out.Events, out.Totals["events"] = events.Items, events.Total
	return out, nil
}
