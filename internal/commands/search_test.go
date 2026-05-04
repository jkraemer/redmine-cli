package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSearch_JSON_Output(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":7,"title":"#1459 (open): Build","type":"issue","url":"https://example.com/issues/1459","datetime":"2026-05-04T08:20:57Z"}],"total_count":1,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "build"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"type": "issue"`) {
		t.Errorf("missing type=issue in JSON output: %s", out.String())
	}
	if !strings.Contains(out.String(), `"id": 7`) {
		t.Errorf("missing id in JSON output: %s", out.String())
	}
}

func TestSearch_Markdown_Output(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"id":7,"title":"Build CLI","type":"issue","datetime":"2026-05-04T08:20:57Z"},{"id":2,"title":"Wiki Page","type":"wiki-page","datetime":"2026-05-03T08:00:00Z"}],"total_count":2,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "cli"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, "| Type") || !strings.Contains(body, "| ID") || !strings.Contains(body, "| Title") || !strings.Contains(body, "| Datetime") {
		t.Errorf("missing markdown headers:\n%s", body)
	}
	if !strings.Contains(body, "Build CLI") {
		t.Errorf("missing first row title: %s", body)
	}
	// Order should be preserved as returned by the API.
	first := strings.Index(body, "Build CLI")
	second := strings.Index(body, "Wiki Page")
	if first < 0 || second < 0 || first > second {
		t.Errorf("rows not in API order:\n%s", body)
	}
}

func TestSearch_QueryConcatenation(t *testing.T) {
	var seenQuery string
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "foo", "bar", "baz"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	// url.Values encodes spaces as "+" by default.
	if !strings.Contains(seenQuery, "q=foo+bar+baz") && !strings.Contains(seenQuery, "q=foo%20bar%20baz") {
		t.Errorf("multi-word query not concatenated: %s", seenQuery)
	}
}

func TestSearch_FilterFlags(t *testing.T) {
	var seenQuery string
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "foo", "--issues", "--titles-only", "--scope", "my_projects", "--project", "myproj"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"issues=1", "titles_only=1", "scope=my_projects", "project_id=myproj"} {
		if !strings.Contains(seenQuery, want) {
			t.Errorf("query missing %q: %s", want, seenQuery)
		}
	}
	for _, unwanted := range []string{"wiki_pages=", "projects=1"} {
		if strings.Contains(seenQuery, unwanted) {
			t.Errorf("query should not contain %q: %s", unwanted, seenQuery)
		}
	}
}

func TestSearch_AllTypesShorthand(t *testing.T) {
	var seenQuery string
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "foo", "--all-types"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"issues=1", "wiki_pages=1", "projects=1"} {
		if !strings.Contains(seenQuery, want) {
			t.Errorf("query missing %q: %s", want, seenQuery)
		}
	}
}

// TestSearch_All_PaginatesAcrossPages mocks 3 pages (total_count=250)
// with internal page size 100 and verifies that --all collects all 250
// items via three sequential API calls.
func TestSearch_All_PaginatesAcrossPages(t *testing.T) {
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
			items = append(items, fmt.Sprintf(`{"id":%d,"title":"t%d","type":"issue","datetime":"2026-05-04T00:00:00Z"}`, i+1, i+1))
		}
		fmt.Fprintf(w, `{"results":[%s],"total_count":%d,"offset":%d,"limit":%d}`, strings.Join(items, ","), total, off, pageSize)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "foo", "--all"})
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
	results, _ := got["results"].([]any)
	if len(results) != total {
		t.Errorf("expected %d results, got %d", total, len(results))
	}
}

// TestSearch_All_RespectsCap mocks total_count=1500 and expects --all
// to refuse with an error mentioning the count and "narrow".
func TestSearch_All_RespectsCap(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[],"total_count":1500,"offset":0,"limit":100}`))
	})
	defer stop()

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "foo", "--all"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "1500") || !strings.Contains(err.Error(), "narrow") {
		t.Errorf("err=%q", err.Error())
	}
}

func TestSearch_InvalidScope(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"search", "foo", "--scope", "bogus"})
	if err := root.Execute(); err == nil {
		t.Errorf("expected error for invalid scope")
	}
}
