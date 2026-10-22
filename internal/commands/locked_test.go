package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// attachmentBytes is the raw content the locked test server serves for
// attachment 42.
const attachmentBytes = "\x00raw attachment bytes\xff"

// newLockedServer starts a fake Redmine that counts every request it
// receives. It answers issues list and attachment 42 (metadata + content).
func newLockedServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/issues.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
		case "/attachments/42.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"attachment":{"id":42,"filename":"f.bin","filesize":%d,"content_type":"application/octet-stream","content_url":"%s/attachments/download/42/f.bin"}}`, len(attachmentBytes), srv.URL)
		case "/attachments/download/42/f.bin":
			_, _ = w.Write([]byte(attachmentBytes))
		default:
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// lockedEnv writes the default config file (url = srvURL, api_key = "k")
// under a fresh XDG_CONFIG_HOME, enables locked mode and clears the env vars
// that locked mode must ignore.
func lockedEnv(t *testing.T, srvURL string) {
	t.Helper()
	writeLockedConfig(t, fmt.Sprintf("url = %q\napi_key = \"k\"\n", srvURL))
}

// writeLockedConfig is lockedEnv with caller-supplied config file contents.
func writeLockedConfig(t *testing.T, contents string) {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "redmine-cli")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("REDMINE_LOCKED", "1")
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_READ_ONLY", "")
}

// runLocked executes the real root command with args and returns stdout and
// the command error.
func runLocked(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, _, err := runLockedWithStderr(t, args...)
	return out, err
}

// runLockedWithStderr is runLocked that also returns stderr.
func runLockedWithStderr(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

// assertLockedRefusal checks that err is a locked-mode refusal (exit 8).
func assertLockedRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrLocked) {
		t.Errorf("err=%v, want wraps ErrLocked", err)
	}
	if code := exitCodeFor(err); code != 8 {
		t.Errorf("exit code=%d, want 8", code)
	}
}

