package lessons

import (
	"aiq.local/core/internal/platform/lessonstore"
	"context"
)

type Service struct{ Repo *Repository }

func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	v, e := s.Repo.List(ctx, f)
	return v, lessonstore.Translate(e)
}

func (s *Service) Get(ctx context.Context, id int64) (Lesson, error) {
	v, e := s.Repo.Get(ctx, id)
	return v, lessonstore.Translate(e)
}

func (s *Service) Save(ctx context.Context, id int64, in Input, patch *Update) (Lesson, error) {
	v, e := s.Repo.Save(ctx, id, in, patch)
	return v, lessonstore.Translate(e)
}

func (s *Service) Complete(ctx context.Context, id int64) (Lesson, error) {
	v, e := s.Repo.Complete(ctx, id)
	return v, lessonstore.Translate(e)
}

func (s *Service) Reschedule(ctx context.Context, id int64, in RescheduleInput) (Lesson, error) {
	v, e := s.Repo.Reschedule(ctx, id, in)
	return v, lessonstore.Translate(e)
}

func (s *Service) CorrectSnapshot(ctx context.Context, id int64, in SnapshotInput) (Lesson, error) {
	v, e := s.Repo.CorrectSnapshot(ctx, id, in)
	return v, lessonstore.Translate(e)
}

func (s *Service) SnapshotHistory(ctx context.Context, id int64, page, size int) (SnapshotPage, error) {
	v, e := s.Repo.SnapshotHistory(ctx, id, page, size)
	return v, lessonstore.Translate(e)
}
