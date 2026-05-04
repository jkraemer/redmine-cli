# Redmine CLI Phase 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship a working `redmine-cli` Go binary with read-only commands (`issues list`, `issues get`, `attachments download`, `projects list`), agent-friendly `--agent --help`, and JSON/Markdown output.

**Architecture:** Cobra CLI on top of an oapi-codegen-generated Redmine API client. Vendored OpenAPI spec from `d-yoshi/redmine-openapi` at a pinned commit. Auth via `X-Redmine-API-Key` header from env or TOML config. Mock-only tests with `httptest`.

**Tech Stack:** Go 1.22+, Cobra (`github.com/spf13/cobra`), oapi-codegen (`github.com/oapi-codegen/oapi-codegen/v2`), `github.com/BurntSushi/toml`. No other heavy deps.

**Reference:** `/tmp/basecamp-cli/` (cloned for pattern reference only — do not copy code). Design doc: `docs/plans/2026-05-04-redmine-cli-design.md`.

**Test endpoint:** `https://rm.jkraemer.net` with API key from existing memory (`bf7a275517bf0f6d14255f1570a7daee69fc1b50`). DO NOT use it in committed tests — mocks only.

---

## Task 0: Toolchain Setup

**Files:** none (environment only)

**Step 1: Install Go via asdf**

```bash
asdf plugin add golang
asdf install golang 1.22.5
asdf global golang 1.22.5
go version
```

Expected: `go version go1.22.5 linux/amd64`. If `asdf plugin add` fails with network issues, fall back to direct install of `golang-go` deb package or download tarball into `~/.local/go`.

**Step 2: Install oapi-codegen**

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
ls ~/go/bin/oapi-codegen
```

Expected: file exists.

**Step 3: Verify**

```bash
~/go/bin/oapi-codegen --version
```

Expected: prints version. Note: GOPATH/bin may need to be on PATH or referenced absolutely.

---

## Task 1: Repo Skeleton + go.mod

**Files:**
- Create: `/home/fera/agents/coding/projects/redmine-cli/go.mod`
- Create: `/home/fera/agents/coding/projects/redmine-cli/.gitignore`
- Create: `/home/fera/agents/coding/projects/redmine-cli/README.md`
- Create: `/home/fera/agents/coding/projects/redmine-cli/Makefile`

**Step 1: Initialize module**

```bash
cd /home/fera/agents/coding/projects/redmine-cli
go mod init github.com/jkraemer/redmine-cli
```

Module path: `github.com/jkraemer/redmine-cli` (matches Jens' GitHub identity).

**Step 2: Create .gitignore**

```
/redmine-cli
/dist/
.env
*.test
*.out
.DS_Store
```

**Step 3: Create Makefile**

```makefile
.PHONY: build test generate clean fmt vet

BIN := redmine-cli
PKG := ./cmd/redmine-cli

build:
	go build -o $(BIN) $(PKG)

test:
	go test ./...

generate:
	oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
```

**Step 4: Create README.md** (minimal, expand later)

```markdown
# redmine-cli

Agent-friendly CLI for the Redmine/Planio API.

## Build

    make build

## Configure

Set env vars:

    export REDMINE_URL=https://your.redmine.example
    export REDMINE_API_KEY=...

Or create `~/.config/redmine-cli/config.toml`:

    url = "https://your.redmine.example"
    api_key = "..."

## Use

    ./redmine-cli projects list
    ./redmine-cli issues list --project myproj --status open --limit 10
    ./redmine-cli issues get 1459
    ./redmine-cli attachments download 42

See `./redmine-cli --agent --help` for machine-readable help.
```

**Step 5: Commit**

```bash
cd /home/fera/agents/coding/projects/redmine-cli
git init
git add .
git commit -m "chore: initialize repo skeleton"
```

---

## Task 2: Vendor OpenAPI spec + oapi-codegen config

**Files:**
- Create: `api/openapi.yaml` (downloaded)
- Create: `api/oapi-codegen.yaml`
- Create: `api/SOURCE.md` (provenance)

**Step 1: Download pinned spec**

```bash
cd /home/fera/agents/coding/projects/redmine-cli
mkdir -p api internal/client
COMMIT=b38f1e1b97d231106c055f77eede85a4b636b2ad
curl -fsSL "https://raw.githubusercontent.com/d-yoshi/redmine-openapi/${COMMIT}/openapi.yaml" -o api/openapi.yaml
wc -l api/openapi.yaml
```

Expected: file with several thousand lines.

**Step 2: Write SOURCE.md**

```markdown
# OpenAPI Spec Source

Vendored from https://github.com/d-yoshi/redmine-openapi
Pinned commit: b38f1e1b97d231106c055f77eede85a4b636b2ad
License: MIT (per upstream)

To update:

    COMMIT=<new-sha>
    curl -fsSL "https://raw.githubusercontent.com/d-yoshi/redmine-openapi/${COMMIT}/openapi.yaml" -o api/openapi.yaml
    make generate
    go test ./...
```

**Step 3: Create oapi-codegen config**

```yaml
package: client
output: internal/client/client.gen.go
generate:
  models: true
  client: true
output-options:
  skip-prune: true
