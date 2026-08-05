package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// M3: a 429 with Retry-After should sleep that long and retry once for GETs.
// We deliberately do NOT retry mutating calls (POST/PUT) to avoid duplicates.
func TestDo_Retries429WithRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	start := time.Now()
	_, err := c.ListProjects(context.Background(), ListProjectsParams{})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 calls (one retry), got %d", got)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected at least ~1s wait, got %v", elapsed)
	}
}

// 429 without Retry-After should not be retried — we have no idea how long
// to wait, so surfacing the error is more correct than guessing.
func TestDo_DoesNotRetry429WithoutRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(429)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	_, err := c.ListProjects(context.Background(), ListProjectsParams{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected single call, got %d", got)
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 429 {
		t.Errorf("expected 429 *Error, got %v", err)
	}
}

// A Retry-After larger than our cap should not be respected — the CLI is
// interactive enough that an hours-long sleep is worse than failing fast.
func TestDo_DoesNotRetry429WithExcessiveRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	_, err := c.ListProjects(context.Background(), ListProjectsParams{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected single call, got %d", got)
	}
}

// A malicious or compromised Redmine server that returns a 3xx redirect to a
// different host must not be followed: Go's stdlib only strips a fixed list of
// auth headers (Authorization, Cookie, ...) on cross-host redirects and has no
// knowledge of X-Redmine-API-Key, so a redirect to attacker.example would
// exfiltrate the API key. DefaultHTTPClient's CheckRedirect must refuse the
// hop.
func TestDefaultHTTPClient_RefusesCrossHostRedirect_APIKey(t *testing.T) {
	var attackerCalls int32
	var attackerKey string
	attackerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attackerCalls, 1)
		attackerKey = r.Header.Get("X-Redmine-API-Key")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"projects":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer attackerSrv.Close()

	redmineSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attackerSrv.URL+"/projects.json", http.StatusFound)
	}))
	defer redmineSrv.Close()

	c := New(redmineSrv.URL, "test-key", DefaultHTTPClient())
	_, err := c.ListProjects(context.Background(), ListProjectsParams{})
	if err == nil {
		t.Fatalf("expected cross-host redirect to be refused")
	}
	if got := atomic.LoadInt32(&attackerCalls); got != 0 {
		t.Errorf("attacker host received %d request(s); expected 0", got)
	}
	if attackerKey != "" {
		t.Errorf("API key leaked off-host: %q", attackerKey)
	}
}

// Same guarantee on the OAuth Bearer code path. Go's stdlib strips
// "Authorization" on cross-host redirects, but we still refuse the hop so an
// attacker can't observe that a redirect to their host even occurred (and to
// keep behavior consistent between the two auth modes).
func TestDefaultHTTPClient_RefusesCrossHostRedirect_Bearer(t *testing.T) {
	var attackerCalls int32
	var attackerAuth string
	attackerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attackerCalls, 1)
		attackerAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"projects":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer attackerSrv.Close()

	redmineSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attackerSrv.URL+"/projects.json", http.StatusFound)
	}))
	defer redmineSrv.Close()

	c := NewWithToken(redmineSrv.URL, "bearer-xyz", DefaultHTTPClient())
	_, err := c.ListProjects(context.Background(), ListProjectsParams{})
	if err == nil {
		t.Fatalf("expected cross-host redirect to be refused")
	}
	if got := atomic.LoadInt32(&attackerCalls); got != 0 {
		t.Errorf("attacker host received %d request(s); expected 0", got)
	}
	if attackerAuth != "" {
		t.Errorf("Authorization leaked off-host: %q", attackerAuth)
	}
}

