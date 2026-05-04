package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTimeLog_DryRun_NoNetworkCall(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called in dry-run mode (path=%s)", r.URL.Path)
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"time", "log", "--hours", "0.5", "--activity", "8", "--issue", "1459", "--comments", "test"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run should exit 0: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["dry_run"] != true || got["method"] != "POST" || got["path"] != "/time_entries.json" {
		t.Errorf("preview wrong: %v", got)
	}
	body, _ := got["body"].(map[string]any)
	te, _ := body["time_entry"].(map[string]any)
	if te["issue_id"] != float64(1459) || te["hours"] != 0.5 || te["activity_id"] != float64(8) {
		t.Errorf("body wrong: %v", te)
	}
}

func TestTimeLog_Confirm_SendsPOST(t *testing.T) {
	var seenMethod, seenPath, seenAPIKey, seenContentType string
	var seenBody []byte
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		seenMethod = r.Method
		seenPath = r.URL.Path
		seenAPIKey = r.Header.Get("X-Redmine-API-Key")
		seenContentType = r.Header.Get("Content-Type")
		seenBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"time_entry":{"id":99,"hours":0.5,"activity":{"id":8,"name":"Dev"},"project":{"id":1,"name":"P"},"user":{"id":1,"name":"U"},"spent_on":"2026-05-04","created_on":"2026-05-04T00:00:00Z"}}`))
	})
	defer stop()

	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"time", "log", "--hours", "0.5", "--activity", "8", "--issue", "1459", "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if seenMethod != "POST" || seenPath != "/time_entries.json" {
		t.Errorf("wrong request: %s %s", seenMethod, seenPath)
	}
	if seenAPIKey != "k" {
		t.Errorf("api key=%s", seenAPIKey)
	}
	if seenContentType != "application/json" {
		t.Errorf("content-type=%s", seenContentType)
	}
	var bodyJSON map[string]any
	if err := json.Unmarshal(seenBody, &bodyJSON); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	wrapped, _ := bodyJSON["time_entry"].(map[string]any)
	if wrapped["issue_id"] != float64(1459) || wrapped["hours"] != 0.5 {
		t.Errorf("body wrong: %v", wrapped)
	}
	if !strings.Contains(out.String(), `"id": 99`) {
		t.Errorf("output missing time entry:\n%s", out.String())
	}
}

func TestTimeLog_Validation(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called for validation errors")
	})
	defer stop()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no hours", []string{"time", "log", "--activity", "8", "--issue", "1"}, "--hours"},
		{"no activity", []string{"time", "log", "--hours", "1", "--issue", "1"}, "--activity"},
		{"both issue and project", []string{"time", "log", "--hours", "1", "--activity", "8", "--issue", "1", "--project", "p"}, "exactly one"},
		{"neither issue nor project", []string{"time", "log", "--hours", "1", "--activity", "8"}, "exactly one"},
		{"bad date", []string{"time", "log", "--hours", "1", "--activity", "8", "--issue", "1", "--date", "not-a-date"}, "YYYY-MM-DD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			rc := &runCtx{out: &out, errOut: &errOut, client: c, format: "json"}
			root := buildRootForTest(rc)
			root.SetArgs(tc.args)
			err := root.Execute()
			if err == nil {
				t.Fatalf("expected error, got nil. output=%s", out.String())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err=%q, want contains %q", err.Error(), tc.want)
			}
		})
	}
}
