# OAuth 2.0 Auth Flow Implementation Plan

> **For Claude Code:** Implement this plan task-by-task in `/home/jk/projects/redmine_cli`.
> All commands run from that directory unless stated otherwise.

**Goal:** Implement `redmine-cli auth login/logout/status` using the OAuth 2.0
authorization-code + PKCE + OOB flow. Fixes issue #1461.

**Architecture:**
- New `internal/auth/` package owns token storage, PKCE, and the OAuth HTTP exchange.
- New `internal/commands/auth.go` adds the `auth` subcommand tree.
- `internal/config/config.go` gets an `AuthMethod()` helper.
- `internal/api/client.go` gains a `NewWithToken` constructor that uses a Bearer token.
- `internal/commands/root.go` updated to init the client via OAuth token when available.

**OAuth Flow (OOB / headless):**
1. Generate PKCE code_verifier + code_challenge (S256).
2. Build the authorization URL: `{url}/oauth/authorize?response_type=code&client_id=...&redirect_uri=urn:ietf:wg:oauth:2.0:oob&code_challenge=...&code_challenge_method=S256`.
3. Print the URL; prompt user to paste the authorization code.
4. POST to `{url}/oauth/token` to exchange code → access_token + refresh_token.
5. Write token JSON to `~/.config/redmine-cli/token.json` (chmod 0600).

**No new external dependencies.** Use only Go stdlib (`crypto/sha256`, `encoding/base64`,
`net/http`, `encoding/json`, `bufio`). Do NOT add golang.org/x/oauth2.

---

## Task 1: Create internal/auth package

**Files:**
- Create: `internal/auth/token.go`  — token storage struct + read/write/delete
- Create: `internal/auth/pkce.go`   — PKCE verifier/challenge generation
- Create: `internal/auth/flow.go`   — authorization URL builder + code exchange
- Create: `internal/auth/auth_test.go` — tests for PKCE and token round-trip

### internal/auth/token.go

```go
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Token holds the stored OAuth token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// Expired reports whether the access token has expired (with a 30s buffer).
func (t *Token) Expired() bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(30 * time.Second).After(t.ExpiresAt)
}

// TokenPath returns the path to the token file.
func TokenPath() (string, error) {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "redmine-cli", "token.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "redmine-cli", "token.json"), nil
}

// LoadToken reads the stored token from disk. Returns nil, nil if no token exists.
func LoadToken() (*Token, error) {
	path, err := TokenPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// SaveToken writes the token to disk with mode 0600.
func SaveToken(t *Token) error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// DeleteToken removes the token file. Returns nil if the file does not exist.
func DeleteToken() error {
	path, err := TokenPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
```

### internal/auth/pkce.go

```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// GenerateVerifier returns a cryptographically random PKCE code_verifier (43-128 chars).
func GenerateVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Challenge computes the S256 code_challenge from a verifier.
func Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
```

### internal/auth/flow.go

```go
package auth

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const redirectURI = "urn:ietf:wg:oauth:2.0:oob"

// AuthorizeURL builds the authorization URL the user must open in their browser.
func AuthorizeURL(baseURL, clientID, verifier string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	u.Path = "/oauth/authorize"
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", Challenge(verifier))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// tokenResponse is the raw JSON from /oauth/token.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // seconds; 0 means no expiry
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Exchange trades the authorization code for a token.
func Exchange(baseURL, clientID, code, verifier string) (*Token, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/oauth/token"
	resp, err := http.PostForm(endpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("token error %s: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("no access_token in response (status %d): %s", resp.StatusCode, body)
	}
	t := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
	}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return t, nil
}

// Refresh uses the refresh_token to obtain a new access token.
func Refresh(baseURL, clientID, refreshToken string) (*Token, error) {
	endpoint := strings.TrimRight(baseURL, "/") + "/oauth/token"
	resp, err := http.PostForm(endpoint, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
	})
	if err != nil {
		return nil, fmt.Errorf("token refresh: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("refresh error %s: %s", tr.Error, tr.ErrorDesc)
	}
	t := &Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: refreshToken, // keep old refresh token if new one not issued
		TokenType:    tr.TokenType,
	}
	if tr.RefreshToken != "" {
		t.RefreshToken = tr.RefreshToken
	}
	if tr.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return t, nil
}

// PromptCode prints the authorization URL and reads the code from r (typically os.Stdin).
func PromptCode(out io.Writer, in io.Reader, authURL string) (string, error) {
	fmt.Fprintf(out, "\nOpen this URL in your browser:\n\n  %s\n\n", authURL)
	fmt.Fprintf(out, "Paste the authorization code shown by Redmine: ")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return "", fmt.Errorf("no code entered")
	}
	code := strings.TrimSpace(scanner.Text())
	if code == "" {
		return "", fmt.Errorf("empty authorization code")
	}
	return code, nil
}
```

### internal/auth/auth_test.go

```go
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
```

**Verify:** `go test ./internal/auth/` — all tests pass.

---

## Task 2: Update API client to support Bearer token auth

**Files:**
- Modify: `internal/api/client.go`

Add a `NewWithToken` constructor and update `do()` to use Bearer when the token field is set:

```go
// Client is the Redmine HTTP client.
type Client struct {
	baseURL string
	apiKey  string
	token   string // OAuth Bearer token (takes priority over apiKey)
	http    *http.Client
}

// New creates a Client using an API key.
func New(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: httpClient}
}

// NewWithToken creates a Client using an OAuth Bearer token.
func NewWithToken(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: httpClient}
}
```

