package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/auth"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ReadOnlyFromEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "https://x")
	t.Setenv("REDMINE_API_KEY", "k")
	t.Setenv("REDMINE_READ_ONLY", "true")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ReadOnly {
		t.Errorf("ReadOnly=false, want true")
	}
}

func TestLoad_ReadOnlyFromTOML(t *testing.T) {
	p := writeConfig(t, `url = "https://x"
api_key = "k"
read_only = true
`)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_READ_ONLY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ReadOnly {
		t.Errorf("ReadOnly=false, want true")
	}
}

func TestLoad_ReadOnlyEnvFalseOverridesTOMLTrue(t *testing.T) {
	p := writeConfig(t, `url = "https://x"
api_key = "k"
read_only = true
`)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_READ_ONLY", "false")

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ReadOnly {
		t.Errorf("ReadOnly=true, want false (env must override file)")
	}
}

func TestLoad_ReadOnlyEnvTrueOverridesTOMLFalse(t *testing.T) {
	p := writeConfig(t, `url = "https://x"
api_key = "k"
read_only = false
`)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_READ_ONLY", "true")

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ReadOnly {
		t.Errorf("ReadOnly=false, want true (env must override file)")
	}
}

func TestLoad_ReadOnlyDefaultsFalse(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "https://x")
	t.Setenv("REDMINE_API_KEY", "k")
	t.Setenv("REDMINE_READ_ONLY", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ReadOnly {
		t.Errorf("ReadOnly=true, want false by default")
	}
}

func TestLoad_ReadOnlyInvalidEnvErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "https://x")
	t.Setenv("REDMINE_API_KEY", "k")
	t.Setenv("REDMINE_READ_ONLY", "yes-please")

	_, err := Load("")
	if err == nil {
		t.Fatal("expected error for invalid REDMINE_READ_ONLY, got nil")
	}
}

func TestReadOnly_BestEffortEnvOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_READ_ONLY", "1")
	if !ReadOnly("") {
		t.Errorf("ReadOnly()=false, want true from env")
	}
}

func TestReadOnly_BestEffortFileOnly(t *testing.T) {
	// No URL/API key configured: ReadOnly must still resolve, unlike Load.
	p := writeConfig(t, "read_only = true\n")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_READ_ONLY", "")
	if !ReadOnly(p) {
		t.Errorf("ReadOnly(%q)=false, want true from file", p)
	}
}

func TestReadOnly_BestEffortInvalidEnvIsFalse(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_READ_ONLY", "garbage")
	if ReadOnly("") {
		t.Errorf("ReadOnly()=true, want false (invalid env swallowed)")
	}
}

func TestReadOnly_BestEffortMissingIsFalse(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_READ_ONLY", "")
	if ReadOnly("") {
		t.Errorf("ReadOnly()=true, want false when nothing configured")
	}
}

func TestSaveToken_PreservesReadOnly(t *testing.T) {
	p := writeConfig(t, `url = "https://x"
oauth_client_id = "cid"
read_only = true
`)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_READ_ONLY", "")

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{AccessToken: "AT", TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}

	cfg2, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg2.ReadOnly {
		t.Errorf("read_only clobbered by SaveToken: ReadOnly=false")
	}
}
