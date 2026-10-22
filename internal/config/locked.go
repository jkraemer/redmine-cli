package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrInvalidLocked is wrapped by Locked when REDMINE_LOCKED is set to a
// value that is not a boolean. An unparsable value must never silently
// mean "unlocked".
var ErrInvalidLocked = errors.New("invalid REDMINE_LOCKED value")

// ErrLockedNeedsAPIKey is returned by LoadLocked when the config file
// selects OAuth or has no api_key; locked mode only supports API-key auth.
var ErrLockedNeedsAPIKey = errors.New("locked mode requires api_key auth in the config file (no oauth_client_id)")

// Locked reports whether locked mode is requested via REDMINE_LOCKED.
// Empty means off; a non-empty value must parse as a bool.
func Locked() (bool, error) {
	v := os.Getenv("REDMINE_LOCKED")
	s := strings.TrimSpace(v)
	if s == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("%w: %q must be true or false", ErrInvalidLocked, v)
	}
	return b, nil
}

// LoadLocked loads configuration for locked mode: only the default config
// file is read, credential/OAuth/read-only env vars are ignored, and the
// result is always read-only. REDMINE_FORMAT is still honored.
func LoadLocked() (*Config, error) {
	cfg, err := load("", true)
	if err != nil {
		return nil, err
	}
	if cfg.OAuthClientID != "" || cfg.APIKey == "" {
		return nil, ErrLockedNeedsAPIKey
	}
	return cfg, nil
}
