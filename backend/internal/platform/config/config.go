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
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Address: os.Getenv("HTTP_ADDR"), Origin: os.Getenv("APP_ORIGIN")}
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
	return c, nil
}
