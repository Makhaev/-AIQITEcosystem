package main

import (
	"os"
	"strings"
	"testing"
)

func TestBootstrapRequiresExplicitCredentials(t *testing.T) {
	for _, missing := range []string{"ADMIN_EMAIL", "ADMIN_NAME", "ADMIN_PASSWORD"} {
		t.Run(missing, func(t *testing.T) {
			t.Setenv("ADMIN_EMAIL", "admin@example.test")
			t.Setenv("ADMIN_NAME", "Test Admin")
			t.Setenv("ADMIN_PASSWORD", "TestOnly-Admin-4829")
			t.Setenv(missing, "")
			err := run()
			if err == nil || !strings.Contains(err.Error(), "no default password") {
				t.Fatalf("bootstrap did not reject missing %s: %v", missing, err)
			}
			if _, exists := os.LookupEnv("ADMIN_PASSWORD"); exists {
				t.Fatal("bootstrap retained password in process environment")
			}
		})
	}
}
