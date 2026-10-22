package attendance

import (
	lessonmodel "aiq.local/core/internal/domain/lesson"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/lessonstore"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
	"unicode/utf8"
)

type Repository struct{ Pool *pgxpool.Pool }

func (r *Repository) begin(ctx context.Context, resource access.Resource, action access.Action, write bool) (pgx.Tx, httpx.Actor, error) {
	return access.Begin(ctx, r.Pool, resource, action, write)
}

func InitializeJournal(ctx context.Context, tx pgx.Tx, actor httpx.Actor, v *lessonmodel.Lesson) error {
	if v.RosterInitializedAt != nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id,student_name)
 SELECT $1,$2,e.id,e.student_id,s.name FROM
 (SELECT DISTINCT ON (student_id) id,student_id FROM group_enrollments
  WHERE group_id=$2 AND starts_on<=$3::date AND (ends_on IS NULL OR ends_on>=$3::date)
  ORDER BY student_id,starts_on DESC,id DESC) e JOIN students s ON s.id=e.student_id
 ON CONFLICT(lesson_id,student_id) DO NOTHING`, v.ID, v.GroupID, v.StartsAt.In(lessonstore.Moscow).Format("2006-01-02"))
	if err != nil {
		return err
	}
	var initialized time.Time
	if err = tx.QueryRow(ctx, `UPDATE lessons SET roster_initialized_at=clock_timestamp() WHERE id=$1 RETURNING roster_initialized_at`, v.ID).Scan(&initialized); err != nil {
		return err
	}
	v.RosterInitializedAt = &initialized
	return database.AuditObject(ctx, tx, &actor.ID, "lesson.roster_initialized", "lesson", v.ID, "roster")
}

func ReadJournal(ctx context.Context, tx pgx.Tx, actor httpx.Actor, v lessonmodel.Lesson) (Journal, error) {
	result := Journal{LessonID: v.ID, LessonDate: v.StartsAt.In(lessonstore.Moscow).Format("2006-01-02"), InitializedAt: v.RosterInitializedAt, Items: []Participant{}}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attendance_records WHERE lesson_id=$1 AND status<>'unmarked')`, v.ID).Scan(&result.HasMarks); err != nil {
		return result, err
	}
	all, own, _ := lessonstore.Flags(actor)
	canSeeAll := all || own && v.TeacherUserID != nil && *v.TeacherUserID == actor.ID
	rows, err := tx.Query(ctx, `SELECT a.id,a.enrollment_id,a.student_id,a.student_name,a.status,a.comment FROM attendance_records a JOIN students s ON s.id=a.student_id WHERE a.lesson_id=$1 AND a.included AND ($2 OR s.curator_id=$3) ORDER BY a.student_name,a.student_id`, v.ID, canSeeAll, actor.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item Participant
		if err = rows.Scan(&item.ID, &item.EnrollmentID, &item.StudentID, &item.Name, &item.Status, &item.Comment); err != nil {
			rows.Close()
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	err = rows.Err()
	rows.Close()
	return result, err
}

func (r *Repository) Journal(ctx context.Context, id int64) (Journal, error) {
	tx, actor, err := r.begin(ctx, access.Attendance, access.Read, true)
	if err != nil {
		return Journal{}, err
	}
	defer tx.Rollback(ctx)
	v, err := lessonstore.LockLesson(ctx, tx, actor, id)
	if err != nil {
		return Journal{}, err
	}
	if err = InitializeJournal(ctx, tx, actor, &v); err != nil {
		return Journal{}, err
	}
	result, err := ReadJournal(ctx, tx, actor, v)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func RosterIDs(ctx context.Context, tx pgx.Tx, id int64) ([]int64, error) {
	result := []int64{}
	rows, err := tx.Query(ctx, `SELECT enrollment_id FROM lesson_participants WHERE lesson_id=$1 AND included ORDER BY enrollment_id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var value int64
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, value)
	}
	err = rows.Err()
	rows.Close()
	return result, err
}

func (r *Repository) CorrectRoster(ctx context.Context, id int64, in RosterInput) (Journal, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.EnrollmentIDs == nil {
		return Journal{}, httpx.Invalid("enrollment_ids", "Передайте состав; пустой массив исключает всех учеников.")
	}
	if n := utf8.RuneCountInString(in.Reason); n < 1 || n > 1000 || strings.ContainsRune(in.Reason, 0) {
		return Journal{}, httpx.Invalid("reason", "Причина корректировки: от 1 до 1000 символов.")
	}
	tx, actor, err := r.begin(ctx, access.Lessons, access.Manage, true)
	if err != nil {
		return Journal{}, err
	}
	defer tx.Rollback(ctx)
	v, err := lessonstore.LockLesson(ctx, tx, actor, id)
	if err != nil {
		return Journal{}, err
	}
	result, err := CorrectRoster(ctx, tx, actor, v, in, false)
	if err != nil {
		return Journal{}, err
	}
	return result, tx.Commit(ctx)
}

func CorrectRoster(ctx context.Context, tx pgx.Tx, actor httpx.Actor, v lessonmodel.Lesson, in RosterInput, onDate bool) (Journal, error) {
	id := v.ID
	var err error
	if err = InitializeJournal(ctx, tx, actor, &v); err != nil {
		return Journal{}, err
	}
	before, err := RosterIDs(ctx, tx, id)
	if err != nil {
		return Journal{}, err
	}
	type pupil struct {
		enrollmentID, studentID int64
		name                    string
	}
	pupils := []pupil{}
	seen := map[int64]bool{}
	for _, enrollmentID := range in.EnrollmentIDs {
		if enrollmentID <= 0 {
			return Journal{}, httpx.Invalid("enrollment_ids", "Проверьте идентификаторы зачислений.")
		}
		var p pupil
		p.enrollmentID = enrollmentID
		err = tx.QueryRow(ctx, `SELECT e.student_id,s.name FROM group_enrollments e JOIN students s ON s.id=e.student_id WHERE e.id=$1 AND e.group_id=$2 AND (NOT $3 OR (e.starts_on<=$4::date AND (e.ends_on IS NULL OR e.ends_on>=$4::date)))`, enrollmentID, v.GroupID, onDate, v.StartsAt.In(lessonstore.Moscow).Format("2006-01-02")).Scan(&p.studentID, &p.name)
		if err == pgx.ErrNoRows {
			if onDate {
				return Journal{}, httpx.Invalid("enrollment_ids", "Выберите зачисления этой группы, действующие на новую московскую дату занятия.")
			}
			return Journal{}, httpx.Invalid("enrollment_ids", "Можно выбрать только зачисления этой группы.")
		}
		if err != nil {
			return Journal{}, err
		}
		if seen[p.studentID] {
			return Journal{}, httpx.Invalid("enrollment_ids", "Один ученик не может быть включён дважды.")
		}
		seen[p.studentID] = true
		pupils = append(pupils, p)
	}
	if _, err = tx.Exec(ctx, `UPDATE lesson_participants SET included=false,updated_at=clock_timestamp() WHERE lesson_id=$1 AND included`, id); err != nil {
		return Journal{}, err
	}
	for _, p := range pupils {
		_, err = tx.Exec(ctx, `INSERT INTO lesson_participants(lesson_id,group_id,enrollment_id,student_id,student_name) VALUES($1,$2,$3,$4,$5) ON CONFLICT(lesson_id,student_id) DO UPDATE SET included=true,enrollment_id=EXCLUDED.enrollment_id,updated_at=clock_timestamp()`, id, v.GroupID, p.enrollmentID, p.studentID, p.name)
		if err != nil {
			return Journal{}, err
		}
	}
	after, err := RosterIDs(ctx, tx, id)
	if err != nil {
		return Journal{}, err
	}
	details, err := json.Marshal(map[string]any{"reason_provided": true, "before": before, "after": after})
	if err != nil {
		return Journal{}, err
	}
	type composition struct {
		Roster []int64 `json:"roster"`
	}
	fields := database.ChangedFields(composition{before}, composition{after}, "roster")
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs(actor_id,action,object_type,object_id,request_id,details,changed_fields) VALUES($1,'lesson.roster_corrected','lesson',$2,$3,$4::jsonb,$5)`, actor.ID, id, httpx.RequestID(ctx), string(details), fields); err != nil {
		return Journal{}, err
	}
	result, err := ReadJournal(ctx, tx, actor, v)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (r *Repository) Mark(ctx context.Context, id int64, in MarkInput) error {
	tx, actor, err := r.begin(ctx, access.Attendance, access.Mark, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	v, err := lessonstore.LockLesson(ctx, tx, actor, id)
	if err != nil {
		return err
	}
	if !access.LessonAllowed(actor, v.TeacherUserID, true) {
		return httpx.Forbidden()
	}
	if v.Status == "cancelled" {
		return httpx.E(409, "lesson_cancelled", "Отменённое занятие не допускает новых отметок.")
	}
	if time.Now().Before(v.StartsAt) {
		return httpx.E(409, "lesson_not_started", "Посещаемость можно отметить после начала занятия.")
	}
	if v.Status == "completed" && access.ScopeFor(actor, access.Attendance, access.Mark) != access.All {
		return httpx.Forbidden()
	}
	if err = InitializeJournal(ctx, tx, actor, &v); err != nil {
		return err
	}
	statusChanged, commentChanged := false, false
	for _, item := range in.Items {
		var oldStatus, oldComment string
		e := tx.QueryRow(ctx, `SELECT status,comment FROM attendance_records WHERE lesson_id=$1 AND student_id=$2 AND included`, id, item.StudentID).Scan(&oldStatus, &oldComment)
		if e == pgx.ErrNoRows {
			return httpx.Invalid("items", "Ученик отсутствует в сохранённом составе журнала.")
		}
		if e != nil {
			return e
		}
		statusChanged = statusChanged || oldStatus != item.Status
		commentChanged = commentChanged || oldComment != item.Comment
		tag, e := tx.Exec(ctx, `UPDATE attendance SET status=$3,comment=$4,updated_at=clock_timestamp() WHERE id=(SELECT id FROM lesson_participants WHERE lesson_id=$1 AND student_id=$2 AND included)`, id, item.StudentID, item.Status, item.Comment)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return httpx.Invalid("items", "Ученик отсутствует в сохранённом составе журнала.")
		}
	}
	action := "attendance.updated"
	if v.Status == "completed" {
		action = "attendance.corrected"
	}
	fields := []string{}
	if statusChanged {
		fields = append(fields, "attendance.status")
	}
	if commentChanged {
		fields = append(fields, "attendance.comment")
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, action, "lesson", id, fields...); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
