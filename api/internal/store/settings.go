package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/collinpendleton/backhog/api/internal/models"
)

// Setting keys held in app_settings. These are the admin-editable half of
// the configuration; the environment still owns everything about the
// deployment itself (paths, credentials, ports).
const (
	// SettingRegistrationEnabled gates sign-up without an invite.
	SettingRegistrationEnabled = "registration_enabled"
	// SettingDefaultRole is the role a self-service sign-up lands on.
	SettingDefaultRole = "default_role"
)

// defaultSettings is what a missing row reads as. The table is seeded by the
// migration, so this only covers a row deleted by hand — but the settings
// endpoint must never fail closed on registration in a way that could leave
// a fresh install with no way in.
var defaultSettings = models.ServerSettings{
	RegistrationEnabled: true,
	DefaultRole:         models.RoleReader,
}

// Settings reads the server settings, filling in defaults for anything
// missing or unparseable.
func (s *Store) Settings(ctx context.Context) (models.ServerSettings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM app_settings`)
	if err != nil {
		return defaultSettings, err
	}
	defer rows.Close()

	out := defaultSettings
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return defaultSettings, err
		}
		switch key {
		case SettingRegistrationEnabled:
			out.RegistrationEnabled = value == "true"
		case SettingDefaultRole:
			if models.ValidRole(value) {
				out.DefaultRole = value
			}
		}
	}
	if err := rows.Err(); err != nil {
		return defaultSettings, err
	}
	return out, nil
}

// SaveSettings writes the server settings and returns them as stored. An
// unknown default role is rejected rather than silently corrected: it can
// only come from a malformed request, and quietly storing something else
// would make the panel lie about what it saved.
func (s *Store) SaveSettings(ctx context.Context, in models.ServerSettings) (models.ServerSettings, error) {
	if !models.ValidRole(in.DefaultRole) {
		return models.ServerSettings{}, errors.New("unknown role: " + in.DefaultRole)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.ServerSettings{}, err
	}
	defer tx.Rollback()

	registration := "false"
	if in.RegistrationEnabled {
		registration = "true"
	}
	for key, value := range map[string]string{
		SettingRegistrationEnabled: registration,
		SettingDefaultRole:         in.DefaultRole,
	} {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO app_settings (key, value, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE
			  SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
			key, value); err != nil {
			return models.ServerSettings{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return models.ServerSettings{}, err
	}
	return in, nil
}

// AppSetting reads one settings row as text. A missing row is "".
func (s *Store) AppSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

// AppSettingInt reads one settings row as an integer. A missing or
// unparseable row is 0 — the honest starting position for a cursor, and
// nothing else should be reading ints from this table.
func (s *Store) AppSettingInt(ctx context.Context, key string) (int64, error) {
	var value string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return n, nil
}

// SetAppSetting upserts one settings row — internal bookkeeping keys the
// admin panel never renders.
func (s *Store) SetAppSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_settings (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE
		  SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		key, value)
	return err
}
