// Package config loads CLI configuration from environment variables and
// an optional TOML config file at $XDG_CONFIG_HOME/redmine-cli/config.toml
// (defaulting to ~/.config/redmine-cli/config.toml).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds resolved CLI configuration.
type Config struct {
	URL           string
	APIKey        string
	OAuthClientID string
	DefaultFormat string
	// Warnings collects non-fatal issues found while loading config —
	// e.g. an API key file that is readable by group/other. The CLI
	// surfaces them on stderr but they don't block startup.
	Warnings []string
}

type fileConfig struct {
	URL           string `toml:"url"`
	APIKey        string `toml:"api_key"`
	OAuthClientID string `toml:"oauth_client_id"`
	DefaultFormat string `toml:"default_format"`
}

// ErrMissingURL is returned when no URL can be resolved.
var ErrMissingURL = errors.New("redmine URL not configured (set REDMINE_URL or url in config.toml)")

// ErrMissingAPIKey is returned when no API key can be resolved.
var ErrMissingAPIKey = errors.New("redmine API key not configured (set REDMINE_API_KEY or api_key in config.toml)")

// Load resolves configuration from env vars (highest priority) then a TOML
// file at $XDG_CONFIG_HOME/redmine-cli/config.toml.
func Load() (*Config, error) {
	var fc fileConfig
	cfg := &Config{}
	if path := configPath(); path != "" {
		if info, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, &fc); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
			if w := insecurePermWarning(path, info); w != "" {
				cfg.Warnings = append(cfg.Warnings, w)
			}
		}
	}

	cfg.URL = firstNonEmpty(os.Getenv("REDMINE_URL"), fc.URL)
	cfg.APIKey = firstNonEmpty(os.Getenv("REDMINE_API_KEY"), fc.APIKey)
	cfg.OAuthClientID = firstNonEmpty(os.Getenv("REDMINE_OAUTH_CLIENT_ID"), fc.OAuthClientID)
	cfg.DefaultFormat = firstNonEmpty(os.Getenv("REDMINE_FORMAT"), fc.DefaultFormat, "json")

	if cfg.URL == "" {
		return nil, ErrMissingURL
	}
	if cfg.APIKey == "" && cfg.OAuthClientID == "" {
		return nil, ErrMissingAPIKey
	}
	return cfg, nil
}

// insecurePermWarning returns a non-empty warning when the config file's
// permissions allow group or other to read it. The file holds a long-lived
// API key, so loose permissions are worth surfacing even if the file
// otherwise loads cleanly. Returns "" on Windows or for tight perms.
func insecurePermWarning(path string, info os.FileInfo) string {
	mode := info.Mode().Perm()
	if mode&0o077 == 0 {
		return ""
	}
	return fmt.Sprintf("config file %s is readable by group/other (mode %#o); consider chmod 600", path, mode)
}

func configPath() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "redmine-cli", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "redmine-cli", "config.toml")
	}
	return ""
}

// AuthMethod returns "oauth" if oauth_client_id is configured, else "apikey".
func (c *Config) AuthMethod() string {
	if c.OAuthClientID != "" {
		return "oauth"
	}
	return "apikey"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
