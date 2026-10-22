package curriculum

import "testing"

func validDocument() Document {
	return Document{Name: "Программа", Sections: []Section{{Key: "s", Name: "Раздел", Modules: []Module{{Key: "m", Name: "Модуль", Lessons: []Template{{Key: "t", Topic: "Тема", DurationMinutes: 45, Goals: "Цель", Outline: "План", Exercises: "Упражнение", Equipment: "Не требуется", Materials: "Конспект", Criteria: "Критерий"}}}}}}}
}
func TestDocumentValidationAndPublicationCompleteness(t *testing.T) {
	for _, tc := range []struct {
		name     string
		edit     func(*Document)
		complete bool
		valid    bool
	}{
		{"complete", func(*Document) {}, true, true},
		{"empty draft", func(d *Document) { d.Sections = nil }, false, true},
		{"empty publication", func(d *Document) { d.Sections = nil }, true, false},
		{"duplicate key", func(d *Document) { d.Sections[0].Modules[0].Key = "s" }, false, false},
		{"missing material", func(d *Document) { d.Sections[0].Modules[0].Lessons[0].Materials = " " }, true, false},
		{"draft material", func(d *Document) { d.Sections[0].Modules[0].Lessons[0].Materials = "" }, false, true},
		{"invalid duration", func(d *Document) { d.Sections[0].Modules[0].Lessons[0].DurationMinutes = 0 }, false, false},
		{"nul", func(d *Document) { d.Name = "Bad\x00Name" }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := validDocument()
			tc.edit(&d)
			e := validateDocument(&d, tc.complete)
			if (e == nil) != tc.valid {
				t.Fatalf("result: %v", e)
			}
		})
	}
}
