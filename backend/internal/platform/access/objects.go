package access

import (
	"context"

	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// Full family projections require All or assignment to the concrete pupil.
const StudentFullReadScopeSQL = `($1 OR ($2 AND s.curator_id=$3))`
const ParentReadScopeSQL = `($1 OR ($2 AND EXISTS(SELECT 1 FROM student_parents sp JOIN students s ON s.id=sp.student_id WHERE sp.parent_id=p.id AND s.curator_id=$3)))`

// StudentReadScopeSQL binds all, assigned, actor ID, own, in that order.
// Frozen rosters, including exclusions, override later enrollment changes.
const StudentReadScopeSQL = `($1 OR ($2 AND s.curator_id=$3) OR ($4 AND EXISTS(
 SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id
 WHERE t.user_id=$3 AND l.status<>'cancelled' AND
 ((l.roster_initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM lesson_participants a
 WHERE a.lesson_id=l.id AND a.student_id=s.id AND a.included)) OR
 (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e
 WHERE e.group_id=l.group_id AND e.student_id=s.id
 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date
 AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)))))))`

// GroupReadScopeSQL and LessonReadScopeSQL bind all, own, assigned, actor ID.
const GroupReadScopeSQL = `($1 OR ($2 AND (t.user_id=$4 OR EXISTS(SELECT 1 FROM lessons l JOIN teachers lt ON lt.id=l.teacher_id WHERE l.group_id=g.id AND lt.user_id=$4))) OR ($3 AND EXISTS(SELECT 1 FROM group_enrollments ge JOIN students s ON s.id=ge.student_id WHERE ge.group_id=g.id AND s.curator_id=$4)))`

const LessonReadScopeSQL = `($1 OR ($2 AND t.user_id=$4) OR ($3 AND (
 (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.group_id=l.group_id AND s.curator_id=$4 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)))
 OR (l.roster_initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM lesson_participants a JOIN students s ON s.id=a.student_id WHERE a.lesson_id=l.id AND a.included AND s.curator_id=$4))
)))`

// ObjectReadable uses exactly the predicates used by card/list repositories.
// Call inside the same transaction as assignment. Management writers share
// LockManagement, so roles, roster and enrollment changes cannot race assignment.
func ObjectReadable(ctx context.Context, tx pgx.Tx, actor httpx.Actor, kind string, id int64) (bool, error) {
	var query string
	var args []any
	switch kind {
	case "student":
		scope := ScopeFor(actor, Students, Read)
		query = `SELECT EXISTS(SELECT 1 FROM students s WHERE ` + StudentReadScopeSQL + ` AND s.id=$5)`
		args = []any{scope == All, scope&Assigned != 0, actor.ID, scope&Own != 0, id}
	case "group", "lesson":
		resource, from, predicate, object := Groups, `groups g LEFT JOIN teachers t ON t.id=g.teacher_id`, GroupReadScopeSQL, "g"
		if kind == "lesson" {
			resource, from, predicate, object = Lessons, `lessons l JOIN teachers t ON t.id=l.teacher_id`, LessonReadScopeSQL, "l"
		}
		scope := ScopeFor(actor, resource, Read)
		query = `SELECT EXISTS(SELECT 1 FROM ` + from + ` WHERE ` + predicate + ` AND ` + object + `.id=$5)`
		args = []any{scope == All, scope&Own != 0, scope&Assigned != 0, actor.ID, id}
	default:
		return false, nil
	}
	var allowed bool
	err := tx.QueryRow(ctx, query, args...).Scan(&allowed)
	return allowed, err
}