```

**Step 4: Generate client**

```bash
~/go/bin/oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
ls -la internal/client/
```

Expected: `client.gen.go` exists, several hundred KB.

**Step 5: Verify it builds**

```bash
go build ./internal/client/
```

If generation fails or build fails, examine the spec for unsupported features and add `output-options: { skip-fmt: false }` or use `--initialism-overrides` flags. Last resort: manually trim the spec to the four endpoints we use.

**Step 6: Commit**

```bash
git add api/ internal/client/
git commit -m "feat: vendor d-yoshi OpenAPI spec and generate client"
```

---

## Task 3: Config Loader (env + TOML)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_EnvOnly(t *testing.T) {
	t.Setenv("REDMINE_URL", "https://example.com")
	t.Setenv("REDMINE_API_KEY", "key123")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "https://example.com" {
		t.Errorf("URL=%q", cfg.URL)
	}
	if cfg.APIKey != "key123" {
		t.Errorf("APIKey=%q", cfg.APIKey)
	}
	if cfg.DefaultFormat != "json" {
		t.Errorf("DefaultFormat=%q want json", cfg.DefaultFormat)
	}
}

func TestLoad_TOMLFallback(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `url = "https://from-toml.example"
api_key = "toml-key"
default_format = "markdown"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "https://from-toml.example" {
		t.Errorf("URL=%q", cfg.URL)
	}
	if cfg.APIKey != "toml-key" {
		t.Errorf("APIKey=%q", cfg.APIKey)
	}
	if cfg.DefaultFormat != "markdown" {
		t.Errorf("DefaultFormat=%q", cfg.DefaultFormat)
	}
}

func TestLoad_EnvOverridesTOML(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "redmine-cli")
	_ = os.MkdirAll(cfgDir, 0o755)
	_ = os.WriteFile(filepath.Join(cfgDir, "config.toml"),
		[]byte(`url="https://toml"\napi_key="toml"`), 0o600)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("REDMINE_URL", "https://env.example")
	t.Setenv("REDMINE_API_KEY", "env-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.URL != "https://env.example" {
		t.Errorf("URL=%q", cfg.URL)
	}
}

func TestLoad_MissingURL(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "k")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

**Step 2: Run test (expect fail)**

```bash
go test ./internal/config/
```

Expected: build error, package undefined.

**Step 3: Add toml dep**

```bash
go get github.com/BurntSushi/toml@v1.4.0
```

**Step 4: Implement**

```go
// Package config loads CLI configuration from environment variables and
// an optional TOML config file at $XDG_CONFIG_HOME/redmine-cli/config.toml
// (defaulting to ~/.config/redmine-cli/config.toml).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds resolved CLI configuration.
type Config struct {
	URL           string
	APIKey        string
	DefaultFormat string
}

type fileConfig struct {
	URL           string `toml:"url"`
	APIKey        string `toml:"api_key"`
	DefaultFormat string `toml:"default_format"`
}

// ErrMissingURL is returned when no URL can be resolved.
var ErrMissingURL = errors.New("redmine URL not configured (set REDMINE_URL or url in config.toml)")

// ErrMissingAPIKey is returned when no API key can be resolved.
var ErrMissingAPIKey = errors.New("redmine API key not configured (set REDMINE_API_KEY or api_key in config.toml)")

// Load resolves configuration from env vars (highest priority) then a TOML
// file at $XDG_CONFIG_HOME/redmine-cli/config.toml.
func Load() (*Config, error) {
	var fc fileConfig
	if path := configPath(); path != "" {
		if _, err := os.Stat(path); err == nil {
			if _, err := toml.DecodeFile(path, &fc); err != nil {
				return nil, fmt.Errorf("parse %s: %w", path, err)
			}
		}
	}

	cfg := &Config{
		URL:           firstNonEmpty(os.Getenv("REDMINE_URL"), fc.URL),
		APIKey:        firstNonEmpty(os.Getenv("REDMINE_API_KEY"), fc.APIKey),
		DefaultFormat: firstNonEmpty(os.Getenv("REDMINE_FORMAT"), fc.DefaultFormat, "json"),
	}

	if cfg.URL == "" {
		return nil, ErrMissingURL
	}
	if cfg.APIKey == "" {
		return nil, ErrMissingAPIKey
	}
	return cfg, nil
}

func configPath() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "redmine-cli", "config.toml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "redmine-cli", "config.toml")
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
```

**Step 5: Run tests**

```bash
go test ./internal/config/ -v
```

Expected: all pass.

