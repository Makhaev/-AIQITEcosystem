package events

import (
	"aiq.local/core/internal/platform/httpx"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

type Service struct{ Repo *Repository }

func validate(in *Input, publish bool) error {
	for _, f := range []struct {
		key      string
		value    *string
		max      int
		required bool
	}{
		{"title", &in.Title, 200, true}, {"type", &in.Type, 100, publish}, {"description", &in.Description, 5000, publish},
		{"organizer", &in.Organizer, 200, publish}, {"location", &in.Location, 1000, publish}, {"conditions", &in.Conditions, 5000, publish},
		{"required_documents", &in.RequiredDocuments, 5000, false},
	} {
		*f.value = strings.TrimSpace(*f.value)
		if (f.required && *f.value == "") || utf8.RuneCountInString(*f.value) > f.max || strings.ContainsRune(*f.value, 0) {
			return httpx.Invalid(f.key, "Заполните поле и соблюдайте ограничение длины.")
		}
	}
	in.TimeZone = strings.TrimSpace(in.TimeZone)
	if in.TimeZone == "" {
		in.TimeZone = "Europe/Moscow"
	}
	if len(in.TimeZone) > 100 || (in.TimeZone != "UTC" && !strings.Contains(in.TimeZone, "/")) {
		return httpx.Invalid("time_zone", "Укажите часовой пояс IANA, например Europe/Moscow.")
	}
	if _, err := time.LoadLocation(in.TimeZone); err != nil {
		return httpx.Invalid("time_zone", "Неизвестный часовой пояс.")
	}
	if in.Capacity < 0 || in.Capacity > 1000000 || (publish && in.Capacity == 0) {
		return httpx.Invalid("capacity", "Вместимость: от 1 до 1000000; в черновике можно оставить 0.")
	}
	for _, f := range []struct {
		key   string
		value *time.Time
	}{{"starts_at", in.StartsAt}, {"ends_at", in.EndsAt}, {"registration_starts_at", in.RegistrationStartsAt}, {"registration_ends_at", in.RegistrationEndsAt}} {
		if (publish && f.value == nil) || (f.value != nil && (f.value.IsZero() || f.value.Year() < 1 || f.value.Year() > 9999)) {
			return httpx.Invalid(f.key, "Укажите корректную дату и время.")
		}
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.StartsAt.Before(*in.EndsAt) {
		return httpx.Invalid("ends_at", "Окончание должно быть позже начала.")
	}
	if in.RegistrationStartsAt != nil && in.RegistrationEndsAt != nil && !in.RegistrationStartsAt.Before(*in.RegistrationEndsAt) {
		return httpx.Invalid("registration_ends_at", "Окончание регистрации должно быть позже её начала.")
	}
	if in.RegistrationEndsAt != nil && in.StartsAt != nil && in.RegistrationEndsAt.After(*in.StartsAt) {
		return httpx.Invalid("registration_ends_at", "Регистрация должна закончиться не позже начала события.")
	}
	if len(in.ResponsibleIDs) > 20 || (publish && len(in.ResponsibleIDs) == 0) {
		return httpx.Invalid("responsible_ids", "Назначьте от 1 до 20 ответственных сотрудников.")
	}
	sort.Slice(in.ResponsibleIDs, func(i, j int) bool { return in.ResponsibleIDs[i] < in.ResponsibleIDs[j] })
	for i, id := range in.ResponsibleIDs {
		if id <= 0 || (i > 0 && id == in.ResponsibleIDs[i-1]) {
			return httpx.Invalid("responsible_ids", "Ответственные должны быть уникальными действующими сотрудниками.")
		}
	}
	if in.ResponsibleIDs == nil {
		in.ResponsibleIDs = []int64{}
	}
	return nil
}
func validFilter(f *Filter) error {
	if f.Size < 0 || f.Size > httpx.MaxPageSize {
		return httpx.Invalid("page_size", "Размер страницы: от 1 до 100.")
	}
	f.Size = httpx.PageSize(f.Size)
	f.Search = strings.ToLower(strings.TrimSpace(f.Search))
	if f.Page < 1 || f.Page > 1000000 {
		return httpx.Invalid("page", "Некорректная страница.")
	}
	if utf8.RuneCountInString(f.Search) > 200 || strings.ContainsRune(f.Search, 0) {
		return httpx.Invalid("search", "Поиск: до 200 символов.")
	}
	switch f.Status {
	case "", "draft", "published", "registration_closed", "completed", "cancelled":
		return nil
	}
	return httpx.Invalid("status", "Неизвестное состояние события.")
}
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.E(404, "not_found", "Событие недоступно.")
	}
	return err
}
func (s *Service) List(ctx context.Context, f Filter) (Page[Event], error) {
	if err := validFilter(&f); err != nil {
		return Page[Event]{}, err
	}
	return s.Repo.List(ctx, f)
}
func (s *Service) Staff(ctx context.Context, f Filter) (Page[Person], error) {
	if err := validFilter(&f); err != nil {
		return Page[Person]{}, err
	}
	return s.Repo.Staff(ctx, f)
}
func (s *Service) Get(ctx context.Context, id int64) (Event, error) {
	v, e := s.Repo.Get(ctx, id)
	return v, translate(e)
}
func (s *Service) Save(ctx context.Context, id int64, in Input) (Event, error) {
	if id > 0 && in.Revision < 1 {
		return Event{}, httpx.Invalid("revision", "Укажите текущую редакцию.")
	}
	if err := validate(&in, false); err != nil {
		return Event{}, err
	}
	v, e := s.Repo.Save(ctx, id, in)
	return v, translate(e)
}
func (s *Service) Action(ctx context.Context, id int64, in ActionInput) (Event, error) {
	if in.Revision < 1 {
		return Event{}, httpx.Invalid("revision", "Укажите текущую редакцию.")
	}
	switch in.Action {
	case "publish", "close_registration", "complete", "cancel":
	default:
		return Event{}, httpx.Invalid("action", "Неизвестное действие.")
	}
	v, e := s.Repo.Action(ctx, id, in)
	return v, translate(e)
}
