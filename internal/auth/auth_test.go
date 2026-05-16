package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestExchange_ParsesScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":3600,"scope":"view_project edit_issues"}`)
	}))
	defer srv.Close()

	tok, err := Exchange(srv.URL, "cid", "shh", "code", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Scope != "view_project edit_issues" {
		t.Errorf("Scope=%q want %q", tok.Scope, "view_project edit_issues")
	}
}

func TestRefresh_ParsesScopeWhenPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"AT2","token_type":"Bearer","expires_in":3600,"scope":"view_project"}`)
	}))
	defer srv.Close()

	tok, err := Refresh(srv.URL, "cid", "", "RT")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Scope != "view_project" {
		t.Errorf("Scope=%q want view_project", tok.Scope)
	}
}

func TestRefreshWithScope_KeepsPriorScopeWhenServerOmits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"AT2","token_type":"Bearer","expires_in":3600}`)
	}))
	defer srv.Close()

	tok, err := RefreshWithScope(srv.URL, "cid", "", "RT", "view_project edit_issues")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Scope != "view_project edit_issues" {
		t.Errorf("Scope=%q want preserved prior scope", tok.Scope)
	}
}

func TestRefreshWithScope_ServerEchoWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"AT2","token_type":"Bearer","expires_in":3600,"scope":"view_project"}`)
	}))
	defer srv.Close()

	tok, err := RefreshWithScope(srv.URL, "cid", "", "RT", "view_project edit_issues")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Scope != "view_project" {
		t.Errorf("Scope=%q want server-reported scope to win", tok.Scope)
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
