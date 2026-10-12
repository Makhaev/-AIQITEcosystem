package families

import (
	"context"
	"net/http"
	"time"
	_ "time/tzdata"

	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
)

// A saved roster overrides later enrollment changes, including exclusion from it.
const lessonMember = `((l.roster_initialized_at IS NOT NULL AND EXISTS(
 SELECT 1 FROM attendance a WHERE a.lesson_id=l.id AND a.student_id=$1 AND a.included)) OR
 (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e
 WHERE e.group_id=l.group_id AND e.student_id=$1
 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date
 AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date))))`

const studentVisible = `($1 OR ($2 AND s.curator_id=$3) OR ($4 AND EXISTS(
 SELECT 1 FROM lessons l JOIN teachers t ON t.id=l.teacher_id
 WHERE t.user_id=$3 AND l.status<>'cancelled' AND
 ((l.roster_initialized_at IS NOT NULL AND EXISTS(SELECT 1 FROM attendance a
 WHERE a.lesson_id=l.id AND a.student_id=s.id AND a.included)) OR
 (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e
 WHERE e.group_id=l.group_id AND e.student_id=s.id
 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date
 AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date)))))))`

type StudentEnrollment struct {
	ID          int64   `json:"id"`
	GroupID     int64   `json:"group_id"`
	GroupName   string  `json:"group_name"`
	GroupStatus string  `json:"group_status"`
	ProgramID   int64   `json:"program_id"`
	ProgramName string  `json:"program_name"`
	TeacherID   *int64  `json:"teacher_id"`
	TeacherName *string `json:"teacher_name"`
	StartsOn    string  `json:"starts_on"`
	EndsOn      *string `json:"ends_on"`
}
type StudentLesson struct {
	ID          int64     `json:"id"`
	GroupID     int64     `json:"group_id"`
	GroupName   string    `json:"group_name"`
	Topic       string    `json:"topic"`
	TeacherName string    `json:"teacher_name"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Location    string    `json:"location"`
}
type StudentAttendance struct {
	StudentLesson
	Status  string `json:"status"`
	Comment string `json:"comment"`
}
type AttendanceSummary struct {
	From     string              `json:"from"`
	To       string              `json:"to"`
	Present  int                 `json:"present"`
	Late     int                 `json:"late"`
	Absent   int                 `json:"absent"`
	Excused  int                 `json:"excused"`
	Unmarked int                 `json:"unmarked"`
	Percent  *float64            `json:"percent"`
	Items    []StudentAttendance `json:"items"`
}
type StudentChange struct {
	ID         int64     `json:"id"`
	OccurredAt time.Time `json:"occurred_at"`
	ActorName  *string   `json:"actor_name"`
	Action     string    `json:"action"`
	ObjectType string    `json:"object_type"`
	ObjectID   *int64    `json:"object_id"`
}
type StudentOverview struct {
	Student
	FullAccess  bool                `json:"full_access"`
	Parents     []ParentLink        `json:"parents,omitempty"`
	Enrollments []StudentEnrollment `json:"enrollments"`
	Upcoming    []StudentLesson     `json:"upcoming_lessons"`
	Attendance  AttendanceSummary   `json:"attendance"`
	History     []StudentChange     `json:"history,omitempty"`
}

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
	v, err := h.Repo.Student360(r.Context(), id, start, end.AddDate(0, 0, 1))
	respond(w, r, v, err, 200)
}

func (r *Repository) Student360(ctx context.Context, id int64, start, end time.Time) (StudentOverview, error) {
	result := StudentOverview{Enrollments: []StudentEnrollment{}, Upcoming: []StudentLesson{}, Attendance: AttendanceSummary{From: start.Format("2006-01-02"), To: end.AddDate(0, 0, -1).Format("2006-01-02"), Items: []StudentAttendance{}}}
	tx, actor, err := r.begin(ctx, access.Students, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	scope := access.ScopeFor(actor, access.Students, access.Read)
	result.Student, err = scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+studentFrom+` WHERE `+studentVisible+` AND s.id=$5`, scope == access.All, scope&access.Assigned != 0, actor.ID, scope&access.Own != 0, id))
	if err != nil {
		return result, err
	}
	result.FullAccess = access.StudentAllowed(actor, access.StudentLinks{CuratorID: result.CuratorID}, true)
	if result.FullAccess {
		result.Parents, err = studentParents(ctx, tx, id)
		if err != nil {
			return result, err
		}
		result.History, err = studentHistory(ctx, tx, id)
		if err != nil {
			return result, err
		}
	}
	rows, err := tx.Query(ctx, `SELECT e.id,g.id,g.name,g.status,p.id,p.name,t.id,t.name,e.starts_on::text,e.ends_on::text
 FROM group_enrollments e JOIN groups g ON g.id=e.group_id JOIN programs p ON p.id=g.program_id LEFT JOIN teachers t ON t.id=g.teacher_id
 WHERE e.student_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM lessons l JOIN teachers lt ON lt.id=l.teacher_id
 WHERE l.group_id=g.id AND lt.user_id=$3 AND l.status<>'cancelled' AND `+lessonMember+`)) ORDER BY e.starts_on DESC,e.id DESC`, id, result.FullAccess, actor.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v StudentEnrollment
		if err = rows.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.GroupStatus, &v.ProgramID, &v.ProgramName, &v.TeacherID, &v.TeacherName, &v.StartsOn, &v.EndsOn); err != nil {
			rows.Close()
			return result, err
		}
		result.Enrollments = append(result.Enrollments, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	const columns = `l.id,l.group_id,g.name,l.topic,t.name,l.starts_at,l.ends_at,l.location`
	rows, err = tx.Query(ctx, `SELECT `+columns+` FROM lessons l JOIN groups g ON g.id=l.group_id JOIN teachers t ON t.id=l.teacher_id
 WHERE l.status='scheduled' AND l.starts_at>=now() AND ($2 OR t.user_id=$3) AND `+lessonMember+` ORDER BY l.starts_at,l.id LIMIT 10`, id, result.FullAccess, actor.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v StudentLesson
		if err = rows.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.Topic, &v.TeacherName, &v.StartsAt, &v.EndsAt, &v.Location); err != nil {
			rows.Close()
			return result, err
		}
		result.Upcoming = append(result.Upcoming, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, `SELECT `+columns+`,a.status,a.comment FROM attendance a JOIN lessons l ON l.id=a.lesson_id
 JOIN groups g ON g.id=l.group_id JOIN teachers t ON t.id=l.teacher_id
 WHERE a.student_id=$1 AND a.included AND l.status='completed' AND ($2 OR t.user_id=$3)
 AND l.starts_at>=$4 AND l.starts_at<$5 ORDER BY l.starts_at DESC,l.id DESC`, id, result.FullAccess, actor.ID, start, end)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v StudentAttendance
		if err = rows.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.Topic, &v.TeacherName, &v.StartsAt, &v.EndsAt, &v.Location, &v.Status, &v.Comment); err != nil {
			rows.Close()
			return result, err
		}
		result.Attendance.Items = append(result.Attendance.Items, v)
		switch v.Status {
		case "present":
			result.Attendance.Present++
		case "late":
			result.Attendance.Late++
		case "absent":
			result.Attendance.Absent++
		case "excused":
			result.Attendance.Excused++
		case "unmarked":
			result.Attendance.Unmarked++
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	a := &result.Attendance
	if total := a.Present + a.Late + a.Absent + a.Excused; total > 0 {
		percent := float64(a.Present+a.Late) / float64(total) * 100
		a.Percent = &percent
	}
	return result, tx.Commit(ctx)
}

func studentParents(ctx context.Context, tx pgx.Tx, id int64) ([]ParentLink, error) {
	result := []ParentLink{}
	rows, err := tx.Query(ctx, `SELECT `+parentColumns+`,sp.relationship,sp.is_primary FROM parents p JOIN student_parents sp ON sp.parent_id=p.id WHERE sp.student_id=$1 ORDER BY sp.is_primary DESC,p.name_search,p.id`, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		v := ParentLink{Link: Link{StudentID: id}}
		if err = rows.Scan(&v.ID, &v.Name, &v.Phone, &v.Email, &v.Status, &v.CreatedAt, &v.UpdatedAt, &v.Relationship, &v.IsPrimary); err != nil {
			return result, err
		}
		v.ParentID = v.ID
		result = append(result, v)
	}
	return result, rows.Err()
}
func studentHistory(ctx context.Context, tx pgx.Tx, id int64) ([]StudentChange, error) {
	result := []StudentChange{}
	// Never return authentication events or details containing another pupil's roster.
	rows, err := tx.Query(ctx, `SELECT a.id,a.occurred_at,u.name,a.action,a.object_type,a.object_id
 FROM audit_logs a LEFT JOIN users u ON u.id=a.actor_id WHERE
 (a.object_type='student' AND a.object_id=$1) OR
 (a.object_type='enrollment' AND EXISTS(SELECT 1 FROM group_enrollments e WHERE e.id=a.object_id AND e.student_id=$1)) OR
 (a.object_type='parent' AND EXISTS(SELECT 1 FROM student_parents sp WHERE sp.parent_id=a.object_id AND sp.student_id=$1)) OR
 (a.object_type='lesson' AND a.action IN ('attendance.updated','attendance.corrected','lesson.roster_corrected')
 AND EXISTS(SELECT 1 FROM attendance att WHERE att.lesson_id=a.object_id AND att.student_id=$1))
 ORDER BY a.occurred_at DESC,a.id DESC`, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var v StudentChange
		if err = rows.Scan(&v.ID, &v.OccurredAt, &v.ActorName, &v.Action, &v.ObjectType, &v.ObjectID); err != nil {
			return result, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}
