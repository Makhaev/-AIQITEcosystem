package lessons

import (
	"errors"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var moscow = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		panic(err)
	}
	return loc
}()

func normalize(in *Input) error {
	in.Topic = strings.TrimSpace(in.Topic)
	in.Location = strings.TrimSpace(in.Location)
	if n := utf8.RuneCountInString(in.Topic); n < 1 || n > 200 || strings.ContainsRune(in.Topic, 0) {
		return httpx.Invalid("topic", "Тема: от 1 до 200 символов.")
	}
	if utf8.RuneCountInString(in.Location) > 500 || strings.ContainsRune(in.Location, 0) {
		return httpx.Invalid("location", "Место проведения: не более 500 символов.")
	}
	if in.GroupID <= 0 {
		return httpx.Invalid("group_id", "Выберите группу.")
	}
	if in.TeacherID <= 0 {
		return httpx.Invalid("teacher_id", "Выберите педагога.")
	}
	if in.ModuleID != nil && *in.ModuleID <= 0 {
		return httpx.Invalid("module_id", "Выберите модуль или оставьте поле пустым.")
	}
	if in.StartsAt.IsZero() || in.StartsAt.Year() < 1 || in.StartsAt.Year() > 9999 {
		return httpx.Invalid("starts_at", "Укажите начало с часовым поясом в формате RFC3339.")
	}
	if in.EndsAt.IsZero() || in.EndsAt.Year() < 1 || in.EndsAt.Year() > 9999 {
		return httpx.Invalid("ends_at", "Укажите окончание с часовым поясом в формате RFC3339.")
	}
	in.StartsAt = in.StartsAt.UTC().Truncate(time.Microsecond)
	in.EndsAt = in.EndsAt.UTC().Truncate(time.Microsecond)
	if !in.EndsAt.After(in.StartsAt) {
		return httpx.Invalid("ends_at", "Окончание должно быть позже начала.")
	}
	if in.Status == "" {
		in.Status = "scheduled"
	}
	if !validStatus(in.Status) {
		return httpx.Invalid("status", "Допустимые статусы: scheduled, completed и cancelled.")
	}
	return nil
}
func validStatus(status string) bool {
	return status == "scheduled" || status == "completed" || status == "cancelled"
}
func patchRequired[T any](in Optional[T], out *T, key string) error {
	if !in.Set {
		return nil
	}
	if in.Value == nil {
		return httpx.Invalid(key, "Поле обязательно.")
	}
	*out = *in.Value
	return nil
}
func apply(in *Input, p Update) error {
	if !p.GroupID.Set && !p.ModuleID.Set && !p.Topic.Set && !p.TeacherID.Set && !p.StartsAt.Set && !p.EndsAt.Set && !p.Location.Set && !p.Status.Set {
		return httpx.E(400, "empty_update", "Укажите поля для изменения.")
	}
	if err := patchRequired(p.GroupID, &in.GroupID, "group_id"); err != nil {
		return err
	}
	if err := patchRequired(p.TeacherID, &in.TeacherID, "teacher_id"); err != nil {
		return err
	}
	if err := patchRequired(p.Topic, &in.Topic, "topic"); err != nil {
		return err
	}
	if err := patchRequired(p.StartsAt, &in.StartsAt, "starts_at"); err != nil {
		return err
	}
	if err := patchRequired(p.EndsAt, &in.EndsAt, "ends_at"); err != nil {
		return err
	}
	if err := patchRequired(p.Status, &in.Status, "status"); err != nil {
		return err
	}
	if p.Status.Set && in.Status == "" {
		return httpx.Invalid("status", "Статус обязателен.")
	}
	if p.ModuleID.Set {
		in.ModuleID = p.ModuleID.Value
	}
	if p.Location.Set {
		in.Location = ""
		if p.Location.Value != nil {
			in.Location = *p.Location.Value
		}
	}
	return nil
}
func parseDate(value, key string) (time.Time, error) {
	v, err := time.ParseInLocation("2006-01-02", value, moscow)
	if err != nil || v.Year() < 1 || v.Format("2006-01-02") != value {
		return time.Time{}, httpx.Invalid(key, "Укажите дату в формате YYYY-MM-DD.")
	}
	return v, nil
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Занятие не найдено.")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "lessons_completion_time":
		return httpx.E(409, "lesson_not_ended", "Завершить занятие можно только после его окончания.")
	case "lessons_completion_unmarked":
		return httpx.E(409, "attendance_unmarked", "Перед завершением сохраните отметки для всех учеников.")
	case "lessons_cancellation_marks":
		return httpx.E(409, "lesson_has_marks", "Нельзя отменить занятие с внесёнными отметками посещаемости.")
	case "attendance_lesson_cancelled":
		return httpx.E(409, "lesson_cancelled", "Отменённое занятие не допускает новых отметок.")
	case "attendance_lesson_not_started":
		return httpx.E(409, "lesson_not_started", "Посещаемость можно отметить после начала занятия.")
	case "lessons_teacher_overlap":
		return httpx.E(409, "teacher_overlap", "У педагога уже есть занятие в это время.")
	case "lessons_group_overlap":
		return httpx.E(409, "group_overlap", "У группы уже есть занятие в это время.")
	case "lessons_roster_frozen", "attendance_lesson_group":
		return httpx.E(409, "journal_frozen", "После открытия журнала нельзя менять группу или московскую дату занятия.")
	case "lessons_ends_at_check":
		return httpx.Invalid("ends_at", "Окончание должно быть позже начала.")
	case "lessons_module_program":
		return httpx.Invalid("module_id", "Выберите модуль программы этой группы.")
	case "lessons_group_program":
		return httpx.Invalid("group_id", "Группа не найдена.")
	case "lessons_teacher_id_fkey":
		return httpx.Invalid("teacher_id", "Педагог не найден.")
	}
	if pg.Code == "40001" || pg.Code == "40P01" {
		return httpx.E(409, "concurrent_change", "Данные изменились одновременно. Повторите операцию.")
	}
	return err
}
