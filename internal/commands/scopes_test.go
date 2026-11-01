package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/auth"
	"github.com/jkraemer/redmine-cli/internal/config"
)

// forbiddenServer answers every request with 403 and a Redmine-style body.
func forbiddenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"errors":["You are not authorized to access this page."]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeOAuthConfig writes a config file pointing at srvURL with OAuth
// client credentials and a stored, unexpired token carrying scope. Env
// vars that would override the file are cleared.
func writeOAuthConfig(t *testing.T, srvURL, scope string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	contents := fmt.Sprintf("url = %q\noauth_client_id = \"cid\"\n", srvURL)
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")
	t.Setenv("REDMINE_READ_ONLY", "")
	t.Setenv("REDMINE_LOCKED", "")
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{
		AccessToken: "AT",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       scope,
	}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestForbidden_OAuth_NamesGrantedAndRequiredScopes(t *testing.T) {
	srv := forbiddenServer(t)
	p := writeOAuthConfig(t, srv.URL, "view_issues edit_issues")

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "search", "phase 2"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"redmine API 403",
		`OAuth token scope: "view_issues edit_issues"`,
		"this command needs: search_project",
		"oauth_scopes",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Errorf("wrapped error lost *api.Error 403: %v", err)
	}
	if code := exitCodeFor(err); code != 4 {
		t.Errorf("exit code=%d, want 4", code)
	}
}

func TestForbidden_OAuth_UnknownCommandOmitsNeeds(t *testing.T) {
	srv := forbiddenServer(t)
	p := writeOAuthConfig(t, srv.URL, "view_project")

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	// users list is admin-only and has no scope table entry.
	root.SetArgs([]string{"--config", p, "users", "list"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, `OAuth token scope: "view_project"`) {
		t.Errorf("error %q missing granted scope", msg)
	}
	if strings.Contains(msg, "this command needs") {
		t.Errorf("error %q must not guess a required scope", msg)
	}
	if code := exitCodeFor(err); code != 4 {
		t.Errorf("exit code=%d, want 4", code)
	}
}

func TestForbidden_OAuth_EmptyScopeReported(t *testing.T) {
	srv := forbiddenServer(t)
	p := writeOAuthConfig(t, srv.URL, "")

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "wiki", "list", "--project", "x"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "OAuth token scope: (none reported)") {
		t.Errorf("error %q missing none-reported marker", msg)
	}
	if !strings.Contains(msg, "this command needs: view_wiki_pages") {
		t.Errorf("error %q missing required scope", msg)
	}
}

func TestForbidden_APIKey_MessageUnchanged(t *testing.T) {
	srv := forbiddenServer(t)
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(fmt.Sprintf("url = %q\napi_key = \"k\"\n", srv.URL)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")
	t.Setenv("REDMINE_LOCKED", "")

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "search", "phase 2"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("not an *api.Error: %v", err)
	}
	if err.Error() != apiErr.Error() {
		t.Errorf("API-key 403 was decorated: %q", err.Error())
	}
	if strings.Contains(err.Error(), "scope") {
		t.Errorf("API-key 403 mentions scope: %q", err.Error())
	}
	if code := exitCodeFor(err); code != 4 {
		t.Errorf("exit code=%d, want 4", code)
	}
}

func TestDecorateForbidden_LeavesOtherErrorsAlone(t *testing.T) {
	rc := &runCtx{authMethod: "oauth", oauthScope: "view_issues"}
	cmd := &cobra.Command{Use: "search"}
	notFound := &api.Error{Status: 404, URL: "u", Body: "b"}
	if got := decorateForbidden(rc, cmd, notFound); got != notFound {
		t.Errorf("404 was changed: %v", got)
	}
	plain := errors.New("boom")
	if got := decorateForbidden(rc, cmd, plain); got != plain {
		t.Errorf("plain error was changed: %v", got)
	}
	if got := decorateForbidden(rc, cmd, nil); got != nil {
		t.Errorf("nil became %v", got)
	}
}