func TestLocked_AllowsReads(t *testing.T) {
	srv, hits := newLockedServer(t)
	lockedEnv(t, srv.URL)

	if _, err := runLocked(t, "issues", "list"); err != nil {
		t.Fatalf("issues list: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("hits=%d, want 1", n)
	}
}

func TestLocked_UsesFileNotEnvURL(t *testing.T) {
	srv, hits := newLockedServer(t)
	envSrv, envHits := newLockedServer(t)
	lockedEnv(t, srv.URL)
	t.Setenv("REDMINE_URL", envSrv.URL)

	if _, err := runLocked(t, "issues", "list"); err != nil {
		t.Fatalf("issues list: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("file server hits=%d, want 1", n)
	}
	if n := envHits.Load(); n != 0 {
		t.Errorf("env server hits=%d, want 0", n)
	}
}

func TestLocked_RefusesWriteWithoutConfirm(t *testing.T) {
	srv, hits := newLockedServer(t)
	lockedEnv(t, srv.URL)
	notes := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notes, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := runLockedWithStderr(t, "issues", "update", "7", "--notes-file", notes)
	assertLockedRefusal(t, err)
	if strings.Contains(out, "SECRET") {
		t.Errorf("stdout leaks notes file content:\n%s", out)
	}
	if strings.Contains(errOut, "SECRET") {
		t.Errorf("stderr leaks notes file content:\n%s", errOut)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("hits=%d, want 0", n)
	}
}

func TestLocked_RefusesEveryWriteCommand(t *testing.T) {
	cases := [][]string{
		{"issues", "create", "--subject", "s", "--project", "p"},
		{"issues", "update", "7", "--notes", "x"},
		{"issues", "watchers", "add", "7", "3"},
		{"issues", "watchers", "remove", "7", "3"},
		{"time", "log", "--issue", "7", "--hours", "1"},
		{"wiki", "put", "P", "--project", "p", "--text", "x"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			srv, hits := newLockedServer(t)
			lockedEnv(t, srv.URL)

			_, err := runLocked(t, args...)
			assertLockedRefusal(t, err)
			if n := hits.Load(); n != 0 {
				t.Errorf("hits=%d, want 0", n)
			}
		})
	}
}

func TestLocked_RefusesAuth(t *testing.T) {
	for _, sub := range []string{"login", "status", "logout"} {
		t.Run(sub, func(t *testing.T) {
			srv, hits := newLockedServer(t)
			lockedEnv(t, srv.URL)

			_, err := runLocked(t, "auth", sub)
			assertLockedRefusal(t, err)
			if n := hits.Load(); n != 0 {
				t.Errorf("hits=%d, want 0", n)
			}
		})
	}
}

func TestLocked_RefusesConfigFlag(t *testing.T) {
	cases := [][]string{
		{"--config=/x"},
		{"-c/x"},
		{"-c", "/x"},
		{"--config", ""},
	}
	for _, flagArgs := range cases {
		t.Run(strings.Join(flagArgs, " "), func(t *testing.T) {
			srv, hits := newLockedServer(t)
			lockedEnv(t, srv.URL)

			_, err := runLocked(t, append(flagArgs, "issues", "list")...)
			assertLockedRefusal(t, err)
			if n := hits.Load(); n != 0 {
				t.Errorf("hits=%d, want 0", n)
			}
		})
	}
}

func TestLocked_DownloadDefaultsToStdout(t *testing.T) {
	srv, _ := newLockedServer(t)
	lockedEnv(t, srv.URL)
	// Point the user cache dir at an empty temp dir so a regression that
	// writes to the default path is visible and doesn't touch the real cache.
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	out, err := runLocked(t, "attachments", "download", "42")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if out != attachmentBytes {
		t.Errorf("out=%q, want raw bytes %q", out, attachmentBytes)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("cache dir not empty: %v", entries)
	}
}

func TestLocked_RefusesDownloadToPath(t *testing.T) {
	srv, hits := newLockedServer(t)
	lockedEnv(t, srv.URL)
	dest := filepath.Join(t.TempDir(), "x")

	_, err := runLocked(t, "attachments", "download", "42", "-o", dest)
	assertLockedRefusal(t, err)
	if n := hits.Load(); n != 0 {
		t.Errorf("hits=%d, want 0", n)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("destination file was created (stat err=%v)", statErr)
	}

	out, err := runLocked(t, "attachments", "download", "42", "-o", "-")
	if err != nil {
		t.Fatalf("download -o -: %v", err)
	}
	if out != attachmentBytes {
		t.Errorf("out=%q, want raw bytes %q", out, attachmentBytes)
	}
}

func TestLocked_InvalidValueExit3(t *testing.T) {
	srv, hits := newLockedServer(t)
	lockedEnv(t, srv.URL)
	t.Setenv("REDMINE_LOCKED", "yes")

	_, err := runLocked(t, "issues", "list")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := exitCodeFor(err); code != 3 {
		t.Errorf("exit code=%d, want 3 (err=%v)", code, err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("hits=%d, want 0", n)
	}
}

func TestLocked_OAuthConfigExit3(t *testing.T) {
	srv, hits := newLockedServer(t)
	writeLockedConfig(t, fmt.Sprintf("url = %q\noauth_client_id = \"cid\"\n", srv.URL))

	_, err := runLocked(t, "issues", "list")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := exitCodeFor(err); code != 3 {
		t.Errorf("exit code=%d, want 3 (err=%v)", code, err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("hits=%d, want 0", n)
	}
}

func TestLocked_VersionWorks(t *testing.T) {
	srv, _ := newLockedServer(t)
	lockedEnv(t, srv.URL)

	if _, err := runLocked(t, "version"); err != nil {
		t.Errorf("version: %v (exit %d)", err, exitCodeFor(err))
	}
}

func TestLocked_PlainHelpWorks(t *testing.T) {
	srv, hits := newLockedServer(t)
	lockedEnv(t, srv.URL)

	out, err := runLocked(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v (exit %d)", err, exitCodeFor(err))
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("--help output lacks usage:\n%s", out)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("hits=%d, want 0", n)
	}
}

// agentHelp runs the real root command with args and decodes the
// --agent --help JSON it prints.
func agentHelp(t *testing.T, args ...string) map[string]any {
	t.Helper()
	out, err := runLocked(t, args...)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	return doc
}

func blockedSubcommands(doc map[string]any) map[string]bool {
	blocked := map[string]bool{}
	for _, s := range doc["subcommands"].([]any) {
		sc := s.(map[string]any)
		b, _ := sc["blocked"].(bool)
		blocked[sc["name"].(string)] = b
	}
	return blocked
}

func TestLocked_AgentHelpMarksBlocked(t *testing.T) {
	srv, _ := newLockedServer(t)
	lockedEnv(t, srv.URL)

	doc := agentHelp(t, "issues", "--agent", "--help")
	if doc["locked"] != true || doc["read_only"] != true {
		t.Fatalf("locked/read_only = %v/%v, want true/true", doc["locked"], doc["read_only"])
	}
	blocked := blockedSubcommands(doc)
	for _, name := range []string{"create", "update"} {
		if !blocked[name] {
			t.Errorf("%s should be blocked", name)
		}
	}
	for _, name := range []string{"list", "get"} {
		if blocked[name] {
			t.Errorf("%s should not be blocked", name)
		}
	}
	wantNote := "locked mode (redmine-safe) is active: write and auth commands are unavailable and exit 8; --config is refused; attachments download writes to stdout only"
	notes, _ := doc["notes"].([]any)
	found := false
	for _, n := range notes {
		found = found || n == wantNote
	}
	if !found {
		t.Errorf("notes %v missing locked note", notes)
	}

	leaf := agentHelp(t, "issues", "create", "--agent", "--help")
	if leaf["blocked"] != true {
		t.Errorf("issues create leaf should be blocked: %v", leaf)
	}
}

func TestLocked_AgentHelpAuthBlocked(t *testing.T) {
	srv, _ := newLockedServer(t)
	lockedEnv(t, srv.URL)

	doc := agentHelp(t, "--agent", "--help")
	if !blockedSubcommands(doc)["auth"] {
		t.Error("auth should be blocked")
	}
	leaf := agentHelp(t, "auth", "login", "--agent", "--help")
	if leaf["blocked"] != true {
		t.Errorf("auth login leaf should be blocked: %v", leaf)
	}
}

func TestLocked_AgentHelpIgnoresConfigFlag(t *testing.T) {
	srv, _ := newLockedServer(t)
	lockedEnv(t, srv.URL)
	p := filepath.Join(t.TempDir(), "other.toml")
	if err := os.WriteFile(p, []byte("read_only = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	doc := agentHelp(t, "-c", p, "issues", "--agent", "--help")
	if doc["read_only"] != true || doc["locked"] != true {
		t.Fatalf("read_only/locked = %v/%v, want true/true", doc["read_only"], doc["locked"])
	}
}

func TestLocked_AgentHelpInvalidValueFailsClosed(t *testing.T) {
	srv, _ := newLockedServer(t)
	lockedEnv(t, srv.URL)
	t.Setenv("REDMINE_LOCKED", "yes")

	doc := agentHelp(t, "issues", "--agent", "--help")
	if doc["locked"] != true {
		t.Fatalf("locked = %v, want true", doc["locked"])
	}
}

func TestAgentHelp_NotLockedOmitsField(t *testing.T) {
	srv, _ := newLockedServer(t)
	lockedEnv(t, srv.URL)
	t.Setenv("REDMINE_LOCKED", "")

	doc := agentHelp(t, "issues", "--agent", "--help")
	if _, ok := doc["locked"]; ok {
		t.Errorf("locked key present in normal mode: %v", doc["locked"])
	}
}
