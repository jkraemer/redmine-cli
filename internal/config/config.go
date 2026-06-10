// Package config loads CLI configuration from environment variables and
// an optional TOML config file at $XDG_CONFIG_HOME/redmine-cli/config.toml
// (defaulting to ~/.config/redmine-cli/config.toml).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/jkraemer/redmine-cli/internal/auth"
)

// Config holds resolved CLI configuration.
type Config struct {
	URL               string
	APIKey            string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthScopes       []string
	DefaultFormat     string
	// ReadOnly restricts the CLI to read-only actions: any write (a command
	// run with --confirm) is refused. Resolved from REDMINE_READ_ONLY (env,
	// highest priority) then read_only in the config file.
	ReadOnly bool
	// Token holds OAuth tokens loaded from the config file's [token]
	// section. Nil if no token is stored.
	Token *auth.Token
	// Path is the resolved config file path (explicit or default). May be
	// empty if no path could be resolved. The file may or may not exist.
	Path string
	// Warnings collects non-fatal issues found while loading config —
	// e.g. an API key file that is readable by group/other. The CLI
	// surfaces them on stderr but they don't block startup.
	Warnings []string
}

type fileConfig struct {
	URL               string     `toml:"url,omitempty"`
	APIKey            string     `toml:"api_key,omitempty"`
	OAuthClientID     string     `toml:"oauth_client_id,omitempty"`
	OAuthClientSecret string     `toml:"oauth_client_secret,omitempty"`
	OAuthScopes       []string   `toml:"oauth_scopes,omitempty"`
	DefaultFormat     string     `toml:"default_format,omitempty"`
	ReadOnly          bool       `toml:"read_only,omitempty"`
	Token             *fileToken `toml:"token,omitempty"`
}

type fileToken struct {
	AccessToken  string    `toml:"access_token"`
	RefreshToken string    `toml:"refresh_token,omitempty"`
	TokenType    string    `toml:"token_type"`
	ExpiresAt    time.Time `toml:"expires_at,omitempty"`
	Scope        string    `toml:"scope,omitempty"`
}

// ErrMissingURL is returned when no URL can be resolved.
var ErrMissingURL = errors.New("redmine URL not configured (set REDMINE_URL or url in config.toml)")

// ErrMissingAPIKey is returned when no API key can be resolved.
var ErrMissingAPIKey = errors.New("redmine API key not configured (set REDMINE_API_KEY or api_key in config.toml)")

// Load resolves configuration from env vars (highest priority) then a TOML
// file. If path is empty, falls back to the default discovery path. If path
// is non-empty and the file is missing, returns an error.
func Load(path string) (*Config, error) {
	var fc fileConfig
	cfg := &Config{}
	resolved := path
	if resolved == "" {
		resolved = defaultConfigPath()
	}
	cfg.Path = resolved

	if resolved != "" {
		info, err := os.Stat(resolved)
		switch {
		case err == nil:
			if _, err := toml.DecodeFile(resolved, &fc); err != nil {
				return nil, fmt.Errorf("parse %s: %w", resolved, err)
			}
			if w := insecurePermWarning(resolved, info); w != "" {
				cfg.Warnings = append(cfg.Warnings, w)
			}
		case os.IsNotExist(err):
			if path != "" {
				return nil, fmt.Errorf("config file %s: %w", path, err)
			}
			// default path missing is fine — env-only mode
		default:
			return nil, err
		}
	}

	cfg.URL = firstNonEmpty(os.Getenv("REDMINE_URL"), fc.URL)
	cfg.APIKey = firstNonEmpty(os.Getenv("REDMINE_API_KEY"), fc.APIKey)
	cfg.OAuthClientID = firstNonEmpty(os.Getenv("REDMINE_OAUTH_CLIENT_ID"), fc.OAuthClientID)
	cfg.OAuthClientSecret = firstNonEmpty(os.Getenv("REDMINE_OAUTH_CLIENT_SECRET"), fc.OAuthClientSecret)
	cfg.OAuthScopes = resolveScopes(os.Getenv("REDMINE_OAUTH_SCOPES"), fc.OAuthScopes)
	cfg.DefaultFormat = firstNonEmpty(os.Getenv("REDMINE_FORMAT"), fc.DefaultFormat, "json")

	ro, err := resolveReadOnly(os.Getenv("REDMINE_READ_ONLY"), fc.ReadOnly)
	if err != nil {
		return nil, err
	}
	cfg.ReadOnly = ro

	if fc.Token != nil && fc.Token.AccessToken != "" {
		cfg.Token = &auth.Token{
			AccessToken:  fc.Token.AccessToken,
			RefreshToken: fc.Token.RefreshToken,
			TokenType:    fc.Token.TokenType,
			ExpiresAt:    fc.Token.ExpiresAt,
			Scope:        fc.Token.Scope,
		}
	}

	if cfg.Token == nil && cfg.Path == defaultConfigPath() {
		if leg := readLegacyToken(); leg != nil {
			cfg.Token = leg
		}
	}

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

func defaultConfigPath() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "redmine-cli", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "redmine-cli", "config.toml")
	}
	return ""
}

