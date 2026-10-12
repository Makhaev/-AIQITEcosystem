package teachers

import (
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/access"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func NormalizePhone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > 100 {
		return "", httpx.Invalid("phone", "Телефон: не более 100 символов.")
	}
	if value == "" {
		return "", nil
	}
	var number strings.Builder
	for index, character := range value {
		if character >= '0' && character <= '9' {
			number.WriteRune(character)
		} else if character == '+' && index == 0 {
			number.WriteRune(character)
		} else if character != '(' && character != ')' && character != '-' && !unicode.IsSpace(character) {
			return "", httpx.Invalid("phone", "Укажите телефон в международном формате, например +7 900 123-45-67.")
		}
	}
	phone := number.String()
	if len(phone) == 11 && (phone[0] == '8' || phone[0] == '7') {
		phone = "+7" + phone[1:]
	}
	if len(phone) < 8 || len(phone) > 16 || phone[0] != '+' || phone[1] == '0' {
		return "", httpx.Invalid("phone", "Укажите телефон в международном формате, например +7 900 123-45-67.")
	}
	return phone, nil
}

func normalize(in CreateInput) (CreateInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if size := utf8.RuneCountInString(in.Name); size < 1 || size > 200 {
		return in, httpx.Invalid("name", "ФИО: от 1 до 200 символов.")
	}
	var err error
	if in.Phone, err = NormalizePhone(in.Phone); err != nil {
		return in, err
	}
	if strings.TrimSpace(in.Email) != "" {
		if in.Email, err = users.NormalizeEmail(in.Email); err != nil {
			return in, err
		}
	} else {
		in.Email = ""
	}
	in.Specialization = strings.TrimSpace(in.Specialization)
	if utf8.RuneCountInString(in.Specialization) > 500 {
		return in, httpx.Invalid("specialization", "Специализация: не более 500 символов.")
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "archived" {
		return in, httpx.Invalid("status", "Допустимые статусы: active и archived.")
	}
	if in.UserID != nil && *in.UserID <= 0 {
		return in, httpx.Invalid("user_id", "Некорректная учётная запись.")
	}
	return in, nil
}

func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Педагог не найден.")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "teachers_user_unique" {
		return httpx.E(409, "teacher_user_exists", "Эта учётная запись уже связана с другим педагогом.")
	}
	return err
}

func (s *Service) Create(ctx context.Context, input CreateInput) (Teacher, error) {
	if access.ScopeFor(httpx.CurrentActor(ctx), access.Teachers, access.Manage) != access.All {
		return Teacher{}, httpx.Forbidden()
	}
	in, err := normalize(input)
	if err != nil {
		return Teacher{}, err
	}
	value, err := s.Repo.Create(ctx, in)
	return value, translate(err)
}
func (s *Service) Update(ctx context.Context, id int64, input UpdateInput) (Teacher, error) {
	if access.ScopeFor(httpx.CurrentActor(ctx), access.Teachers, access.Manage) != access.All {
		return Teacher{}, httpx.Forbidden()
	}
	if !input.Name.Set && !input.Phone.Set && !input.Email.Set && !input.Specialization.Set && !input.Status.Set && !input.UserID.Set {
		return Teacher{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
	}
	if input.Name.Set && input.Name.Value == nil {
		return Teacher{}, httpx.Invalid("name", "ФИО обязательно.")
	}
	if input.Status.Set && (input.Status.Value == nil || *input.Status.Value == "") {
		return Teacher{}, httpx.Invalid("status", "Статус обязателен.")
	}
	value, err := s.Repo.Update(ctx, id, input)
	return value, translate(err)
}
func (s *Service) Get(ctx context.Context, id int64) (Teacher, error) {
	value, err := s.Repo.Get(ctx, id)
	return value, translate(err)
}
func (s *Service) List(ctx context.Context, filter Filter) (Page, error) {
	filter.Search = strings.ToLower(strings.TrimSpace(filter.Search))
	if filter.Page < 1 || filter.Page > 1000000 || filter.Size < 1 || filter.Size > 100 {
		return Page{}, httpx.E(400, "invalid_pagination", "Проверьте параметры страницы.")
	}
	if utf8.RuneCountInString(filter.Search) > 200 {
		return Page{}, httpx.Invalid("search", "Поиск: не более 200 символов.")
	}
	if filter.Status != "" && filter.Status != "active" && filter.Status != "archived" {
		return Page{}, httpx.Invalid("status", "Допустимые статусы: active и archived.")
	}
	return s.Repo.List(ctx, filter)
}
