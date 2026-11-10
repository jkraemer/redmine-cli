package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/config"
)

// oauthServer fakes a Redmine with dynamic client registration: RFC 8414
// metadata (unless noMetadata), an RFC 7591 registration endpoint, and the
// Doorkeeper token endpoint. It counts registrations and records the
// client_id sent to the token endpoint.
type oauthServer struct {
	*httptest.Server
	registrations atomic.Int32
	tokenClientID string
	tokenVerifier string
}

func newOAuthServer(t *testing.T, noMetadata bool) *oauthServer {
	t.Helper()
	s := &oauthServer{}
	mux := http.NewServeMux()
	if !noMetadata {
		mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"issuer":                s.URL,
				"registration_endpoint": s.URL + "/mcp/register",
			})
		})
	}
	mux.HandleFunc("/mcp/register", func(w http.ResponseWriter, r *http.Request) {
		s.registrations.Add(1)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"client_id":     "registered-id",
			"client_secret": "unused-secret",
			"scope":         "view_project view_issues",
		})
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		s.tokenClientID = r.PostForm.Get("client_id")
		s.tokenVerifier = r.PostForm.Get("code_verifier")
		if r.PostForm.Get("client_secret") != "" {
			t.Errorf("public client must not send client_secret")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "AT",
			"refresh_token": "RT",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// runAuthLogin runs `auth login` against the config file at p, feeding an
// authorization code on stdin.
func runAuthLogin(t *testing.T, p string) (string, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_SECRET", "")
	t.Setenv("REDMINE_LOCKED", "")
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetIn(strings.NewReader("the-code\n"))
	root.SetArgs([]string{"--config", p, "auth", "login"})
	err := root.Execute()
	return out.String(), err
}

// authorizeScope returns the scope parameter of the authorization URL that
// `auth login` printed in out.
func authorizeScope(t *testing.T, out string) string {
	t.Helper()
	for _, f := range strings.Fields(out) {
		if strings.Contains(f, "/oauth/authorize?") {
			u, err := url.Parse(f)
			if err != nil {
				t.Fatal(err)
			}
			return u.Query().Get("scope")
		}
	}
	t.Fatalf("no authorization URL in output:\n%s", out)
	return ""
}

func TestAuthLogin_RegistersClientWhenNoneConfigured(t *testing.T) {
	srv := newOAuthServer(t, false)
	p := writeConfigFile(t, `url = "`+srv.URL+`"`+"\n")

	out, err := runAuthLogin(t, p)
	if err != nil {
		t.Fatalf("auth login: %v\n%s", err, out)
	}
	if n := srv.registrations.Load(); n != 1 {
		t.Errorf("registrations=%d want 1", n)
	}
	if srv.tokenClientID != "registered-id" {
		t.Errorf("token exchange client_id=%q want registered-id", srv.tokenClientID)
	}
	if srv.tokenVerifier == "" {
		t.Error("token exchange must use PKCE for a registered public client")
	}
	if !strings.Contains(out, "client_id=registered-id") {
		t.Errorf("authorize URL must use the registered client:\n%s", out)
	}
	if got := authorizeScope(t, out); got != "view_project view_issues" {
		t.Errorf("authorize URL scope=%q want the registered scopes", got)
	}
	if !strings.Contains(out, "Registered OAuth client") {
		t.Errorf("output does not mention the registration:\n%s", out)
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OAuthClientID != "registered-id" {
		t.Errorf("saved oauth_client_id=%q", cfg.OAuthClientID)
	}
	if !reflect.DeepEqual(cfg.OAuthScopes, []string{"view_project", "view_issues"}) {
		t.Errorf("saved oauth_scopes=%v want registered scopes", cfg.OAuthScopes)
	}
	if cfg.OAuthClientSecret != "" {
		t.Errorf("returned client_secret must not be saved, got %q", cfg.OAuthClientSecret)
	}
	if cfg.Token == nil || cfg.Token.AccessToken != "AT" {
		t.Errorf("token not saved: %+v", cfg.Token)
	}
}

func TestAuthLogin_ConfiguredScopesAreKeptOnRegistration(t *testing.T) {
	srv := newOAuthServer(t, false)
	p := writeConfigFile(t, `url = "`+srv.URL+`"`+"\n"+`oauth_scopes = ["view_issues"]`+"\n")

	out, err := runAuthLogin(t, p)
	if err != nil {
		t.Fatalf("auth login: %v\n%s", err, out)
	}
	if got := authorizeScope(t, out); got != "view_issues" {
		t.Errorf("authorize URL scope=%q want view_issues", got)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.OAuthScopes, []string{"view_issues"}) {
		t.Errorf("oauth_scopes=%v want configured [view_issues]", cfg.OAuthScopes)
	}
}

func TestAuthLogin_SwitchesAPIKeyConfigToOAuth(t *testing.T) {
	srv := newOAuthServer(t, false)
	p := writeConfigFile(t, `url = "`+srv.URL+`"`+"\n"+`api_key = "k"`+"\n")

	out, err := runAuthLogin(t, p)
	if err != nil {
		t.Fatalf("auth login: %v\n%s", err, out)
	}
	if n := srv.registrations.Load(); n != 1 {
		t.Errorf("registrations=%d want 1", n)
	}
	if !strings.Contains(out, "api_key") {
		t.Errorf("output should note that api_key is no longer used:\n%s", out)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthMethod() != "oauth" {
		t.Errorf("AuthMethod=%q want oauth", cfg.AuthMethod())
	}
}

func TestAuthLogin_ConfiguredClientIDSkipsRegistration(t *testing.T) {
	srv := newOAuthServer(t, false)
	p := writeConfigFile(t, `url = "`+srv.URL+`"`+"\n"+`oauth_client_id = "existing"`+"\n")

	out, err := runAuthLogin(t, p)
	if err != nil {
		t.Fatalf("auth login: %v\n%s", err, out)
	}
	if n := srv.registrations.Load(); n != 0 {
		t.Errorf("registrations=%d want 0", n)
	}
	if srv.tokenClientID != "existing" {
		t.Errorf("token exchange client_id=%q want existing", srv.tokenClientID)
	}
}

func TestAuthLogin_UnsupportedRegistrationIsConfigError(t *testing.T) {
	srv := newOAuthServer(t, true)
	p := writeConfigFile(t, `url = "`+srv.URL+`"`+"\n")

	_, err := runAuthLogin(t, p)
	assertExitCode(t, err, 3)
	if !strings.Contains(err.Error(), "oauth_client_id") {
		t.Errorf("err=%v should tell the user to configure oauth_client_id", err)
	}
	cfg, lerr := config.LoadForLogin(p)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.OAuthClientID != "" {
		t.Errorf("oauth_client_id=%q must stay unset", cfg.OAuthClientID)
	}
}