**Step 6: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "feat: add config loader (env + TOML)"
```

---

## Task 4: Output Formatters (JSON + Markdown)

**Files:**
- Create: `internal/output/output.go`
- Create: `internal/output/output_test.go`

**Step 1: Write tests**

```go
package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestJSON_Object(t *testing.T) {
	var buf bytes.Buffer
	err := JSON(&buf, map[string]any{"a": 1, "b": "x"})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"a": 1`) || !strings.Contains(out, `"b": "x"`) {
		t.Errorf("unexpected output: %s", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("expected trailing newline")
	}
}

func TestMarkdownTable_Basic(t *testing.T) {
	var buf bytes.Buffer
	headers := []string{"ID", "Name"}
	rows := [][]string{{"1", "alpha"}, {"22", "beta"}}
	if err := MarkdownTable(&buf, headers, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	want := "| ID | Name  |\n| -- | ----- |\n| 1  | alpha |\n| 22 | beta  |\n"
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestMarkdownTable_EmptyRows(t *testing.T) {
	var buf bytes.Buffer
	if err := MarkdownTable(&buf, []string{"A"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(no results)") {
		t.Errorf("expected empty marker: %s", buf.String())
	}
}
```

**Step 2: Run, expect fail**

```bash
go test ./internal/output/
```

**Step 3: Implement**

```go
// Package output formats data for stdout: pretty JSON or Markdown tables.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// JSON writes a 2-space-indented JSON document to w with a trailing newline.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// MarkdownTable writes a GitHub-flavored Markdown table to w. Column widths
// are sized to the widest cell. If rows is empty, writes "(no results)".
func MarkdownTable(w io.Writer, headers []string, rows [][]string) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(w, "(no results)")
		return err
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}

	writeRow := func(cells []string) error {
		parts := make([]string, len(cells))
		for i, c := range cells {
			parts[i] = padRight(c, widths[i])
		}
		_, err := fmt.Fprintf(w, "| %s |\n", strings.Join(parts, " | "))
		return err
	}

	if err := writeRow(headers); err != nil {
		return err
	}
	sep := make([]string, len(headers))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	if err := writeRow(sep); err != nil {
		return err
	}
	for _, r := range rows {
		if err := writeRow(r); err != nil {
			return err
		}
	}
	return nil
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
```

**Step 4: Run tests**

```bash
go test ./internal/output/ -v
```

Expected: pass.

**Step 5: Commit**

```bash
git add internal/output/
git commit -m "feat: add JSON and Markdown table output formatters"
```

---

## Task 5: Thin HTTP Client Wrapper

**Note:** Generated `oapi-codegen` client is large and may not match Redmine
exactly. We wrap it (or replace it for the four endpoints we need) with a
small typed client that:

- adds the `X-Redmine-API-Key` header
- handles JSON encoding/decoding
- maps HTTP status to typed errors

**Files:**
- Create: `internal/api/client.go`
- Create: `internal/api/client_test.go`
- Create: `internal/api/types.go`
- Create: `internal/api/errors.go`

(We use `internal/api/` for our typed wrapper; `internal/client/` holds the raw generated code as a reference / for future write ops.)

**Step 1: Write tests**

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Redmine-API-Key"); got != "test-key" {
			t.Errorf("missing/wrong api key header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestListProjects_OK(t *testing.T) {
	body := `{"projects":[{"id":1,"identifier":"foo","name":"Foo"}],"total_count":1,"offset":0,"limit":25}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.ListProjects(context.Background(), ListProjectsParams{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Projects) != 1 || res.Projects[0].Name != "Foo" {
		t.Errorf("unexpected: %+v", res)
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	srv := newTestServer(t, 404, `{}`)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	_, err := c.GetIssue(context.Background(), 9999, nil)
	var apiErr *Error
	if err == nil || !asAPIError(err, &apiErr) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if apiErr.Status != 404 {
		t.Errorf("status=%d", apiErr.Status)
	}
}

func TestListIssues_QueryParams(t *testing.T) {
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	_, err := c.ListIssues(context.Background(), ListIssuesParams{
		ProjectID: "myproj",
		StatusID:  "open",
		Limit:     50,
		Offset:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"project_id=myproj", "status_id=open", "limit=50", "offset=10"} {
		if !strings.Contains(seenQuery, want) {
			t.Errorf("query missing %q: %s", want, seenQuery)
		}
	}
}

func TestDownloadAttachment_FollowsContentURL(t *testing.T) {
	// First request: attachment metadata
	// Second request: file content (different host/path)
	var fileSrv *httptest.Server
	fileSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("file-bytes"))
	}))
	defer fileSrv.Close()

	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"attachment":{"id":42,"filename":"hi.txt","content_url":"` + fileSrv.URL + `/file"}}`
		_, _ = w.Write([]byte(body))
	}))
	defer metaSrv.Close()

	c := New(metaSrv.URL, "test-key", metaSrv.Client())
	att, body, err := c.GetAttachment(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if att.Filename != "hi.txt" {
		t.Errorf("filename=%q", att.Filename)
	}
	defer body.Close()
	got := readAllString(t, body)
	if got != "file-bytes" {
		t.Errorf("body=%q", got)
	}
}

func readAllString(t *testing.T, r interface{ Read([]byte) (int, error) }) string {
	t.Helper()
	buf := make([]byte, 0, 64)
	tmp := make([]byte, 32)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf)
}

func asAPIError(err error, target **Error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*Error); ok {
		*target = e
		return true
	}
	return false
}
```

**Step 2: Implement types**

```go
// File: internal/api/types.go
package api

// Project is a Redmine project (subset used by the CLI).
type Project struct {
	ID         int    `json:"id"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Status     int    `json:"status,omitempty"`
}

type listProjectsResponse struct {
	Projects   []Project `json:"projects"`
	TotalCount int       `json:"total_count"`
	Offset     int       `json:"offset"`
	Limit      int       `json:"limit"`
}

// ListProjectsResult is what we return to callers.
type ListProjectsResult struct {
	Projects   []Project `json:"projects"`
	TotalCount int       `json:"total_count"`
	Offset     int       `json:"offset"`
	Limit      int       `json:"limit"`
}

// IDName is a common shape for nested {id,name} fields in Redmine.
type IDName struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Issue is a Redmine issue (subset used by the CLI).
type Issue struct {
	ID          int     `json:"id"`
	Subject     string  `json:"subject"`
	Description string  `json:"description,omitempty"`
	Project     IDName  `json:"project"`
	Tracker     IDName  `json:"tracker"`
	Status      IDName  `json:"status"`
	Priority    IDName  `json:"priority"`
	Author      IDName  `json:"author"`
	AssignedTo  *IDName `json:"assigned_to,omitempty"`
	StartDate   string  `json:"start_date,omitempty"`
	DueDate     string  `json:"due_date,omitempty"`
	DoneRatio   int     `json:"done_ratio,omitempty"`
	CreatedOn   string  `json:"created_on,omitempty"`
	UpdatedOn   string  `json:"updated_on,omitempty"`

	Journals    []Journal    `json:"journals,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Journal is one entry in an issue's history.
type Journal struct {
	ID        int     `json:"id"`
	User      IDName  `json:"user"`
	Notes     string  `json:"notes,omitempty"`
	CreatedOn string  `json:"created_on,omitempty"`
	Details   []any   `json:"details,omitempty"`
}

// Attachment metadata.
type Attachment struct {
	ID          int    `json:"id"`
	Filename    string `json:"filename"`
	Filesize    int64  `json:"filesize"`
	ContentType string `json:"content_type,omitempty"`
	ContentURL  string `json:"content_url"`
	Description string `json:"description,omitempty"`
	Author      IDName `json:"author"`
	CreatedOn   string `json:"created_on"`
}

// ListIssuesParams holds query params for /issues.json.
type ListIssuesParams struct {
	ProjectID   string
	StatusID    string // "open", "closed", "*", or numeric
	AssignedTo  string // user id or "me"
	UpdatedOn   string // operator-style filter, e.g. ">=2026-01-01"
	Limit       int    // default 25, max 100
	Offset      int
	Sort        string
	Include     []string // attachments, relations
}

// ListIssuesResult holds the listing payload.
type ListIssuesResult struct {
	Issues     []Issue `json:"issues"`
	TotalCount int     `json:"total_count"`
	Offset     int     `json:"offset"`
	Limit      int     `json:"limit"`
}

// ListProjectsParams holds query params for /projects.json.
type ListProjectsParams struct {
	Limit  int
	Offset int
}

// GetIssueParams covers the include flags accepted by /issues/{id}.json.
type GetIssueParams struct {
	Include []string // journals, attachments, relations, children
}
```

**Step 3: Implement errors**

```go
// File: internal/api/errors.go
package api

import "fmt"

// Error is returned for non-2xx responses.
type Error struct {
	Status int
	Body   string // first ~512 bytes of response body
	URL    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("redmine API %d at %s: %s", e.Status, e.URL, e.Body)
}
```

**Step 4: Implement client**

```go
// File: internal/api/client.go
//
// Client is a thin typed wrapper around the Redmine REST API. We do not use
// the oapi-codegen-generated client directly because (a) the spec covers
// many endpoints we do not need, (b) hand-rolling four endpoints is
// straightforward and avoids generated-code awkwardness for mixed-type
// fields. The generated code lives in internal/client/ for reference and
// future expansion.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client is the Redmine HTTP client.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New creates a Client. If httpClient is nil, http.DefaultClient is used.
func New(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    httpClient,
	}
}

