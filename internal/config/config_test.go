package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jkraemer/redmine-cli/internal/auth"
)

func TestLoad_EnvOnly(t *testing.T) {
	t.Setenv("REDMINE_URL", "https://example.com")
	t.Setenv("REDMINE_API_KEY", "key123")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load("")
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

	cfg, err := Load("")
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

	cfg, err := Load("")
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

	cfg, err := Load("")
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

	cfg, err := Load("")
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

	_, err := Load("")
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

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"view_project", "view_issues", "edit_issues"}
	if !reflect.DeepEqual(cfg.OAuthScopes, want) {
		t.Errorf("OAuthScopes=%v want %v", cfg.OAuthScopes, want)
	}
}

func TestLoad_ExplicitPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "alt.toml")
	if err := os.WriteFile(p, []byte(`url = "https://alt.example"
api_key = "alt-key"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.URL != "https://alt.example" {
		t.Errorf("URL=%q", cfg.URL)
	}
	if cfg.Path != p {
		t.Errorf("Path=%q want %q", cfg.Path, p)
	}
}

func TestLoad_ExplicitPathMissing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	_, err := Load("/no/such/file.toml")
	if err == nil {
		t.Fatal("expected error for missing explicit path")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
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

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"view_project", "view_issues"}
	if !reflect.DeepEqual(cfg.OAuthScopes, want) {
		t.Errorf("OAuthScopes=%v want %v", cfg.OAuthScopes, want)
	}
}

func TestLoad_ParsesTokenSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	contents := `url = "https://x"
oauth_client_id = "cid"

[token]
access_token = "AT"
refresh_token = "RT"
token_type = "Bearer"
expires_at = 2030-01-01T00:00:00Z
scope = "view_project edit_issues"
`
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token == nil {
		t.Fatal("Token nil")
	}
	if cfg.Token.AccessToken != "AT" {
		t.Errorf("AccessToken=%q", cfg.Token.AccessToken)
	}
	if cfg.Token.RefreshToken != "RT" {
		t.Errorf("RefreshToken=%q", cfg.Token.RefreshToken)
	}
	if cfg.Token.TokenType != "Bearer" {
		t.Errorf("TokenType=%q", cfg.Token.TokenType)
	}
	if cfg.Token.Scope != "view_project edit_issues" {
		t.Errorf("Scope=%q", cfg.Token.Scope)
	}
	if cfg.Token.ExpiresAt.Year() != 2030 {
		t.Errorf("ExpiresAt=%v", cfg.Token.ExpiresAt)
	}
}

func TestLoad_NoTokenSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	if err := os.WriteFile(p, []byte(`url="https://x"`+"\n"+`api_key="k"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != nil {
		t.Errorf("expected nil Token, got %+v", cfg.Token)
	}
}

func TestSaveToken_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	if err := os.WriteFile(p, []byte(`url = "https://x"
oauth_client_id = "cid"
oauth_scopes = ["view_project", "edit_issues"]
default_format = "markdown"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	tok := &auth.Token{
		AccessToken:  "AT",
		RefreshToken: "RT",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Round(time.Second),
		Scope:        "view_project",
	}
	if err := cfg.SaveToken(tok); err != nil {
		t.Fatal(err)
	}

	cfg2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Token == nil || cfg2.Token.AccessToken != "AT" {
		t.Errorf("token not persisted: %+v", cfg2.Token)
	}
	if cfg2.Token.RefreshToken != "RT" {
		t.Errorf("RefreshToken=%q", cfg2.Token.RefreshToken)
	}
	if cfg2.URL != "https://x" {
		t.Errorf("URL clobbered: %q", cfg2.URL)
	}
	if cfg2.OAuthClientID != "cid" {
		t.Errorf("client id clobbered: %q", cfg2.OAuthClientID)
	}
	if !reflect.DeepEqual(cfg2.OAuthScopes, []string{"view_project", "edit_issues"}) {
		t.Errorf("scopes clobbered: %v", cfg2.OAuthScopes)
	}
	if cfg2.DefaultFormat != "markdown" {
		t.Errorf("format clobbered: %q", cfg2.DefaultFormat)
	}

	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %#o, want 0600", info.Mode().Perm())
	}
}

func TestSaveToken_OmitsEmptyKeysInOutput(t *testing.T) {
	// After a SaveToken on a sparse config, the file must not gain
	// noise like oauth_client_id = "" for unset keys.
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	if err := os.WriteFile(p, []byte(`url = "https://x"
api_key = "k"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{AccessToken: "AT", TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, noise := range []string{`oauth_client_id = ""`, `oauth_client_secret = ""`, `default_format = ""`} {
		if strings.Contains(string(body), noise) {
			t.Errorf("file contains empty-key noise %q:\n%s", noise, body)
		}
	}
}

// TestSaveToken_PreservesUnknownKeys: the token rewrite must not silently
// delete keys this CLI version doesn't know about (typos, future settings).
// This matters because SaveToken runs implicitly on token refresh, not just
// on explicit auth login.
func TestSaveToken_PreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	if err := os.WriteFile(p, []byte(`url = "https://x"
oauth_client_id = "cid"
custom_note = "keep me"

[future_section]
knob = 42
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{AccessToken: "AT", TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`custom_note = "keep me"`, "[future_section]", "knob = 42"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("unknown key %q dropped by SaveToken:\n%s", want, body)
		}
	}
	cfg2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Token == nil || cfg2.Token.AccessToken != "AT" {
		t.Errorf("token not persisted: %+v", cfg2.Token)
	}
}

func TestDeleteToken_PreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	if err := os.WriteFile(p, []byte(`url = "https://x"
oauth_client_id = "cid"
custom_note = "keep me"

[token]
access_token = "AT"
token_type = "Bearer"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.DeleteToken(); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `custom_note = "keep me"`) {
		t.Errorf("unknown key dropped by DeleteToken:\n%s", body)
	}
	if strings.Contains(string(body), "access_token") {
		t.Errorf("token section not removed:\n%s", body)
	}
}

func TestSaveToken_CreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fresh.toml")
	// Don't create the file yet — Load with explicit path should fail,
	// so we construct Config directly for this test.
	cfg := &Config{Path: p}
	tok := &auth.Token{AccessToken: "AT", TokenType: "Bearer"}
	if err := cfg.SaveToken(tok); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %#o, want 0600", info.Mode().Perm())
	}
}

func TestDeleteToken_MissingFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "absent.toml")
	cfg := &Config{Path: p}
	if err := cfg.DeleteToken(); err != nil {
		t.Fatalf("DeleteToken on missing file: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("DeleteToken should not create the file; stat err=%v", err)
	}
	if cfg.Token != nil {
		t.Errorf("cfg.Token=%+v want nil", cfg.Token)
	}
}

