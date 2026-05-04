package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// TestProjectsList_All_PaginatesAcrossPages mocks 3 pages (total_count=250)
// with internal page size 100 and verifies that --all collects all 250
// items via three sequential API calls.
func TestProjectsList_All_PaginatesAcrossPages(t *testing.T) {
	const total = 250
	var calls int32
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		offset := r.URL.Query().Get("offset")
		limit := r.URL.Query().Get("limit")
		if limit != "100" {
			t.Errorf("expected limit=100, got %s", limit)
		}
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
			items = append(items, fmt.Sprintf(`{"id":%d,"identifier":"p%d","name":"Proj %d"}`, i+1, i+1, i+1))
		}
		fmt.Fprintf(w, `{"projects":[%s],"total_count":%d,"offset":%d,"limit":%d}`, strings.Join(items, ","), total, off, pageSize)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"projects", "list", "--all"})
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
	projects, _ := got["projects"].([]any)
	if len(projects) != total {
		t.Errorf("expected %d projects, got %d", total, len(projects))
	}
}

// TestProjectsList_All_RespectsCap mocks total_count=1500 and expects --all
// to refuse with an error mentioning the count and "narrow".
func TestProjectsList_All_RespectsCap(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[],"total_count":1500,"offset":0,"limit":100}`))
	})
	defer stop()

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"projects", "list", "--all"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "1500") || !strings.Contains(err.Error(), "narrow") {
		t.Errorf("err=%q", err.Error())
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