const maxBodyExcerpt = 512

func (c *Client) do(ctx context.Context, method, path string, q url.Values) (*http.Response, error) {
	full := c.baseURL + path
	if q != nil && len(q) > 0 {
		full += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Redmine-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyExcerpt))
		_ = resp.Body.Close()
		return nil, &Error{Status: resp.StatusCode, Body: string(body), URL: full}
	}
	return resp, nil
}

func (c *Client) doJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.do(ctx, "GET", path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ListProjects lists projects.
func (c *Client) ListProjects(ctx context.Context, p ListProjectsParams) (*ListProjectsResult, error) {
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	var raw listProjectsResponse
	if err := c.doJSON(ctx, "/projects.json", q, &raw); err != nil {
		return nil, err
	}
	return &ListProjectsResult{
		Projects:   raw.Projects,
		TotalCount: raw.TotalCount,
		Offset:     raw.Offset,
		Limit:      raw.Limit,
	}, nil
}

// ListIssues lists issues with the given filter params.
func (c *Client) ListIssues(ctx context.Context, p ListIssuesParams) (*ListIssuesResult, error) {
	q := url.Values{}
	if p.ProjectID != "" {
		q.Set("project_id", p.ProjectID)
	}
	if p.StatusID != "" {
		q.Set("status_id", p.StatusID)
	}
	if p.AssignedTo != "" {
		q.Set("assigned_to_id", p.AssignedTo)
	}
	if p.UpdatedOn != "" {
		q.Set("updated_on", p.UpdatedOn)
	}
	if p.Sort != "" {
		q.Set("sort", p.Sort)
	}
	if len(p.Include) > 0 {
		q.Set("include", strings.Join(p.Include, ","))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	var res ListIssuesResult
	if err := c.doJSON(ctx, "/issues.json", q, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetIssue fetches a single issue with optional includes.
func (c *Client) GetIssue(ctx context.Context, id int, p *GetIssueParams) (*Issue, error) {
	q := url.Values{}
	if p != nil && len(p.Include) > 0 {
		q.Set("include", strings.Join(p.Include, ","))
	}
	var wrapper struct {
		Issue Issue `json:"issue"`
	}
	if err := c.doJSON(ctx, fmt.Sprintf("/issues/%d.json", id), q, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Issue, nil
}

// GetAttachment fetches metadata for an attachment and returns an open
// reader for the file content. The caller must close the body.
func (c *Client) GetAttachment(ctx context.Context, id int) (*Attachment, io.ReadCloser, error) {
	var wrapper struct {
		Attachment Attachment `json:"attachment"`
	}
	if err := c.doJSON(ctx, fmt.Sprintf("/attachments/%d.json", id), nil, &wrapper); err != nil {
		return nil, nil, err
	}
	if wrapper.Attachment.ContentURL == "" {
		return nil, nil, fmt.Errorf("attachment %d: no content_url returned", id)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", wrapper.Attachment.ContentURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-Redmine-API-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyExcerpt))
		_ = resp.Body.Close()
		return nil, nil, &Error{Status: resp.StatusCode, Body: string(body), URL: wrapper.Attachment.ContentURL}
	}
	return &wrapper.Attachment, resp.Body, nil
}
```

**Step 5: Run tests**

```bash
go test ./internal/api/ -v
```

Expected: all pass.

**Step 6: Commit**

```bash
git add internal/api/
git commit -m "feat: add typed Redmine API client (read ops)"
```

---

## Task 6: Agent-Help Walker

**Files:**
- Create: `internal/agenthelp/agenthelp.go`
- Create: `internal/agenthelp/agenthelp_test.go`

**Step 1: Test**

```go
package agenthelp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRender_RootCommand(t *testing.T) {
	root := &cobra.Command{
		Use:   "redmine-cli",
		Short: "CLI for Redmine",
	}
	root.PersistentFlags().String("format", "json", "Output format")
	sub := &cobra.Command{
		Use:   "issues",
		Short: "Issue operations",
	}
	sub.AddCommand(&cobra.Command{Use: "list", Short: "List issues"})
	root.AddCommand(sub)

	var buf bytes.Buffer
	if err := Render(&buf, root); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got["command"] != "redmine-cli" {
		t.Errorf("command=%v", got["command"])
	}
	subs, _ := got["subcommands"].([]any)
	if len(subs) != 1 {
		t.Fatalf("subcommands=%d", len(subs))
	}
	if !strings.Contains(buf.String(), "issues") {
		t.Error("missing issues sub")
	}
}

func TestRender_Subcommand(t *testing.T) {
	root := &cobra.Command{Use: "redmine-cli"}
	issues := &cobra.Command{Use: "issues", Short: "Issue ops"}
	list := &cobra.Command{Use: "list", Short: "List issues"}
	list.Flags().Int("limit", 25, "max results")
	issues.AddCommand(list)
	root.AddCommand(issues)

	var buf bytes.Buffer
	if err := Render(&buf, list); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(buf.Bytes(), &got)
	if got["path"] != "redmine-cli issues list" {
		t.Errorf("path=%v", got["path"])
	}
	flags, _ := got["flags"].([]any)
	if len(flags) == 0 {
		t.Error("expected flags")
	}
}
```

**Step 2: Implement**

```go
// Package agenthelp emits a structured JSON description of any cobra command.
package agenthelp

import (
	"encoding/json"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Flag describes a single cobra flag in JSON-friendly form.
type Flag struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
}

// Subcommand is a name + short description.
type Subcommand struct {
	Name  string `json:"name"`
	Short string `json:"short"`
	Path  string `json:"path"`
}

// Help is the rendered output structure.
type Help struct {
	Command        string       `json:"command"`
	Path           string       `json:"path"`
	Short          string       `json:"short"`
	Long           string       `json:"long,omitempty"`
	Usage          string       `json:"usage"`
	Flags          []Flag       `json:"flags"`
	InheritedFlags []Flag       `json:"inherited_flags,omitempty"`
	Subcommands    []Subcommand `json:"subcommands,omitempty"`
	Notes          []string     `json:"notes,omitempty"`
}

// Render writes a JSON Help document for cmd to w.
func Render(w io.Writer, cmd *cobra.Command) error {
	h := Help{
		Command: cmd.Name(),
		Path:    cmd.CommandPath(),
		Short:   cmd.Short,
		Long:    cmd.Long,
		Usage:   cmd.UseLine(),
	}
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		h.Flags = append(h.Flags, flagToJSON(f))
	})
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		h.InheritedFlags = append(h.InheritedFlags, flagToJSON(f))
	})
	for _, c := range cmd.Commands() {
		if c.Hidden {
			continue
		}
		h.Subcommands = append(h.Subcommands, Subcommand{
			Name:  c.Name(),
			Short: c.Short,
			Path:  c.CommandPath(),
		})
	}
	if notes, ok := cmd.Annotations["agent-notes"]; ok && notes != "" {
		h.Notes = []string{notes}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(h)
}

func flagToJSON(f *pflag.Flag) Flag {
	return Flag{
		Name:      f.Name,
		Shorthand: f.Shorthand,
		Type:      f.Value.Type(),
		Default:   f.DefValue,
		Usage:     f.Usage,
	}
}
```

**Step 3: Run tests**

```bash
go get github.com/spf13/cobra@v1.8.1
go test ./internal/agenthelp/ -v
```

Expected: pass.

**Step 4: Commit**

```bash
git add internal/agenthelp/ go.mod go.sum
git commit -m "feat: add agenthelp JSON renderer for cobra commands"
```

---

## Task 7: Cobra Root Command + Wiring

**Files:**
- Create: `internal/commands/root.go`
- Create: `internal/commands/root_test.go`
- Create: `cmd/redmine-cli/main.go`

**Step 1: Implement root**

```go
// File: internal/commands/root.go
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/agenthelp"
	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/config"
)

// runCtx holds context shared by all commands.
type runCtx struct {
	out, errOut io.Writer
	client      *api.Client
	format      string // "json" or "markdown"
	agentHelp   bool
}

// Build constructs the root command with all subcommands wired in.
// out and errOut are used in place of os.Stdout/os.Stderr (testable).
func Build(out, errOut io.Writer) *cobra.Command {
	rc := &runCtx{out: out, errOut: errOut}

	root := &cobra.Command{
		Use:           "redmine-cli",
		Short:         "Agent-friendly CLI for the Redmine/Planio API",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVarP(&rc.format, "format", "f", "", "Output format: json or markdown (default from config)")
	root.PersistentFlags().BoolVar(&rc.agentHelp, "agent", false, "When combined with --help, emit machine-readable JSON")

	// Intercept --agent --help at any level by overriding HelpFunc.
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if rc.agentHelp {
			_ = agenthelp.Render(rc.out, cmd)
			return
		}
		// fall back to default help on stderr
		_ = cmd.Usage()
	})

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		// Skip client init for help-only invocations.
		if cmd.Name() == "help" || cmd.Name() == "redmine-cli" {
			return nil
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if rc.format == "" {
			rc.format = cfg.DefaultFormat
		}
		rc.client = api.New(cfg.URL, cfg.APIKey, http.DefaultClient)
		return nil
	}

	root.AddCommand(newProjectsCmd(rc))
	root.AddCommand(newIssuesCmd(rc))
	root.AddCommand(newAttachmentsCmd(rc))

	return root
}

