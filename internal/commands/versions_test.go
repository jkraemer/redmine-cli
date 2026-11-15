package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const versionsFixture = `{"versions":[{"id":3,"project":{"id":1,"name":"P"},"name":"1.0","status":"open","due_date":"2026-12-01","sharing":"none"},{"id":4,"project":{"id":2,"name":"Other"},"name":"2.0","status":"locked","sharing":"system"}],"total_count":2}`

func TestVersionsList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/myproj/versions.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(versionsFixture))
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"versions", "list", "--project", "myproj"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	versions, _ := got["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("versions=%v", got)
	}
	first, _ := versions[0].(map[string]any)
	if first["name"] != "1.0" || first["status"] != "open" {
		t.Errorf("first version=%v", first)
	}
}

func TestVersionsList_Markdown(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(versionsFixture))
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"versions", "list", "-p", "myproj"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"| ID", "Status", "Due Date", "Project", "| 3", "1.0", "open", "2026-12-01", "| 4", "locked", "Other"} {
		if !strings.Contains(s, want) {
			t.Errorf("table missing %q:\n%s", want, s)
		}
	}
}

func TestVersionsList_RequiresProject(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"versions", "list"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error without --project")
	}
}
