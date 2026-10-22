package dashboard

import (
	"net/url"
	"testing"
	"time"
)

func TestPeriodDefaultsUseMoscowMonthAndInclusiveEnd(t *testing.T) {
	// Still September in UTC, already October in Moscow.
	now := time.Date(2026, 9, 30, 21, 30, 0, 0, time.UTC)
	start, end, err := periodFrom(url.Values{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if start.UTC().Format(time.RFC3339) != "2026-09-30T21:00:00Z" || end.UTC().Format(time.RFC3339) != "2026-10-31T21:00:00Z" {
		t.Fatalf("wrong default Moscow month/day: %v, %v", start, end)
	}
	q := url.Values{"date_from": {"2024-02-29"}, "date_to": {"2024-02-29"}}
	start, end, err = periodFrom(q, now)
	if err != nil || end.Sub(start) != 24*time.Hour {
		t.Fatalf("leap day: %v, %v, %v", start, end, err)
	}
}

func TestDefaultMonthLeapAndYearBoundaries(t *testing.T) {
	for _, tc := range []struct{ now, from, to string }{
		{"2024-02-28T23:00:00Z", "2024-02-01", "2024-03-01"},
		{"2026-02-28T20:59:00Z", "2026-02-01", "2026-03-01"},
		{"2100-02-28T12:00:00Z", "2100-02-01", "2100-03-01"},
		{"2026-12-31T20:59:00Z", "2026-12-01", "2027-01-01"},
		{"2026-12-31T21:00:00Z", "2027-01-01", "2027-02-01"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		start, end, err := periodFrom(url.Values{}, now)
		if err != nil || start.Format("2006-01-02") != tc.from || end.Format("2006-01-02") != tc.to {
			t.Fatalf("month %s: %v %v %v", tc.now, start, end, err)
		}
	}
}

func TestCustomPeriodRetainsIndependentMonthDefaults(t *testing.T) {
	now := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, from, to, wantStart, wantEnd string
	}{
		{"empty", "", "", "2026-10-01", "2026-11-01"},
		{"only_from", "2026-09-20", "", "2026-09-20", "2026-11-01"},
		{"only_to", "", "2026-11-03", "2026-10-01", "2026-11-04"},
		{"inclusive_day", "2024-02-29", "2024-02-29", "2024-02-29", "2024-03-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := periodFrom(url.Values{"date_from": {tc.from}, "date_to": {tc.to}}, now)
			if err != nil || start.Format("2006-01-02") != tc.wantStart || end.Format("2006-01-02") != tc.wantEnd {
				t.Fatalf("period=%v..%v err=%v; want %s..%s", start, end, err, tc.wantStart, tc.wantEnd)
			}
			_, offset := start.Zone()
			if start.Location().String() != "Europe/Moscow" || offset != 3*60*60 {
				t.Fatalf("period is not based on Moscow calendar: %v", start)
			}
		})
	}
}
