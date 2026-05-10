package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
)

func TestParseAttachSpecs_BarePath(t *testing.T) {
	specs, err := parseAttachSpecs([]string{"/tmp/foo.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("len=%d", len(specs))
	}
	want := attachSpec{Path: "/tmp/foo.pdf"}
	if specs[0] != want {
		t.Errorf("got=%+v want=%+v", specs[0], want)
	}
}

func TestParseAttachSpecs_JSON(t *testing.T) {
	in := `{"path":"/tmp/foo.pdf","filename":"report.pdf","description":"Q3","content_type":"application/pdf"}`
	specs, err := parseAttachSpecs([]string{in})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("len=%d", len(specs))
	}
	want := attachSpec{
		Path:        "/tmp/foo.pdf",
		Filename:    "report.pdf",
		Description: "Q3",
		ContentType: "application/pdf",
	}
	if specs[0] != want {
		t.Errorf("got=%+v want=%+v", specs[0], want)
	}
}

func TestParseAttachSpecs_Mixed(t *testing.T) {
	in := []string{
		"/tmp/bare.txt",
		`{"path":"/tmp/named.bin","filename":"keep.bin"}`,
	}
	specs, err := parseAttachSpecs(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("len=%d", len(specs))
	}
	if specs[0] != (attachSpec{Path: "/tmp/bare.txt"}) {
		t.Errorf("specs[0]=%+v", specs[0])
	}
	if specs[1] != (attachSpec{Path: "/tmp/named.bin", Filename: "keep.bin"}) {
		t.Errorf("specs[1]=%+v", specs[1])
	}
}

func TestParseAttachSpecs_JSONLeadingWhitespace(t *testing.T) {
	specs, err := parseAttachSpecs([]string{"  \t{\"path\":\"/tmp/x\"}"})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].Path != "/tmp/x" {
		t.Errorf("specs=%+v", specs)
	}
}

func TestParseAttachSpecs_BarePathLeadingWhitespace(t *testing.T) {
	specs, err := parseAttachSpecs([]string{"  foo.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("len=%d", len(specs))
	}
	if specs[0].Path != "foo.pdf" {
		t.Errorf("path=%q, want %q", specs[0].Path, "foo.pdf")
	}
}

func TestParseAttachSpecs_JSONUnknownField(t *testing.T) {
	_, err := parseAttachSpecs([]string{`{"path":"x","tpyo":"foo"}`})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--attach 1: invalid JSON:") {
		t.Errorf("err=%q, want '--attach 1: invalid JSON:' prefix", err)
	}
	if !strings.Contains(err.Error(), "tpyo") {
		t.Errorf("err=%q, want mention of unknown field 'tpyo'", err)
	}
}

func TestParseAttachSpecs_EmptyInput(t *testing.T) {
	for _, in := range [][]string{nil, {}} {
		specs, err := parseAttachSpecs(in)
		if err != nil {
			t.Errorf("err=%v", err)
		}
		if specs != nil {
			t.Errorf("expected nil, got %+v", specs)
		}
	}
}

func TestParseAttachSpecs_MalformedJSON(t *testing.T) {
	_, err := parseAttachSpecs([]string{`{"path": broken}`})
	if err == nil {
		t.Fatal("expected error")
	}
	// 1-based index
	if !strings.Contains(err.Error(), "--attach 1") {
		t.Errorf("err=%q, want mention of --attach 1", err)
	}
}

func TestParseAttachSpecs_JSONEmptyPath(t *testing.T) {
	_, err := parseAttachSpecs([]string{`{"path":"","filename":"x"}`})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--attach 1") {
		t.Errorf("err=%q, want mention of --attach 1", err)
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Errorf("err=%q, want 'path is required'", err)
	}
}

func TestParseAttachSpecs_BareEmptyString(t *testing.T) {
	_, err := parseAttachSpecs([]string{""})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "path is required") {
		t.Errorf("err=%q", err)
	}
}