// Execute runs the root with os.Args[1:] and exits with the right code.
func Execute() {
	root := Build(os.Stdout, os.Stderr)
	err := root.Execute()
	if err == nil {
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(exitCodeFor(err))
}

func exitCodeFor(err error) int {
	if errors.Is(err, config.ErrMissingURL) || errors.Is(err, config.ErrMissingAPIKey) {
		return 3
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 401:
			return 3
		case 403:
			return 4
		case 404:
			return 2
		case 429:
			return 5
		}
		if apiErr.Status >= 500 {
			return 7
		}
	}
	// network-style errors: detect by url.Error type
	type netErr interface{ Timeout() bool }
	var ne netErr
	if errors.As(err, &ne) {
		return 6
	}
	return 1
}

// applyContext returns a context for subcommands.
func (rc *runCtx) ctx() context.Context {
	return context.Background()
}
```

**Step 2: Implement main.go**

```go
// File: cmd/redmine-cli/main.go
package main

import "github.com/jkraemer/redmine-cli/internal/commands"

func main() {
	commands.Execute()
}
```

**Step 3: Add stub subcommand files** (so root compiles before next tasks)

```go
// File: internal/commands/projects.go
package commands

import "github.com/spf13/cobra"

func newProjectsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "projects", Short: "Project operations"}
	return c
}
```

```go
// File: internal/commands/issues.go
package commands

