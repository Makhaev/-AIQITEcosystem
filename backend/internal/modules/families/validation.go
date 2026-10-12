package families

import (
	"aiq.local/core/internal/modules/teachers"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/httpx"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"time"
	"unicode/utf8"
)

func cardFields(name, status *string) error {
	*name = strings.TrimSpace(*name)
	if n := utf8.RuneCountInString(*name); n < 1 || n > 200 {
		return httpx.Invalid("name", "ФИО: от 1 до 200 символов.")
	}
	if *status == "" {
		*status = "active"
	}
	if *status != "active" && *status != "archived" {
		return httpx.Invalid("status", "Допустимые статусы: active и archived.")
	}
	return nil
}
func normalizeStudent(in *StudentInput) error {
	if err := cardFields(&in.Name, &in.Status); err != nil {
		return err
	}
	if in.CuratorID != nil && *in.CuratorID <= 0 {
		return httpx.Invalid("curator_id", "Некорректный куратор.")
	}
	if in.BirthDate != nil {
		date, err := time.Parse("2006-01-02", *in.BirthDate)
		today := time.Now().In(time.FixedZone("Europe/Moscow", 3*60*60)).Format("2006-01-02")
		if err != nil || date.Year() < 1 || *in.BirthDate > today {
			return httpx.Invalid("birth_date", "Укажите существующую дату рождения, не позднее сегодняшней даты.")
		}
	}
	return nil
}
func normalizeParent(in *ParentInput) error {
	if err := cardFields(&in.Name, &in.Status); err != nil {
		return err
	}
	var err error
	if in.Phone, err = teachers.NormalizePhone(in.Phone); err != nil {
		return err
	}
	in.Email = strings.TrimSpace(in.Email)
	if in.Email != "" {
		if in.Email, err = users.NormalizeEmail(in.Email); err != nil {
			return err
		}
	}
	if in.Phone == "" && in.Email == "" {
		return httpx.Invalid("phone", "Укажите хотя бы один контакт: телефон или email.")
	}
	return nil
}
func relationship(value string) (string, error) {
	value = strings.TrimSpace(value)
	if n := utf8.RuneCountInString(value); n < 1 || n > 100 {
		return "", httpx.Invalid("relationship", "Тип представительства: от 1 до 100 символов.")
	}
	return value, nil
}
func patchText(f Optional[string], target *string, required bool, key string) error {
	if !f.Set {
		return nil
	}
	if f.Value == nil {
		if required {
			return httpx.Invalid(key, "Поле обязательно.")
		}
		*target = ""
	} else {
		*target = *f.Value
	}
	if required && *target == "" {
		return httpx.Invalid(key, "Поле обязательно.")
	}
	return nil
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Карточка или связь не найдена.")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		if pg.ConstraintName == "student_parents_unique" {
			return httpx.E(409, "link_exists", "Этот родитель уже связан с учеником.")
		}
		if pg.ConstraintName == "student_parents_one_primary" {
			return httpx.E(409, "primary_contact_exists", "У ученика уже есть основной контакт. Сначала снимите этот признак у текущего контакта.")
		}
	}
	return err
}
