package config

import (
	"testing"
)

func TestEventSelfRegistrationAgeConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused")
	t.Setenv("APP_ORIGIN", "http://localhost")
	t.Setenv("TRUST_PROXY", "")
	for _, value := range []string{"", "0", "14", "100"} {
		t.Setenv("EVENT_SELF_REGISTRATION_MIN_AGE", value)
		cfg, err := Load()
		if err != nil || value == "14" && cfg.EventSelfRegistrationMinAge != 14 || value == "" && cfg.EventSelfRegistrationMinAge != 0 {
			t.Fatalf("age %q: %+v %v", value, cfg, err)
		}
	}
	for _, value := range []string{"-1", "101", "14.5", "invalid"} {
		t.Setenv("EVENT_SELF_REGISTRATION_MIN_AGE", value)
		if _, err := Load(); err == nil {
			t.Fatalf("accepted invalid age %q", value)
		}
	}
}

func TestCookieSecurityFollowsOrigin(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("TRUST_PROXY", "")
	for _, tc := range []struct {
		origin string
		secure bool
	}{
		{"http://localhost:5173", false},
		{"https://aiq.example/", true},
	} {
		t.Setenv("APP_ORIGIN", tc.origin)
		cfg, err := Load()
		if err != nil || cfg.SecureCookies != tc.secure || cfg.Address != ":8080" || cfg.TrustProxy {
			t.Fatalf("unexpected origin config: %+v %v", cfg, err)
		}
	}
	t.Setenv("APP_ORIGIN", "https://aiq.example")
	t.Setenv("TRUST_PROXY", "true")
	if cfg, err := Load(); err != nil || !cfg.TrustProxy {
		t.Fatalf("explicit proxy trust ignored: %+v %v", cfg, err)
	}
}

func TestInvalidConfigurationIsRejected(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused")
	t.Setenv("TRUST_PROXY", "false")
	for _, origin := range []string{"", "aiq.example", "ftp://aiq.example", "https://user:password@aiq.example", "https://aiq.example/app", "https://aiq.example?query=1", "https://aiq.example#fragment"} {
		t.Setenv("APP_ORIGIN", origin)
		if _, err := Load(); err == nil {
			t.Errorf("accepted invalid origin %q", origin)
		}
	}
	t.Setenv("APP_ORIGIN", "https://aiq.example")
	t.Setenv("TRUST_PROXY", "invalid")
	if _, err := Load(); err == nil {
		t.Fatal("accepted invalid proxy configuration")
	}
	t.Setenv("TRUST_PROXY", "false")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("accepted missing database URL")
	}
}
