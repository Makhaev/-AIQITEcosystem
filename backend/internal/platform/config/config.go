package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL, Address, Origin string
	SecureCookies                bool
	TrustProxy                   bool
	SMTP                         SMTPConfig
	EventSelfRegistrationMinAge  int
}

type SMTPConfig struct{ Host, Port, Username, Password, From, TLSMode, CAFile string }

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Address: os.Getenv("HTTP_ADDR"), Origin: os.Getenv("APP_ORIGIN")}
	c.SMTP = SMTPConfig{Host: os.Getenv("SMTP_HOST"), Port: os.Getenv("SMTP_PORT"), Username: os.Getenv("SMTP_USERNAME"), Password: os.Getenv("SMTP_PASSWORD"), From: os.Getenv("SMTP_FROM"), TLSMode: os.Getenv("SMTP_TLS_MODE"), CAFile: os.Getenv("SMTP_CA_FILE")}
	if c.Address == "" {
		c.Address = ":8080"
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return c, fmt.Errorf("APP_ORIGIN must be an absolute http(s) origin without a path")
	}
	c.Origin = strings.TrimSuffix(c.Origin, "/")
	c.SecureCookies = u.Scheme == "https"
	if value := os.Getenv("EVENT_SELF_REGISTRATION_MIN_AGE"); value != "" {
		c.EventSelfRegistrationMinAge, err = strconv.Atoi(value)
		if err != nil || c.EventSelfRegistrationMinAge < 0 || c.EventSelfRegistrationMinAge > 100 {
			return c, fmt.Errorf("EVENT_SELF_REGISTRATION_MIN_AGE must be 0 (disabled) or an agreed age from 1 to 100")
		}
	}
	if value := os.Getenv("TRUST_PROXY"); value != "" {
		c.TrustProxy, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("TRUST_PROXY must be true or false")
		}
	}
	return c, nil
}
