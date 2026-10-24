package progress

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"strings"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func (s *Service) Report(ctx context.Context, id int64) (Reports, error) {
	return s.Repo.Report(ctx, id)
}
func (s *Service) Overview(ctx context.Context, id, assignment int64) (Overview, error) {
	return s.Repo.Overview(ctx, id, assignment)
}
func (s *Service) SaveReport(ctx context.Context, id int64, in ReportInput) (Report, error) {
	in.Outcome = strings.TrimSpace(in.Outcome)
	in.Comment = strings.TrimSpace(in.Comment)
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Revision < 0 || in.State != "draft" && in.State != "submitted" {
		return Report{}, httpx.Invalid("state", "Укажите черновик или отправленный отчёт и текущую редакцию.")
	}
	if in.PlanResult != "complete" && in.PlanResult != "partial" && in.PlanResult != "not_completed" {
		return Report{}, httpx.Invalid("plan_result", "Укажите выполнение плана.")
	}
	if in.State == "submitted" && in.Outcome == "" {
		return Report{}, httpx.Invalid("outcome", "Опишите фактический результат занятия.")
	}
	if in.PlanResult == "partial" && in.Comment == "" {
		return Report{}, httpx.Invalid("comment", "При частичном выполнении нужен комментарий.")
	}
	if in.ConfirmElement && in.PlanResult == "not_completed" {
		return Report{}, httpx.Invalid("confirm_element", "Невыполненный план нельзя подтвердить.")
	}
	for _, v := range []string{in.Outcome, in.Comment, in.Reason} {
		if utf8.RuneCountInString(v) > 10000 || strings.ContainsRune(v, 0) {
			return Report{}, httpx.Invalid("text", "Не более 10000 символов в поле.")
		}
	}
	return s.Repo.SaveReport(ctx, id, in)
}