import "github.com/spf13/cobra"

func newIssuesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "issues", Short: "Issue operations"}
	return c
}
```

```go
// File: internal/commands/attachments.go
package commands

import "github.com/spf13/cobra"

func newAttachmentsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Attachment operations"}
	return c
}
```

**Step 4: Build**

```bash
go build ./cmd/redmine-cli/
./redmine-cli --help
./redmine-cli --agent --help
```

Expected: --help prints usage; --agent --help prints JSON.

**Step 5: Test root**

```go
// File: internal/commands/root_test.go
package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestRoot_AgentHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(&out, &errOut)
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

func TestRoot_Help_ListsSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(&out, &errOut)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	combined := out.String() + errOut.String()
	for _, want := range []string{"projects", "issues", "attachments"} {
		if !strings.Contains(combined, want) {
			t.Errorf("help missing %q\n%s", want, combined)
		}
	}
}
```

```bash
go test ./internal/commands/ -v
```

Expected: pass.

**Step 6: Commit**

```bash
git add cmd/ internal/commands/
git commit -m "feat: add cobra root and agent-help wiring"
```

---

## Task 8: `projects list` Command

**Files:**
- Modify: `internal/commands/projects.go`
- Create: `internal/commands/projects_test.go`

**Step 1: Implement**

```go
package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newProjectsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "projects", Short: "Project operations"}
	c.AddCommand(newProjectsListCmd(rc))
	return c
}

func newProjectsListCmd(rc *runCtx) *cobra.Command {
	var limit, offset int
	var all bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all && offset > 0 {
				return fmt.Errorf("--all and --offset are mutually exclusive")
			}
			if limit < 0 || limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}
			ctx := rc.ctx()

			var collected []api.Project
			pageLimit := limit
			if pageLimit == 0 {
				pageLimit = 25
			}
			pageOffset := offset
			for {
				res, err := rc.client.ListProjects(ctx, api.ListProjectsParams{
					Limit:  pageLimit,
					Offset: pageOffset,
				})
				if err != nil {
					return err
				}
				collected = append(collected, res.Projects...)
				if !all || len(collected) >= res.TotalCount || len(res.Projects) == 0 {
					break
				}
				pageOffset += len(res.Projects)
			}

			return renderProjects(rc, collected)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages")
	return cmd
}

func renderProjects(rc *runCtx, projects []api.Project) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(projects))
		for i, p := range projects {
			rows[i] = []string{fmt.Sprintf("%d", p.ID), p.Identifier, p.Name}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Identifier", "Name"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"projects": projects})
}
```

**Step 2: Test**

```go
package commands

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
)

func newClientForTest(t *testing.T, handler http.HandlerFunc) (*api.Client, func()) {
	t.Helper()
	srv := httptest.NewServer(handler)
	c := api.New(srv.URL, "k", srv.Client())
	return c, srv.Close
}

func TestProjectsList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[{"id":1,"identifier":"foo","name":"Foo"}],"total_count":1,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"projects", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"name": "Foo"`) {
		t.Errorf("missing project name: %s", out.String())
	}
}

func TestProjectsList_Markdown(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":1,"identifier":"foo","name":"Foo"}],"total_count":1,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"projects", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "| ID | Identifier | Name |") {
		t.Errorf("not a markdown table:\n%s", out.String())
	}
}
```

**Step 3: Add buildRootForTest helper** (in `root_test.go`)

```go
// Add to root_test.go
import "github.com/spf13/cobra"

func buildRootForTest(rc *runCtx) *cobra.Command {
	root := &cobra.Command{Use: "redmine-cli", SilenceUsage: true, SilenceErrors: true}
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error { return nil }
	root.AddCommand(newProjectsCmd(rc))
	root.AddCommand(newIssuesCmd(rc))
	root.AddCommand(newAttachmentsCmd(rc))
	return root
}
```

**Step 4: Run tests**

```bash
go test ./internal/commands/ -v
```

**Step 5: Commit**

```bash
git add internal/commands/projects.go internal/commands/projects_test.go internal/commands/root_test.go
git commit -m "feat: implement projects list command"
```

---

## Task 9: `issues list` and `issues get`

**Files:**
- Modify: `internal/commands/issues.go`
- Create: `internal/commands/issues_test.go`

**Step 1: Implement**

