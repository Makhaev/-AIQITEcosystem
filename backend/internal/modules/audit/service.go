package audit

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

type Service struct{ Repo *Repository }

func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	v, e := s.Repo.List(ctx, f)
	return v, serviceError(e)
}

func (s *Service) Get(ctx context.Context, id int64) (Entry, error) {
	v, e := s.Repo.Get(ctx, id)
	return v, serviceError(e)
}

func serviceError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Запись журнала не найдена.")
	}
	return err
}
