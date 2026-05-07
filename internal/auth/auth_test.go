package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func TestAuthorizeURL(t *testing.T) {
	const base = "https://redmine.example"
	const clientID = "cid"
	verifier, err := GenerateVerifier()
	if err != nil {
		t.Fatal(err)
	}

	pkceURL, err := AuthorizeURL(base, clientID, verifier, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pkceURL, "code_challenge=") {
		t.Errorf("PKCE URL missing code_challenge: %s", pkceURL)
	}
	if !strings.Contains(pkceURL, "code_challenge_method=S256") {
		t.Errorf("PKCE URL missing code_challenge_method: %s", pkceURL)
	}

	confURL, err := AuthorizeURL(base, clientID, verifier, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(confURL, "code_challenge") {
		t.Errorf("confidential URL must not contain code_challenge: %s", confURL)
	}
	if strings.Contains(confURL, "code_challenge_method") {
		t.Errorf("confidential URL must not contain code_challenge_method: %s", confURL)
	}
}

func TestAuthorizeURL_WithScopes(t *testing.T) {
	const base = "https://redmine.example"
	verifier, _ := GenerateVerifier()
	scopes := []string{"view_project", "edit_issues"}

	got, err := AuthorizeURL(base, "cid", verifier, true, scopes)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if s := u.Query().Get("scope"); s != "view_project edit_issues" {
		t.Errorf("scope=%q want %q", s, "view_project edit_issues")
	}
}

func TestAuthorizeURL_NoScopesOmitsParam(t *testing.T) {
	verifier, _ := GenerateVerifier()
	got, err := AuthorizeURL("https://x", "cid", verifier, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "scope=") {
		t.Errorf("expected no scope param when scopes is empty: %s", got)
	}
}

func TestExchangeConfidential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if got := r.PostForm.Get("client_secret"); got != "shh" {
			t.Errorf("client_secret = %q, want %q", got, "shh")
		}
		if got := r.PostForm.Get("client_id"); got != "cid" {
			t.Errorf("client_id = %q, want %q", got, "cid")
		}
		if got := r.PostForm.Get("code"); got != "the-code" {
			t.Errorf("code = %q, want %q", got, "the-code")
		}
		if v, ok := r.PostForm["code_verifier"]; ok {
			t.Errorf("code_verifier must be absent in confidential mode, got %v", v)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	tok, err := Exchange(srv.URL, "cid", "shh", "the-code", "ignored-verifier")
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "AT" {
		t.Errorf("AccessToken = %q, want AT", tok.AccessToken)
	}
	if tok.RefreshToken != "RT" {
		t.Errorf("RefreshToken = %q, want RT", tok.RefreshToken)
	}
	if tok.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be set when expires_in is returned")
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
