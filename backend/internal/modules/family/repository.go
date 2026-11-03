package family

import (
	"context"
	"errors"
	"strings"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ Pool *pgxpool.Pool }

func missing() error {
	return httpx.E(404, "not_found", "Данные не найдены или доступ отозван.")
}
func has(a httpx.Actor, role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// $1 actor ID, $2 student role, $3 parent role. Staff roles never widen a family projection.
const familyScope = `s.status='active' AND (
 ($2 AND EXISTS(SELECT 1 FROM student_accounts sa WHERE sa.student_id=s.id AND sa.user_id=$1))
 OR ($3 AND EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id
 JOIN student_parents sp ON sp.parent_id=p.id JOIN family_access fa ON fa.student_id=sp.student_id AND fa.parent_id=sp.parent_id
 WHERE pa.user_id=$1 AND sp.student_id=s.id AND p.status='active' AND fa.confirmed)))`

func familyArgs(a httpx.Actor) []any { return []any{a.ID, has(a, "student"), has(a, "parent")} }
func audience(ctx context.Context, tx pgx.Tx, a httpx.Actor, sid int64) (bool, bool, error) {
	var own, parent bool
	err := tx.QueryRow(ctx, `SELECT ($2 AND EXISTS(SELECT 1 FROM student_accounts WHERE student_id=s.id AND user_id=$1)),
 ($3 AND EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id JOIN student_parents sp ON sp.parent_id=p.id
 JOIN family_access fa ON fa.student_id=sp.student_id AND fa.parent_id=sp.parent_id WHERE pa.user_id=$1 AND sp.student_id=s.id AND p.status='active' AND fa.confirmed))
 FROM students s WHERE s.id=$4 AND `+familyScope, append(familyArgs(a), sid)...).Scan(&own, &parent)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, missing()
	}
	return own, parent, err
}
func staffStudent(ctx context.Context, tx pgx.Tx, a httpx.Actor, sid int64) error {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students WHERE id=$1 AND ($2 OR ($3 AND curator_id=$4)))`, sid, a.Admin(), has(a, "curator"), a.ID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return missing()
	}
	return nil
}
func (r *Repository) Students(ctx context.Context, f Filter) (Page[Student], error) {
	out := Page[Student]{Items: []Student{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	args := append(familyArgs(a), strings.ToLower(f.Search))
	where := ` FROM students s WHERE ` + familyScope + ` AND strpos(lower(s.name),$4)>0`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, args...).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT s.id,s.name,($2 AND EXISTS(SELECT 1 FROM student_accounts sa WHERE sa.student_id=s.id AND sa.user_id=$1))`+where+` ORDER BY s.id LIMIT $6 OFFSET $5`, append(args, (f.Page-1)*out.PageSize, out.PageSize)...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v Student
		if err = rows.Scan(&v.ID, &v.Name, &v.Own); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

const lessonMember = `((l.roster_initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM lesson_participants lp WHERE lp.lesson_id=l.id AND lp.student_id=$1 AND lp.included))
 OR (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e WHERE e.group_id=l.group_id AND e.student_id=$1 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date))))`

func (r *Repository) Lessons(ctx context.Context, sid int64, f Filter) (Page[Lesson], error) {
	out := Page[Lesson]{Items: []Lesson{}, Page: f.Page, PageSize: httpx.PageSize(f.Size)}
	tx, a, err := access.Begin(ctx, r.Pool, access.Family, access.Read, false)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = audience(ctx, tx, a, sid); err != nil {
		return out, err
	}
	where := ` FROM lessons l WHERE ` + lessonMember + ` AND strpos(lower(l.topic),lower($2))>0
 AND ($3='' OR l.starts_at>=(NULLIF($3,'')::date::timestamp AT TIME ZONE 'Europe/Moscow'))
 AND ($4='' OR l.starts_at<((NULLIF($4,'')::date+1)::timestamp AT TIME ZONE 'Europe/Moscow'))`
	if err = tx.QueryRow(ctx, `SELECT count(*)`+where, sid, f.Search, f.From, f.To).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT l.id,l.topic,l.starts_at,l.ends_at,l.status,COALESCE((SELECT at.status FROM lesson_participants p JOIN attendance at ON at.id=p.id WHERE p.lesson_id=l.id AND p.student_id=$1 AND p.included),'unmarked')`+where+` ORDER BY l.starts_at DESC,l.id DESC LIMIT $6 OFFSET $5`, sid, f.Search, f.From, f.To, (f.Page-1)*out.PageSize, out.PageSize)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var v Lesson
		if err = rows.Scan(&v.ID, &v.Topic, &v.StartsAt, &v.EndsAt, &v.Status, &v.Attendance); err != nil {
			rows.Close()
			return out, err
		}
		out.Items = append(out.Items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
