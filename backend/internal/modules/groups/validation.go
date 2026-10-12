package groups

import (
	"aiq.local/core/internal/platform/httpx"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"time"
	"unicode/utf8"
)

func normalize(in *CreateInput) error {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 200 || strings.ContainsRune(in.Name, 0) {
		return httpx.Invalid("name", "Название: от 1 до 200 символов.")
	}
	if in.ProgramID <= 0 {
		return httpx.Invalid("program_id", "Выберите программу.")
	}
	if in.TeacherID != nil && *in.TeacherID <= 0 {
		return httpx.Invalid("teacher_id", "Выберите педагога.")
	}
	if in.Capacity < 1 || int64(in.Capacity) > 2147483647 {
		return httpx.Invalid("capacity", "Вместимость: целое число от 1 до 2147483647.")
	}
	if in.Status == "" {
		in.Status = "draft"
	}
	if in.Status != "draft" && in.Status != "active" && in.Status != "archived" {
		return httpx.Invalid("status", "Допустимые статусы: draft, active и archived.")
	}
	return nil
}
func validateDate(value, key string) error {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Year() < 1 || parsed.Format("2006-01-02") != value {
		return httpx.Invalid(key, "Укажите существующую дату в формате YYYY-MM-DD.")
	}
	return nil
}
func patchRequired[T any](field Optional[T], target *T, key string) error {
	if !field.Set {
		return nil
	}
	if field.Value == nil {
		return httpx.Invalid(key, "Поле обязательно.")
	}
	*target = *field.Value
	return nil
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Группа или зачисление не найдены.")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	switch pg.ConstraintName {
	case "group_enrollments_current_unique":
		return httpx.E(409, "already_enrolled", "Ученик уже зачислен в эту группу.")
	case "groups_capacity_exceeded":
		return httpx.E(409, "group_full", "Вместимость группы превышена. Освободите место или увеличьте вместимость.")
	case "groups_program_has_history", "lessons_group_program":
		return httpx.E(409, "group_has_history", "Нельзя менять программу группы с историей зачислений или занятий.")
	case "enrollments_active_group":
		return httpx.E(409, "group_not_active", "Зачисление возможно только в активную группу.")
	case "enrollments_active_student":
		return httpx.E(409, "student_archived", "Архивного ученика нельзя зачислить в группу.")
	case "group_enrollments_student_id_fkey":
		return httpx.Invalid("student_id", "Ученик не найден.")
	case "groups_program_id_fkey":
		return httpx.Invalid("program_id", "Программа не найдена.")
	case "groups_teacher_id_fkey":
		return httpx.Invalid("teacher_id", "Педагог не найден.")
	case "groups_active_program":
		return httpx.E(409, "program_not_active", "Группу можно связать только с активной программой.")
	case "group_enrollments_ends_on_check":
		return httpx.Invalid("ends_on", "Окончание не может быть раньше начала зачисления.")
	}
	if pg.Code == "40001" || pg.Code == "40P01" {
		return httpx.E(409, "concurrent_change", "Данные изменились одновременно. Повторите операцию.")
	}
	return err
}
