package curriculum

import "testing"

func TestProgressWeightValidationDoesNotInventLegacyWeights(t *testing.T) {
	for _, tc := range []struct {
		required bool
		weight   *int
		valid    bool
	}{
		{false, nil, true}, {true, nil, false}, {true, ptr(0), false}, {true, ptr(-1), false},
		{true, ptr(1), true}, {true, ptr(1000), true}, {true, ptr(1001), false}, {false, ptr(1), false},
	} {
		d := Document{Name: "Program", Sections: []Section{{Key: "section", Name: "Section", Modules: []Module{{Key: "module", Name: "Module", Lessons: []Template{{Key: "lesson", Topic: "Lesson", DurationMinutes: 45, Required: tc.required, Weight: tc.weight}}}}}}}
		err := validateDocument(&d, false)
		if (err == nil) != tc.valid {
			t.Fatalf("required=%v weight=%v: %v", tc.required, tc.weight, err)
		}
		if tc.weight == nil && d.Sections[0].Modules[0].Lessons[0].Weight != nil {
			t.Fatal("invented a weight")
		}
	}
}
func ptr(n int) *int { return &n }