```go
package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newIssuesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "issues", Short: "Issue operations"}
	c.AddCommand(newIssuesListCmd(rc))
	c.AddCommand(newIssuesGetCmd(rc))
	return c
}

func newIssuesListCmd(rc *runCtx) *cobra.Command {
	var (
		project, status, assignee, updatedSince, sort string
		limit, offset                                 int
		all                                           bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all && offset > 0 {
				return fmt.Errorf("--all and --offset are mutually exclusive")
			}
			if limit < 0 || limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}

			p := api.ListIssuesParams{
				ProjectID:  project,
				StatusID:   status,
				AssignedTo: assignee,
				Sort:       sort,
				Limit:      limit,
				Offset:     offset,
			}
			if updatedSince != "" {
				p.UpdatedOn = ">=" + updatedSince
			}
			if p.Limit == 0 {
				p.Limit = 25
			}

			ctx := rc.ctx()
			var collected []api.Issue
			for {
				res, err := rc.client.ListIssues(ctx, p)
				if err != nil {
					return err
				}
				collected = append(collected, res.Issues...)
				if !all || len(collected) >= res.TotalCount || len(res.Issues) == 0 {
					break
				}
				p.Offset += len(res.Issues)
			}
			return renderIssueList(rc, collected)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Filter by project identifier or ID")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status: open, closed, *, or numeric ID")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Filter by assignee user ID or 'me'")
	cmd.Flags().StringVar(&updatedSince, "updated-since", "", "Filter by updated_on >= YYYY-MM-DD")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort expression, e.g. updated_on:desc")
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages")
	return cmd
}

func newIssuesGetCmd(rc *runCtx) *cobra.Command {
	var includes []string
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single issue by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid issue id %q", args[0])
			}
			issue, err := rc.client.GetIssue(rc.ctx(), id, &api.GetIssueParams{Include: includes})
			if err != nil {
				return err
			}
			return renderIssueDetail(rc, issue)
		},
	}
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Include extras: journals, attachments, relations, children")
	return cmd
}

func renderIssueList(rc *runCtx, issues []api.Issue) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(issues))
		for i, is := range issues {
			rows[i] = []string{
				fmt.Sprintf("#%d", is.ID),
				is.Project.Name,
				is.Tracker.Name,
				is.Status.Name,
				is.Subject,
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Project", "Tracker", "Status", "Subject"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"issues": issues})
}

func renderIssueDetail(rc *runCtx, is *api.Issue) error {
	if rc.format == "markdown" {
		var b strings.Builder
		fmt.Fprintf(&b, "# #%d %s\n\n", is.ID, is.Subject)
		fmt.Fprintf(&b, "- **Project:** %s\n", is.Project.Name)
		fmt.Fprintf(&b, "- **Tracker:** %s\n", is.Tracker.Name)
		fmt.Fprintf(&b, "- **Status:** %s\n", is.Status.Name)
		fmt.Fprintf(&b, "- **Priority:** %s\n", is.Priority.Name)
		fmt.Fprintf(&b, "- **Author:** %s\n", is.Author.Name)
		if is.AssignedTo != nil {
			fmt.Fprintf(&b, "- **Assigned to:** %s\n", is.AssignedTo.Name)
		}
		if is.DueDate != "" {
			fmt.Fprintf(&b, "- **Due:** %s\n", is.DueDate)
		}
		fmt.Fprintf(&b, "\n%s\n", is.Description)
		if len(is.Journals) > 0 {
			fmt.Fprintf(&b, "\n## Journals\n\n")
			for _, j := range is.Journals {
				if j.Notes == "" {
					continue
				}
				fmt.Fprintf(&b, "**%s** (%s):\n%s\n\n", j.User.Name, j.CreatedOn, j.Notes)
			}
		}
		_, err := fmt.Fprint(rc.out, b.String())
		return err
	}
	return output.JSON(rc.out, map[string]any{"issue": is})
}
```

**Step 2: Tests**

```go
package commands

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestIssuesList_FiltersAndJSON(t *testing.T) {
	var seen string
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"issues":[{"id":1,"subject":"hi","project":{"id":1,"name":"P"},"tracker":{"id":1,"name":"Bug"},"status":{"id":1,"name":"New"},"priority":{"id":1,"name":"Normal"},"author":{"id":1,"name":"A"}}],"total_count":1,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "list", "--project", "myproj", "--status", "open", "--limit", "10"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"project_id=myproj", "status_id=open", "limit=10"} {
		if !strings.Contains(seen, want) {
			t.Errorf("query missing %q: %s", want, seen)
		}
	}
	if !strings.Contains(out.String(), `"subject": "hi"`) {
		t.Errorf("output: %s", out.String())
	}
}

func TestIssuesGet_Markdown(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issue":{"id":7,"subject":"Subj","description":"Body","project":{"id":1,"name":"P"},"tracker":{"id":1,"name":"Bug"},"status":{"id":1,"name":"New"},"priority":{"id":1,"name":"Normal"},"author":{"id":1,"name":"A"}}}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "get", "7"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "# #7 Subj") {
		t.Errorf("markdown header missing:\n%s", out.String())
	}
}
```

**Step 3: Run**

```bash
go test ./internal/commands/ -v
```

**Step 4: Commit**

```bash
git add internal/commands/issues.go internal/commands/issues_test.go
git commit -m "feat: implement issues list and get commands"
```

---

## Task 10: `attachments download`

**Files:**
- Modify: `internal/commands/attachments.go`
- Create: `internal/commands/attachments_test.go`

**Step 1: Implement**

```go
package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/output"
)

func newAttachmentsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Attachment operations"}
	c.AddCommand(newAttachmentsDownloadCmd(rc))
	return c
}

func newAttachmentsDownloadCmd(rc *runCtx) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download an attachment to a local file (prints the path)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid attachment id %q", args[0])
			}
			meta, body, err := rc.client.GetAttachment(rc.ctx(), id)
			if err != nil {
				return err
			}
			defer body.Close()

			dest := outPath
			if dest == "" {
				dir := filepath.Join(os.TempDir(), "redmine-cli")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				dest = filepath.Join(dir, fmt.Sprintf("%d-%s", meta.ID, meta.Filename))
			}

			f, err := os.Create(dest)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(f, body); err != nil {
				return err
			}
			abs, err := filepath.Abs(dest)
			if err != nil {
				abs = dest
			}

			if rc.format == "markdown" {
				_, err := fmt.Fprintf(rc.out, "Saved attachment %d to `%s`\n", meta.ID, abs)
				return err
			}
			return output.JSON(rc.out, map[string]any{
				"id":           meta.ID,
				"filename":     meta.Filename,
				"path":         abs,
				"content_type": meta.ContentType,
				"size":         meta.Filesize,
			})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Output path (default: $TMPDIR/redmine-cli/<id>-<filename>)")
	return cmd
}
```

**Step 2: Test**

