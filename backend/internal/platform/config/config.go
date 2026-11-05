package config

import (
	"aiq.local/core/internal/platform/httpx"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL, Address, Origin string
	SecureCookies                bool
	TrustProxy                   bool
	Student360                   bool
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Address: os.Getenv("HTTP_ADDR"), Origin: os.Getenv("APP_ORIGIN"), Student360: true}
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
	if value := os.Getenv("TRUST_PROXY"); value != "" {
		c.TrustProxy, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("TRUST_PROXY must be true or false")
		}
	}
	if value := os.Getenv("FEATURE_STUDENT360"); value != "" {
		c.Student360, err = strconv.ParseBool(value)
		if err != nil {
			return c, fmt.Errorf("FEATURE_STUDENT360 must be true or false")
		}
	}
	return c, nil
}

// Optional modules stay closed until implemented and verified. Student 360 has
// existing integration coverage and can be enabled independently of the MVP.
func (c Config) Features() map[string]bool {
	return map[string]bool{"student360": c.Student360, "projects": false, "achievements": false, "skills": false, "events": false, "parent_requests": false, "metrics": false, "notifications": false, "materials": false, "settings": false, "tasks": false, "dashboard": false, "audit": false}
}
func (c Config) Gate(name string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !c.Features()[name] {
			httpx.Fail(w, r, httpx.E(404, "feature_disabled", "Функция недоступна в пилотном выпуске."))
			return
		}
		next.ServeHTTP(w, r)
	})
}
