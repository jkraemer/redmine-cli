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
