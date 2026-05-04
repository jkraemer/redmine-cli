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
