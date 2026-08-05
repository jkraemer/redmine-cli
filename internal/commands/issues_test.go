package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
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

func TestIssuesList_IncludePassedAsQuery(t *testing.T) {
	var seen string
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "list", "--include", "attachments,relations"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen, "include=attachments%2Crelations") {
		t.Errorf("query missing include: %s", seen)
	}
}

func TestIssuesCreate_DryRun_NoNetworkCall(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode (path=%s)", r.URL.Path)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create", "--project", "myproj", "--tracker", "2", "--subject", "hello"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run should exit 0, got: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["dry_run"] != true {
		t.Errorf("dry_run flag missing: %v", got)
	}
	if got["method"] != "POST" {
		t.Errorf("method=%v", got["method"])
	}
	if got["path"] != "/issues.json" {
		t.Errorf("path=%v", got["path"])
	}
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	if issue == nil || issue["project_id"] != "myproj" || issue["subject"] != "hello" {
		t.Errorf("issue body wrong: %v", body)
	}
}

func TestIssuesCreate_Confirm_SendsPOST(t *testing.T) {
	var seenMethod, seenPath, seenAPIKey, seenContentType string
	var seenBody []byte
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		seenAPIKey = r.Header.Get("X-Redmine-API-Key")
		seenContentType = r.Header.Get("Content-Type")
		seenBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"issue":{"id":42,"subject":"hello","project":{"id":1,"name":"P"},"tracker":{"id":2,"name":"Bug"},"status":{"id":1,"name":"New"},"priority":{"id":1,"name":"Normal"},"author":{"id":1,"name":"A"}}}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "myproj", "--tracker", "2", "--subject", "hi", "--description", "body",
		"--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if seenMethod != "POST" || seenPath != "/issues.json" {
		t.Errorf("wrong request: %s %s", seenMethod, seenPath)
	}
	if seenAPIKey != "k" {
		t.Errorf("api key=%s", seenAPIKey)
	}
	if seenContentType != "application/json" {
		t.Errorf("content-type=%s", seenContentType)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(seenBody, &bodyJSON); err != nil {
		t.Fatalf("body not JSON: %v\n%s", err, string(seenBody))
	}
	wrapped, _ := bodyJSON["issue"].(map[string]any)
	if wrapped["project_id"] != "myproj" || wrapped["subject"] != "hi" || wrapped["description"] != "body" {
		t.Errorf("body wrong: %v", wrapped)
	}
	if !strings.Contains(out.String(), `"id": 42`) {
		t.Errorf("output missing decoded issue:\n%s", out.String())
	}
}

func TestIssuesCreate_MissingRequired(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for validation errors")
	})
	defer stop()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no project", []string{"issues", "create", "--tracker", "2", "--subject", "x"}, "--project"},
		{"no tracker", []string{"issues", "create", "--project", "p", "--subject", "x"}, "--tracker"},
		{"no subject", []string{"issues", "create", "--project", "p", "--tracker", "2"}, "--subject"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
			root := buildRootForTest(rc)
			root.SetArgs(tc.args)
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error, got nil. output=%s", out.String())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%q, want contains %q", err.Error(), tc.want)
			}
		})
	}
}

func TestIssuesUpdate_DryRun_PointerSemantics(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode")
	})
	defer stop()

	// Pass --description "" explicitly. Should appear in JSON; subject
	// (not passed) should NOT appear.
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--description", ""})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["method"] != "PUT" || got["path"] != "/issues/7.json" {
		t.Errorf("wrong request preview: %v", got)
	}
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	if _, ok := issue["description"]; !ok {
		t.Errorf("description should be present (explicitly empty): %v", issue)
	}
	if _, ok := issue["subject"]; ok {
		t.Errorf("subject should be omitted (not set): %v", issue)
	}
}

