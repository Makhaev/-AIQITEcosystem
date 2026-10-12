package users

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestEmailNormalization(t *testing.T) {
	email, err := NormalizeEmail("  AdMiN@EXAMPLE.TEST  ")
	if err != nil || email != "admin@example.test" {
		t.Fatalf("email=%q error=%v", email, err)
	}
	for _, email := range []string{"", "not-an-email", "Admin <admin@example.test>", "admin@example.test\nother@example.test", strings.Repeat("a", 243) + "@example.test"} {
		if _, err := NormalizeEmail(email); err == nil {
			t.Errorf("accepted invalid email %q", email)
		}
	}
}

func TestPasswordBoundariesAndBcrypt(t *testing.T) {
	for _, password := range []string{"", "12345678901", strings.Repeat("a", 73), strings.Repeat("я", 37), strings.Repeat("🔑", 19)} {
		if _, err := HashPassword(password); err == nil {
			t.Errorf("accepted invalid password with %d bytes", len(password))
		}
	}
	for _, password := range []string{"123456789012", strings.Repeat("a", 72), strings.Repeat("я", 12), strings.Repeat("🔑", 18)} {
		first, err := HashPassword(password)
		if err != nil {
			t.Fatal(err)
		}
		if cost, err := bcrypt.Cost([]byte(first)); err != nil || cost != bcrypt.DefaultCost {
			t.Fatalf("unexpected bcrypt cost: %d %v", cost, err)
		}
		if bcrypt.CompareHashAndPassword([]byte(first), []byte(password)) != nil {
			t.Fatal("bcrypt hash does not verify original password")
		}
		second, err := HashPassword(password)
		if err != nil || first == second {
			t.Fatal("each hash must use a fresh salt")
		}
	}
}
