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
	if got["read_only"] != true {
		t.Errorf("preview read_only=%v, want true", got["read_only"])
	}
	if hits != 0 {
		t.Errorf("preview made %d requests, want 0", hits)
	}
}

func TestRenderDryRun_ReadOnly_JSONHasFlag(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "json", readOnly: true}
	body := map[string]any{"issue": map[string]any{"notes": "x"}}
	if err := renderDryRun(rc, "PUT", "/issues/7.json", body, nil); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["read_only"] != true {
		t.Errorf("read_only=%v, want true", got["read_only"])
	}
	if got["dry_run"] != true {
		t.Errorf("dry_run=%v, want true (preview still shown)", got["dry_run"])
	}
}

func TestRenderDryRun_NotReadOnly_JSONOmitsFlag(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "json", readOnly: false}
	if err := renderDryRun(rc, "PUT", "/x", map[string]any{"issue": map[string]any{}}, nil); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if _, ok := got["read_only"]; ok {
		t.Errorf("read_only should be omitted when not read-only, got %v", got["read_only"])
	}
}

func TestRenderDryRun_ReadOnly_MarkdownFooter(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "markdown", readOnly: true}
	if err := renderDryRun(rc, "PUT", "/x", map[string]any{"issue": map[string]any{}}, nil); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "re-run with --confirm to send") {
		t.Errorf("read-only footer must not advise re-running with --confirm:\n%s", s)
	}
	if !strings.Contains(s, "read-only mode is active") {
		t.Errorf("footer should mention read-only mode:\n%s", s)
	}
}

func TestReadOnly_AgentHelpMarksWriteSubcommands(t *testing.T) {
	p := writeReadOnlyConfig(t, "https://x")

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "issues", "--agent", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["read_only"] != true {
		t.Errorf("read_only=%v, want true", got["read_only"])
	}
	subs, _ := got["subcommands"].([]any)
	blocked := map[string]bool{}
	for _, s := range subs {
		m := s.(map[string]any)
		blocked[m["name"].(string)] = m["blocked"] == true
	}
	for _, name := range []string{"create", "update"} {
		if !blocked[name] {
			t.Errorf("write subcommand %q not marked blocked; subs=%v", name, subs)
		}
	}
	for _, name := range []string{"list", "get"} {
		if blocked[name] {
			t.Errorf("read subcommand %q must not be blocked; subs=%v", name, subs)
		}
	}
}

func TestReadOnly_AgentHelpMarksWriteLeaf(t *testing.T) {
	p := writeReadOnlyConfig(t, "https://x")

	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--config", p, "issues", "create", "--agent", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["blocked"] != true {
		t.Errorf("create leaf blocked=%v, want true", got["blocked"])
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