func TestIssuesUpdate_Confirm_SendsPUT(t *testing.T) {
	var seenMethod, seenPath string
	var seenBody []byte
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(204)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--notes", "test note", "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if seenMethod != "PUT" || seenPath != "/issues/7.json" {
		t.Errorf("wrong request: %s %s", seenMethod, seenPath)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(seenBody, &bodyJSON); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	wrapped, _ := bodyJSON["issue"].(map[string]any)
	if wrapped["notes"] != "test note" {
		t.Errorf("notes not sent: %v", wrapped)
	}
	// Confirmation output should mention the issue ID.
	if !strings.Contains(out.String(), "7") {
		t.Errorf("output missing id:\n%s", out.String())
	}
}

func TestIssuesUpdate_NoFields(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for validation errors")
	})
	defer stop()

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Errorf("err=%q", err.Error())
	}
}

// A malicious Redmine user with project edit access can embed ANSI escape
// sequences (e.g. OSC 52 for clipboard hijack) in fields like subject,
// description, or journal notes. Markdown output goes to a terminal — those
// bytes must be stripped before printing.
func TestIssuesGet_Markdown_StripsTerminalEscapes(t *testing.T) {
	// Note: JSON string literals here use a real ESC (0x1b) and BEL (0x07).
	body := "{\"issue\":{\"id\":7," +
		"\"subject\":\"Innocent\\u001b]52;c;PAYLOAD\\u0007subj\"," +
		"\"description\":\"line1\\u001b[2Jline2\"," +
		"\"project\":{\"id\":1,\"name\":\"P\\u0007rj\"}," +
		"\"tracker\":{\"id\":1,\"name\":\"Bug\"}," +
		"\"status\":{\"id\":1,\"name\":\"New\"}," +
		"\"priority\":{\"id\":1,\"name\":\"Normal\"}," +
		"\"author\":{\"id\":1,\"name\":\"A\\u001bvil\"}," +
		"\"journals\":[{\"id\":1,\"notes\":\"hi\\u001b[Athere\",\"created_on\":\"t\",\"user\":{\"id\":1,\"name\":\"u\"}}]}}"
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "get", "7", "--include", "journals"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, bad := range []string{"\x1b", "\x07"} {
		if strings.Contains(got, bad) {
			t.Errorf("output contains raw control byte %q:\n%s", bad, got)
		}
	}
	// Visible (non-control) bytes around the escape sequence survive: we
	// strip ONLY the bytes a terminal would interpret (ESC, BEL, etc.), not
	// the printable payload that follows them, which is harmless on its own.
	for _, want := range []string{"Innocent", "PAYLOAD", "subj", "line1", "line2", "Prj", "Avil"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
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

func TestIssuesGet_Markdown_Attachments(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issue":{"id":7,"subject":"Subj","description":"Body","project":{"id":1,"name":"P"},"tracker":{"id":1,"name":"Bug"},"status":{"id":1,"name":"New"},"priority":{"id":1,"name":"Normal"},"author":{"id":1,"name":"A"},"attachments":[{"id":42,"filename":"diagram.png","filesize":2048,"content_type":"image/png","content_url":"https://example.test/attachments/download/42/diagram.png","description":"architecture sketch","author":{"id":1,"name":"A"},"created_on":"2026-05-01T12:00:00Z"},{"id":43,"filename":"notes.txt","filesize":12,"content_url":"https://example.test/attachments/download/43/notes.txt","author":{"id":1,"name":"A"},"created_on":"2026-05-02T12:00:00Z"}]}}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "get", "7", "--include", "attachments"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"## Attachments",
		"diagram.png",
		"architecture sketch",
		"https://example.test/attachments/download/42/diagram.png",
		"notes.txt",
		"https://example.test/attachments/download/43/notes.txt",
		"42",
		"43",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q:\n%s", want, got)
		}
	}
}

func TestIssuesGet_Markdown_NoAttachmentsSection_WhenEmpty(t *testing.T) {
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
	if strings.Contains(out.String(), "## Attachments") {
		t.Errorf("attachments section should be omitted when none present:\n%s", out.String())
	}
}

