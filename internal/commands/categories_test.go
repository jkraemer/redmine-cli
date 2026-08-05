package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCategoriesList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/myproj/issue_categories.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issue_categories":[{"id":3,"name":"UI","project":{"id":1,"name":"P"},"assigned_to":{"id":2,"name":"Jens"}}]}`))
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"categories", "list", "--project", "myproj"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	cats, _ := got["issue_categories"].([]any)
	if len(cats) != 1 {
		t.Fatalf("issue_categories=%v", got)
	}
}

func TestCategoriesList_Markdown(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issue_categories":[{"id":3,"name":"UI","project":{"id":1,"name":"P"},"assigned_to":{"id":2,"name":"Jens"}},{"id":4,"name":"Backend","project":{"id":1,"name":"P"}}]}`))
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"categories", "list", "-p", "myproj"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "| 3") || !strings.Contains(out.String(), "Jens") {
		t.Errorf("table wrong:\n%s", out.String())
	}
}

func TestCategoriesList_RequiresProject(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"categories", "list"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error without --project")
	}
}