func TestPreflightAttachSpecs_Happy(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightAttachSpecs([]attachSpec{{Path: a}, {Path: b}}); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestPreflightAttachSpecs_MissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.txt")
	err := preflightAttachSpecs([]attachSpec{{Path: missing}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("err=%q, want mention of %q", err, missing)
	}
}

func TestPreflightAttachSpecs_DirectoryRejected(t *testing.T) {
	dir := t.TempDir()
	err := preflightAttachSpecs([]attachSpec{{Path: dir}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("err=%q, want mention of 'not a regular file'", err)
	}
}

func TestPreflightAttachSpecs_AggregatesFailures(t *testing.T) {
	dir := t.TempDir()
	missingA := filepath.Join(dir, "a.txt")
	missingB := filepath.Join(dir, "b.txt")
	err := preflightAttachSpecs([]attachSpec{{Path: missingA}, {Path: missingB}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), missingA) {
		t.Errorf("err missing %q: %v", missingA, err)
	}
	if !strings.Contains(err.Error(), missingB) {
		t.Errorf("err missing %q: %v", missingB, err)
	}
}

// uploadEchoHandler returns an http.HandlerFunc that records each call by
// incrementing *calls and replies with {"upload":{"id":n,"token":"tok-n"}}.
// Use it as the upload-side branch of a path-dispatching test handler.
func uploadEchoHandler(t *testing.T, calls *int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(uploadResponseBody(n)))
	}
}

// uploadResponseBody renders the canonical /uploads.json success body for
// call number n. Kept as its own helper so handlers that need custom
// behaviour (e.g. mid-batch failure) can still reuse the success shape.
func uploadResponseBody(n int32) string {
	return fmt.Sprintf(`{"upload":{"id":%d,"token":"tok-%d"}}`, n, n)
}

// mustWriteTempFile writes content to dir/name with mode 0644 and returns
// the absolute path; it fails the test on error.
func mustWriteTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// uploadServer counts calls and returns sequential tokens "tok-1", "tok-2",
// ... unless failOn is non-zero, in which case the matching call returns 500.
type uploadServer struct {
	t       *testing.T
	calls   int32
	failOn  int32 // 1-based call index that should return 500; 0 means no failure
	records []uploadRecord
	mu      chan struct{} // simple guard; we don't expect concurrency
}

type uploadRecord struct {
	Path        string
	Query       string
	ContentType string
	Body        []byte
}

func newUploadServer(t *testing.T, failOn int32) (*uploadServer, *httptest.Server) {
	t.Helper()
	us := &uploadServer{t: t, failOn: failOn, mu: make(chan struct{}, 1)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		us.mu <- struct{}{}
		defer func() { <-us.mu }()
		n := atomic.AddInt32(&us.calls, 1)
		body, _ := io.ReadAll(r.Body)
		us.records = append(us.records, uploadRecord{
			Path:        r.URL.Path,
			Query:       r.URL.RawQuery,
			ContentType: r.Header.Get("Content-Type"),
			Body:        body,
		})
		if us.failOn != 0 && n == us.failOn {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = fmt.Fprintf(w, `{"upload":{"id":%d,"token":"tok-%d"}}`, n, n)
	}))
	return us, srv
}