func TestIssuesUpdate_NotesFile_DryRun(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode")
	})
	defer stop()

	dir := t.TempDir()
	notesPath := filepath.Join(dir, "notes.txt")
	contents := "Line 1\nLine 2\n   trailing spaces   \n\n"
	if err := os.WriteFile(notesPath, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--notes-file", notesPath})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	if issue["notes"] != contents {
		t.Errorf("notes mismatch.\nwant: %q\ngot:  %q", contents, issue["notes"])
	}
}

func TestIssuesUpdate_NotesFile_ConflictsWithNotes(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for validation errors")
	})
	defer stop()

	dir := t.TempDir()
	notesPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notesPath, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--notes", "x", "--notes-file", notesPath})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "--notes") || !strings.Contains(err.Error(), "--notes-file") {
		t.Errorf("err=%q, want mentions of --notes and --notes-file", err.Error())
	}
}

func TestIssuesUpdate_NotesFile_ReadError(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for read errors")
	})
	defer stop()

	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--notes-file", missing})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("err=%q, want mention of path %q", err.Error(), missing)
	}
}

func TestIssuesCreate_CustomFields_InBody(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode")
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "p", "--tracker", "2", "--subject", "s",
		"--cf", "1=hello", "--cf", "2=world"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	cfs, ok := issue["custom_fields"].([]any)
	if !ok {
		t.Fatalf("custom_fields not a list: %v", issue["custom_fields"])
	}
	if len(cfs) != 2 {
		t.Fatalf("expected 2 custom fields, got %d", len(cfs))
	}
	first, _ := cfs[0].(map[string]any)
	second, _ := cfs[1].(map[string]any)
	if first["id"].(float64) != 1 || first["value"] != "hello" {
		t.Errorf("first cf wrong: %v", first)
	}
	if second["id"].(float64) != 2 || second["value"] != "world" {
		t.Errorf("second cf wrong: %v", second)
	}
}

func TestIssuesUpdate_CustomFields_InBody(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode")
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7",
		"--cf", "3=alpha", "--cf", "4=beta"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	cfs, ok := issue["custom_fields"].([]any)
	if !ok {
		t.Fatalf("custom_fields not a list: %v", issue["custom_fields"])
	}
	if len(cfs) != 2 {
		t.Fatalf("expected 2 custom fields, got %d", len(cfs))
	}
	first, _ := cfs[0].(map[string]any)
	if first["id"].(float64) != 3 || first["value"] != "alpha" {
		t.Errorf("first cf wrong: %v", first)
	}
}

func TestIssuesCreate_CustomFields_DuplicateID_Errors(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for validation errors")
	})
	defer stop()

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "p", "--tracker", "2", "--subject", "s",
		"--cf", "5=a", "--cf", "5=b"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "5") {
		t.Errorf("err=%q, want duplicate id 5 mention", err.Error())
	}
}

// TestIssuesList_All_PaginatesAcrossPages mocks 3 pages (total_count=250)
// with internal page size 100 and verifies that --all collects all 250
// items via three sequential API calls.
func TestIssuesList_All_PaginatesAcrossPages(t *testing.T) {
	const total = 250
	var calls int32
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		offset := r.URL.Query().Get("offset")
		limit := r.URL.Query().Get("limit")
		if limit != "100" {
			t.Errorf("expected limit=100, got %s", limit)
		}
		// Determine which page based on offset.
		var off int
		if offset == "" {
			off = 0
		} else {
			fmt.Sscanf(offset, "%d", &off)
		}
		pageSize := 100
		end := off + pageSize
		if end > total {
			end = total
		}
		var items []string
		for i := off; i < end; i++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"subject":"s%d","project":{"id":1,"name":"P"},"tracker":{"id":1,"name":"T"},"status":{"id":1,"name":"S"},"priority":{"id":1,"name":"N"},"author":{"id":1,"name":"A"}}`, i+1, i+1))
		}
		fmt.Fprintf(w, `{"issues":[%s],"total_count":%d,"offset":%d,"limit":%d}`, strings.Join(items, ","), total, off, pageSize)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "list", "--all"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("expected 3 API calls, got %d", got)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	issues, _ := got["issues"].([]any)
	if len(issues) != total {
		t.Errorf("expected %d issues, got %d", total, len(issues))
	}
}

// TestIssuesList_All_RespectsCap mocks total_count=1500 and expects --all
// to refuse with an error mentioning the count and "narrow".
func TestIssuesList_All_RespectsCap(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issues":[],"total_count":1500,"offset":0,"limit":100}`))
	})
	defer stop()

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "list", "--all"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "1500") {
		t.Errorf("err=%q, want mention of 1500", err.Error())
	}
	if !strings.Contains(err.Error(), "narrow") {
		t.Errorf("err=%q, want mention of 'narrow'", err.Error())
	}
}

