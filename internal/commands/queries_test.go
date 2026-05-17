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

func TestQueriesList_JSON_RendersGlobalAndProjectScoped(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"queries":[{"id":1,"name":"All open","is_public":true,"project_id":null},{"id":2,"name":"Project bugs","is_public":false,"project_id":7}],"total_count":2,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"queries", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	queries, _ := got["queries"].([]any)
	if len(queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(queries))
	}
	first := queries[0].(map[string]any)
	if first["project_id"] != nil {
		t.Errorf("expected project_id=nil for global query, got %v", first["project_id"])
	}
	second := queries[1].(map[string]any)
	if pid, _ := second["project_id"].(float64); pid != 7 {
		t.Errorf("expected project_id=7 for second query, got %v", second["project_id"])
	}
}

func TestQueriesList_Markdown_BlankProjectIDForGlobal(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"queries":[{"id":1,"name":"All open","is_public":true,"project_id":null},{"id":2,"name":"Bugs","is_public":false,"project_id":7}],"total_count":2,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"queries", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"ID", "Name", "Public", "Project ID"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing column header %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "All open") || !strings.Contains(s, "Bugs") {
		t.Errorf("missing rows:\n%s", s)
	}
	// Verify the Project ID cell is blank for the global query and "7" for the
	// project-scoped one. Cells are pipe-delimited; the Project ID is the
	// fourth data cell (after the leading "|").
	for _, line := range strings.Split(s, "\n") {
		if !strings.Contains(line, "All open") && !strings.Contains(line, "Bugs") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 4 {
			t.Errorf("row has fewer than 4 cells: %q", line)
			continue
		}
		pid := strings.TrimSpace(cells[3])
		switch {
		case strings.Contains(line, "All open") && pid != "":
			t.Errorf("global query row should have blank Project ID, got %q in %q", pid, line)
		case strings.Contains(line, "Bugs") && pid != "7":
			t.Errorf("project-scoped row should have Project ID=7, got %q in %q", pid, line)
		}
	}
}

func TestQueriesList_All_PaginatesAcrossPages(t *testing.T) {
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
		if offset != "" {
			fmt.Sscanf(offset, "%d", &off)
		}
		pageSize := 100
		end := off + pageSize
		if end > total {
			end = total
		}
		var items []string
		for i := off; i < end; i++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"name":"Q%d","is_public":true,"project_id":null}`, i+1, i+1))
		}
		fmt.Fprintf(w, `{"queries":[%s],"total_count":%d,"offset":%d,"limit":%d}`, strings.Join(items, ","), total, off, pageSize)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"queries", "list", "--all"})
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
	if qs, _ := got["queries"].([]any); len(qs) != total {
		t.Errorf("expected %d queries, got %d", total, len(qs))
	}
}

func TestQueriesList_All_RespectsCap(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"queries":[],"total_count":1500,"offset":0,"limit":100}`))
	})
	defer stop()
	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"queries", "list", "--all"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "1500") || !strings.Contains(err.Error(), "narrow") {
		t.Errorf("err=%q", err.Error())
	}
}

func TestQueriesList_Empty(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"queries":[],"total_count":0,"offset":0,"limit":25}`))
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"queries", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if qs, _ := got["queries"].([]any); len(qs) != 0 {
		t.Errorf("expected empty queries, got %d entries: %s", len(qs), out.String())
	}
}
