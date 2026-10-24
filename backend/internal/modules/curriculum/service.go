package curriculum

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"aiq.local/core/internal/platform/httpx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Service struct{ Repo *Repository }

var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)

func textField(value *string, field string, min, max int) error {
	*value = strings.TrimSpace(*value)
	n := utf8.RuneCountInString(*value)
	if n < min || n > max || strings.ContainsRune(*value, 0) {
		return httpx.Invalid(field, "Проверьте длину и содержимое поля.")
	}
	return nil
}
func validateDocument(d *Document, complete bool) error {
	if err := textField(&d.Name, "name", 1, 200); err != nil {
		return err
	}
	if err := textField(&d.Description, "description", 0, 5000); err != nil {
		return err
	}
	if d.Sections == nil {
		d.Sections = []Section{}
	}
	if len(d.Sections) > 50 || complete && len(d.Sections) == 0 {
		return httpx.Invalid("sections", "Добавьте разделы программы (не более 50).")
	}
	keys := map[string]bool{}
	checkKey := func(key string) error {
		if !keyPattern.MatchString(key) || keys[key] {
			return httpx.Invalid("key", "Ключи структуры должны быть уникальны: буквы, цифры, _ и -.")
		}
		keys[key] = true
		return nil
	}
	moduleCount, templateCount := 0, 0
	for si := range d.Sections {
		s := &d.Sections[si]
		if err := checkKey(s.Key); err != nil {
			return err
		}
		if err := textField(&s.Name, "section_name", 1, 200); err != nil {
			return err
		}
		if s.Modules == nil {
			s.Modules = []Module{}
		}
		if complete && len(s.Modules) == 0 {
			return httpx.Invalid("modules", "В каждом разделе нужен модуль.")
		}
		for mi := range s.Modules {
			m := &s.Modules[mi]
			moduleCount++
			if err := checkKey(m.Key); err != nil {
				return err
			}
			if err := textField(&m.Name, "module_name", 1, 200); err != nil {
				return err
			}
			if err := textField(&m.Description, "module_description", 0, 5000); err != nil {
				return err
			}
			if m.Lessons == nil {
				m.Lessons = []Template{}
			}
			if complete && len(m.Lessons) == 0 {
				return httpx.Invalid("lessons", "В каждом модуле нужен шаблон занятия.")
			}
			for ti := range m.Lessons {
				t := &m.Lessons[ti]
				if t.Required && (t.Weight == nil || *t.Weight < 1 || *t.Weight > 1000) || !t.Required && t.Weight != nil {
					return httpx.Invalid("weight", "Для обязательного элемента укажите целый вес от 1 до 1000; у необязательного вес отсутствует.")
				}
				templateCount++
				if err := checkKey(t.Key); err != nil {
					return err
				}
				if err := textField(&t.Topic, "topic", 1, 200); err != nil {
					return err
				}
				if t.DurationMinutes < 1 || t.DurationMinutes > 1440 {
					return httpx.Invalid("duration_minutes", "Длительность: от 1 до 1440 минут.")
				}
				min := 0
				if complete {
					min = 1
				}
				for _, f := range []struct {
					key   string
					value *string
				}{
					{"goals", &t.Goals}, {"outline", &t.Outline}, {"exercises", &t.Exercises},
					{"equipment", &t.Equipment}, {"materials", &t.Materials}, {"criteria", &t.Criteria},
				} {
					if err := textField(f.value, f.key, min, 20000); err != nil {
						return err
					}
				}
			}
		}
	}
	if moduleCount > 200 || templateCount > 1000 {
		return httpx.Invalid("sections", "Лимит версии: 200 модулей и 1000 шаблонов.")
	}
	return nil
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Версия или связанный объект не найдены.")
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23514", "23503":
			return httpx.E(409, pg.ConstraintName, "Операция нарушает правила версии или сохранения истории.")
		case "40001", "40P01":
			return httpx.E(409, "concurrent_change", "Данные изменились одновременно. Обновите страницу.")
		case "23505":
			return httpx.E(409, "concurrent_change", "Версия уже создана. Обновите список.")
		}
	}
	return err
}
func (s *Service) List(ctx context.Context, programID int64) (VersionPage, error) {
	v, e := s.Repo.List(ctx, programID)
	return v, translate(e)
}
func (s *Service) Get(ctx context.Context, programID, id int64) (Card, error) {
	v, e := s.Repo.Get(ctx, programID, id)
	return v, translate(e)
}
func (s *Service) Create(ctx context.Context, programID int64, in CreateInput) (Version, error) {
	if in.SourceVersionID != nil && *in.SourceVersionID <= 0 {
		return Version{}, httpx.Invalid("source_version_id", "Выберите исходную версию.")
	}
	v, e := s.Repo.Create(ctx, programID, in)
	return v, translate(e)
}
func (s *Service) Save(ctx context.Context, programID, id int64, in SaveInput) (Version, error) {
	if in.Revision <= 0 {
		return Version{}, httpx.Invalid("revision", "Укажите ревизию документа.")
	}
	if e := validateDocument(&in.Document, false); e != nil {
		return Version{}, e
	}
	v, e := s.Repo.Save(ctx, programID, id, in)
	return v, translate(e)
}
func (s *Service) Action(ctx context.Context, programID, id int64, in ActionInput) (Version, error) {
	if in.Revision <= 0 {
		return Version{}, httpx.Invalid("revision", "Укажите ревизию документа.")
	}
	if in.Action != "submit" && in.Action != "return" && in.Action != "publish" && in.Action != "archive" {
		return Version{}, httpx.Invalid("action", "Неизвестное действие.")
	}
	min := 0
	if in.Action == "return" {
		min = 1
	}
	if e := textField(&in.Comment, "comment", min, 1000); e != nil {
		return Version{}, e
	}
	v, e := s.Repo.Action(ctx, programID, id, in)
	return v, translate(e)
}
