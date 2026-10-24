package students

import (
	"aiq.local/core/internal/platform/access"
	attendancecalc "aiq.local/core/internal/platform/attendance"
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

const lessonMember = `((l.roster_initialized_at IS NOT NULL AND EXISTS(
 SELECT 1 FROM lesson_participants a WHERE a.lesson_id=l.id AND a.student_id=$1 AND a.included)) OR
 (l.roster_initialized_at IS NULL AND EXISTS(SELECT 1 FROM group_enrollments e
 WHERE e.group_id=l.group_id AND e.student_id=$1
 AND e.starts_on<=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date
 AND (e.ends_on IS NULL OR e.ends_on>=(l.starts_at AT TIME ZONE 'Europe/Moscow')::date))))`

const studentVisible = access.StudentReadScopeSQL

const parentColumns = `p.id,p.name,p.phone,p.email,p.status,p.created_at,p.updated_at`

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
	ID            int64     `json:"id"`
	GroupID       int64     `json:"group_id"`
	GroupName     string    `json:"group_name"`
	ProgramName   string    `json:"program_name"`
	ModuleName    *string   `json:"module_name"`
	SnapshotState string    `json:"snapshot_state"`
	Topic         string    `json:"topic"`
	TeacherName   string    `json:"teacher_name"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	Location      string    `json:"location"`
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
	StudentResponse
	*StudentAge
	FullAccess  bool                `json:"full_access"`
	Parents     []ParentLink        `json:"parents,omitempty"`
	Enrollments []StudentEnrollment `json:"enrollments"`
	Upcoming    []StudentLesson     `json:"upcoming_lessons"`
	Attendance  AttendanceSummary   `json:"attendance"`
	History     []StudentChange     `json:"history,omitempty"`
	Education   *EducationOverview  `json:"education,omitempty"`
}

func (r *Repository) Student360(ctx context.Context, id int64, start, end time.Time) (StudentOverview, error) {
	result := StudentOverview{Enrollments: []StudentEnrollment{}, Upcoming: []StudentLesson{}, Attendance: AttendanceSummary{From: start.Format("2006-01-02"), To: end.AddDate(0, 0, -1).Format("2006-01-02"), Items: []StudentAttendance{}}}
	tx, actor, err := r.begin(ctx, access.Students, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	scope := access.ScopeFor(actor, access.Students, access.Read)
	student, err := scanStudent(tx.QueryRow(ctx, `SELECT `+studentColumns+studentFrom+` WHERE `+studentVisible+` AND s.id=$5`, scope == access.All, scope&access.Assigned != 0, actor.ID, scope&access.Own != 0, id))
	if err != nil {
		return result, err
	}
	result.FullAccess = access.StudentAllowed(actor, access.StudentLinks{CuratorID: student.CuratorID}, true)
	result.StudentResponse = studentResponse(student, result.FullAccess)
	if result.FullAccess {
		result.StudentAge = &StudentAge{Age: ageOn(student.BirthDate, time.Now())}
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
	const columns = `l.id,l.group_id,g.name,CASE WHEN l.status='completed' AND l.snapshot_state='captured' THEN l.snapshot_topic ELSE l.topic END,t.name,l.starts_at,l.ends_at,l.location,
 CASE WHEN l.status='completed' AND l.snapshot_state='captured' THEN l.snapshot_program_name ELSE CASE WHEN l.version_module_key<>'' THEN l.plan_snapshot->>'program_name' ELSE p.name END END,
 CASE WHEN l.status='completed' AND l.snapshot_state='captured' THEN l.snapshot_module_name ELSE CASE WHEN l.version_module_key<>'' THEN l.plan_snapshot->>'module_name' ELSE m.name END END,l.snapshot_state`
	rows, err = tx.Query(ctx, `SELECT `+columns+` FROM lessons l JOIN groups g ON g.id=l.group_id JOIN teachers t ON t.id=l.teacher_id JOIN programs p ON p.id=l.program_id LEFT JOIN program_modules m ON m.id=l.module_id
 WHERE l.status='scheduled' AND l.starts_at>=now() AND ($2 OR t.user_id=$3) AND `+lessonMember+` ORDER BY l.starts_at,l.id LIMIT 10`, id, result.FullAccess, actor.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v StudentLesson
		if err = rows.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.Topic, &v.TeacherName, &v.StartsAt, &v.EndsAt, &v.Location, &v.ProgramName, &v.ModuleName, &v.SnapshotState); err != nil {
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
	rows, err = tx.Query(ctx, `SELECT `+columns+`,a.status,a.comment FROM attendance_records a JOIN lessons l ON l.id=a.lesson_id
 JOIN groups g ON g.id=l.group_id JOIN teachers t ON t.id=l.teacher_id JOIN programs p ON p.id=l.program_id LEFT JOIN program_modules m ON m.id=l.module_id
 WHERE a.student_id=$1 AND a.included AND l.status='completed' AND ($2 OR t.user_id=$3)
 AND l.starts_at>=$4 AND l.starts_at<$5 ORDER BY l.starts_at DESC,l.id DESC`, id, result.FullAccess, actor.ID, start, end)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var v StudentAttendance
		if err = rows.Scan(&v.ID, &v.GroupID, &v.GroupName, &v.Topic, &v.TeacherName, &v.StartsAt, &v.EndsAt, &v.Location, &v.ProgramName, &v.ModuleName, &v.SnapshotState, &v.Status, &v.Comment); err != nil {
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
	a.Percent = attendancecalc.Percent(a.Present, a.Late, a.Absent, a.Excused)
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
 AND EXISTS(SELECT 1 FROM lesson_participants att WHERE att.lesson_id=a.object_id AND att.student_id=$1))
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
