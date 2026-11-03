package auth

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/logging"
	"golang.org/x/crypto/bcrypt"
)

type ResetSender interface {
	Ready() error
	SendPasswordReset(context.Context, string, string) error
}

func invalidReset() error {
	return httpx.E(400, "reset_invalid", "Ссылка недействительна, истекла или уже использована. Запросите новую ссылку.")
}
func unchangedPassword() error {
	return httpx.Invalid("password", "Новый пароль должен отличаться от текущего.")
}
func resetLimited() error {
	return httpx.E(429, "rate_limited", "Слишком много запросов. Повторите через 30 минут.")
}

func (s *Service) RequestPasswordReset(ctx context.Context, email, ip string) error {
	allowed, err := s.Repo.AllowReset(ctx, "request-ip:"+ip, 20)
	if err != nil {
		return err
	}
	if !allowed {
		return resetLimited()
	}
	normalized, validation := users.NormalizeEmail(email)
	if validation != nil {
		normalized = ""
	}
	allowed, err = s.Repo.AllowReset(ctx, "request-email:"+normalized, 5)
	if err != nil {
		return err
	}
	// Silent email throttling is identical for known, unknown and blocked accounts.
	if !allowed {
		return nil
	}
	select {
	case s.resetJobs <- struct{}{}:
	default:
		return httpx.E(503, "unavailable", "Сервис временно занят. Повторите позже.")
	}
	// No account lookup or SMTP wait on the HTTP response path. Bounded work,
	// independent deadline, and no request body/token in diagnostics.
	go func() {
		defer func() { <-s.resetJobs }()
		deliveryCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if err := s.deliverReset(deliveryCtx, normalized); err != nil {
			logging.Failure("password reset delivery failed", err)
		}
	}()
	return nil
}

func (s *Service) deliverReset(ctx context.Context, email string) error {
	if s.ResetSender == nil {
		return nil
	}
	if err := s.ResetSender.Ready(); err != nil {
		return err
	}
	origin, err := url.Parse(s.ResetOrigin)
	if err != nil || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return httpx.E(503, "mail_unavailable", "Адрес сайта не настроен.")
	}
	token, err := Token()
	if err != nil {
		return err
	}
	issued, err := s.Repo.IssueReset(ctx, email, Hash(token))
	if err != nil || !issued {
		return err
	}
	link := strings.TrimRight(s.ResetOrigin, "/") + "/#/reset-password?token=" + url.QueryEscape(token)
	if err = s.ResetSender.SendPasswordReset(ctx, email, link); err != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if cleanupErr := s.Repo.RevokeReset(cleanup, Hash(token)); cleanupErr != nil {
			logging.Failure("password reset revocation failed", cleanupErr)
		}
		return err
	}
	slog.Info("password reset mail handed off")
	return nil
}

func (s *Service) ResetPassword(ctx context.Context, token, password, ip string) error {
	allowed, err := s.Repo.AllowReset(ctx, "attempt-ip:"+ip, 30)
	if err != nil {
		return err
	}
	if !allowed {
		return resetLimited()
	}
	if !ValidToken(token) {
		return invalidReset()
	}
	allowed, err = s.Repo.AllowReset(ctx, "attempt-token:"+token, 5)
	if err != nil {
		return err
	}
	if !allowed {
		return resetLimited()
	}
	hash, err := users.HashPassword(password)
	if err != nil {
		return err
	}
	return s.Repo.ResetPassword(ctx, Hash(token), hash, func(oldHash string) bool {
		return bcrypt.CompareHashAndPassword([]byte(oldHash), []byte(password)) == nil
	})
}
