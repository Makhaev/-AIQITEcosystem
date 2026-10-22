package lessons

import (
	"aiq.local/core/internal/modules/attendance"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/lessonstore"
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func sameModule(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func validReason(reason string) error {
	if n := utf8.RuneCountInString(strings.TrimSpace(reason)); n < 1 || n > 1000 || strings.ContainsRune(reason, 0) {
		return httpx.Invalid("reason", "Причина: от 1 до 1000 символов.")
	}
	return nil
}

type RescheduleInput struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	RosterInput
}

func (r *Repository) Reschedule(ctx context.Context, id int64, in RescheduleInput) (Lesson, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if err := validReason(in.Reason); err != nil {
		return Lesson{}, err
	}
	if in.EnrollmentIDs == nil {
		return Lesson{}, httpx.Invalid("enrollment_ids", "Передайте полный новый состав занятия.")
	}
	tx, actor, err := r.begin(ctx, access.Lessons, access.Manage, true)
	if err != nil {
		return Lesson{}, err
	}
	defer tx.Rollback(ctx)
	v, err := lessonstore.LockLesson(ctx, tx, actor, id)
	if err != nil {
		return v, err
	}
	if v.Status != "scheduled" {
		return v, httpx.E(409, "reschedule_not_scheduled", "Перенос доступен только запланированному занятию.")
	}
	check := Input{GroupID: v.GroupID, ModuleID: v.ModuleID, TeacherID: v.TeacherID, Topic: v.Topic, Location: v.Location, Status: v.Status, StartsAt: in.StartsAt, EndsAt: in.EndsAt}
	if err = normalize(&check); err != nil {
		return v, err
	}
	if err = attendance.InitializeJournal(ctx, tx, actor, &v); err != nil {
		return v, err
	}
	before, err := attendance.RosterIDs(ctx, tx, id)
	if err != nil {
		return v, err
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('aiq.roster_reschedule',$1,true)`, strconv.FormatInt(id, 10)); err != nil {
		return v, err
	}
	if _, err = tx.Exec(ctx, `UPDATE lessons SET starts_at=$2,ends_at=$3,updated_at=clock_timestamp() WHERE id=$1`, id, check.StartsAt, check.EndsAt); err != nil {
		return v, err
	}
	newLesson, err := lessonstore.Load(ctx, tx, id, false)
	if err != nil {
		return v, err
	}
	if _, err = attendance.CorrectRoster(ctx, tx, actor, newLesson, in.RosterInput, true); err != nil {
		return v, err
	}
	after, err := attendance.RosterIDs(ctx, tx, id)
	if err != nil {
		return v, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO lesson_reschedules(lesson_id,actor_id,old_starts_at,old_ends_at,starts_at,ends_at,previous_roster,roster,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, actor.ID, v.StartsAt, v.EndsAt, newLesson.StartsAt, newLesson.EndsAt, before, after, in.Reason); err != nil {
		return v, err
	}
	if err = database.AuditObject(ctx, tx, &actor.ID, "lesson.rescheduled", "lesson", id, "starts_at", "ends_at", "roster"); err != nil {
		return v, err
	}
	newLesson, err = lessonstore.Load(ctx, tx, id, false)
	if err != nil {
		return v, err
	}
	return newLesson, tx.Commit(ctx)
}

type SnapshotInput struct {
	ProgramName string  `json:"program_name"`
	ModuleName  *string `json:"module_name"`
	Topic       string  `json:"topic"`
	Reason      string  `json:"reason"`
}

func (r *Repository) CorrectSnapshot(ctx context.Context, id int64, in SnapshotInput) (Lesson, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if err := validReason(in.Reason); err != nil {
		return Lesson{}, err
	}
	in.ProgramName = strings.TrimSpace(in.ProgramName)
	in.Topic = strings.TrimSpace(in.Topic)
	values := map[string]string{"program_name": in.ProgramName, "topic": in.Topic}
	if in.ModuleName != nil {
		value := strings.TrimSpace(*in.ModuleName)
		in.ModuleName = &value
		values["module_name"] = value
	}
	for field, value := range values {
		if n := utf8.RuneCountInString(value); n < 1 || n > 200 || strings.ContainsRune(value, 0) {
			return Lesson{}, httpx.Invalid(field, "Укажите название от 1 до 200 символов.")
		}
	}
	tx, actor, err := r.begin(ctx, access.Lessons, access.Manage, true)
	if err != nil {
		return Lesson{}, err
	}
	defer tx.Rollback(ctx)
	v, err := lessonstore.LockLesson(ctx, tx, actor, id)
	if err != nil {
		return v, err
	}
	if v.Status != "completed" {
		return v, httpx.E(409, "lesson_not_completed", "Снимок корректируется только у завершённого занятия.")
	}
	hasModule := v.ModuleID != nil || v.VersionModuleKey != ""
	if hasModule != (in.ModuleName != nil) {
		return v, httpx.Invalid("module_name", "Название модуля обязательно при наличии модуля занятия.")
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('aiq.snapshot_actor',$1,true),set_config('aiq.snapshot_reason',$2,true)`, strconv.FormatInt(actor.ID, 10), in.Reason); err != nil {
		return v, err
	}
	if _, err = tx.Exec(ctx, `UPDATE lessons SET snapshot_program_name=$2,snapshot_module_name=$3,snapshot_topic=$4,topic=$4,topic_search=$5,snapshot_state='captured',snapshot_captured_at=COALESCE(snapshot_captured_at,clock_timestamp()),updated_at=clock_timestamp() WHERE id=$1`, id, in.ProgramName, in.ModuleName, in.Topic, strings.ToLower(in.Topic)); err != nil {
		return v, err
	}
	updated, err := lessonstore.Load(ctx, tx, id, false)
	if err != nil {
		return v, err
	}
	fields := database.ChangedFields(v, updated, "program_name", "module_name", "topic", "snapshot_state")
	if err = database.AuditObject(ctx, tx, &actor.ID, "lesson.snapshot_corrected", "lesson", id, fields...); err != nil {
		return v, err
	}
	return updated, tx.Commit(ctx)
}

type SnapshotVersion struct {
	ID          int64      `json:"id"`
	ActorID     *int64     `json:"actor_id"`
	ProgramName *string    `json:"program_name"`
	ModuleName  *string    `json:"module_name"`
	Topic       *string    `json:"topic"`
	CapturedAt  *time.Time `json:"captured_at"`
	State       string     `json:"state"`
	PreservedAt time.Time  `json:"preserved_at"`
	Reason      *string    `json:"reason"`
}

type SnapshotPage struct {
	Items    []SnapshotVersion `json:"items"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
}

func (r *Repository) SnapshotHistory(ctx context.Context, id int64, page, size int) (SnapshotPage, error) {
	result := SnapshotPage{Items: []SnapshotVersion{}, Page: page, PageSize: size}
	tx, actor, err := r.begin(ctx, access.Lessons, access.Read, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if access.ScopeFor(actor, access.Lessons, access.Manage) != access.All {
		return result, httpx.Forbidden()
	}
	if _, err = lessonstore.Load(ctx, tx, id, false); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM lesson_snapshot_versions WHERE lesson_id=$1`, id).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT id,actor_id,program_name,module_name,topic,captured_at,state,preserved_at,reason FROM lesson_snapshot_versions WHERE lesson_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`, id, size, (page-1)*size)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var v SnapshotVersion
		if err = rows.Scan(&v.ID, &v.ActorID, &v.ProgramName, &v.ModuleName, &v.Topic, &v.CapturedAt, &v.State, &v.PreservedAt, &v.Reason); err != nil {
			return result, err
		}
		result.Items = append(result.Items, v)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	return result, tx.Commit(ctx)
}
