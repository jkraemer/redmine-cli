package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
)

// Wiki page Text/Title/Author are server-controlled and reach the terminal
// in markdown mode. Any ANSI escape sequences embedded by a malicious wiki
// editor must be stripped before printing.
func TestWikiGet_Markdown_StripsTerminalEscapes(t *testing.T) {
	body := "{\"wiki_page\":{" +
		"\"title\":\"My\\u001b]52;c;X\\u0007Page\"," +
		"\"text\":\"hello\\u001b[2Jworld\"," +
		"\"version\":1," +
		"\"author\":{\"id\":1,\"name\":\"A\\u0007uthor\"}," +
		"\"updated_on\":\"2026-01-01\"}}"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "markdown"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"wiki", "get", "MyPage", "--project", "P"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, bad := range []string{"\x1b", "\x07"} {
		if strings.Contains(got, bad) {
			t.Errorf("output contains raw control byte %q:\n%s", bad, got)
		}
	}
	// Only the control bytes are stripped; the printable payload between
	// them survives, which is harmless without the leading ESC/BEL.
	for _, want := range []string{"My]52;c;XPage", "hello[2Jworld", "Author"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestWikiPut_Attach_Confirm_Single verifies that wiki put with --attach
// uploads the file first, then issues a PUT whose wiki_page.uploads
// references the returned token. The text field must be carried through.
func TestWikiPut_Attach_Confirm_Single(t *testing.T) {
	var (
		uploadCalls, putCalls int32
		putBody               []byte
		putPath               string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/uploads.json":
			n := atomic.AddInt32(&uploadCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(uploadResponseBody(n)))
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/projects/P/wiki/"):
			atomic.AddInt32(&putCalls, 1)
			putPath = r.URL.Path
			putBody, _ = io.ReadAll(r.Body)
			// 204 No Content; client will then issue a follow-up GET.
			w.WriteHeader(204)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/projects/P/wiki/"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"wiki_page":{"title":"MyPage","text":"hello","version":1,"author":{"id":1,"name":"A"},"created_on":"2026-01-01","updated_on":"2026-01-01"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	path := mustWriteTempFile(t, dir, "hello.txt", "hi")

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"wiki", "put", "MyPage",
		"--project", "P", "--text", "hello",
		"--attach", path, "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 1 {
		t.Errorf("upload calls=%d, want 1", got)
	}
	if got := atomic.LoadInt32(&putCalls); got != 1 {
		t.Errorf("put calls=%d, want 1", got)
	}
	if putPath != "/projects/P/wiki/MyPage.json" {
		t.Errorf("put path=%q", putPath)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(putBody, &bodyJSON); err != nil {
		t.Fatalf("put body not JSON: %v", err)
	}
	page, _ := bodyJSON["wiki_page"].(map[string]any)
	if page["text"] != "hello" {
		t.Errorf("text=%v", page["text"])
	}
	uploads, ok := page["uploads"].([]any)
	if !ok || len(uploads) != 1 {
		t.Fatalf("wiki_page.uploads wrong: %v", page["uploads"])
	}
	u0, _ := uploads[0].(map[string]any)
	if u0["token"] != "tok-1" {
		t.Errorf("token=%v", u0["token"])
	}
	if u0["filename"] != "hello.txt" {
		t.Errorf("filename=%v", u0["filename"])
	}
}

// TestWikiPut_Attach_DryRun verifies dry-run mode: no HTTP calls,
// would_upload surfaced, placeholder token in body's wiki_page.uploads.
func TestWikiPut_Attach_DryRun(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode (path=%s)", r.URL.Path)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"wiki", "put", "MyPage",
		"--project", "P", "--text", "hello",
		"--attach", "foo.txt"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["method"] != "PUT" || got["path"] != "/projects/P/wiki/MyPage.json" {
		t.Errorf("wrong request preview: %v", got)
	}
	wu, ok := got["would_upload"].([]any)
	if !ok {
		t.Fatalf("would_upload not an array: %T %v", got["would_upload"], got["would_upload"])
	}
	if len(wu) != 1 {
		t.Fatalf("len(would_upload)=%d", len(wu))
	}
	body, _ := got["body"].(map[string]any)
	page, _ := body["wiki_page"].(map[string]any)
	if page["text"] != "hello" {
		t.Errorf("text=%v", page["text"])
	}
	uploads, ok := page["uploads"].([]any)
	if !ok || len(uploads) != 1 {
		t.Fatalf("wiki_page.uploads wrong: %v", page["uploads"])
	}
	u0, _ := uploads[0].(map[string]any)
	if u0["token"] != "<UPLOAD-TOKEN-FOR-foo.txt>" {
		t.Errorf("token=%v", u0["token"])
	}
}

// TestWikiPut_Attach_PreflightFailure_NoServerCall verifies a missing
// local file aborts before any HTTP call.
func TestWikiPut_Attach_PreflightFailure_NoServerCall(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		t.Errorf("server should not be called when pre-flight fails (path=%s)", r.URL.Path)
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	missing := "/nonexistent/does-not-exist.txt"

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"wiki", "put", "MyPage",
		"--project", "P", "--text", "hello",
		"--attach", missing, "--confirm"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("err=%q, want mention of %q", err.Error(), missing)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("calls=%d, want 0", got)
	}
}

// TestWikiPut_Attach_MidBatchUploadFailure verifies that a 500 on the
// second upload short-circuits before the PUT.
func TestWikiPut_Attach_MidBatchUploadFailure(t *testing.T) {
	var (
		uploadCalls, putCalls int32
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/uploads.json":
			n := atomic.AddInt32(&uploadCalls, 1)
			if n == 2 {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = w.Write([]byte(uploadResponseBody(n)))
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/projects/P/wiki/"):
			atomic.AddInt32(&putCalls, 1)
			t.Errorf("PUT should not be called when an upload fails")
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	pa := mustWriteTempFile(t, dir, "a.txt", "a")
	pb := mustWriteTempFile(t, dir, "b.txt", "b")

	var out, errOut bytes.Buffer
	rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"wiki", "put", "MyPage",
		"--project", "P", "--text", "hello",
		"--attach", pa, "--attach", pb, "--confirm"})
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "attach 2/2") {
		t.Errorf("err=%q, want 'attach 2/2'", err.Error())
	}
	if !strings.Contains(err.Error(), pb) {
		t.Errorf("err=%q, want mention of %q", err.Error(), pb)
	}
	if got := atomic.LoadInt32(&uploadCalls); got != 2 {
		t.Errorf("upload calls=%d, want 2", got)
	}
	if got := atomic.LoadInt32(&putCalls); got != 0 {
		t.Errorf("put calls=%d, want 0", got)
	}
}
