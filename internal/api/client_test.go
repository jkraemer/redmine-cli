package api

import (
	"context"
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
