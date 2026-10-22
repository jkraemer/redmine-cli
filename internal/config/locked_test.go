package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDefaultConfig writes contents to the default config path under a
// fresh XDG_CONFIG_HOME, which is what LoadLocked reads.
func writeDefaultConfig(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := filepath.Join(dir, "redmine-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLocked_Values(t *testing.T) {
	tests := []struct {
		env     string
		want    bool
		invalid bool
	}{
		{env: "", want: false},
		{env: "1", want: true},
		{env: "true", want: true},
		{env: " true ", want: true},
		{env: "0", want: false},
		{env: "false", want: false},
		{env: "yes", invalid: true},
		{env: "on", invalid: true},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv("REDMINE_LOCKED", tt.env)
			got, err := Locked()
			if tt.invalid {
				if !errors.Is(err, ErrInvalidLocked) {
					t.Fatalf("err=%v, want ErrInvalidLocked", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Locked: %v", err)
			}
			if got != tt.want {
				t.Errorf("Locked=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadLocked_IgnoresCredentialEnv(t *testing.T) {
	writeDefaultConfig(t, `url = "https://file.example"
api_key = "filekey"
`)
	t.Setenv("REDMINE_URL", "https://env.example")
	t.Setenv("REDMINE_API_KEY", "envkey")

	cfg, err := LoadLocked()
	if err != nil {
		t.Fatalf("LoadLocked: %v", err)
	}
	if cfg.URL != "https://file.example" {
		t.Errorf("URL=%q, want file value", cfg.URL)
	}
	if cfg.APIKey != "filekey" {
		t.Errorf("APIKey=%q, want file value", cfg.APIKey)
	}
}

func TestLoadLocked_ForcesReadOnly(t *testing.T) {
	writeDefaultConfig(t, `url = "https://x"
api_key = "k"
read_only = false
`)
	t.Setenv("REDMINE_READ_ONLY", "false")

	cfg, err := LoadLocked()
	if err != nil {
		t.Fatalf("LoadLocked: %v", err)
	}
	if !cfg.ReadOnly {
		t.Errorf("ReadOnly=false, want true")
	}
}

func TestLoadLocked_RefusesOAuth(t *testing.T) {
	writeDefaultConfig(t, `url = "https://x"
oauth_client_id = "x"
`)

	_, err := LoadLocked()
	if !errors.Is(err, ErrLockedNeedsAPIKey) {
		t.Fatalf("err=%v, want ErrLockedNeedsAPIKey", err)
	}
}

func TestLoadLocked_IgnoresOAuthEnv(t *testing.T) {
	writeDefaultConfig(t, `url = "https://x"
api_key = "k"
`)
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "x")

	cfg, err := LoadLocked()
	if err != nil {
		t.Fatalf("LoadLocked: %v", err)
	}
	if got := cfg.AuthMethod(); got != "apikey" {
		t.Errorf("AuthMethod=%q, want apikey", got)
	}
}

func TestLoadLocked_ParseErrorHidesContent(t *testing.T) {
	writeDefaultConfig(t, `api_key = "SECRETKEY`)

	_, err := LoadLocked()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error leaks file content: %v", err)
	}
}

func TestLoadLocked_MissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, err := LoadLocked()
	if !errors.Is(err, ErrMissingURL) {
		t.Fatalf("err=%v, want ErrMissingURL", err)
	}
}

func TestLoadLocked_NoAPIKey(t *testing.T) {
	writeDefaultConfig(t, `url = "https://x"
`)

	_, err := LoadLocked()
	if !errors.Is(err, ErrLockedNeedsAPIKey) {
		t.Fatalf("err=%v, want ErrLockedNeedsAPIKey", err)
	}
}
