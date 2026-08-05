package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
)

// singleHostAttachmentSrv mirrors how a real Redmine instance serves
// metadata and content from the same origin: GET /attachments/<id>.json
// returns metadata whose content_url points back to the same server.
func singleHostAttachmentSrv(t *testing.T, filename, payload string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".json") {
			w.Header().Set("Content-Type", "application/json")
			body := `{"attachment":{"id":7,"filename":` + jsonString(filename) + `,"filesize":7,"content_type":"text/plain","content_url":"` + srv.URL + `/f","author":{"id":1,"name":"A"},"created_on":"2026-01-01"}}`
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(payload))
	}))
	return srv
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestAttachmentsDownload_DefaultPath: without -o, downloads land in the
// per-user cache dir (0700), not a world-shared predictable temp path where
// another local user could pre-create the directory or plant symlinks.
func TestAttachmentsDownload_DefaultPath(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	srv := singleHostAttachmentSrv(t, "file.txt", "payload")
	defer srv.Close()

	c := api.New(srv.URL, "k", srv.Client())
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"attachments", "download", "7"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	path, _ := got["path"].(string)
	wantDir := filepath.Join(cacheHome, "redmine-cli")
	if filepath.Dir(path) != wantDir {
		t.Errorf("path=%s, want inside %s", path, wantDir)
	}
	if !strings.HasSuffix(path, "7-file.txt") {
		t.Errorf("path=%s", path)
	}
	info, err := os.Stat(wantDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("download dir mode %#o, want 0700", perm)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Errorf("file=%q", data)
	}
}

// H1: server-supplied filename containing path components must not let the
// download escape the download directory. We require it to resolve to a
// basename inside the cache dir, regardless of what the server sent.
func TestAttachmentsDownload_StripsPathFromFilename(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	srv := singleHostAttachmentSrv(t, "../../evil.txt", "payload")
	defer srv.Close()

	c := api.New(srv.URL, "k", srv.Client())
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"attachments", "download", "7"})
	if err := root.Execute(); err != nil {
		t.Fatalf("expected sanitized download to succeed, got: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	path, _ := got["path"].(string)
	expectedDir := filepath.Join(cacheHome, "redmine-cli")
	if filepath.Dir(path) != expectedDir {
		t.Errorf("download escaped cache dir: path=%q dir=%q", path, expectedDir)
	}
	if filepath.Base(path) != "7-evil.txt" {
		t.Errorf("expected sanitized basename 7-evil.txt, got %q", filepath.Base(path))
	}
	_ = os.Remove(path)
}

// H1: pathological filenames (".", "..", empty) provide no usable basename
// and must be rejected outright rather than silently saved as something else.
func TestAttachmentsDownload_RejectsPathologicalFilename(t *testing.T) {
	cases := []string{"", ".", "..", "../"}
	for _, name := range cases {
		t.Run("filename="+name, func(t *testing.T) {
			srv := singleHostAttachmentSrv(t, name, "payload")
			defer srv.Close()

			c := api.New(srv.URL, "k", srv.Client())
			var out bytes.Buffer
			rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
			root := buildRootForTest(rc)
			root.SetArgs([]string{"attachments", "download", "7"})
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error for filename %q, got nil; output=%s", name, out.String())
			}
		})
	}
}
