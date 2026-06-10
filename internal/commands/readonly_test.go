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
	"testing"
)

// writeReadOnlyConfig writes a config.toml with read_only = true pointing at
// srvURL and returns its path. It also clears the relevant env vars so the
// file is authoritative.
func writeReadOnlyConfig(t *testing.T, srvURL string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")
	contents := fmt.Sprintf("url = %q\napi_key = \"k\"\nread_only = true\n", srvURL)
	if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_READ_ONLY", "")
	return p
}

func TestReadOnly_BlocksConfirmedWrite(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(204)
	}))
	defer srv.Close()
	p := writeReadOnlyConfig(t, srv.URL)

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "issues", "update", "7", "--notes", "x", "--confirm"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrReadOnly) {
		t.Errorf("err=%v, want wraps ErrReadOnly", err)
	}
	if code := exitCodeFor(err); code != 8 {
		t.Errorf("exit code=%d, want 8", code)
	}
	if hits != 0 {
		t.Errorf("server received %d requests, want 0 (no write must reach the server)", hits)
	}
}

func TestReadOnly_AllowsPreview(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		hits++
	}))
	defer srv.Close()
	p := writeReadOnlyConfig(t, srv.URL)

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "issues", "update", "7", "--notes", "x"})
	if err := root.Execute(); err != nil {
		t.Fatalf("preview should succeed in read-only mode: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["method"] != "PUT" {
		t.Errorf("preview method=%v, want PUT", got["method"])
	}
	if hits != 0 {
		t.Errorf("preview made %d requests, want 0", hits)
	}
}

func TestReadOnly_AllowsReads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()
	p := writeReadOnlyConfig(t, srv.URL)

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "issues", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("read command must work in read-only mode: %v", err)
	}
}
