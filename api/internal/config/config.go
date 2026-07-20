package config

import (
	"errors"
	"os"
	"strings"
)

// Config holds runtime configuration, all sourced from the environment.
type Config struct {
	Addr         string
	DatabasePath string
	CoverDir     string
	IGDBClientID string
	IGDBSecret   string
	Production   bool
	// CookieSecure marks the session cookie Secure. It defaults to off because
	// the common self-hosted setup is plain HTTP on a LAN address, and browsers
	// silently discard Secure cookies on non-HTTPS origins other than localhost
	// — which looks exactly like "login does nothing". Turn it on when serving
	// through an HTTPS reverse proxy.
	CookieSecure bool
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	c := Config{
		Addr:         env("ADDR", ":8080"),
		DatabasePath: env("DATABASE_PATH", "./backhog.db"),
		CoverDir:     env("COVER_DIR", "./covers"),
		IGDBClientID: os.Getenv("IGDB_CLIENT_ID"),
		IGDBSecret:   os.Getenv("IGDB_CLIENT_SECRET"),
		Production:   strings.EqualFold(env("APP_ENV", "development"), "production"),
		CookieSecure: strings.EqualFold(env("COOKIE_SECURE", "false"), "true"),
	}
	if c.DatabasePath == "" {
		return c, errors.New("DATABASE_PATH must not be empty")
	}
	return c, nil
}

// MetadataEnabled reports whether IGDB credentials are present. Without them the
// app still runs against its local game cache; only lookup of new games fails.
func (c Config) MetadataEnabled() bool {
	return c.IGDBClientID != "" && c.IGDBSecret != ""
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