func TestUploadAttachments_Single(t *testing.T) {
	us, srv := newUploadServer(t, 0)
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	refs, err := uploadAttachments(context.Background(), c, []attachSpec{{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("len=%d", len(refs))
	}
	if refs[0].Token != "tok-1" {
		t.Errorf("token=%q", refs[0].Token)
	}
	if refs[0].Filename != "hello.txt" {
		t.Errorf("filename=%q", refs[0].Filename)
	}
	if atomic.LoadInt32(&us.calls) != 1 {
		t.Errorf("calls=%d", us.calls)
	}
	if !strings.Contains(us.records[0].Query, "filename=hello.txt") {
		t.Errorf("query missing filename: %s", us.records[0].Query)
	}
	if string(us.records[0].Body) != "hi" {
		t.Errorf("body=%q", us.records[0].Body)
	}
}

func TestUploadAttachments_TwoInOrder(t *testing.T) {
	us, srv := newUploadServer(t, 0)
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	pa := filepath.Join(dir, "a.txt")
	pb := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(pa, []byte("aa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, []byte("bbbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs, err := uploadAttachments(context.Background(), c, []attachSpec{{Path: pa}, {Path: pb}})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("len=%d", len(refs))
	}
	if refs[0].Token != "tok-1" || refs[1].Token != "tok-2" {
		t.Errorf("tokens=%q,%q", refs[0].Token, refs[1].Token)
	}
	if refs[0].Filename != "a.txt" || refs[1].Filename != "b.bin" {
		t.Errorf("names=%q,%q", refs[0].Filename, refs[1].Filename)
	}
	if atomic.LoadInt32(&us.calls) != 2 {
		t.Errorf("calls=%d", us.calls)
	}
	if string(us.records[0].Body) != "aa" || string(us.records[1].Body) != "bbbb" {
		t.Errorf("bodies=%q,%q", us.records[0].Body, us.records[1].Body)
	}
}

func TestUploadAttachments_MidBatchFailure(t *testing.T) {
	us, srv := newUploadServer(t, 2)
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	pa := filepath.Join(dir, "a.txt")
	pb := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(pa, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pb, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := uploadAttachments(context.Background(), c, []attachSpec{{Path: pa}, {Path: pb}})
	if err == nil {
		t.Fatal("expected error")
	}
	// Error should mention "attach 2/2 (path)"
	if !strings.Contains(err.Error(), "attach 2/2") {
		t.Errorf("err=%q, want 'attach 2/2'", err)
	}
	if !strings.Contains(err.Error(), pb) {
		t.Errorf("err=%q, want path %q", err, pb)
	}
	// First upload still happened
	if atomic.LoadInt32(&us.calls) != 2 {
		t.Errorf("calls=%d, want 2 (first upload still happens)", us.calls)
	}
}

func TestUploadAttachments_CustomFilenameAndContentType(t *testing.T) {
	us, srv := newUploadServer(t, 0)
	defer srv.Close()
	c := api.New(srv.URL, "k", srv.Client())

	dir := t.TempDir()
	path := filepath.Join(dir, "raw.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	specs := []attachSpec{{
		Path:        path,
		Filename:    "report.pdf",
		ContentType: "application/pdf",
		Description: "Q3",
	}}
	refs, err := uploadAttachments(context.Background(), c, specs)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("len=%d", len(refs))
	}
	if refs[0].Filename != "report.pdf" {
		t.Errorf("filename=%q", refs[0].Filename)
	}
	if refs[0].ContentType != "application/pdf" {
		t.Errorf("ct=%q", refs[0].ContentType)
	}
	if refs[0].Description != "Q3" {
		t.Errorf("desc=%q", refs[0].Description)
	}
	q := us.records[0].Query
	if !strings.Contains(q, "filename=report.pdf") {
		t.Errorf("query missing filename: %s", q)
	}
	if !strings.Contains(q, "content_type=application%2Fpdf") {
		t.Errorf("query missing content_type: %s", q)
	}
}

func TestUploadAttachments_EmptyInput(t *testing.T) {
	for _, in := range [][]attachSpec{nil, {}} {
		refs, err := uploadAttachments(context.Background(), nil, in)
		if err != nil {
			t.Errorf("err=%v", err)
		}
		if refs != nil {
			t.Errorf("refs=%+v, want nil", refs)
		}
	}
}

func TestAttachDryRun_TokenShape(t *testing.T) {
	specs := []attachSpec{
		{Path: "/tmp/foo.pdf"},
		{Path: "/tmp/raw.bin", Filename: "report.pdf", Description: "Q3", ContentType: "application/pdf"},
	}
	got := attachDryRun(specs)
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Token != "<UPLOAD-TOKEN-FOR-foo.pdf>" {
		t.Errorf("token[0]=%q", got[0].Token)
	}
	if got[0].Filename != "foo.pdf" {
		t.Errorf("filename[0]=%q", got[0].Filename)
	}
	if got[1].Token != "<UPLOAD-TOKEN-FOR-report.pdf>" {
		t.Errorf("token[1]=%q", got[1].Token)
	}
	if got[1].Filename != "report.pdf" {
		t.Errorf("filename[1]=%q", got[1].Filename)
	}
	if got[1].Description != "Q3" {
		t.Errorf("desc=%q", got[1].Description)
	}
	if got[1].ContentType != "application/pdf" {
		t.Errorf("ct=%q", got[1].ContentType)
	}
}

func TestAttachDryRun_EmptyInput(t *testing.T) {
	got := attachDryRun(nil)
	if len(got) != 0 {
		t.Errorf("got=%+v, want empty", got)
	}
}

// Dry-run renderer tests.

func TestRenderDryRun_NoUploads_MarkdownUnchanged(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "markdown"}
	body := map[string]any{"issue": map[string]any{"subject": "x"}}
	if err := renderDryRun(rc, "POST", "/issues.json", body, nil); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "DRY RUN -- would POST /issues.json with body:") {
		t.Errorf("missing DRY RUN line:\n%s", got)
	}
	if strings.Contains(got, "Would upload") {
		t.Errorf("should not mention uploads:\n%s", got)
	}
}

