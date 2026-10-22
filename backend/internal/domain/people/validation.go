package people

import (
	"aiq.local/core/internal/platform/httpx"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"unicode/utf8"
)

func CardFields(name, status *string) error {
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

func Relationship(value string) (string, error) {
	value = strings.TrimSpace(value)
	if n := utf8.RuneCountInString(value); n < 1 || n > 100 {
		return "", httpx.Invalid("relationship", "Тип представительства: от 1 до 100 символов.")
	}
	return value, nil
}

func PatchText(f Optional[string], target *string, required bool, key string) error {
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

func Translate(err error) error {
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
