package events

import (
	"aiq.local/core/internal/modules/files"
	"aiq.local/core/internal/platform/httpx"
	"context"
)

func validPage(page int) error {
	if page < 1 || page > 1000000 {
		return httpx.Invalid("page", "Некорректная страница.")
	}
	return nil
}
func (s *Service) Register(ctx context.Context, eid, rid int64, in RegistrationInput) (Registration, error) {
	if rid > 0 {
		if in.Revision < 1 || in.StudentID != 0 {
			return Registration{}, httpx.Invalid("revision", "Укажите редакцию заявки; изменение ученика недоступно.")
		}
		switch in.Action {
		case "confirmed", "cancelled", "attended", "no_show":
		default:
			return Registration{}, httpx.Invalid("action", "Неизвестное действие.")
		}
	}
	v, e := s.Repo.Register(ctx, eid, rid, in)
	return v, translate(e)
}
func (s *Service) Registrations(ctx context.Context, eid int64, page int, sizes ...int) (ParticipationPage, error) {
	if e := validPage(page); e != nil {
		return ParticipationPage{}, e
	}
	v, e := s.Repo.Registrations(ctx, eid, page, sizes...)
	return v, translate(e)
}
func (s *Service) Candidates(ctx context.Context, eid int64, f Filter) (Page[Person], error) {
	if e := validFilter(&f); e != nil {
		return Page[Person]{}, e
	}
	v, e := s.Repo.Candidates(ctx, eid, f)
	return v, translate(e)
}
func (s *Service) History(ctx context.Context, eid, rid int64) ([]History, error) {
	v, e := s.Repo.History(ctx, eid, rid)
	return v, translate(e)
}
func (s *Service) HistoryPage(ctx context.Context, eid, rid int64, page, size int) (Page[History], error) {
	v, e := s.Repo.HistoryPage(ctx, eid, rid, page, size)
	return v, translate(e)
}
func (s *Service) Documents(ctx context.Context, eid, rid int64, page int, sizes ...int) (Page[files.File], error) {
	if e := validPage(page); e != nil {
		return Page[files.File]{}, e
	}
	v, e := s.Repo.Documents(ctx, eid, rid, page, sizes...)
	return v, translate(e)
}
func (s *Service) Upload(ctx context.Context, eid, rid int64, in files.Input) (files.File, error) {
	media, _, e := files.ValidateContent(&in)
	if e != nil {
		return files.File{}, e
	}
	v, e := s.Repo.Upload(ctx, eid, rid, in, media)
	return v, translate(e)
}
func (s *Service) Download(ctx context.Context, eid, rid, did int64) (files.File, error) {
	v, e := s.Repo.Download(ctx, eid, rid, did)
	return v, translate(e)
}
