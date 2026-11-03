package notifications

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"strings"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func (s *Service) Deliveries(ctx context.Context, page int, status string, sizes ...int) (DeliveryPage, error) {
	if page < 1 || page > 1000000 {
		return DeliveryPage{}, httpx.Invalid("page", "Некорректная страница.")
	}
	switch status {
	case "", "pending", "processing", "sent", "failed":
	default:
		return DeliveryPage{}, httpx.Invalid("status", "Неизвестный статус.")
	}
	return s.Repo.Deliveries(ctx, page, status, sizes...)
}
func (s *Service) Retry(ctx context.Context, id int64, in RetryInput) error {
	in.Reason = strings.TrimSpace(in.Reason)
	if id < 1 || in.Revision < 1 {
		return httpx.Invalid("revision", "Укажите текущую редакцию отправки.")
	}
	if in.Reason == "" || utf8.RuneCountInString(in.Reason) > 1000 || strings.ContainsRune(in.Reason, 0) {
		return httpx.Invalid("reason", "Укажите причину повтора: до 1000 символов.")
	}
	if !in.AcknowledgeDuplicateRisk {
		return httpx.Invalid("acknowledge_duplicate_risk", "Подтвердите возможность повторного письма при неопределённом результате провайдера.")
	}
	return s.Repo.Retry(ctx, id, in)
}

func (s *Service) List(ctx context.Context, page int, unread bool, sizes ...int) (Page, error) {
	if page < 1 || page > 1000000 {
		return Page{}, httpx.Invalid("page", "Некорректная страница.")
	}
	return s.Repo.List(ctx, page, unread, sizes...)
}
func (s *Service) Read(ctx context.Context, id int64) error {
	if id < 1 {
		return httpx.Invalid("id", "Некорректное уведомление.")
	}
	return s.Repo.Read(ctx, id)
}
