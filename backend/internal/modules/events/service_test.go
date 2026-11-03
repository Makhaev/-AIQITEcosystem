package events

import (
	"strings"
	"testing"
	"time"
)

func TestEventValidation(t *testing.T) {
	stamp := func(raw string) *time.Time {
		v, e := time.Parse(time.RFC3339, raw)
		if e != nil {
			t.Fatal(e)
		}
		return &v
	}
	base := Input{Title: "Мастер-класс", Type: "Встреча", Description: "Описание", Organizer: "Центр", ResponsibleIDs: []int64{1}, StartsAt: stamp("2027-03-28T10:00:00+02:00"), EndsAt: stamp("2027-03-28T12:00:00+02:00"), TimeZone: "Europe/Berlin", Location: "Зал", Capacity: 20, RegistrationStartsAt: stamp("2027-03-01T09:00:00+01:00"), RegistrationEndsAt: stamp("2027-03-28T09:00:00+02:00"), Conditions: "Для всех"}
	if err := validate(&base, true); err != nil {
		t.Fatal(err)
	}
	draft := Input{Title: " Draft "}
	if err := validate(&draft, false); err != nil || draft.Title != "Draft" || draft.TimeZone != "Europe/Moscow" {
		t.Fatal(draft, err)
	}
	if err := validate(&draft, true); err == nil {
		t.Fatal("incomplete publication accepted")
	}
	for name, change := range map[string]func(*Input){
		"empty title": func(v *Input) { v.Title = " " }, "long title": func(v *Input) { v.Title = strings.Repeat("я", 201) },
		"nul": func(v *Input) { v.Description = "\x00" }, "unknown zone": func(v *Input) { v.TimeZone = "Europe/Nowhere" }, "local zone": func(v *Input) { v.TimeZone = "Local" },
		"end order": func(v *Input) { v.EndsAt = v.StartsAt }, "window order": func(v *Input) { v.RegistrationStartsAt = v.RegistrationEndsAt },
		"late registration": func(v *Input) { v.RegistrationEndsAt = v.EndsAt }, "zero capacity": func(v *Input) { v.Capacity = 0 }, "large capacity": func(v *Input) { v.Capacity = 1000001 },
		"no staff": func(v *Input) { v.ResponsibleIDs = nil }, "duplicate staff": func(v *Input) { v.ResponsibleIDs = []int64{2, 2} }, "negative staff": func(v *Input) { v.ResponsibleIDs = []int64{-1} },
		"missing start": func(v *Input) { v.StartsAt = nil }, "missing description": func(v *Input) { v.Description = "" },
	} {
		t.Run(name, func(t *testing.T) {
			v := base
			v.ResponsibleIDs = append([]int64{}, base.ResponsibleIDs...)
			change(&v)
			if validate(&v, true) == nil {
				t.Fatal("invalid value accepted")
			}
		})
	}
}
