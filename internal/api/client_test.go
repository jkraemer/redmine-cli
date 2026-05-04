package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Redmine-API-Key"); got != "test-key" {
			t.Errorf("missing/wrong api key header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestListProjects_OK(t *testing.T) {
	body := `{"projects":[{"id":1,"identifier":"foo","name":"Foo"}],"total_count":1,"offset":0,"limit":25}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.ListProjects(context.Background(), ListProjectsParams{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Projects) != 1 || res.Projects[0].Name != "Foo" {
		t.Errorf("unexpected: %+v", res)
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	srv := newTestServer(t, 404, `{}`)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	_, err := c.GetIssue(context.Background(), 9999, nil)
	var apiErr *Error
	if err == nil || !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if apiErr.Status != 404 {
		t.Errorf("status=%d", apiErr.Status)
	}
}

func TestListIssues_QueryParams(t *testing.T) {
	var seenQuery string
	var seenKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		seenKey = r.Header.Get("X-Redmine-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	_, err := c.ListIssues(context.Background(), ListIssuesParams{
		ProjectID: "myproj",
		StatusID:  "open",
		Limit:     50,
		Offset:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenKey != "test-key" {
		t.Errorf("api key header not propagated: %q", seenKey)
	}
	for _, want := range []string{"project_id=myproj", "status_id=open", "limit=50", "offset=10"} {
		if !strings.Contains(seenQuery, want) {
			t.Errorf("query missing %q: %s", want, seenQuery)
		}
	}
}

func TestDownloadAttachment_FollowsContentURL(t *testing.T) {
	// Second request: file content (different host/path)
	var fileKey string
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileKey = r.Header.Get("X-Redmine-API-Key")
		_, _ = w.Write([]byte("file-bytes"))
	}))
	defer fileSrv.Close()

	// First request: attachment metadata
	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"attachment":{"id":42,"filename":"hi.txt","content_url":"` + fileSrv.URL + `/file"}}`
		_, _ = w.Write([]byte(body))
	}))
	defer metaSrv.Close()

	c := New(metaSrv.URL, "test-key", metaSrv.Client())
	att, body, err := c.GetAttachment(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if att.Filename != "hi.txt" {
		t.Errorf("filename=%q", att.Filename)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "file-bytes" {
		t.Errorf("body=%q", string(got))
	}
	if fileKey != "test-key" {
		t.Errorf("api key header missing on file fetch: %q", fileKey)
	}
}

func TestCreateIssue_WrapsAndUnwraps(t *testing.T) {
	var seenMethod, seenPath, seenContentType, seenAPIKey string
	var seenBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		seenContentType = r.Header.Get("Content-Type")
		seenAPIKey = r.Header.Get("X-Redmine-API-Key")
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"issue":{"id":42,"subject":"hello","project":{"id":1,"name":"P"},"tracker":{"id":2,"name":"Bug"},"status":{"id":1,"name":"New"},"priority":{"id":1,"name":"Normal"},"author":{"id":1,"name":"A"}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	got, err := c.CreateIssue(context.Background(), IssueCreate{
		ProjectID: "myproj",
		TrackerID: 2,
		Subject:   "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenMethod != "POST" {
		t.Errorf("method=%s", seenMethod)
	}
	if seenPath != "/issues.json" {
		t.Errorf("path=%s", seenPath)
	}
	if seenContentType != "application/json" {
		t.Errorf("content-type=%s", seenContentType)
	}
	if seenAPIKey != "test-key" {
		t.Errorf("api key=%s", seenAPIKey)
	}
	wrapped, ok := seenBody["issue"].(map[string]any)
	if !ok {
		t.Fatalf("body missing issue wrapper: %v", seenBody)
	}
	if wrapped["project_id"] != "myproj" || wrapped["subject"] != "hello" {
		t.Errorf("wrapped body wrong: %v", wrapped)
	}
	if got.ID != 42 || got.Subject != "hello" {
		t.Errorf("decoded issue wrong: %+v", got)
	}
}

func TestUpdateIssue_NoContent(t *testing.T) {
	var seenMethod, seenPath string
	var seenBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	subj := "new subject"
	if err := c.UpdateIssue(context.Background(), 7, IssueUpdate{Subject: &subj}); err != nil {
		t.Fatal(err)
	}
	if seenMethod != "PUT" {
		t.Errorf("method=%s", seenMethod)
	}
	if seenPath != "/issues/7.json" {
		t.Errorf("path=%s", seenPath)
	}
	wrapped, ok := seenBody["issue"].(map[string]any)
	if !ok {
		t.Fatalf("body missing issue wrapper: %v", seenBody)
	}
	if wrapped["subject"] != "new subject" {
		t.Errorf("subject not sent: %v", wrapped)
	}
}

func TestUpdateIssue_PointerSemantics_OmitsUnsetFields(t *testing.T) {
	var seenBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	notes := ""
	// Only Notes set; explicitly empty string. All other fields should
	// be omitted from the JSON entirely.
	if err := c.UpdateIssue(context.Background(), 7, IssueUpdate{Notes: &notes}); err != nil {
		t.Fatal(err)
	}
	wrapped, _ := seenBody["issue"].(map[string]any)
	if _, ok := wrapped["notes"]; !ok {
		t.Errorf("notes should be present (even if empty): %v", wrapped)
	}
	for _, k := range []string{"subject", "description", "status_id", "priority_id"} {
		if _, ok := wrapped[k]; ok {
			t.Errorf("unset field %q should be omitted: %v", k, wrapped)
		}
	}
}

func TestGetCurrentUser_OK(t *testing.T) {
	body := `{"user":{"id":7,"login":"jens","firstname":"Jens","lastname":"K","mail":"jk@example.com","admin":true}}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	u, err := c.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != 7 || u.Login != "jens" || !u.Admin {
		t.Errorf("unexpected: %+v", u)
	}
}

func TestListUsers_OK(t *testing.T) {
	body := `{"users":[{"id":1,"firstname":"A","lastname":"B","mail":"a@b.c"}],"total_count":1,"offset":0,"limit":25}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.ListUsers(context.Background(), ListUsersParams{Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Users) != 1 || res.Users[0].Mail != "a@b.c" {
		t.Errorf("unexpected: %+v", res)
	}
}

func TestListTrackers_OK(t *testing.T) {
	body := `{"trackers":[{"id":1,"name":"Bug","default_status":{"id":1,"name":"New"},"description":"a bug"}]}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.ListTrackers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Trackers) != 1 || res.Trackers[0].Name != "Bug" {
		t.Errorf("unexpected: %+v", res)
	}
	if res.Trackers[0].DefaultStatus == nil || res.Trackers[0].DefaultStatus.Name != "New" {
		t.Errorf("default_status not decoded: %+v", res.Trackers[0])
	}
}

func TestListStatuses_OK(t *testing.T) {
	body := `{"issue_statuses":[{"id":1,"name":"New","is_closed":false},{"id":5,"name":"Closed","is_closed":true}]}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.ListStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.IssueStatuses) != 2 {
		t.Fatalf("got %d statuses", len(res.IssueStatuses))
	}
	if !res.IssueStatuses[1].IsClosed {
		t.Errorf("expected second status closed: %+v", res.IssueStatuses[1])
	}
}

func TestListPriorities_OK(t *testing.T) {
	body := `{"issue_priorities":[{"id":3,"name":"Normal","is_default":true,"active":true}]}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.ListPriorities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.IssuePriorities) != 1 || !res.IssuePriorities[0].IsDefault {
		t.Errorf("unexpected: %+v", res)
	}
}

func TestListActivities_OK(t *testing.T) {
	body := `{"time_entry_activities":[{"id":8,"name":"Development","is_default":true,"active":true}]}`
	srv := newTestServer(t, 200, body)
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.ListActivities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TimeEntryActivities) != 1 || res.TimeEntryActivities[0].Name != "Development" {
		t.Errorf("unexpected: %+v", res)
	}
}

func TestSearch_QueryAndFilters(t *testing.T) {
	var seenQuery, seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"id":7,"title":"#1459 (open): Build","type":"issue","url":"https://example.com/issues/1459","datetime":"2026-05-04T08:20:57Z"}],"total_count":1,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	res, err := c.Search(context.Background(), SearchParams{
		Q:          "foo bar",
		Issues:     true,
		Wiki:       true,
		Projects:   true,
		TitlesOnly: true,
		Scope:      "my_projects",
		ProjectID:  "myproj",
		Limit:      50,
		Offset:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenPath != "/search.json" {
		t.Errorf("path=%s", seenPath)
	}
	// Accept either "+" (form encoding) or "%20" for the space.
	if !strings.Contains(seenQuery, "q=foo+bar") && !strings.Contains(seenQuery, "q=foo%20bar") {
		t.Errorf("missing q in query: %s", seenQuery)
	}
	for _, want := range []string{
		"issues=1", "wiki_pages=1", "projects=1", "titles_only=1",
		"scope=my_projects", "project_id=myproj",
		"limit=50", "offset=10",
	} {
		if !strings.Contains(seenQuery, want) {
			t.Errorf("query missing %q: %s", want, seenQuery)
		}
	}
	if len(res.Results) != 1 || res.Results[0].Type != "issue" || res.Results[0].ID != 7 {
		t.Errorf("decoded results wrong: %+v", res)
	}
}

func TestSearch_OmitsUnsetFilters(t *testing.T) {
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	if _, err := c.Search(context.Background(), SearchParams{Q: "hello"}); err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"issues=", "wiki_pages=", "projects=", "titles_only=", "scope=", "project_id=", "limit=", "offset="} {
		if strings.Contains(seenQuery, unwanted) {
			t.Errorf("query should not contain %q: %s", unwanted, seenQuery)
		}
	}
}

func TestLogTime_WrapsAndUnwraps(t *testing.T) {
	var seenMethod, seenPath string
	var seenBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&seenBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"time_entry":{"id":99,"hours":1.5,"activity":{"id":8,"name":"Dev"},"project":{"id":1,"name":"P"},"user":{"id":1,"name":"U"},"spent_on":"2026-05-04","created_on":"2026-05-04T00:00:00Z","issue":{"id":42}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	got, err := c.LogTime(context.Background(), TimeEntryCreate{
		IssueID:    42,
		Hours:      1.5,
		ActivityID: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if seenMethod != "POST" || seenPath != "/time_entries.json" {
		t.Errorf("wrong request: %s %s", seenMethod, seenPath)
	}
	wrapped, ok := seenBody["time_entry"].(map[string]any)
	if !ok {
		t.Fatalf("missing time_entry wrapper: %v", seenBody)
	}
	if wrapped["hours"] != 1.5 || wrapped["activity_id"] != float64(8) {
		t.Errorf("body wrong: %v", wrapped)
	}
	if got.ID != 99 || got.Hours != 1.5 || got.Issue == nil || got.Issue.ID != 42 {
		t.Errorf("decoded time entry wrong: %+v", got)
	}
}
