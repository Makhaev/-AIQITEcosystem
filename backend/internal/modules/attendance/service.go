package attendance

import (
	"aiq.local/core/internal/platform/httpx"
	"aiq.local/core/internal/platform/lessonstore"
	"context"
	"strings"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func validateMarks(in *MarkInput) error {
	if in.Items == nil {
		return httpx.Invalid("items", "Передайте отметки посещаемости.")
	}
	seen := map[int64]bool{}
	for i := range in.Items {
		item := &in.Items[i]
		item.Comment = strings.TrimSpace(item.Comment)
		if item.StudentID <= 0 || seen[item.StudentID] {
			return httpx.Invalid("items", "Проверьте идентификаторы: ученики не должны повторяться.")
		}
		seen[item.StudentID] = true
		if item.Status != "unmarked" && item.Status != "present" && item.Status != "absent" && item.Status != "late" && item.Status != "excused" {
			return httpx.Invalid("items", "Недопустимая отметка посещаемости.")
		}
		if utf8.RuneCountInString(item.Comment) > 500 || strings.ContainsRune(item.Comment, 0) {
			return httpx.Invalid("items", "Комментарий: не более 500 символов.")
		}
	}
	return nil
}

func (s *Service) Journal(ctx context.Context, id int64) (Journal, error) {
	v, e := s.Repo.Journal(ctx, id)
	return v, lessonstore.Translate(e)
}
func (s *Service) CorrectRoster(ctx context.Context, id int64, in RosterInput) (Journal, error) {
	v, e := s.Repo.CorrectRoster(ctx, id, in)
	return v, lessonstore.Translate(e)
}
func (s *Service) Mark(ctx context.Context, id int64, in MarkInput) error {
	if e := validateMarks(&in); e != nil {
		return e
	}
	return lessonstore.Translate(s.Repo.Mark(ctx, id, in))
}
