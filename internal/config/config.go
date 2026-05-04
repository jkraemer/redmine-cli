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
	DefaultFormat string
}

type fileConfig struct {
	URL           string `toml:"url"`
	APIKey        string `toml:"api_key"`
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
	if path := configPath(); path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, &fc); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
		}
	}

	cfg := &Config{
		URL:           firstNonEmpty(os.Getenv("REDMINE_URL"), fc.URL),
		APIKey:        firstNonEmpty(os.Getenv("REDMINE_API_KEY"), fc.APIKey),
		DefaultFormat: firstNonEmpty(os.Getenv("REDMINE_FORMAT"), fc.DefaultFormat, "json"),
	}

	if cfg.URL == "" {
		return nil, ErrMissingURL
	}
	if cfg.APIKey == "" {
		return nil, ErrMissingAPIKey
	}
	return cfg, nil
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
