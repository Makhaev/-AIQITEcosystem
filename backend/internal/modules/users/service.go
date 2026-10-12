package users

import (
	"context"
	"net/mail"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
	"golang.org/x/crypto/bcrypt"
)

type Service struct{ Repo *Repository }

func NormalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	address, err := mail.ParseAddress(s)
	if err != nil || address.Address != s || len(s) > 254 {
		return "", httpx.Invalid("email", "Укажите корректный email.")
	}
	return s, nil
}
func HashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < 12 || len(password) > 72 {
		return "", httpx.Invalid("password", "Пароль: от 12 символов до 72 байт UTF-8.")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
func ValidateRoles(roles []string) error {
	if len(roles) == 0 {
		return httpx.Invalid("roles", "Выберите хотя бы одну роль.")
	}
	seen := map[string]bool{}
	for _, role := range roles {
		if (role != "admin" && role != "leader" && role != "teacher" && role != "curator") || seen[role] {
			return httpx.Invalid("roles", "Недопустимая или повторная роль.")
		}
		seen[role] = true
	}
	return nil
}
func validateName(name string) error {
	if n := utf8.RuneCountInString(name); n < 1 || n > 200 {
		return httpx.Invalid("name", "Имя: от 1 до 200 символов.")
	}
	return nil
}
func (s *Service) Create(ctx context.Context, in CreateInput) (User, error) {
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		return User{}, err
	}
	in.Email = email
	in.Name = strings.TrimSpace(in.Name)
	if err = validateName(in.Name); err != nil {
		return User{}, err
	}
	if err = ValidateRoles(in.Roles); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	u, err := s.Repo.Create(ctx, in, hash, false)
	return u, database.Translate(err)
}
func (s *Service) Bootstrap(ctx context.Context, in CreateInput) (User, error) {
	in.Roles = []string{"admin"}
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		return User{}, err
	}
	in.Email = email
	in.Name = strings.TrimSpace(in.Name)
	if err = validateName(in.Name); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	u, err := s.Repo.Create(ctx, in, hash, true)
	return u, database.Translate(err)
}
func (s *Service) Update(ctx context.Context, id int64, in UpdateInput) (User, error) {
	if in.Name == nil && in.Roles == nil && in.Blocked == nil && in.Password == nil {
		return User{}, httpx.E(400, "empty_update", "Укажите поля для изменения.")
	}
	if in.Name != nil {
		trimmed := strings.TrimSpace(*in.Name)
		in.Name = &trimmed
		if err := validateName(trimmed); err != nil {
			return User{}, err
		}
	}
	if in.Roles != nil {
		if err := ValidateRoles(in.Roles); err != nil {
			return User{}, err
		}
	}
	var hash *string
	if in.Password != nil {
		h, err := HashPassword(*in.Password)
		if err != nil {
			return User{}, err
		}
		hash = &h
	}
	u, err := s.Repo.Update(ctx, id, in, hash)
	return u, database.Translate(err)
}
func (s *Service) List(ctx context.Context, page, size int, role string) (Page, error) {
	if role != "" {
		if err := ValidateRoles([]string{role}); err != nil {
			return Page{}, httpx.Invalid("role", "Недопустимая роль.")
		}
	}
	return s.Repo.List(ctx, page, size, role)
}
