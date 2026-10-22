package students

import (
	"aiq.local/core/internal/domain/people"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"time"
)

type Service struct{ Repo *Repository }

func normalizeStudent(in *StudentInput) error {
	if err := people.CardFields(&in.Name, &in.Status); err != nil {
		return err
	}
	if in.CuratorID != nil && *in.CuratorID <= 0 {
		return httpx.Invalid("curator_id", "Некорректный куратор.")
	}
	if in.BirthDate != nil {
		date, err := time.Parse("2006-01-02", *in.BirthDate)
		today := time.Now().In(time.FixedZone("Europe/Moscow", 3*60*60)).Format("2006-01-02")
		if err != nil || date.Year() < 1 || *in.BirthDate > today {
			return httpx.Invalid("birth_date", "Укажите существующую дату рождения, не позднее сегодняшней даты.")
		}
	}
	return nil
}

func (s *Service) Students(ctx context.Context, f Filter) (Page[StudentResponse], error) {
	v, err := s.Repo.Students(ctx, f)
	return v, people.Translate(err)
}

func (s *Service) Student(ctx context.Context, id int64) (StudentCardResponse, error) {
	v, err := s.Repo.Student(ctx, id)
	return v, people.Translate(err)
}

func (s *Service) SaveStudent(ctx context.Context, id int64, in StudentInput, patch *StudentUpdate) (Student, error) {
	v, err := s.Repo.SaveStudent(ctx, id, in, patch)
	return v, people.Translate(err)
}

func (s *Service) SaveLink(ctx context.Context, id int64, in LinkInput, patch *LinkUpdate) (Link, error) {
	v, err := s.Repo.SaveLink(ctx, id, in, patch)
	return v, people.Translate(err)
}

func (s *Service) Student360(ctx context.Context, id int64, start, end time.Time) (StudentOverview, error) {
	v, err := s.Repo.Student360(ctx, id, start, end)
	return v, people.Translate(err)
}

// Preparation is called after the repository locks the existing row, so PATCH
// validates the final record and persists it in the same transaction.
func prepareStudent(old Student, in StudentInput, patch *StudentUpdate) (StudentInput, string, error) {
	var err error
	action := "student.created"
	if patch != nil {
		in = StudentInput{Name: old.Name, BirthDate: old.BirthDate, Status: old.Status, CuratorID: old.CuratorID}
		if !patch.Name.Set && !patch.BirthDate.Set && !patch.Status.Set && !patch.CuratorID.Set {
			return in, "", httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		if err = people.PatchText(patch.Name, &in.Name, true, "name"); err != nil {
			return in, "", err
		}
		if err = people.PatchText(patch.Status, &in.Status, true, "status"); err != nil {
			return in, "", err
		}
		if patch.BirthDate.Set {
			in.BirthDate = patch.BirthDate.Value
		}
		if patch.CuratorID.Set {
			in.CuratorID = patch.CuratorID.Value
		}
		action = "student.updated"
		if in.Status != old.Status {
			if in.Status == "archived" {
				action = "student.archived"
			} else {
				action = "student.restored"
			}
		}
	}
	if err = normalizeStudent(&in); err != nil {
		return in, "", err
	}

	return in, action, nil
}