// TestIssuesCreate_Attach_DryRun verifies that a dry-run create with --attach
// surfaces a top-level "would_upload" entry and embeds a placeholder token in
// the issue body's "uploads" array. No HTTP requests should be made.
func TestIssuesCreate_Attach_DryRun(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode (path=%s)", r.URL.Path)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "P", "--tracker", "1", "--subject", "S",
		"--attach", "foo.txt"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	wu, ok := got["would_upload"].([]any)
	if !ok {
		t.Fatalf("would_upload not an array: %T %v", got["would_upload"], got["would_upload"])
	}
	if len(wu) != 1 {
		t.Fatalf("len(would_upload)=%d", len(wu))
	}
	first, _ := wu[0].(map[string]any)
	if first["path"] != "foo.txt" {
		t.Errorf("would_upload[0].path=%v", first["path"])
	}

	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	uploads, ok := issue["uploads"].([]any)
	if !ok {
		t.Fatalf("issue.uploads not an array: %v", issue["uploads"])
	}
	if len(uploads) != 1 {
		t.Fatalf("len(issue.uploads)=%d", len(uploads))
	}
	u0, _ := uploads[0].(map[string]any)
	if u0["token"] != "<UPLOAD-TOKEN-FOR-foo.txt>" {
		t.Errorf("token=%v", u0["token"])
	}
	if u0["filename"] != "foo.txt" {
		t.Errorf("filename=%v", u0["filename"])
	}
}

// TestIssuesCreate_Attach_Confirm_Single verifies the wired confirm path:
// the upload happens before the create, and the resulting upload token is
// referenced in the create body's uploads array.
func TestIssuesCreate_Attach_Confirm_Single(t *testing.T) {
	var (
		uploadCalls, createCalls int32
		createBody               []byte
	)
	uploadH := uploadEchoHandler(t, &uploadCalls)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/uploads.json":
			uploadH(w, r)
		case "/issues.json":
			atomic.AddInt32(&createCalls, 1)
			createBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"issue":{"id":42,"subject":"S","project":{"id":1,"name":"P"},"tracker":{"id":1,"name":"T"},"status":{"id":1,"name":"New"},"priority":{"id":1,"name":"N"},"author":{"id":1,"name":"A"}}}`))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	path := mustWriteTempFile(t, t.TempDir(), "hello.txt", "hi")

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "P", "--tracker", "1", "--subject", "S",
		"--attach", path, "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 1 {
		t.Errorf("upload calls=%d, want 1", got)
	}
	if got := atomic.LoadInt32(&createCalls); got != 1 {
		t.Errorf("create calls=%d, want 1", got)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(createBody, &bodyJSON); err != nil {
		t.Fatalf("create body not JSON: %v", err)
	}
	issue, _ := bodyJSON["issue"].(map[string]any)
	uploads, ok := issue["uploads"].([]any)
	if !ok || len(uploads) != 1 {
		t.Fatalf("issue.uploads wrong: %v", issue["uploads"])
	}
	u0, _ := uploads[0].(map[string]any)
	if u0["token"] != "tok-1" {
		t.Errorf("token=%v", u0["token"])
	}
	if u0["filename"] != "hello.txt" {
		t.Errorf("filename=%v", u0["filename"])
	}
}

// TestIssuesCreate_Attach_Confirm_Multi verifies two attachments end up in
// the create body in spec order with their respective tokens.
func TestIssuesCreate_Attach_Confirm_Multi(t *testing.T) {
	var (
		uploadCalls, createCalls int32
		createBody               []byte
	)
	uploadH := uploadEchoHandler(t, &uploadCalls)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/uploads.json":
			uploadH(w, r)
		case "/issues.json":
			atomic.AddInt32(&createCalls, 1)
			createBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"issue":{"id":42,"subject":"S","project":{"id":1,"name":"P"},"tracker":{"id":1,"name":"T"},"status":{"id":1,"name":"New"},"priority":{"id":1,"name":"N"},"author":{"id":1,"name":"A"}}}`))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	pa := mustWriteTempFile(t, dir, "a.txt", "aa")
	pb := mustWriteTempFile(t, dir, "b.bin", "bb")

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "P", "--tracker", "1", "--subject", "S",
		"--attach", pa, "--attach", pb, "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 2 {
		t.Errorf("upload calls=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&createCalls); got != 1 {
		t.Errorf("create calls=%d, want 1", got)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(createBody, &bodyJSON); err != nil {
		t.Fatalf("create body not JSON: %v", err)
	}
	issue, _ := bodyJSON["issue"].(map[string]any)
	uploads, _ := issue["uploads"].([]any)
	if len(uploads) != 2 {
		t.Fatalf("len(uploads)=%d", len(uploads))
	}
	u0, _ := uploads[0].(map[string]any)
	u1, _ := uploads[1].(map[string]any)
	if u0["token"] != "tok-1" || u0["filename"] != "a.txt" {
		t.Errorf("uploads[0]=%v", u0)
	}
	if u1["token"] != "tok-2" || u1["filename"] != "b.bin" {
		t.Errorf("uploads[1]=%v", u1)
	}
}

