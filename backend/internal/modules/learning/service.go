package learning

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

var Kinds = []string{"goals", "observations", "assignments", "attempts", "projects", "achievements"}

func validKind(kind string) bool {
	for _, v := range Kinds {
		if v == kind {
			return true
		}
	}
	return false
}
func validate(in *Input) error {
	if !validKind(in.Kind) {
		return httpx.Invalid("kind", "Неизвестный образовательный раздел.")
	}
	c := &in.Content
	if (in.Kind == "observations" || in.Kind == "achievements") && c.OccurredOn > time.Now().In(time.FixedZone("Europe/Moscow", 10800)).Format("2006-01-02") {
		return httpx.Invalid("occurred_on", "Дата факта не может быть в будущем.")
	}
	c.Title = strings.TrimSpace(c.Title)
	if utf8.RuneCountInString(c.Title) < 1 || utf8.RuneCountInString(c.Title) > 2000 {
		return httpx.Invalid("title", "Название или наблюдаемый результат: 1–2000 символов.")
	}
	for _, v := range []string{c.Title, c.TemplateKey, c.Fact, c.Interpretation, c.Context, c.Instruction, c.Criteria, c.Answer, c.Contribution, in.Reason, c.ModelVersion, c.SkillName, c.SkillKey, c.Channel, c.Sender} {
		if utf8.RuneCountInString(v) > 10000 || strings.ContainsRune(v, 0) {
			return httpx.Invalid("content", "Не более 10000 символов в поле.")
		}
	}
	for _, d := range []string{c.OccurredOn, c.StartsOn, c.EndsOn, c.DueOn} {
		if d != "" {
			v, e := time.Parse("2006-01-02", d)
			if e != nil || v.Year() < 1 {
				return httpx.Invalid("date", "Укажите существующую календарную дату.")
			}
		}
	}
	if c.EndsOn != "" && (c.StartsOn == "" || c.EndsOn < c.StartsOn) {
		return httpx.Invalid("ends_on", "Конец участия не раньше начала.")
	}
	require := func(ok bool, field, msg string) error {
		if !ok {
			return httpx.Invalid(field, msg)
		}
		return nil
	}
	var err error
	switch in.Kind {
	case "observations":
		err = require(strings.TrimSpace(c.Fact) != "" && c.OccurredOn != "" && c.Context != "" && c.ModelVersion != "" && c.SkillKey != "" && c.SkillName != "", "fact", "Укажите факт, дату, контекст, навык, ключ и версию модели.")
	case "goals":
		err = require(c.DueOn != "" && c.ModelVersion != "" && c.SkillKey != "", "due_on", "Для цели нужны срок, ключ навыка и версия модели.")
	case "assignments":
		err = require(c.Instruction != "" && c.Criteria != "" && c.DueOn != "" && c.AllowLate != nil && c.ProgramVersionID != nil && c.TemplateKey != "" && (c.Format == "text" || c.Format == "link" || c.Format == "file"), "instruction", "Укажите инструкцию, критерии, срок, политику опоздания, шаблон опубликованной программы и формат text/link/file.")
	case "attempts":
		err = require(in.ParentID != nil && c.Channel != "" && c.Sender != "", "parent_id", "Для попытки нужны назначение, канал поступления и подлинный отправитель.")
	case "projects":
		err = require(c.StartsOn != "" && c.Contribution != "" && c.Criteria != "", "contribution", "Укажите начало участия, индивидуальный вклад и критерии его проверки.")
	case "achievements":
		err = require(c.OccurredOn != "" && len(in.Sources) > 0, "sources", "Для достижения нужны дата и принятая попытка или вклад.")
	}
	if err != nil {
		return err
	}
	if len(in.FileIDs) > 10 || len(in.Sources) > 20 {
		return httpx.Invalid("sources", "Не более 10 файлов и 20 оснований.")
	}
	return nil
}
func (s *Service) Create(ctx context.Context, student int64, in Input) (Card, error) {
	if err := validate(&in); err != nil {
		return Card{}, err
	}
	return s.Repo.Create(ctx, student, in)
}
func (s *Service) Get(ctx context.Context, id int64, revision int) (Card, error) {
	return s.Repo.Get(ctx, id, revision)
}
func (s *Service) List(ctx context.Context, student int64, f Filter) (Block, error) {
	return s.Repo.List(ctx, student, f)
}
func (s *Service) Action(ctx context.Context, id int64, in ActionInput) (Card, error) {
	if in.Revision < 1 || utf8.RuneCountInString(in.Reason) > 10000 || strings.ContainsRune(in.Reason, 0) {
		return Card{}, httpx.Invalid("revision", "Укажите текущую редакцию и допустимый текст причины.")
	}
	return s.Repo.Action(ctx, id, in)
}
func (s *Service) Correct(ctx context.Context, id int64, in Input) (Card, error) {
	if err := validate(&in); err != nil {
		return Card{}, err
	}
	if in.Revision < 1 || strings.TrimSpace(in.Reason) == "" {
		return Card{}, httpx.Invalid("reason", "Укажите текущую редакцию и причину исправления.")
	}
	return s.Repo.Correct(ctx, id, in)
}
func validateAnswer(format, answer string, files int) bool {
	switch format {
	case "text":
		return strings.TrimSpace(answer) != ""
	case "link":
		u, e := url.Parse(answer)
		return e == nil && u.Host != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https")
	case "file":
		return files > 0
	}
	return false
}
