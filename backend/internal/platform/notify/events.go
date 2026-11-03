// Package notify records minimal notifications in the caller's transaction.
// It accepts no message bodies, observations, attachments, addresses or tokens.
package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func Account(ctx context.Context, tx pgx.Tx, kind string, objectID, userID int64, version string) error {
	if kind != "invitation" && kind != "password_reset" {
		return fmt.Errorf("unsupported account notification")
	}
	// These security emails retain their existing one-time-link delivery flow.
	_, err := tx.Exec(ctx, `INSERT INTO notifications(user_id,kind,object_id,event_version,audience)
 SELECT id,$1,$2,$3,'account' FROM users WHERE id=$4 AND NOT blocked ON CONFLICT DO NOTHING`, kind, objectID, version, userID)
	return err
}

func Family(ctx context.Context, tx pgx.Tx, kind string, objectID int64, version string, sid int64) error {
	switch kind {
	case "lesson_rescheduled", "lesson_cancelled", "assignment_assigned", "attempt_reviewed", "report_published", "participation_confirmed", "participation_cancelled":
	default:
		return fmt.Errorf("unsupported family notification")
	}
	_, err := tx.Exec(ctx, `INSERT INTO notifications(user_id,student_id,kind,object_id,event_version,audience)
 SELECT id,$1,$2,$3,$4,'family' FROM users WHERE notification_family_access(id,$1) ON CONFLICT DO NOTHING`, sid, kind, objectID, version)
	return err
}

func RequestAnswer(ctx context.Context, tx pgx.Tx, id int64, version int) error {
	_, err := tx.Exec(ctx, `INSERT INTO notifications(user_id,student_id,kind,object_id,event_version,audience)
 SELECT created_by,student_id,'request_answered',id,$2,'family' FROM family_requests WHERE id=$1 ON CONFLICT DO NOTHING`, id, fmt.Sprint(version))
	return err
}

// LessonStudents captures the existing audience before a reschedule changes it.
func LessonStudents(ctx context.Context, tx pgx.Tx, id int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `SELECT s.id FROM students s JOIN lessons l ON l.id=$1 WHERE
 (l.roster_initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM lesson_participants p WHERE p.lesson_id=l.id AND p.student_id=s.id AND p.included)) OR
 (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e WHERE e.group_id=l.group_id AND e.student_id=s.id AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)))`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var sid int64
		if err = rows.Scan(&sid); err != nil {
			return nil, err
		}
		out = append(out, sid)
	}
	return out, rows.Err()
}
func Lesson(ctx context.Context, tx pgx.Tx, kind string, id int64, stamp time.Time, before []int64) error {
	if kind != "lesson_rescheduled" && kind != "lesson_cancelled" {
		return fmt.Errorf("unsupported lesson notification")
	}
	after, err := LessonStudents(ctx, tx, id)
	if err != nil {
		return err
	}
	version := stamp.UTC().Format(time.RFC3339Nano)
	seen := map[int64]bool{}
	for _, sid := range append(before, after...) {
		if seen[sid] {
			continue
		}
		seen[sid] = true
		if err = Family(ctx, tx, kind, id, version, sid); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO notifications(user_id,kind,object_id,event_version,audience)
 SELECT t.user_id,$2,l.id,$3,'teacher' FROM lessons l JOIN teachers t ON t.id=l.teacher_id
 JOIN users u ON u.id=t.user_id AND NOT u.blocked JOIN user_roles ur ON ur.user_id=u.id AND ur.role='teacher'
 WHERE l.id=$1 AND t.status='active' ON CONFLICT DO NOTHING`, id, kind, version)
	return err
}

// Participation is the integration point for event registrations. Call
// only after an authorized participation transition, in the same transaction,
// with its stable event ID and revision. No public event-emission API exists.
func Participation(ctx context.Context, tx pgx.Tx, eventID int64, revision string, studentID int64, confirmed bool) error {
	kind := "participation_cancelled"
	if confirmed {
		kind = "participation_confirmed"
	}
	return Family(ctx, tx, kind, eventID, revision, studentID)
}

// VisibleSQL expects aliases n (notification) and u (its recipient).
// Staff roles cannot expand a notification's family audience.
const VisibleSQL = `NOT u.blocked AND (
 (n.audience='account' AND (
   n.kind='password_reset' OR (n.kind='invitation' AND EXISTS(SELECT 1 FROM invitations i WHERE i.id=n.object_id AND i.email=u.email AND i.revoked_at IS NULL AND (i.used_by=u.id OR i.expires_at>clock_timestamp())))
 )) OR
 (n.audience='teacher' AND EXISTS(SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id JOIN user_roles ur ON ur.user_id=t.user_id AND ur.role='teacher' WHERE l.id=n.object_id AND t.user_id=u.id AND t.status='active')) OR
 (n.audience='family' AND (notification_family_access(u.id,n.student_id) OR
   (n.student_id IS NULL AND n.kind='request_answered' AND EXISTS(SELECT 1 FROM parent_accounts pa JOIN parents p ON p.id=pa.parent_id AND p.status='active' JOIN user_roles ur ON ur.user_id=pa.user_id AND ur.role='parent' WHERE pa.user_id=u.id))) AND (
  (n.kind IN ('lesson_rescheduled','lesson_cancelled') AND EXISTS(SELECT 1 FROM lessons WHERE id=n.object_id)) OR
  (n.kind='assignment_assigned' AND EXISTS(SELECT 1 FROM learning_records WHERE id=n.object_id AND student_id=n.student_id AND kind='assignments' AND state NOT IN ('withdrawn','cancelled'))) OR
  (n.kind='attempt_reviewed' AND EXISTS(SELECT 1 FROM learning_records WHERE id=n.object_id AND student_id=n.student_id AND kind='attempts' AND state IN ('accepted','revision') AND current_revision::text=n.event_version)) OR
  (n.kind='report_published' AND EXISTS(SELECT 1 FROM family_reports WHERE id=n.object_id AND student_id=n.student_id AND published AND published_version::text=n.event_version)) OR
  (n.kind='request_answered' AND EXISTS(SELECT 1 FROM family_requests WHERE id=n.object_id AND created_by=u.id)) OR
  n.kind IN ('participation_confirmed','participation_cancelled') OR
  (n.kind IN ('event_rescheduled','event_cancelled') AND EXISTS(SELECT 1 FROM free_events WHERE id=n.object_id AND status<>'draft'))
 )))`