// Same-host redirects (e.g. /old -> /new on the same Redmine) must still be
// followed; we don't want to break legitimate server-side URL canonicalization.
func TestDefaultHTTPClient_FollowsSameHostRedirect(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			http.Redirect(w, r, "/projects.json?v=2", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"projects":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", DefaultHTTPClient())
	if _, err := c.ListProjects(context.Background(), ListProjectsParams{}); err != nil {
		t.Fatalf("same-host redirect should be followed: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 hits (initial + follow), got %d", got)
	}
}

// Attachment downloads route through the same client; cross-host redirects on
// the content_url fetch (separate from the assertSameOrigin URL-string check)
// must also be refused so the key can't leak via that path.
func TestDefaultHTTPClient_RefusesCrossHostRedirect_OnAttachment(t *testing.T) {
	var attackerCalls int32
	var attackerKey string
	attackerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attackerCalls, 1)
		attackerKey = r.Header.Get("X-Redmine-API-Key")
		_, _ = w.Write([]byte("evil-bytes"))
	}))
	defer attackerSrv.Close()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			w.Header().Set("Content-Type", "application/json")
			body := `{"attachment":{"id":42,"filename":"hi.txt","content_url":"` + srv.URL + `/file"}}`
			_, _ = w.Write([]byte(body))
			return
		}
		// Same-origin content_url then redirects off-host. assertSameOrigin
		// passes (URL string is same-origin); the redirect is what must catch
		// this.
		http.Redirect(w, r, attackerSrv.URL+"/file", http.StatusFound)
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", DefaultHTTPClient())
	if _, _, err := c.GetAttachment(context.Background(), 42); err == nil {
		t.Fatalf("expected cross-host redirect on attachment to be refused")
	}
	if got := atomic.LoadInt32(&attackerCalls); got != 0 {
		t.Errorf("attacker host received %d request(s); expected 0", got)
	}
	if attackerKey != "" {
		t.Errorf("API key leaked off-host via attachment redirect: %q", attackerKey)
	}
}

// M1: production HTTP client must set per-phase timeouts so a server that
// hangs at TCP, TLS, or response-header time can't lock up the CLI. We
// deliberately leave the body read uncapped so streaming large attachments
// still works.
func TestDefaultHTTPClient_HasPhaseTimeouts(t *testing.T) {
	c := DefaultHTTPClient()
	if c == nil || c.Transport == nil {
		t.Fatal("DefaultHTTPClient returned nil or no Transport")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport=%T, want *http.Transport", c.Transport)
	}
	if tr.TLSHandshakeTimeout == 0 {
		t.Error("TLSHandshakeTimeout is zero")
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Error("ResponseHeaderTimeout is zero")
	}
	if tr.ExpectContinueTimeout == 0 {
		t.Error("ExpectContinueTimeout is zero")
	}
	if c.Timeout != 0 {
		t.Errorf("Client.Timeout=%v; want 0 so streaming attachments are not capped", c.Timeout)
	}
}

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

// Real Redmine serves attachment metadata and content from the same origin,
// so the client should send the API key when content_url is on the same
// scheme+host as baseURL.
func TestDownloadAttachment_FollowsSameOriginContentURL(t *testing.T) {
	var fileKey string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			w.Header().Set("Content-Type", "application/json")
			body := `{"attachment":{"id":42,"filename":"hi.txt","content_url":"` + srv.URL + `/file"}}`
			_, _ = w.Write([]byte(body))
			return
		}
		fileKey = r.Header.Get("X-Redmine-API-Key")
		_, _ = w.Write([]byte("file-bytes"))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
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