func TestSaveToken_RejectsNil(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Path: filepath.Join(dir, "cfg.toml")}
	if err := cfg.SaveToken(nil); err == nil {
		t.Fatal("expected error for nil token")
	}
}

func TestDeleteToken_RemovesSection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	if err := os.WriteFile(p, []byte(`url="https://x"
api_key="k"

[token]
access_token = "AT"
token_type = "Bearer"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token == nil {
		t.Fatal("setup: expected token loaded")
	}
	if err := cfg.DeleteToken(); err != nil {
		t.Fatal(err)
	}
	if cfg.Token != nil {
		t.Errorf("cfg.Token not cleared in memory: %+v", cfg.Token)
	}

	cfg2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Token != nil {
		t.Errorf("token still on disk: %+v", cfg2.Token)
	}
	if cfg2.URL != "https://x" {
		t.Errorf("URL clobbered: %q", cfg2.URL)
	}
	if cfg2.APIKey != "k" {
		t.Errorf("APIKey clobbered: %q", cfg2.APIKey)
	}
}

func TestDeleteToken_NoExistingToken(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.toml")
	if err := os.WriteFile(p, []byte(`url="https://x"`+"\n"+`api_key="k"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.DeleteToken(); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_LegacyTokenReadAtDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte(`url="https://x"`+"\n"+`api_key="k"`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := `{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_at":"2030-01-01T00:00:00Z","scope":"view_project"}`
	if err := os.WriteFile(filepath.Join(cfgDir, "token.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token == nil || cfg.Token.AccessToken != "AT" {
		t.Errorf("legacy token not loaded: %+v", cfg.Token)
	}
	if cfg.Token.Scope != "view_project" {
		t.Errorf("Scope=%q", cfg.Token.Scope)
	}
}

func TestLoad_LegacyTokenIgnoredForCustomPath(t *testing.T) {
	// A custom --config must not inherit the default-instance's legacy token.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"access_token":"AT","token_type":"Bearer"}`
	if err := os.WriteFile(filepath.Join(cfgDir, "token.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	custom := filepath.Join(t.TempDir(), "alt.toml")
	if err := os.WriteFile(custom, []byte(`url="https://alt"`+"\n"+`api_key="k"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(custom)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != nil {
		t.Errorf("custom path must not inherit legacy token, got %+v", cfg.Token)
	}
}

func TestSaveToken_RemovesLegacyAtDefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte(`url="https://x"`+"\n"+`oauth_client_id="cid"`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(cfgDir, "token.json")
	if err := os.WriteFile(legacy, []byte(`{"access_token":"old","token_type":"Bearer"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{AccessToken: "new", TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy token.json not removed: %v", err)
	}
}

func TestSaveToken_KeepsLegacyAtCustomPath(t *testing.T) {
	// A custom --config save must NOT delete the default-instance legacy.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(cfgDir, "token.json")
	if err := os.WriteFile(legacy, []byte(`{"access_token":"keep","token_type":"Bearer"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	custom := filepath.Join(t.TempDir(), "alt.toml")
	if err := os.WriteFile(custom, []byte(`url="https://alt"`+"\n"+`oauth_client_id="cid"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load(custom)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{AccessToken: "new", TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("legacy must be preserved for custom-config save: %v", err)
	}
}
