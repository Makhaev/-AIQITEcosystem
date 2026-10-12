package teachers

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTeacherValidation(t *testing.T) {
	value, err := normalize(CreateInput{Name: "  Анна Петрова  ", Phone: "8 (900) 123-45-67", Email: " TEACHER@Example.TEST ", Specialization: "  Мышление  "})
	if err != nil || value.Name != "Анна Петрова" || value.Phone != "+79001234567" || value.Email != "teacher@example.test" || value.Specialization != "Мышление" || value.Status != "active" {
		t.Fatalf("normalization: %+v %v", value, err)
	}
	for _, input := range []CreateInput{{Name: " "}, {Name: strings.Repeat("я", 201)}, {Name: "Teacher", Phone: "123"}, {Name: "Teacher", Phone: "+0123456789"}, {Name: "Teacher", Phone: "+7900a123456"}, {Name: "Teacher", Email: "not-email"}, {Name: "Teacher", Status: "deleted"}, {Name: "Teacher", Specialization: strings.Repeat("я", 501)}} {
		if _, err := normalize(input); err == nil {
			t.Errorf("invalid input accepted: %+v", input)
		}
	}
	for _, phone := range []string{"", "+1 (202) 555-0123", "+966 50 123 4567", "79001234567"} {
		if _, err := NormalizePhone(phone); err != nil {
			t.Errorf("phone rejected: %q", phone)
		}
	}
}
func TestPatchDistinguishesMissingNullAndValues(t *testing.T) {
	var input UpdateInput
	if err := json.Unmarshal([]byte(`{"user_id":null,"phone":null,"name":"Teacher"}`), &input); err != nil {
		t.Fatal(err)
	}
	if !input.UserID.Set || input.UserID.Value != nil || !input.Phone.Set || input.Phone.Value != nil || !input.Name.Set || input.Name.Value == nil || *input.Name.Value != "Teacher" || input.Email.Set {
		t.Fatalf("PATCH semantics incorrect: %+v", input)
	}
}
