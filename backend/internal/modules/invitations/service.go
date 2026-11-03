package invitations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/database"
	"aiq.local/core/internal/platform/httpx"
)

type Sender interface {
	Ready() error
	Send(context.Context, string, string) error
}
type Service struct {
	Repo   *Repository
	Sender Sender
	Origin string
}

func invalid() error {
	return httpx.E(410, "invitation_invalid", "Приглашение недействительно, истекло или уже использовано. Обратитесь к администратору.")
}
func tokenHash(token string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != token {
		return nil, invalid()
	}
	hash := sha256.Sum256([]byte(token))
	return hash[:], nil
}
func (s *Service) Issue(ctx context.Context, in Input, resendID int64) (Invitation, error) {
	if s.Sender == nil {
		return Invitation{}, httpx.E(503, "mail_unavailable", "Отправка приглашений не настроена.")
	}
	if err := s.Sender.Ready(); err != nil {
		return Invitation{}, err
	}
	if resendID == 0 {
		var err error
		in.Email, err = users.NormalizeEmail(in.Email)
		if err != nil {
			return Invitation{}, err
		}
		if in.Role != "parent" && in.Role != "student" {
			return Invitation{}, httpx.Invalid("role", "Выберите родителя или ученика.")
		}
		if in.CardID < 1 {
			return Invitation{}, httpx.Invalid("card_id", "Выберите существующую карточку.")
		}
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return Invitation{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(bytes)
	hash, _ := tokenHash(token)
	inv, err := s.Repo.Issue(ctx, in, resendID, hash)
	if err != nil {
		return inv, database.Translate(err)
	}
	// Only the hash is persisted. The capability is sent in a URL fragment, never
	// in API URLs, audit values, or the administrator's list response.
	if err = s.Sender.Send(ctx, inv.Email, strings.TrimSuffix(s.Origin, "/")+"/#/invitation?token="+token); err != nil {
		// SMTP delivery can be uncertain. Invalidate the capability on failure.
		if revokeErr := s.Repo.DeliveryFailed(ctx, inv.ID); revokeErr != nil {
			return Invitation{}, revokeErr
		}
		return Invitation{}, httpx.E(502, "mail_delivery_failed", "Не удалось подтвердить отправку письма. Ссылка отозвана; повторите приглашение после проверки почты.")
	}
	return inv, nil
}
func (s *Service) Inspect(ctx context.Context, token string) (Preview, error) {
	hash, err := tokenHash(token)
	if err != nil {
		return Preview{}, err
	}
	return s.Repo.Inspect(ctx, hash)
}
func (s *Service) Accept(ctx context.Context, in AcceptInput) (Result, error) {
	hash, err := tokenHash(in.Token)
	if err != nil {
		return Result{}, err
	}
	passwordHash := ""
	if httpx.CurrentActor(ctx).ID == 0 {
		preview, e := s.Repo.Inspect(ctx, hash)
		if e != nil {
			return Result{}, e
		}
		if preview.RequiresLogin {
			return Result{}, httpx.E(401, "invitation_login_required", "Войдите в существующий аккаунт с email приглашения.")
		}
		passwordHash, err = users.HashPassword(in.Password)
		if err != nil {
			return Result{}, err
		}
	} else if in.Password != "" {
		return Result{}, httpx.Invalid("password", "Пароль существующего аккаунта не изменяется при принятии приглашения.")
	}
	result, err := s.Repo.Accept(ctx, hash, passwordHash)
	return result, database.Translate(err)
}
func (s *Service) List(ctx context.Context, page int, sizes ...int) (Page, error) {
	return s.Repo.List(ctx, page, sizes...)
}
func (s *Service) Revoke(ctx context.Context, id int64) error { return s.Repo.Revoke(ctx, id) }
func (s *Service) Links(ctx context.Context) ([]Link, error)  { return s.Repo.Links(ctx) }