// SaveToken persists the OAuth token into the [token] section of the
// config file at c.Path, preserving all other keys. The file is written
// atomically (temp file + rename) with mode 0600. On success, c.Token is
// updated to match.
func (c *Config) SaveToken(tok *auth.Token) error {
	if c.Path == "" {
		return errors.New("config has no path; cannot save token")
	}
	if tok == nil {
		return errors.New("SaveToken: nil token (use DeleteToken to clear)")
	}
	fc, err := readFileConfig(c.Path)
	if err != nil {
		return err
	}
	fc.Token = fromAuthToken(tok)
	if err := writeFileConfig(c.Path, fc); err != nil {
		return err
	}
	c.Token = tok
	if c.Path == defaultConfigPath() {
		if leg := legacyTokenPath(); leg != "" {
			_ = os.Remove(leg) // best-effort
		}
	}
	return nil
}

// DeleteToken removes the [token] section from the config file at c.Path,
// preserving all other keys. After a successful write, c.Token is nil.
// If the config file does not exist, DeleteToken is a successful no-op —
// it does not materialize an empty file.
func (c *Config) DeleteToken() error {
	if c.Path == "" {
		return errors.New("config has no path; cannot delete token")
	}
	if _, err := os.Stat(c.Path); os.IsNotExist(err) {
		c.Token = nil
		return nil
	}
	fc, err := readFileConfig(c.Path)
	if err != nil {
		return err
	}
	fc.Token = nil
	if err := writeFileConfig(c.Path, fc); err != nil {
		return err
	}
	c.Token = nil
	return nil
}

// readFileConfig parses the TOML file at path into a fileConfig. A missing
// file is not an error — it yields a zero-valued fileConfig so callers
// can populate it from scratch.
func readFileConfig(path string) (*fileConfig, error) {
	var fc fileConfig
	if _, err := toml.DecodeFile(path, &fc); err != nil {
		if os.IsNotExist(err) {
			return &fc, nil
		}
		return nil, err
	}
	return &fc, nil
}

// writeFileConfig atomically writes the fileConfig as TOML to path with
// mode 0600 by writing to a temp file in the same directory and renaming.
func writeFileConfig(path string, fc *fileConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".cfg-*.toml")
	if err != nil {
		return err
	}
	enc := toml.NewEncoder(f)
	if err := enc.Encode(fc); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return err
	}
	return os.Rename(f.Name(), path)
}

func fromAuthToken(t *auth.Token) *fileToken {
	if t == nil {
		return nil
	}
	return &fileToken{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		TokenType:    t.TokenType,
		ExpiresAt:    t.ExpiresAt,
		Scope:        t.Scope,
	}
}

// legacyTokenPath returns the path to the pre-1.0
// ~/.config/redmine-cli/token.json, or "" if it can't be resolved.
func legacyTokenPath() string {
	d := defaultConfigPath()
	if d == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(d), "token.json")
}

// readLegacyToken returns the legacy JSON token, or nil if the file is
// missing or unparseable. Errors are swallowed deliberately: a corrupt
// legacy file should not block the new code path.
func readLegacyToken() *auth.Token {
	path := legacyTokenPath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var t auth.Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil
	}
	return &t
}

// ReadOnly reports whether read-only mode is enabled, resolving
// REDMINE_READ_ONLY (highest priority) then read_only in the config file at
// path (empty → default path). Unlike Load it requires neither a URL nor an
// API key and never fails: a malformed file or invalid env value yields false.
// It backs display paths (e.g. --agent --help) that must work even when the
// full config is unusable.
func ReadOnly(path string) bool {
	resolved := path
	if resolved == "" {
		resolved = defaultConfigPath()
	}
	var fileVal bool
	if resolved != "" {
		if fc, err := readFileConfig(resolved); err == nil {
			fileVal = fc.ReadOnly
		}
	}
	ro, _ := resolveReadOnly(os.Getenv("REDMINE_READ_ONLY"), fileVal)
	return ro
}

// resolveReadOnly applies env-over-file precedence to the read-only setting.
// A non-empty REDMINE_READ_ONLY must parse as a bool; an empty value falls
// back to the file's read_only.
func resolveReadOnly(env string, fileVal bool) (bool, error) {
	if s := strings.TrimSpace(env); s != "" {
		v, err := strconv.ParseBool(s)
		if err != nil {
			return false, fmt.Errorf("invalid REDMINE_READ_ONLY value %q: must be true or false", env)
		}
		return v, nil
	}
	return fileVal, nil
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

// resolveScopes returns env-split scopes if non-empty, else the TOML list
// (with empty/whitespace-only entries filtered out). Env values are
// space-separated per RFC 6749 §3.3.
func resolveScopes(env string, fileScopes []string) []string {
	if s := strings.TrimSpace(env); s != "" {
		return strings.Fields(s)
	}
	out := make([]string, 0, len(fileScopes))
	for _, s := range fileScopes {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
