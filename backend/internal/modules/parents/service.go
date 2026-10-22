package parents

import (
	"aiq.local/core/internal/domain/people"
	"aiq.local/core/internal/modules/teachers"
	"aiq.local/core/internal/modules/users"
	"aiq.local/core/internal/platform/httpx"
	"context"
	"strings"
)

type Service struct{ Repo *Repository }

func normalizeParent(in *ParentInput) error {
	if err := people.CardFields(&in.Name, &in.Status); err != nil {
		return err
	}
	var err error
	if in.Phone, err = teachers.NormalizePhone(in.Phone); err != nil {
		return err
	}
	in.Email = strings.TrimSpace(in.Email)
	if in.Email != "" {
		if in.Email, err = users.NormalizeEmail(in.Email); err != nil {
			return err
		}
	}
	if in.Phone == "" && in.Email == "" {
		return httpx.Invalid("phone", "Укажите хотя бы один контакт: телефон или email.")
	}
	return nil
}

func (s *Service) Parents(ctx context.Context, f Filter) (Page[Parent], error) {
	v, err := s.Repo.Parents(ctx, f)
	return v, people.Translate(err)
}

func (s *Service) Parent(ctx context.Context, id int64) (ParentCard, error) {
	v, err := s.Repo.Parent(ctx, id)
	return v, people.Translate(err)
}

func (s *Service) SaveParent(ctx context.Context, id int64, in ParentInput, patch *ParentUpdate) (Parent, error) {
	v, err := s.Repo.SaveParent(ctx, id, in, patch)
	return v, people.Translate(err)
}

func (s *Service) ContactMatches(ctx context.Context, in ContactMatchInput, f Filter) (Page[ContactMatch], error) {
	v, err := s.Repo.ContactMatches(ctx, in, f)
	return v, people.Translate(err)
}

// Preparation is called after the repository locks the existing row, so PATCH
// validates the final record and persists it in the same transaction.
func prepareParent(old Parent, in ParentInput, patch *ParentUpdate) (ParentInput, string, error) {
	var err error
	action := "parent.created"
	if patch != nil {
		in = ParentInput{Name: old.Name, Phone: old.Phone, Email: old.Email, Status: old.Status}
		if !patch.Name.Set && !patch.Phone.Set && !patch.Email.Set && !patch.Status.Set {
			return in, "", httpx.E(400, "empty_update", "Укажите поля для изменения.")
		}
		for _, f := range []struct {
			field    Optional[string]
			target   *string
			required bool
			key      string
		}{{patch.Name, &in.Name, true, "name"}, {patch.Phone, &in.Phone, false, "phone"}, {patch.Email, &in.Email, false, "email"}, {patch.Status, &in.Status, true, "status"}} {
			if err = people.PatchText(f.field, f.target, f.required, f.key); err != nil {
				return in, "", err
			}
		}
		action = "parent.updated"
		if in.Status != old.Status {
			if in.Status == "archived" {
				action = "parent.archived"
			} else {
				action = "parent.restored"
			}
		}
	}
	if err = normalizeParent(&in); err != nil {
		return in, "", err
	}

	return in, action, nil
}
