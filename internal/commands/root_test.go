package commands

import (
	"bytes"
	"context"
	"encoding/json"
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

// buildRootForTest returns a minimal root command with the runCtx pre-populated.
// It bypasses PersistentPreRunE so tests can inject *api.Client directly without
// needing real config or environment variables.
func buildRootForTest(rc *runCtx) *cobra.Command {
	root := &cobra.Command{Use: "redmine-cli", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(rc.out)
	root.SetErr(rc.errOut)
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error { return nil }
	root.AddCommand(newProjectsCmd(rc))
	root.AddCommand(newIssuesCmd(rc))
	root.AddCommand(newAttachmentsCmd(rc))
	root.AddCommand(newTimeCmd(rc))
	root.AddCommand(newUsersCmd(rc))
	root.AddCommand(newTrackersCmd(rc))
	root.AddCommand(newStatusesCmd(rc))
	root.AddCommand(newPrioritiesCmd(rc))
	root.AddCommand(newTimeActivitiesCmd(rc))
	root.AddCommand(newSearchCmd(rc))
	root.AddCommand(newWikiCmd(rc))
	root.AddCommand(newQueriesCmd(rc))
	return root
}

func TestRoot_AgentHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--agent", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["command"] != "redmine-cli" {
		t.Errorf("command=%v", got["command"])
	}
}

// M2: a parent context stored on runCtx must reach the HTTP layer, so
// cancelling it (e.g. on Ctrl-C) interrupts in-flight requests.
func TestRunCtx_CancelsInFlightHTTP(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-block:
		}
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rc := &runCtx{
		out:       &bytes.Buffer{},
		errOut:    &bytes.Buffer{},
		client:    api.New(srv.URL, "k", srv.Client()),
		format:    "json",
		parentCtx: ctx,
	}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"projects", "list"})

	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("server never received request")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected error after context cancel, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("command did not return after context cancel")
	}
}

func TestAuthStatus_ShowsScope(t *testing.T) {
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
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{
		AccessToken: "AT",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "view_project edit_issues",
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}}
	cmd := newAuthStatusCmd(rc)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "view_project edit_issues") {
		t.Errorf("status output missing scope: %s", out.String())
	}
}

func TestAuthStatus_ShowsNoneWhenScopeMissing(t *testing.T) {
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
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{
		AccessToken: "AT",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}}
	cmd := newAuthStatusCmd(rc)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "(none reported)") {
		t.Errorf("status output should mention '(none reported)': %s", out.String())
	}
}

func TestConfigFlag_PicksAlternatePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // hermetic: no default config exists
	altPath := filepath.Join(dir, "alt.toml")
	if err := os.WriteFile(altPath, []byte(`url = "https://alt.example"
oauth_client_id = "cid"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")

	// Seed a token via the alt-config path so auth status has something to show.
	cfg, err := config.Load(altPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveToken(&auth.Token{
		AccessToken: "AT",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "view_project",
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	root := Build(context.Background(), &out, &bytes.Buffer{})
	root.SetArgs([]string{"--config", altPath, "auth", "status"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "view_project") {
		t.Errorf("--config did not select alt file; output: %s", out.String())
	}
}

func TestRoot_Help_ListsSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	combined := out.String() + errOut.String()
	for _, want := range []string{"projects", "issues", "attachments"} {
		if !strings.Contains(combined, want) {
			t.Errorf("help missing %q\n%s", want, combined)
		}
	}
}
