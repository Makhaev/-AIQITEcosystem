package family

import (
	"aiq.local/core/internal/modules/progress"
	"aiq.local/core/internal/platform/access"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

type ProgramProgress struct {
	Title   string   `json:"title"`
	Version int      `json:"version"`
	State   string   `json:"state"`
	Percent *float64 `json:"percent"`
}
type Home struct {
	NextLesson *Lesson             `json:"next_lesson"`
	Programs   []ProgramProgress   `json:"programs"`
	Blocks     map[string][]Record `json:"blocks"`
	Totals     map[string]int      `json:"totals"`
	Parent     *ParentSummary      `json:"parent,omitempty"`
}

func (r *Repository) Home(ctx context.Context, sid int64) (Home, error) {
	return r.home(ctx, sid, false, Filter{})
}

func (r *Repository) ParentHome(ctx context.Context, sid int64, f Filter) (Home, error) {
	return r.home(ctx, sid, true, f)
}

func (r *Repository) home(ctx context.Context, sid int64, parentView bool, f Filter) (Home, error) {
	out := Home{Programs: []ProgramProgress{}, Blocks: map[string][]Record{}, Totals: map[string]int{}}
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if sid == 0 && !parentView {
		if err = tx.QueryRow(ctx, `SELECT sa.student_id FROM student_accounts sa JOIN user_roles ur ON ur.user_id=sa.user_id AND ur.role='student' WHERE sa.user_id=$1`, a.ID).Scan(&sid); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return out, missing()
			}
			return out, err
		}
	}
	own, parent, err := audience(ctx, tx, a, sid)
	if err != nil {
		return out, err
	}
	if (!parentView && !own) || (parentView && !parent) {
		return out, missing()
	}
	if parentView {
		// Use only the parent's projection, including for accounts with multiple roles.
		own = false
	} else {
		parent = false
	}
	var l Lesson
	err = tx.QueryRow(ctx, `SELECT l.id,l.topic,l.starts_at,l.ends_at,l.status FROM lessons l WHERE `+lessonMember+` AND l.status='scheduled' AND l.ends_at>clock_timestamp() ORDER BY l.starts_at,l.id LIMIT 1`, sid).Scan(&l.ID, &l.Topic, &l.StartsAt, &l.EndsAt, &l.Status)
	if err == nil {
		out.NextLesson = &l
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT g.id,p.name FROM group_enrollments e JOIN groups g ON g.id=e.group_id JOIN programs p ON p.id=g.program_id WHERE e.student_id=$1 AND e.starts_on<=(clock_timestamp() AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(clock_timestamp() AT TIME ZONE 'Europe/Moscow')::date) ORDER BY g.id`, sid)
	if err != nil {
		return out, err
	}
	type group struct {
		id   int64
		name string
	}
	groups := []group{}
	for rows.Next() {
		var g group
		if err = rows.Scan(&g.id, &g.name); err != nil {
			rows.Close()
			return out, err
		}
		groups = append(groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, g := range groups {
		v, e := progress.OverviewTx(ctx, tx, g.id, 0)
		if e != nil {
			return out, e
		}
		out.Programs = append(out.Programs, ProgramProgress{Title: g.name, Version: v.VersionNumber, State: v.State, Percent: v.Percent})
	}
	for _, kind := range []string{"assignments", "goals", "attempts", "projects", "observations"} {
		out.Blocks[kind] = []Record{}
		where := recordFrom + ` WHERE ` + visibleRecord + ` AND r.kind=$4 AND (r.kind<>'assignments' OR r.state='assigned') AND (r.kind<>'goals' OR r.state IN ('active','planned'))`
		args := []any{sid, own, parent, kind}
		var total int
		if err = tx.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&total); err != nil {
			return out, err
		}
		out.Totals[kind] = total
		// Deadlines first for active work; recent submissions/feedback first otherwise.
		rows, err = tx.Query(ctx, `SELECT r.id`+where+` ORDER BY CASE WHEN r.kind IN ('assignments','goals') THEN v.content->>'due_on' END ASC NULLS LAST,r.created_at DESC,r.id DESC LIMIT 5`, args...)
		if err != nil {
			return out, err
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return out, err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			v, e := readRecord(ctx, tx, a, id)
			if e != nil {
				return out, e
			}
			out.Blocks[kind] = append(out.Blocks[kind], v)
		}
	}
	if parentView {
		out.Parent, err = parentSummary(ctx, tx, a, sid, f)
		if err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}
