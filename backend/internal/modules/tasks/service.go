package tasks

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Service struct{ Repo *Repository }

func (s *Service) List(ctx context.Context, f Filter) (Page[Task], error) {
	v, e := s.Repo.List(ctx, f)
	return v, serviceError(e)
}

func (s *Service) Get(ctx context.Context, id int64) (Task, error) {
	v, e := s.Repo.Get(ctx, id)
	return v, serviceError(e)
}

func (s *Service) Assignees(ctx context.Context, f Filter) (Page[Assignee], error) {
	v, e := s.Repo.Assignees(ctx, f)
	return v, serviceError(e)
}

func (s *Service) Save(ctx context.Context, id int64, in Input, patch *Update) (Task, error) {
	v, e := s.Repo.Save(ctx, id, in, patch)
	return v, serviceError(e)
}

func (s *Service) Status(ctx context.Context, id int64, status string) (Task, error) {
	v, e := s.Repo.Status(ctx, id, status)
	return v, serviceError(e)
}

func serviceError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Задача не найдена.")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23503" {
		return httpx.Invalid("object_id", "Связанный объект или сотрудник не найден.")
	}
	return err
}