// H2: when the metadata response points content_url at a different origin,
// we must refuse the request rather than ship the API key off-host.
func TestDownloadAttachment_RefusesCrossOriginContentURL(t *testing.T) {
	var fileKey string
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileKey = r.Header.Get("X-Redmine-API-Key")
		_, _ = w.Write([]byte("should-not-be-fetched"))
	}))
	defer fileSrv.Close()

	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"attachment":{"id":42,"filename":"hi.txt","content_url":"` + fileSrv.URL + `/file"}}`
		_, _ = w.Write([]byte(body))
	}))
	defer metaSrv.Close()

	c := New(metaSrv.URL, "test-key", metaSrv.Client())
	_, _, err := c.GetAttachment(context.Background(), 42)
	if err == nil {
		t.Fatalf("expected cross-origin content_url to be rejected, got nil")
	}
	if fileKey != "" {
		t.Errorf("API key was leaked to off-origin host: %q", fileKey)
	}
}

// H2: a malformed content_url should fail closed, not silently succeed.
func TestDownloadAttachment_RefusesMalformedContentURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"attachment":{"id":42,"filename":"hi.txt","content_url":"::not a url::"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	if _, _, err := c.GetAttachment(context.Background(), 42); err == nil {
		t.Fatalf("expected error on malformed content_url, got nil")
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

func TestUploadFile_OK(t *testing.T) {
	var seenMethod, seenPath, seenContentType, seenAccept, seenAPIKey string
	var seenQuery string
	var seenBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		seenQuery = r.URL.RawQuery
		seenContentType = r.Header.Get("Content-Type")
		seenAccept = r.Header.Get("Accept")
		seenAPIKey = r.Header.Get("X-Redmine-API-Key")
		seenBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"upload":{"id":42,"token":"tok-abc"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	payload := []byte("hello attachment world")
	up, err := c.UploadFile(context.Background(), bytes.NewReader(payload), "hi.txt", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	if seenMethod != "POST" {
		t.Errorf("method=%s", seenMethod)
	}
	if seenPath != "/uploads.json" {
		t.Errorf("path=%s", seenPath)
	}
	if seenContentType != "application/octet-stream" {
		t.Errorf("content-type=%s", seenContentType)
	}
	if seenAccept != "application/json" {
		t.Errorf("accept=%s", seenAccept)
	}
	if seenAPIKey != "test-key" {
		t.Errorf("api key=%s", seenAPIKey)
	}
	if !strings.Contains(seenQuery, "filename=hi.txt") {
		t.Errorf("query missing filename: %s", seenQuery)
	}
	// content_type contains a "/" which url.QueryEscape encodes as %2F.
	if !strings.Contains(seenQuery, "content_type=text%2Fplain") {
		t.Errorf("query missing content_type: %s", seenQuery)
	}
	if !bytes.Equal(seenBody, payload) {
		t.Errorf("body mismatch: got %q want %q", string(seenBody), string(payload))
	}
	if up == nil || up.ID != 42 || up.Token != "tok-abc" {
		t.Errorf("upload wrong: %+v", up)
	}
}

// Streaming sanity check: a large body should arrive at the server intact —
// same length, same content. We're not measuring memory, just confirming we
// don't truncate or duplicate bytes along the way.
func TestUploadFile_StreamsLargeBody(t *testing.T) {
	const size = 1 << 20 // 1 MiB
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	var seenLen int
	var seenSum []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seenLen = len(body)
		seenSum = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"upload":{"id":1,"token":"t"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	if _, err := c.UploadFile(context.Background(), bytes.NewReader(payload), "big.bin", "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if seenLen != size {
		t.Fatalf("server got %d bytes, want %d", seenLen, size)
	}
	if !bytes.Equal(seenSum, payload) {
		t.Errorf("server-received bytes differ from sent payload")
	}
}

// When filename and content_type are empty, the corresponding query params
// must be absent so Redmine applies its own defaults (random filename,
// guessed content type).
func TestUploadFile_OmitsEmptyQueryParams(t *testing.T) {
	var seenQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"upload":{"id":1,"token":"t"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	if _, err := c.UploadFile(context.Background(), bytes.NewReader([]byte("x")), "", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seenQuery, "filename") {
		t.Errorf("query should not contain filename when empty: %s", seenQuery)
	}
	if strings.Contains(seenQuery, "content_type") {
		t.Errorf("query should not contain content_type when empty: %s", seenQuery)
	}
}

func TestUploadFile_ErrorPreservesBodyExcerpt(t *testing.T) {
	const errBody = `{"errors":["File is invalid"]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(422)
		_, _ = w.Write([]byte(errBody))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-key", srv.Client())
	_, err := c.UploadFile(context.Background(), bytes.NewReader([]byte("x")), "f.txt", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if apiErr.Status != 422 {
		t.Errorf("status=%d", apiErr.Status)
	}
	if !strings.Contains(apiErr.Body, "File is invalid") {
		t.Errorf("body excerpt missing: %q", apiErr.Body)
	}
}

func TestUploadFile_AuthHeaderToken(t *testing.T) {
	var seenAuth, seenAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenAPIKey = r.Header.Get("X-Redmine-API-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"upload":{"id":1,"token":"t"}}`))
	}))
	defer srv.Close()

	c := NewWithToken(srv.URL, "bearer-xyz", srv.Client())
	if _, err := c.UploadFile(context.Background(), bytes.NewReader([]byte("x")), "", ""); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Bearer bearer-xyz" {
		t.Errorf("Authorization=%q", seenAuth)
	}
	if seenAPIKey != "" {
		t.Errorf("X-Redmine-API-Key should not be sent on token client: %q", seenAPIKey)
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

func TestListQueries_SendsPaginationAndDecodes(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"queries":[{"id":7,"name":"My open","is_public":false,"project_id":null},{"id":9,"name":"Bugs","is_public":true,"project_id":3}],"total_count":2,"offset":0,"limit":25}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "k", srv.Client())
	res, err := c.ListQueries(context.Background(), ListQueriesParams{Limit: 25, Offset: 0})
	if err != nil {
		t.Fatalf("ListQueries: %v", err)
	}
	if !strings.Contains(gotURL, "/queries.json") {
		t.Errorf("URL did not include /queries.json: %s", gotURL)
	}
	if !strings.Contains(gotURL, "limit=25") {
		t.Errorf("URL missing limit=25: %s", gotURL)
	}
	if len(res.Queries) != 2 {
		t.Fatalf("expected 2 queries, got %d", len(res.Queries))
	}
	if res.Queries[0].ProjectID != nil {
		t.Errorf("expected nil ProjectID for global query, got %v", *res.Queries[0].ProjectID)
	}
	if res.Queries[1].ProjectID == nil || *res.Queries[1].ProjectID != 3 {
		t.Errorf("expected ProjectID=3 for second query")
	}
	if res.TotalCount != 2 {
		t.Errorf("TotalCount=%d, want 2", res.TotalCount)
	}
}