// TestIssuesCreate_Attach_PreflightFailure_NoServerCall verifies that a
// missing local file aborts before any HTTP request is made.
func TestIssuesCreate_Attach_PreflightFailure_NoServerCall(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		t.Errorf("server should not be called when pre-flight fails (path=%s)", r.URL.Path)
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "P", "--tracker", "1", "--subject", "S",
		"--attach", missing, "--confirm"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("err=%q, want mention of %q", err.Error(), missing)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("calls=%d, want 0", got)
	}
}

// TestIssuesCreate_Attach_MidBatchUploadFailure verifies that a 500 on the
// second upload short-circuits before the create call, with an error that
// names "attach 2/2 (path)".
func TestIssuesCreate_Attach_MidBatchUploadFailure(t *testing.T) {
	var (
		uploadCalls, createCalls int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/uploads.json":
			n := atomic.AddInt32(&uploadCalls, 1)
			if n == 2 {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = fmt.Fprintf(w, `{"upload":{"id":%d,"token":"tok-%d"}}`, n, n)
		case "/issues.json":
			atomic.AddInt32(&createCalls, 1)
			t.Errorf("create should not be called when an upload fails")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"issue":{}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	pa := filepath.Join(dir, "a.txt")
	pb := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pa, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create",
		"--project", "P", "--tracker", "1", "--subject", "S",
		"--attach", pa, "--attach", pb, "--confirm"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "attach 2/2") {
		t.Errorf("err=%q, want 'attach 2/2'", err.Error())
	}
	if !strings.Contains(err.Error(), pb) {
		t.Errorf("err=%q, want mention of path %q", err.Error(), pb)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 2 {
		t.Errorf("upload calls=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&createCalls); got != 0 {
		t.Errorf("create calls=%d, want 0", got)
	}
}

func TestIssuesCreate_CustomFields_BadFormat(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for validation errors")
	})
	defer stop()

	cases := []struct {
		name string
		val  string
	}{
		{"no equals", "foo"},
		{"non-numeric id", "abc=val"},
		{"zero id", "0=val"},
		{"negative id", "-1=val"},
		{"empty id", "=val"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
			root := buildRootForTest(rc)
			root.SetArgs([]string{"issues", "create",
				"--project", "p", "--tracker", "2", "--subject", "s",
				"--cf", tc.val})
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tc.val)
			}
			if !strings.Contains(err.Error(), "--cf") {
				t.Errorf("err=%q, want mention of --cf", err.Error())
			}
		})
	}
}

