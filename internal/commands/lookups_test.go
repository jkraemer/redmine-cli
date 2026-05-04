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

func TestUsersMe_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/current.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":{"id":7,"login":"jens","firstname":"Jens","lastname":"K","mail":"jk@example.com","admin":true}}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"users", "me"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"login": "jens"`) {
		t.Errorf("missing login: %s", out.String())
	}
	if !strings.Contains(out.String(), `"user"`) {
		t.Errorf("expected user wrapper: %s", out.String())
	}
}

func TestUsersMe_Markdown(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"id":7,"login":"jens","firstname":"Jens","lastname":"K","mail":"jk@example.com"}}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"users", "me"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "| Field | Value") {
		t.Errorf("not a markdown table:\n%s", s)
	}
	if !strings.Contains(s, "jens") || !strings.Contains(s, "Jens K") {
		t.Errorf("missing data:\n%s", s)
	}
}

func TestUsersList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users":[{"id":1,"firstname":"A","lastname":"B","mail":"a@b.c"}],"total_count":1,"offset":0,"limit":25}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"users", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"users"`) || !strings.Contains(out.String(), `"a@b.c"`) {
		t.Errorf("output unexpected: %s", out.String())
	}
}

// TestUsersList_All_PaginatesAcrossPages mocks 3 pages (total_count=250)
// with internal page size 100 and verifies that --all collects all 250
// items via three sequential API calls.
func TestUsersList_All_PaginatesAcrossPages(t *testing.T) {
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
			items = append(items, fmt.Sprintf(`{"id":%d,"login":"u%d","firstname":"F%d","lastname":"L%d","mail":"u%d@example.com"}`, i+1, i+1, i+1, i+1, i+1))
		}
		fmt.Fprintf(w, `{"users":[%s],"total_count":%d,"offset":%d,"limit":%d}`, strings.Join(items, ","), total, off, pageSize)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"users", "list", "--all"})
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
	users, _ := got["users"].([]any)
	if len(users) != total {
		t.Errorf("expected %d users, got %d", total, len(users))
	}
}

func TestTrackersList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trackers.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"trackers":[{"id":1,"name":"Bug","default_status":{"id":1,"name":"New"}}]}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"trackers", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"trackers"`) || !strings.Contains(out.String(), `"Bug"`) {
		t.Errorf("output unexpected: %s", out.String())
	}
}

func TestTrackersList_Markdown(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"trackers":[{"id":1,"name":"Bug","default_status":{"id":1,"name":"New"}}]}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"trackers", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "| ID | Name | Default Status |") {
		t.Errorf("not a markdown table:\n%s", s)
	}
	if !strings.Contains(s, "Bug") || !strings.Contains(s, "New") {
		t.Errorf("missing data:\n%s", s)
	}
}

func TestStatusesList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/issue_statuses.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issue_statuses":[{"id":1,"name":"New","is_closed":false},{"id":5,"name":"Closed","is_closed":true}]}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"statuses", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"issue_statuses"`) || !strings.Contains(out.String(), `"Closed"`) {
		t.Errorf("output unexpected: %s", out.String())
	}
}

func TestPrioritiesList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/enumerations/issue_priorities.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issue_priorities":[{"id":3,"name":"Normal","is_default":true,"active":true}]}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"priorities", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"issue_priorities"`) || !strings.Contains(out.String(), `"Normal"`) {
		t.Errorf("output unexpected: %s", out.String())
	}
}

func TestTimeActivitiesList_JSON(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/enumerations/time_entry_activities.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time_entry_activities":[{"id":8,"name":"Development","is_default":true,"active":true}]}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"time-activities", "list"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"time_entry_activities"`) || !strings.Contains(out.String(), `"Development"`) {
		t.Errorf("output unexpected: %s", out.String())
	}
}
