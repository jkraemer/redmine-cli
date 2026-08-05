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
	"github.com/spf13/pflag"

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
	root.AddCommand(newCategoriesCmd(rc))
	root.AddCommand(newTimeActivitiesCmd(rc))
	root.AddCommand(newSearchCmd(rc))
	root.AddCommand(newWikiCmd(rc))
	root.AddCommand(newQueriesCmd(rc))
	return root
}

func TestResolveFormat(t *testing.T) {
	newFlags := func() *pflag.FlagSet {
		fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
		fs.StringP("format", "f", "", "")
		fs.BoolP("markdown", "m", false, "")
		fs.BoolP("json", "j", false, "")
		return fs
	}

	tests := []struct {
		name    string
		args    []string
		def     string
		want    string
		wantErr bool
	}{
		{name: "none falls back to default", args: nil, def: "markdown", want: "markdown"},
		{name: "none with empty default", args: nil, def: "", want: ""},
		{name: "-m sets markdown", args: []string{"-m"}, def: "json", want: "markdown"},
		{name: "-j sets json", args: []string{"-j"}, def: "markdown", want: "json"},
		{name: "--format value", args: []string{"--format", "markdown"}, def: "json", want: "markdown"},
		{name: "-f value", args: []string{"-f", "json"}, def: "markdown", want: "json"},
		{name: "-m and -j conflict", args: []string{"-m", "-j"}, def: "json", wantErr: true},
		{name: "--format rejects unknown value", args: []string{"--format", "xml"}, def: "json", wantErr: true},
		{name: "invalid config default is rejected", args: nil, def: "markdow", wantErr: true},
		{name: "-m and -f conflict", args: []string{"-m", "-f", "json"}, def: "json", wantErr: true},
		{name: "-j and --format conflict", args: []string{"-j", "--format", "markdown"}, def: "json", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFlags()
			if err := fs.Parse(tt.args); err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, err := resolveFormat(fs, tt.def)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got format %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveFormat = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRoot_MarkdownShorthand drives the real Build wiring to confirm the -m
// persistent flag reaches resolveFormat through a subcommand's Flags() and
// selects markdown output.
func TestRoot_MarkdownShorthand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":1,"identifier":"foo","name":"Foo"}],"total_count":1,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", srv.URL)
	t.Setenv("REDMINE_API_KEY", "k")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")

	var out bytes.Buffer
	root := Build(context.Background(), &out, &bytes.Buffer{})
	root.SetArgs([]string{"projects", "list", "-m"})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "| ID | Identifier | Name |") {
		t.Errorf("-m did not select markdown:\n%s", out.String())
	}
}

// TestRoot_ConflictingFormatFlags confirms that passing both -m and -j is
// refused before any server call.
func TestRoot_ConflictingFormatFlags(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", srv.URL)
	t.Setenv("REDMINE_API_KEY", "k")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")

	root := Build(context.Background(), &bytes.Buffer{}, &bytes.Buffer{})
	root.SetArgs([]string{"projects", "list", "-m", "-j"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for conflicting format flags, got nil")
	}
	if !strings.Contains(err.Error(), "only one") {
		t.Errorf("error should explain the conflict: %q", err.Error())
	}
	if hit {
		t.Error("server was called despite a flag conflict")
	}
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

// TestAuthStatus_PrintsExpiryInUTC pins the expiry display to actual UTC:
// the stored token carries a +02:00 offset, and the "… UTC" line must show
// the converted instant, not the offset's wall-clock time.
func TestAuthStatus_PrintsExpiryInUTC(t *testing.T) {
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
	exp := time.Now().Add(time.Hour).In(time.FixedZone("PLUSTWO", 2*3600))
	if err := cfg.SaveToken(&auth.Token{
		AccessToken: "AT",
		TokenType:   "Bearer",
		ExpiresAt:   exp,
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}}
	cmd := newAuthStatusCmd(rc)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatal(err)
	}
	want := exp.UTC().Format("2006-01-02 15:04:05") + " UTC"
	if !strings.Contains(out.String(), want) {
		t.Errorf("expiry not shown in UTC: want %q in output:\n%s", want, out.String())
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

// TestHelp_ShowsLongDescription: human --help must include the command's
// Long (or Short) description like cobra's default help does — the agent
// JSON isn't the only consumer of that documentation.
func TestHelp_ShowsLongDescription(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"queries", "list", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "does not expose a query's") {
		t.Errorf("help missing the Long description:\n%s", combined)
	}
}

func TestHelp_ShowsShortWhenNoLong(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"issues", "list", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "List issues") {
		t.Errorf("help missing the Short description:\n%s", combined)
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
