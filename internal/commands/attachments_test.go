package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jkraemer/redmine-cli/internal/api"
)

func TestAttachmentsDownload_TempPath(t *testing.T) {
	fileSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("payload"))
	}))
	defer fileSrv.Close()

	metaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := `{"attachment":{"id":7,"filename":"file.txt","filesize":7,"content_type":"text/plain","content_url":"` + fileSrv.URL + `/f","author":{"id":1,"name":"A"},"created_on":"2026-01-01"}}`
		_, _ = w.Write([]byte(body))
	}))
	defer metaSrv.Close()

	c := api.New(metaSrv.URL, "k", metaSrv.Client())
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
	if !strings.HasSuffix(path, "7-file.txt") {
		t.Errorf("path=%s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Errorf("file=%q", data)
	}
	_ = os.Remove(path)
}
