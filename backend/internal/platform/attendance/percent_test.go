package attendance

import (
	"math"
	"testing"
)

func TestPercent(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		present, late, absent, excused int
		want                           float64
	}{
		{"acceptance_six_marks", 3, 1, 1, 1, 200.0 / 3},
		{"present_and_late", 2, 3, 0, 0, 100},
		{"all_absent", 0, 0, 3, 0, 0},
		{"excused_in_denominator", 1, 0, 0, 1, 50},
		{"weighted_marks", 1, 1, 2, 1, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Percent(tc.present, tc.late, tc.absent, tc.excused)
			if got == nil || math.Abs(*got-tc.want) > 1e-12 {
				t.Fatalf("Percent(%d,%d,%d,%d)=%v; want %.15f", tc.present, tc.late, tc.absent, tc.excused, got, tc.want)
			}
		})
	}
}

func TestPercentWithoutMarksIsAbsent(t *testing.T) {
	if got := Percent(0, 0, 0, 0); got != nil {
		t.Fatalf("no marks must return nil, got %v", *got)
	}
}