```go
package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
)

func TestAttachmentsDownload_TempPath(t *testing.T) {
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer fileSrv.Close()

	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"attachment":{"id":7,"filename":"file.txt","filesize":7,"content_type":"text/plain","content_url":"` + fileSrv.URL + `/f","author":{"id":1,"name":"A"},"created_on":"2026-01-01"}}`
		_, _ = w.Write([]byte(body))
	}))
	defer metaSrv.Close()

	c := api.New(metaSrv.URL, "k", metaSrv.Client())
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"attachments", "download", "7"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	path, _ := got["path"].(string)
	if !strings.HasSuffix(path, "7-file.txt") {
		t.Errorf("path=%s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Errorf("file=%q", data)
	}
	_ = os.Remove(path)
}
```

**Step 3: Run**

```bash
go test ./internal/commands/ -v
```

**Step 4: Commit**

```bash
git add internal/commands/attachments.go internal/commands/attachments_test.go
git commit -m "feat: implement attachments download command"
```

---

## Task 11: SKILL.md

**Files:**
- Create: `skills/redmine-cli/SKILL.md`

**Step 1: Write skill**

```markdown
---
name: redmine-cli
description: |
  Interact with Redmine/Planio via the redmine-cli binary. Phase 1 covers
  read operations: list/get issues, list projects, download attachments.
  Use for ANY Redmine question or read action.
triggers:
  - redmine
  - /redmine
  - planio
  - redmine issue
  - redmine project
  - redmine attachment
  - my redmine issues
  - assigned to me in redmine
  - download redmine attachment
  - rm.jkraemer.net
invocable: true
argument-hint: "[action] [args...]"
---

# /redmine - Redmine CLI Workflow

Phase 1 (read-only) coverage: issues, projects, attachments.

## Agent Invariants

1. **Choose the right output mode:** `--format json` (default) for parsing,
   `--format markdown` (`-m`) for human-readable output.
2. **Use `--agent --help` to discover commands** — works at every level and
   returns structured JSON.
3. **For attachments, follow the path** — `attachments download` writes the
   file to disk and prints the absolute path; read it from there with your
   file tool.
4. **Default page size is 25, max 100.** Use `--all` to auto-paginate.

## CLI Introspection

```bash
redmine-cli --agent --help                     # top-level
redmine-cli issues --agent --help              # issue subcommand tree
redmine-cli issues list --agent --help         # full flag list
```

Returns JSON `{command, path, short, long, usage, flags[], inherited_flags[], subcommands[], notes[]}`.

## Quick Reference

| Task | Command |
|------|---------|
| List projects | `redmine-cli projects list` |
| List my open issues | `redmine-cli issues list --assignee me --status open` |
| List recent issues in a project | `redmine-cli issues list --project myproj --updated-since 2026-04-01` |
| Get full issue with journals | `redmine-cli issues get 1459 --include journals,attachments` |
| Download attachment | `redmine-cli attachments download 42` |
| Save attachment to a path | `redmine-cli attachments download 42 -o /tmp/foo.png` |

## Configuration

Env (highest priority):

    export REDMINE_URL=https://your.redmine.example
    export REDMINE_API_KEY=...

Or `~/.config/redmine-cli/config.toml`:

    url = "https://your.redmine.example"
    api_key = "..."
    default_format = "markdown"

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | OK |
| 1 | Usage error |
| 2 | Not found (404) |
| 3 | Auth error / missing config |
| 4 | Forbidden (403) |
| 5 | Rate limited (429) |
| 6 | Network error |
| 7 | API error (5xx) |

## Out of Scope (Phase 1)

Write operations (`create`/`update`), time logging, OAuth, Planio Help Desk,
search. Use the existing Redmine MCP tools for those until Phase 2 ships.
```

**Step 2: Commit**

```bash
git add skills/
git commit -m "docs: add Fera SKILL.md for redmine-cli"
```

---

## Task 12: Final Build, Coverage Check, Smoke Test

**Step 1: Build & verify**

```bash
cd /home/fera/agents/coding/projects/redmine-cli
make build
./redmine-cli --help
./redmine-cli --agent --help | head -20
./redmine-cli projects --agent --help
./redmine-cli issues list --agent --help
```

Expected: each emits sensible help / JSON.

**Step 2: Run all tests with coverage**

```bash
go test -cover ./...
```

Expected: every package PASS; coverage on `internal/api`, `internal/config`,
`internal/output`, `internal/agenthelp`, `internal/commands` ≥80%.

**Step 3: vet + fmt**

```bash
go vet ./...
gofmt -l .
```

Expected: no output from either.

**Step 4: Smoke test against mock**

```bash
# Quick sanity using a one-shot mock (no real API)
cat > /tmp/smoke.go <<'EOF'
package main
import ("fmt";"net/http";"net/http/httptest")
func main() {
  s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type","application/json")
    fmt.Fprintln(w, `{"projects":[{"id":1,"identifier":"smoke","name":"Smoke"}],"total_count":1,"offset":0,"limit":25}`)
  }))
  fmt.Println(s.URL)
  select {}
}
EOF
# (Run separately in real test session)
```

(Skip if cumbersome — `go test ./...` already covers behaviour.)

**Step 5: Final commit**

```bash
git add -A
git status
git commit --allow-empty -m "chore: phase 1 complete (read-only commands)"
git log --oneline
```

---

## Acceptance Criteria

- [ ] `make build` produces `./redmine-cli` binary
- [ ] `go test ./...` passes
- [ ] `go vet ./...` clean
- [ ] `gofmt -l .` empty
- [ ] `./redmine-cli --agent --help` emits valid JSON with subcommands
- [ ] `./redmine-cli projects --agent --help` drills down correctly
- [ ] All four commands implemented: `projects list`, `issues list`,
      `issues get`, `attachments download`
- [ ] `--format json` and `--format markdown` both work for each command
- [ ] Config loads from env vars and TOML; env wins
- [ ] Exit codes map per design (3 for auth, 2 for 404, etc.)
- [ ] `skills/redmine-cli/SKILL.md` exists and documents the binary
