package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPKCE(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) < 43 {
		t.Errorf("verifier too short: %d", len(v))
	}
	c := Challenge(v)
	if c == "" {
		t.Error("empty challenge")
	}
	if c == v {
		t.Error("challenge must differ from verifier")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "cfg"))

	tok := &Token{
		AccessToken:  "abc",
		RefreshToken: "def",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).Round(time.Second),
	}
	if err := SaveToken(tok); err != nil {
		t.Fatal(err)
	}
	path, _ := TokenPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("want mode 0600, got %o", info.Mode().Perm())
	}
	got, err := LoadToken()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != tok.AccessToken || got.RefreshToken != tok.RefreshToken {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if err := DeleteToken(); err != nil {
		t.Fatal(err)
	}
	got, err = LoadToken()
	if err != nil || got != nil {
		t.Errorf("expected nil after delete, got %v %v", got, err)
	}
}

func TestTokenExpired(t *testing.T) {
	past := &Token{ExpiresAt: time.Now().Add(-time.Minute)}
	if !past.Expired() {
		t.Error("expected expired")
	}
	future := &Token{ExpiresAt: time.Now().Add(time.Hour)}
	if future.Expired() {
		t.Error("expected not expired")
	}
	noExpiry := &Token{}
	if noExpiry.Expired() {
		t.Error("zero ExpiresAt should not be expired")
	}
}