// TestIssuesUpdate_Attach_Alone_IsValid verifies that --attach by itself
// satisfies the "at least one mutating field" guard, and that the resulting
// PUT body carries only the uploads (no other issue fields).
func TestIssuesUpdate_Attach_Alone_IsValid(t *testing.T) {
	var (
		uploadCalls, updateCalls int32
		updateBody               []byte
	)
	uploadH := uploadEchoHandler(t, &uploadCalls)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/uploads.json":
			uploadH(w, r)
		case r.URL.Path == "/issues/7.json" && r.Method == "PUT":
			atomic.AddInt32(&updateCalls, 1)
			updateBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	path := mustWriteTempFile(t, t.TempDir(), "hello.txt", "hi")

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--attach", path, "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 1 {
		t.Errorf("upload calls=%d, want 1", got)
	}
	if got := atomic.LoadInt32(&updateCalls); got != 1 {
		t.Errorf("update calls=%d, want 1", got)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(updateBody, &bodyJSON); err != nil {
		t.Fatalf("update body not JSON: %v", err)
	}
	issue, _ := bodyJSON["issue"].(map[string]any)
	uploads, ok := issue["uploads"].([]any)
	if !ok || len(uploads) != 1 {
		t.Fatalf("issue.uploads wrong: %v", issue["uploads"])
	}
	u0, _ := uploads[0].(map[string]any)
	if u0["token"] != "tok-1" {
		t.Errorf("token=%v", u0["token"])
	}
	if u0["filename"] != "hello.txt" {
		t.Errorf("filename=%v", u0["filename"])
	}
	// Only "uploads" should be present — no other issue fields set.
	for k := range issue {
		if k != "uploads" {
			t.Errorf("unexpected field in update body: %q", k)
		}
	}
}

// TestIssuesUpdate_Attach_WithOtherFields verifies that --attach combines
// with other mutating flags in the same PUT body.
func TestIssuesUpdate_Attach_WithOtherFields(t *testing.T) {
	var (
		uploadCalls, updateCalls int32
		updateBody               []byte
	)
	uploadH := uploadEchoHandler(t, &uploadCalls)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/uploads.json":
			uploadH(w, r)
		case r.URL.Path == "/issues/7.json" && r.Method == "PUT":
			atomic.AddInt32(&updateCalls, 1)
			updateBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	path := mustWriteTempFile(t, t.TempDir(), "patch.diff", "diff")

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7",
		"--notes", "see attached", "--attach", path, "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 1 {
		t.Errorf("upload calls=%d, want 1", got)
	}
	if got := atomic.LoadInt32(&updateCalls); got != 1 {
		t.Errorf("update calls=%d, want 1", got)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(updateBody, &bodyJSON); err != nil {
		t.Fatalf("update body not JSON: %v", err)
	}
	issue, _ := bodyJSON["issue"].(map[string]any)
	if issue["notes"] != "see attached" {
		t.Errorf("notes=%v", issue["notes"])
	}
	uploads, ok := issue["uploads"].([]any)
	if !ok || len(uploads) != 1 {
		t.Fatalf("issue.uploads wrong: %v", issue["uploads"])
	}
	u0, _ := uploads[0].(map[string]any)
	if u0["filename"] != "patch.diff" {
		t.Errorf("filename=%v", u0["filename"])
	}
}

// TestIssuesUpdate_Attach_DryRun verifies dry-run mode for update + --attach:
// no HTTP calls, would_upload surfaced, placeholder token in body.
func TestIssuesUpdate_Attach_DryRun(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode (path=%s)", r.URL.Path)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--attach", "foo.txt"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["method"] != "PUT" || got["path"] != "/issues/7.json" {
		t.Errorf("wrong request preview: %v", got)
	}
	wu, ok := got["would_upload"].([]any)
	if !ok {
		t.Fatalf("would_upload not an array: %T %v", got["would_upload"], got["would_upload"])
	}
	if len(wu) != 1 {
		t.Fatalf("len(would_upload)=%d", len(wu))
	}
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	uploads, ok := issue["uploads"].([]any)
	if !ok || len(uploads) != 1 {
		t.Fatalf("issue.uploads wrong: %v", issue["uploads"])
	}
	u0, _ := uploads[0].(map[string]any)
	if u0["token"] != "<UPLOAD-TOKEN-FOR-foo.txt>" {
		t.Errorf("token=%v", u0["token"])
	}
}

// TestIssuesUpdate_Attach_PreflightFailure_NoServerCall verifies that a
// missing local file aborts before any HTTP request.
func TestIssuesUpdate_Attach_PreflightFailure_NoServerCall(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		t.Errorf("server should not be called when pre-flight fails (path=%s)", r.URL.Path)
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7", "--attach", missing, "--confirm"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("err=%q, want mention of %q", err.Error(), missing)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("calls=%d, want 0", got)
	}
}

