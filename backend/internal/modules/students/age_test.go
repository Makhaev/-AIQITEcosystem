package students

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgeCalendarAndMoscowBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, birth, now string
		want             int
	}{
		{"day before birthday", "2013-05-10", "2026-05-09T20:59:59Z", 12},
		{"birthday at Moscow midnight", "2013-05-10", "2026-05-09T21:00:00Z", 13},
		{"day after birthday", "2013-05-10", "2026-05-10T21:00:00Z", 13},
		{"before new year birthday", "2013-01-01", "2025-12-31T20:59:59Z", 12},
		{"new year birthday", "2013-01-01", "2025-12-31T21:00:00Z", 13},
		{"new year without birthday", "2013-12-31", "2025-12-31T21:00:00Z", 12},
		{"leap birthday on non-leap February 28", "2012-02-29", "2025-02-28T20:59:59Z", 12},
		{"leap birthday on non-leap March 1", "2012-02-29", "2025-02-28T21:00:00Z", 13},
		{"before leap birthday", "2012-02-29", "2024-02-28T20:59:59Z", 11},
		{"leap birthday", "2012-02-29", "2024-02-28T21:00:00Z", 12},
		{"after leap birthday", "2012-02-29", "2024-02-29T21:00:00Z", 12},
		{"century is not a leap year", "2000-02-29", "2100-02-28T20:59:59Z", 99},
		{"century March 1", "2000-02-29", "2100-02-28T21:00:00Z", 100},
		{"born today", "2026-05-10", "2026-05-09T21:00:00Z", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tt.now)
			if err != nil {
				t.Fatal(err)
			}
			age := ageOn(&tt.birth, now)
			if age == nil || *age != tt.want {
				t.Fatalf("birth=%s now=%s age=%v expected=%d", tt.birth, tt.now, age, tt.want)
			}
		})
	}
}

func TestAgeIndependentOfServerTimeZone(t *testing.T) {
	previous := time.Local
	t.Cleanup(func() { time.Local = previous })
	birth := "2013-05-10"
	for _, zone := range []*time.Location{time.UTC, time.FixedZone("UTC-8", -8*60*60), time.FixedZone("UTC+12", 12*60*60)} {
		t.Run(zone.String(), func(t *testing.T) {
			time.Local = zone
			midnight := time.Date(2026, 5, 9, 21, 0, 0, 0, time.UTC)
			for _, tc := range []struct {
				now  time.Time
				want int
			}{{midnight.Add(-time.Second), 12}, {midnight, 13}} {
				age := ageOn(&birth, tc.now.Local())
				if age == nil || *age != tc.want {
					t.Fatalf("server=%s now=%s age=%v expected=%d", zone, tc.now.Local(), age, tc.want)
				}
			}
		})
	}
}

func TestAgeUnknown(t *testing.T) {
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	if ageOn(nil, now) != nil {
		t.Fatal("missing birth date must have no age")
	}
	for _, birth := range []string{"", "invalid", "2025-02-29", "2026-05-11"} {
		if ageOn(&birth, now) != nil {
			t.Fatalf("invalid or future birth date %q must have no age", birth)
		}
	}
}

func TestStudentOverviewAgeJSON(t *testing.T) {
	zero, thirteen := 0, 13
	for _, tc := range []struct {
		name string
		age  *StudentAge
		want string
	}{
		{"restricted", nil, ""},
		{"unknown", &StudentAge{}, "null"},
		{"newborn", &StudentAge{Age: &zero}, "0"},
		{"known", &StudentAge{Age: &thirteen}, "13"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(StudentOverview{StudentAge: tc.age})
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if got := string(fields["age"]); got != tc.want {
				t.Fatalf("age JSON=%q expected=%q", got, tc.want)
			}
		})
	}
}
