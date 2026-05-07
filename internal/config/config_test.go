package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoad_EnvOnly(t *testing.T) {
	t.Setenv("REDMINE_URL", "https://example.com")
	t.Setenv("REDMINE_API_KEY", "key123")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "https://example.com" {
		t.Errorf("URL=%q", cfg.URL)
	}
	if cfg.APIKey != "key123" {
		t.Errorf("APIKey=%q", cfg.APIKey)
	}
	if cfg.DefaultFormat != "json" {
		t.Errorf("DefaultFormat=%q want json", cfg.DefaultFormat)
	}
}

func TestLoad_TOMLFallback(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `url = "https://from-toml.example"
api_key = "toml-key"
default_format = "markdown"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "https://from-toml.example" {
		t.Errorf("URL=%q", cfg.URL)
	}
	if cfg.APIKey != "toml-key" {
		t.Errorf("APIKey=%q", cfg.APIKey)
	}
	if cfg.DefaultFormat != "markdown" {
		t.Errorf("DefaultFormat=%q", cfg.DefaultFormat)
	}
}

func TestLoad_EnvOverridesTOML(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte(`url="https://toml"`+"\n"+`api_key="toml"`), 0o600)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "https://env.example")
	t.Setenv("REDMINE_API_KEY", "env-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "https://env.example" {
		t.Errorf("URL=%q", cfg.URL)
	}
}

func TestLoad_WarnsWhenConfigFileIsGroupOrWorldReadable(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`url="https://x"`+"\n"+`api_key="k"`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, cfgPath) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning %s, got %v", cfgPath, cfg.Warnings)
	}
}

func TestLoad_NoWarningWhenConfigFileIsTight(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte(`url="https://x"`+"\n"+`api_key="k"`), 0o600)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", cfg.Warnings)
	}
}

func TestLoad_MissingURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "k")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoad_OAuthScopesTOML(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `url = "https://x"
oauth_client_id = "cid"
oauth_scopes = ["view_project", "view_issues", "edit_issues"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")
	t.Setenv("REDMINE_OAUTH_SCOPES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"view_project", "view_issues", "edit_issues"}
	if !reflect.DeepEqual(cfg.OAuthScopes, want) {
		t.Errorf("OAuthScopes=%v want %v", cfg.OAuthScopes, want)
	}
}

func TestLoad_OAuthScopesEnvOverridesTOML(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte(`url="https://x"`+"\n"+`oauth_client_id="cid"`+"\n"+`oauth_scopes=["from_toml"]`), 0o600)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_OAUTH_SCOPES", "view_project   view_issues") // extra spaces tolerated

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"view_project", "view_issues"}
	if !reflect.DeepEqual(cfg.OAuthScopes, want) {
		t.Errorf("OAuthScopes=%v want %v", cfg.OAuthScopes, want)
	}
}
