package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
)

// runWithConfig runs the CLI with --config p and the given args, with the
// URL/key env vars cleared so the config file is authoritative.
func runWithConfig(t *testing.T, p string, args ...string) error {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_READ_ONLY", "")
	t.Setenv("REDMINE_LOCKED", "")
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs(append([]string{"--config", p}, args...))
	return root.Execute()
}

// writeConfigFile writes contents to a config.toml in a temp dir and
// returns its path.
func writeConfigFile(t *testing.T, contents string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func assertExitCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if code := exitCodeFor(err); code != want {
		t.Errorf("exit code=%d, want %d (err=%v)", code, want, err)
	}
}

func TestExitCode_MissingConfigFileIsConfigError(t *testing.T) {
	err := runWithConfig(t, filepath.Join(t.TempDir(), "missing.toml"), "users", "me")
	assertExitCode(t, err, 3)
}

func TestExitCode_UnreadableConfigFileIsConfigError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files regardless of mode")
	}
	p := writeConfigFile(t, "url = \"http://127.0.0.1\"\napi_key = \"k\"\n")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	err := runWithConfig(t, p, "users", "me")
	assertExitCode(t, err, 3)
}

func TestExitCode_InvalidTOMLIsConfigError(t *testing.T) {
	p := writeConfigFile(t, "url = \n")
	err := runWithConfig(t, p, "users", "me")
	assertExitCode(t, err, 3)
}

func TestExitCode_LockedInvalidTOMLIsConfigError(t *testing.T) {
	writeLockedConfig(t, "url = \n")
	_, err := runLocked(t, "users", "me")
	assertExitCode(t, err, 3)
}

func TestExitCode_UnreachableServerIsNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	p := writeConfigFile(t, fmt.Sprintf("url = %q\napi_key = \"k\"\n", srv.URL))
	err := runWithConfig(t, p, "users", "me")
	assertExitCode(t, err, 6)
}

func TestExitCodeFor_FileErrorIsNotNetworkError(t *testing.T) {
	_, err := os.Stat(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("setup: want a not-exist error, got %v", err)
	}
	if code := exitCodeFor(err); code != 1 {
		t.Errorf("exit code=%d, want 1", code)
	}
}

// TestExitCode_ConnectionResetMidDownloadIsNetworkError: a transport
// failure while streaming the body surfaces as *net.OpError, not wrapped in
// *url.Error, and must still exit 6.
func TestExitCode_ConnectionResetMidDownloadIsNetworkError(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"attachment":{"id":7,"filename":"f.bin","filesize":1000,"content_url":"` + srv.URL + `/f"}}`))
			return
		}
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		// Linger 0 makes Close send a RST instead of a FIN.
		_ = conn.(*net.TCPConn).SetLinger(0)
		_ = conn.Close()
	}))
	defer srv.Close()

	c := api.New(srv.URL, "k", srv.Client())
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"attachments", "download", "42", "-o", "-"})
	assertExitCode(t, root.Execute(), 6)
}
