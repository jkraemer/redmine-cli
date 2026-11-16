package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestWatchersAdd_DryRun(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no server call expected in dry-run (path=%s)", r.URL.Path)
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "watchers", "add", "42", "7"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["dry_run"] != true || got["method"] != "POST" || got["path"] != "/issues/42/watchers.json" {
		t.Errorf("preview wrong: %v", got)
	}
	body, _ := got["body"].(map[string]any)
	if body["user_id"] != float64(7) {
		t.Errorf("body=%v", body)
	}
}

func TestWatchersAdd_Confirm_SendsPOST(t *testing.T) {
	var calls int32
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != "POST" || r.URL.Path != "/issues/42/watchers.json" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "watchers", "add", "42", "7", "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls=%d", calls)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["added"] != true {
		t.Errorf("confirmation output wrong: %v", got)
	}
}

func TestWatchersRemove_DryRun_NoBody(t *testing.T) {
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no server call expected in dry-run")
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "watchers", "remove", "42", "7"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["method"] != "DELETE" || got["path"] != "/issues/42/watchers/7.json" {
		t.Errorf("preview wrong: %v", got)
	}
	if _, present := got["body"]; present {
		t.Errorf("DELETE preview must have no body: %v", got)
	}
}

func TestWatchersRemove_Confirm_SendsDELETE(t *testing.T) {
	var calls int32
	c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != "DELETE" || r.URL.Path != "/issues/42/watchers/7.json" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: "json"}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"issues", "watchers", "remove", "42", "7", "--confirm"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls=%d", calls)
	}
}

func TestWatchers_RejectBadArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"add", "x", "7"}, `invalid issue id "x"`},
		{[]string{"add", "42", "y"}, `invalid user id "y"`},
		{[]string{"add", "0", "7"}, `invalid issue id "0"`},
		{[]string{"add", "42", "0"}, `invalid user id "0"`},
		{[]string{"remove", "0", "7"}, `invalid issue id "0"`},
		{[]string{"remove", "42", "0"}, `invalid user id "0"`},
	} {
		c, stop := newClientForTest(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("%v: no server call expected (%s %s)", tc.args, r.Method, r.URL.Path)
		})
		rc := &runCtx{out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, client: c, format: "json"}
		root := buildRootForTest(rc)
		root.SetArgs(append([]string{"issues", "watchers"}, tc.args...))
		err := root.Execute()
		stop()
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v: err=%v, want %q", tc.args, err, tc.want)
		}
	}
}
