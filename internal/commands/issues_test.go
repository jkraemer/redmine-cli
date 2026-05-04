package commands

import (
	"bytes"
	"encoding/json"
	"io"
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
