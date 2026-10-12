package programs

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func content(name, description *string) error {
	*name = strings.TrimSpace(*name)
	*description = strings.TrimSpace(*description)
	if n := utf8.RuneCountInString(*name); n < 1 || n > 200 {
		return httpx.Invalid("name", "Название: от 1 до 200 символов.")
	}
	if utf8.RuneCountInString(*description) > 5000 {
		return httpx.Invalid("description", "Описание: не более 5000 символов.")
	}
	if strings.ContainsRune(*name, 0) || strings.ContainsRune(*description, 0) {
		return httpx.Invalid("name", "Текст содержит недопустимый символ.")
	}
	return nil
}
func normalize(in *CreateInput) error {
	if err := content(&in.Name, &in.Description); err != nil {
		return err
	}
	if in.Status == "" {
		in.Status = "draft"
	}
	if in.Status != "draft" && in.Status != "active" && in.Status != "archived" {
		return httpx.Invalid("status", "Допустимые статусы: draft, active и archived.")
	}
	return nil
}
func normalizeModule(in *ModuleInput) error {
	if err := content(&in.Name, &in.Description); err != nil {
		return err
	}
	if in.Position < 1 || int64(in.Position) > 2147483647 {
		return httpx.Invalid("position", "Порядковый номер: целое число от 1 до 2147483647.")
	}
	return nil
}
func patchText(field Optional[string], target *string, required bool, key string) error {
	if !field.Set {
		return nil
	}
	if field.Value == nil {
		if required {
			return httpx.Invalid(key, "Поле обязательно.")
		}
		*target = ""
	} else {
		*target = *field.Value
	}
	if required && *target == "" {
		return httpx.Invalid(key, "Поле обязательно.")
	}
	return nil
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Программа или модуль не найдены.")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "program_modules_position_unique" {
		return httpx.E(409, "module_position_exists", "В этой программе уже есть модуль с таким номером. Выберите свободный номер.")
	}
	return err
}
func (s *Service) Create(ctx context.Context, in CreateInput) (Program, error) {
	if err := normalize(&in); err != nil {
		return Program{}, err
	}
	v, e := s.Repo.Save(ctx, 0, in, nil)
	return v, translate(e)
}
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (Program, error) {
	v, e := s.Repo.Save(ctx, id, CreateInput{}, &in)
	return v, translate(e)
}
func (s *Service) Get(ctx context.Context, id int64) (Card, error) {
	v, e := s.Repo.Get(ctx, id)
	return v, translate(e)
}
func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	f.Search = strings.ToLower(strings.TrimSpace(f.Search))
	if f.Page < 1 || f.Page > 1000000 || f.Size < 1 || f.Size > 100 {
		return Page{}, httpx.E(400, "invalid_pagination", "Проверьте параметры страницы.")
	}
	if utf8.RuneCountInString(f.Search) > 200 || strings.ContainsRune(f.Search, 0) {
		return Page{}, httpx.Invalid("search", "Поиск: не более 200 символов без нулевых символов.")
	}
	if f.Status != "" && f.Status != "draft" && f.Status != "active" && f.Status != "archived" {
		return Page{}, httpx.Invalid("status", "Допустимые статусы: draft, active и archived.")
	}
	v, e := s.Repo.List(ctx, f)
	return v, translate(e)
}
func (s *Service) CreateModule(ctx context.Context, id int64, in ModuleInput) (Module, error) {
	if err := normalizeModule(&in); err != nil {
		return Module{}, err
	}
	v, e := s.Repo.SaveModule(ctx, id, 0, in, nil)
	return v, translate(e)
}
func (s *Service) UpdateModule(ctx context.Context, id, moduleID int64, in ModuleUpdate) (Module, error) {
	v, e := s.Repo.SaveModule(ctx, id, moduleID, ModuleInput{}, &in)
	return v, translate(e)
}
func (s *Service) GetModule(ctx context.Context, id, moduleID int64) (Module, error) {
	v, e := s.Repo.GetModule(ctx, id, moduleID)
	return v, translate(e)
}
