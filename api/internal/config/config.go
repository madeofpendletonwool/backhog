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
	SteamAPIKey  string
	Production   bool
	// EpubTextDir holds the canonical texts and their block-offset
	// sidecars for the Books arena ({id}.txt / {id}.blocks.json). A file
	// next to the DB rather than a BLOB: novels are multi-megabyte strings
	// and ranged reads should not drag them through the WAL.
	EpubTextDir string
	// CookieSecure marks the session cookie Secure. It defaults to off because
	// the common self-hosted setup is plain HTTP on a LAN address, and browsers
	// silently discard Secure cookies on non-HTTPS origins other than localhost
	// — which looks exactly like "login does nothing". Turn it on when serving
	// through an HTTPS reverse proxy.
	CookieSecure bool
	// MediaDirs are the read-only library roots for the Books arena, in
	// PATH-style colon-separated form (MEDIA_DIR=/media/audiobooks:/media/ebooks).
	// The directories are bind-mounted into the container and are never written
	// to. Empty means the Books arena scanner is disabled.
	MediaDirs []string
	// AlignWorkerToken authenticates the optional alignment worker
	// container against the /internal API (ALIGN_WORKER_TOKEN). Empty
	// disables the internal endpoints entirely — the alignment queue is
	// inert and everything else keeps working, which is the contract for
	// a deployment that never enables alignment. Never logged.
	AlignWorkerToken string
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	c := Config{
		Addr:         env("ADDR", ":8080"),
		DatabasePath: env("DATABASE_PATH", "./backhog.db"),
		CoverDir:     env("COVER_DIR", "./covers"),
		EpubTextDir:  env("EPUB_TEXT_DIR", "./epub_text"),
		IGDBClientID: os.Getenv("IGDB_CLIENT_ID"),
		IGDBSecret:   os.Getenv("IGDB_CLIENT_SECRET"),
		SteamAPIKey:  os.Getenv("STEAM_API_KEY"),
		Production:   strings.EqualFold(env("APP_ENV", "development"), "production"),
		CookieSecure: strings.EqualFold(env("COOKIE_SECURE", "false"), "true"),
	}
	// BOOK_LIBRARY_DIR is the older spelling; it still works when MEDIA_DIR
	// is unset so existing deployments keep scanning after an upgrade.
	c.MediaDirs = splitPaths(os.Getenv("MEDIA_DIR"))
	if len(c.MediaDirs) == 0 {
		c.MediaDirs = splitPaths(os.Getenv("BOOK_LIBRARY_DIR"))
	}
	c.AlignWorkerToken = os.Getenv("ALIGN_WORKER_TOKEN")
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

// splitPaths splits a colon-separated path list, dropping empty segments.
func splitPaths(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ":")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