func TestListIssues_SendsQueryIDWhenSet(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	if _, err := c.ListIssues(context.Background(), ListIssuesParams{QueryID: 42}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "query_id=42") {
		t.Errorf("URL missing query_id=42: %s", gotURL)
	}
}

func TestListIssues_OmitsQueryIDWhenZero(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = w.Write([]byte(`{"issues":[],"total_count":0,"offset":0,"limit":25}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	if _, err := c.ListIssues(context.Background(), ListIssuesParams{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(gotURL, "query_id") {
		t.Errorf("URL should not contain query_id when unset: %s", gotURL)
	}
}

func TestListCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/my proj/issue_categories.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"issue_categories":[{"id":3,"name":"UI","project":{"id":1,"name":"P"},"assigned_to":{"id":2,"name":"Jens"}},{"id":4,"name":"Backend","project":{"id":1,"name":"P"}}]}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	res, err := c.ListCategories(context.Background(), "my proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.IssueCategories) != 2 {
		t.Fatalf("got %d categories", len(res.IssueCategories))
	}
	if res.IssueCategories[0].AssignedTo == nil || res.IssueCategories[0].AssignedTo.Name != "Jens" {
		t.Errorf("assigned_to not parsed: %+v", res.IssueCategories[0])
	}
	if res.IssueCategories[1].AssignedTo != nil {
		t.Errorf("expected nil assigned_to for category without default assignee")
	}
}

func TestListCustomFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom_fields.json" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"custom_fields":[{"id":5,"name":"Severity","customized_type":"issue","field_format":"list","is_required":true,"multiple":false,"possible_values":[{"value":"low","label":"Low"},{"value":"high","label":"High"}],"trackers":[{"id":1,"name":"Bug"}]}]}`)
	}))
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	res, err := c.ListCustomFields(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.CustomFields) != 1 {
		t.Fatalf("got %d custom fields", len(res.CustomFields))
	}
	cf := res.CustomFields[0]
	if cf.ID != 5 || cf.FieldFormat != "list" || !cf.IsRequired {
		t.Errorf("parsed wrong: %+v", cf)
	}
	if len(cf.PossibleValues) != 2 || cf.PossibleValues[1].Label != "High" {
		t.Errorf("possible_values wrong: %+v", cf.PossibleValues)
	}
}

func TestAddWatcher(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/issues/42/watchers.json" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	if err := c.AddWatcher(context.Background(), 42, 7); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatal(err)
	}
	if body["user_id"] != float64(7) {
		t.Errorf("body=%s", gotBody)
	}
}

func TestRemoveWatcher(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "DELETE" || r.URL.Path != "/issues/42/watchers/7.json" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := New(srv.URL, "k", srv.Client())
	if err := c.RemoveWatcher(context.Background(), 42, 7); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("server not called")
	}
}