func TestRenderDryRun_WithUploads_Markdown(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "markdown"}
	body := map[string]any{"issue": map[string]any{"subject": "x"}}
	specs := []attachSpec{
		{Path: "/local/path/foo.pdf", ContentType: "application/pdf", Description: "Q3 report"},
		{Path: "/local/path/bar.png"},
	}
	if err := renderDryRun(rc, "POST", "/issues.json", body, specs); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "Would upload 2 file(s):") {
		t.Errorf("missing 'Would upload 2 file(s):'\n%s", got)
	}
	// Numbered list items
	if !strings.Contains(got, "1. /local/path/foo.pdf") {
		t.Errorf("missing item 1:\n%s", got)
	}
	if !strings.Contains(got, "2. /local/path/bar.png") {
		t.Errorf("missing item 2:\n%s", got)
	}
	// Parenthetical metadata for first item
	if !strings.Contains(got, "filename=foo.pdf") {
		t.Errorf("missing filename=foo.pdf:\n%s", got)
	}
	if !strings.Contains(got, "content_type=application/pdf") {
		t.Errorf("missing content_type=application/pdf:\n%s", got)
	}
	if !strings.Contains(got, "description=Q3 report") {
		t.Errorf("missing description=Q3 report:\n%s", got)
	}
	// Second item: only filename in parens (default basename)
	if !strings.Contains(got, "filename=bar.png") {
		t.Errorf("missing filename=bar.png:\n%s", got)
	}
	// 'Would upload' must appear before the DRY RUN block.
	upIdx := strings.Index(got, "Would upload")
	dryIdx := strings.Index(got, "DRY RUN -- would")
	if upIdx == -1 || dryIdx == -1 || upIdx > dryIdx {
		t.Errorf("ordering wrong: upIdx=%d dryIdx=%d\n%s", upIdx, dryIdx, got)
	}
}

func TestRenderDryRun_NoUploads_JSONUnchanged(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "json"}
	body := map[string]any{"issue": map[string]any{"subject": "x"}}
	if err := renderDryRun(rc, "POST", "/issues.json", body, nil); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if _, ok := got["would_upload"]; ok {
		t.Errorf("would_upload key should be absent: %v", got)
	}
	if got["dry_run"] != true || got["method"] != "POST" || got["path"] != "/issues.json" {
		t.Errorf("unexpected: %v", got)
	}
}

func TestRenderDryRun_WithUploads_JSON(t *testing.T) {
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, format: "json"}
	body := map[string]any{"issue": map[string]any{"subject": "x"}}
	specs := []attachSpec{
		{Path: "/local/foo.pdf", Filename: "report.pdf", Description: "Q3", ContentType: "application/pdf"},
	}
	if err := renderDryRun(rc, "POST", "/issues.json", body, specs); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	wu, ok := got["would_upload"].([]any)
	if !ok {
		t.Fatalf("would_upload not an array: %T %v", got["would_upload"], got["would_upload"])
	}
	if len(wu) != 1 {
		t.Fatalf("len=%d", len(wu))
	}
	first, _ := wu[0].(map[string]any)
	if first["path"] != "/local/foo.pdf" {
		t.Errorf("path=%v", first["path"])
	}
	if first["filename"] != "report.pdf" {
		t.Errorf("filename=%v", first["filename"])
	}
	if first["description"] != "Q3" {
		t.Errorf("description=%v", first["description"])
	}
	if first["content_type"] != "application/pdf" {
		t.Errorf("content_type=%v", first["content_type"])
	}
}