// TestIssuesUpdate_Attach_MidBatchUploadFailure verifies that a 500 on the
// second upload short-circuits before the PUT call.
func TestIssuesUpdate_Attach_MidBatchUploadFailure(t *testing.T) {
	var (
		uploadCalls, updateCalls int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/uploads.json":
			n := atomic.AddInt32(&uploadCalls, 1)
			if n == 2 {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = fmt.Fprintf(w, `{"upload":{"id":%d,"token":"tok-%d"}}`, n, n)
		case r.URL.Path == "/issues/7.json" && r.Method == "PUT":
			atomic.AddInt32(&updateCalls, 1)
			t.Errorf("update should not be called when an upload fails")
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	pa := filepath.Join(dir, "a.txt")
	pb := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pa, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "7",
		"--attach", pa, "--attach", pb, "--confirm"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "attach 2/2") {
		t.Errorf("err=%q, want 'attach 2/2'", err.Error())
	}
	if !strings.Contains(err.Error(), pb) {
		t.Errorf("err=%q, want mention of %q", err.Error(), pb)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 2 {
		t.Errorf("upload calls=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&updateCalls); got != 0 {
		t.Errorf("update calls=%d, want 0", got)
	}
}

func TestIssuesList_QueryIDFlag_ForwardsToServer(t *testing.T) {
	var gotURL string
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "list", "--query-id", "42"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "query_id=42") {
		t.Errorf("URL missing query_id=42: %s", gotURL)
	}
}

func TestIssuesList_NoQueryID_OmitsParam(t *testing.T) {
	var gotURL string
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotURL, "query_id") {
		t.Errorf("URL should not include query_id when flag absent: %s", gotURL)
	}
}

func TestIssuesCreate_Category_DryRunBody(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode (path=%s)", r.URL.Path)
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "create", "--project", "p", "--tracker", "1",
		"--subject", "s", "--category", "7"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	if issue["category_id"] != "7" {
		t.Errorf("category_id=%v", issue["category_id"])
	}
}

func TestIssuesUpdate_Category_SetAndClear(t *testing.T) {
	for _, tc := range []struct {
		flagVal string
		want    any
	}{
		{"7", "7"},
		{"", ""},
	} {
		c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("server should not be called in dry-run mode")
		})
		var out bytes.Buffer
		rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
		root := buildRootForTest(rc)
		root.SetArgs([]string{"issues", "update", "42", "--category", tc.flagVal})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		stop()
		var got map[string]any
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		body, _ := got["body"].(map[string]any)
		issue, _ := body["issue"].(map[string]any)
		v, present := issue["category_id"]
		if !present || v != tc.want {
			t.Errorf("--category %q: category_id=%v present=%v", tc.flagVal, v, present)
		}
	}
}

func TestIssuesUpdate_NoCategoryFlag_OmitsField(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no server call expected")
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "update", "42", "--subject", "s"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out.Bytes(), &got)
	body, _ := got["body"].(map[string]any)
	issue, _ := body["issue"].(map[string]any)
	if _, present := issue["category_id"]; present {
		t.Errorf("category_id must be omitted when flag not set: %v", issue)
	}
}
