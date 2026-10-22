package groups

import (
	"context"
)

type Service struct{ Repo *Repository }

func (s *Service) List(ctx context.Context, f Filter) (Page, error) {
	v, e := s.Repo.List(ctx, f)
	return v, translate(e)
}

func (s *Service) Get(ctx context.Context, id int64) (Card, error) {
	v, e := s.Repo.Get(ctx, id)
	return v, translate(e)
}

func (s *Service) Save(ctx context.Context, id int64, in CreateInput, patch *UpdateInput) (Group, error) {
	v, e := s.Repo.Save(ctx, id, in, patch)
	return v, translate(e)
}

func (s *Service) Enroll(ctx context.Context, id int64, in EnrollInput) (Enrollment, error) {
	v, e := s.Repo.Enroll(ctx, id, in)
	return v, translate(e)
}

func (s *Service) Close(ctx context.Context, groupID, id int64, in CloseInput) (Enrollment, error) {
	v, e := s.Repo.Close(ctx, groupID, id, in)
	return v, translate(e)
}

func (s *Service) Transfer(ctx context.Context, groupID, id int64, in TransferInput) (TransferResult, error) {
	v, e := s.Repo.Transfer(ctx, groupID, id, in)
	return v, translate(e)
}
