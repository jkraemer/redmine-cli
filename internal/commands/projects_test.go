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
