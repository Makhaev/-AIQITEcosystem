package students

import (
	"net/url"
	"testing"
	"time"
)

func TestEducationPeriod(t *testing.T) {
	now := time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC) // November in Moscow.
	for _, tc := range []struct{ query, from, end string }{
		{"", "2026-11-01T00:00:00+03:00", "2026-12-01T00:00:00+03:00"},
		{"education_from=2024-02-29&education_to=2024-02-29", "2024-02-29T00:00:00+03:00", "2024-03-01T00:00:00+03:00"},
		{"from=2000-01-01&to=2000-01-31", "2026-11-01T00:00:00+03:00", "2026-12-01T00:00:00+03:00"},
	} {
		q, _ := url.ParseQuery(tc.query)
		start, end, err := educationPeriod(q, now)
		if err != nil || start.Format(time.RFC3339) != tc.from || end.Format(time.RFC3339) != tc.end {
			t.Fatalf("%q: %v %v %v", tc.query, start, end, err)
		}
	}
	for _, query := range []string{
		"education_from=2026-10-01", "education_to=2026-10-31", "education_from=&education_to=",
		"education_from=2026-02-29&education_to=2026-03-01",
		"education_from=2026-10-31&education_to=2026-10-01",
		"education_from=0000-01-01&education_to=2026-10-01",
		"education_from=2026-10-01&education_from=2026-10-02&education_to=2026-10-31",
	} {
		q, _ := url.ParseQuery(query)
		if _, _, err := educationPeriod(q, now); err == nil {
			t.Errorf("accepted %q", query)
		}
	}
}