In `do()`, replace the header line:
```go
// old:
req.Header.Set("X-Redmine-API-Key", c.apiKey)

// new:
if c.token != "" {
    req.Header.Set("Authorization", "Bearer "+c.token)
} else {
    req.Header.Set("X-Redmine-API-Key", c.apiKey)
}
```

Apply the same change to `doWriteJSON()`.

**Verify:** `go build ./internal/api/` — no errors.

---

## Task 3: Update root.go to init client via OAuth when token exists

**Files:**
- Modify: `internal/commands/root.go`
- Modify: `internal/config/config.go`

### config.go: add AuthMethod helper

```go
// AuthMethod returns "oauth" if an oauth_client_id is configured, else "apikey".
func (c *Config) AuthMethod() string {
	if c.OAuthClientID != "" {
		return "oauth"
	}
	return "apikey"
}
```

Also remove the `ErrMissingAPIKey` guard from `Load()` when OAuthClientID is set — replace:
```go
if cfg.APIKey == "" {
    return nil, ErrMissingAPIKey
}
```
with:
```go
if cfg.APIKey == "" && cfg.OAuthClientID == "" {
    return nil, ErrMissingAPIKey
}
```

### root.go: update PersistentPreRunE

Import `"github.com/jkraemer/redmine-cli/internal/auth"` and update the client init block:

```go
root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
    if cmd.Name() == "help" || cmd.Name() == "redmine-cli" {
        return nil
    }
    // Skip client init for auth subcommands (they manage the token themselves).
    if cmd.Parent() != nil && cmd.Parent().Name() == "auth" {
        return nil
    }
    cfg, err := config.Load()
    if err != nil {
        return err
    }
    if rc.format == "" {
        rc.format = cfg.DefaultFormat
    }
    if cfg.AuthMethod() == "oauth" {
        tok, err := auth.LoadToken()
        if err != nil {
            return fmt.Errorf("loading OAuth token: %w", err)
        }
        if tok == nil {
            return fmt.Errorf("not authenticated — run: redmine-cli auth login")
        }
        if tok.Expired() && tok.RefreshToken != "" {
            tok, err = auth.Refresh(cfg.URL, cfg.OAuthClientID, tok.RefreshToken)
            if err != nil {
                return fmt.Errorf("token refresh failed: %w (run: redmine-cli auth login)", err)
            }
            if err := auth.SaveToken(tok); err != nil {
                return err
            }
        }
        rc.client = api.NewWithToken(cfg.URL, tok.AccessToken, http.DefaultClient)
    } else {
        rc.client = api.New(cfg.URL, cfg.APIKey, http.DefaultClient)
    }
    return nil
}
```

Add `"net/http"` and `"github.com/jkraemer/redmine-cli/internal/auth"` to imports in root.go.

**Verify:** `go build ./...` — no errors.

---

## Task 4: Add auth command in internal/commands/auth.go

**Files:**
- Create: `internal/commands/auth.go`
- Modify: `internal/commands/root.go` — add `root.AddCommand(newAuthCmd(rc))`

```go
package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/auth"
	"github.com/jkraemer/redmine-cli/internal/config"
)

func newAuthCmd(rc *runCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication (OAuth 2.0)",
	}
	cmd.AddCommand(newAuthLoginCmd(rc))
	cmd.AddCommand(newAuthLogoutCmd())
	cmd.AddCommand(newAuthStatusCmd(rc))
	return cmd
}

func newAuthLoginCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate via OAuth 2.0 (prints URL, prompts for code)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.OAuthClientID == "" {
				return fmt.Errorf("oauth_client_id not configured — set it in config.toml or REDMINE_OAUTH_CLIENT_ID")
			}
			verifier, err := auth.GenerateVerifier()
			if err != nil {
				return fmt.Errorf("generating PKCE verifier: %w", err)
			}
			authURL, err := auth.AuthorizeURL(cfg.URL, cfg.OAuthClientID, verifier)
			if err != nil {
				return fmt.Errorf("building auth URL: %w", err)
			}
			code, err := auth.PromptCode(rc.out, os.Stdin, authURL)
			if err != nil {
				return err
			}
			tok, err := auth.Exchange(cfg.URL, cfg.OAuthClientID, code, verifier)
			if err != nil {
				return fmt.Errorf("token exchange: %w", err)
			}
			if err := auth.SaveToken(tok); err != nil {
				return err
			}
			fmt.Fprintln(rc.out, "Authenticated successfully. Token saved.")
			return nil
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored OAuth token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := auth.DeleteToken(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Logged out.")
			return nil
		},
	}
}

func newAuthStatusCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if cfg.AuthMethod() == "oauth" {
				tok, err := auth.LoadToken()
				if err != nil {
					return err
				}
				if tok == nil {
					fmt.Fprintln(rc.out, "Auth method: oauth\nStatus:      not logged in (run: redmine-cli auth login)")
					return nil
				}
				expiry := "none (non-expiring)"
				if !tok.ExpiresAt.IsZero() {
					expiry = tok.ExpiresAt.Format("2006-01-02 15:04:05 UTC")
					if tok.Expired() {
						expiry += " (EXPIRED)"
					}
				}
				fmt.Fprintf(rc.out, "Auth method: oauth\nStatus:      logged in\nExpires:     %s\n", expiry)
			} else {
				fmt.Fprintln(rc.out, "Auth method: api_key")
			}
			return nil
		},
	}
}
```

**Verify:** `make build` — no errors; `./redmine-cli auth --help` shows login/logout/status subcommands.

---

## Task 5: Run all tests + final build

```bash
go test ./...
make build
./redmine-cli auth status
./redmine-cli auth --help
```

All tests pass, binary builds, `auth status` reports API key auth (existing config has no token).
