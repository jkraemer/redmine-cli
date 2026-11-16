package commands

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// runRelations runs an `issues relations` subcommand against handler and
// returns its output.
func runRelations(t *testing.T, format string, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	c, stop := newClientForTest(t, handler)
	defer stop()
	var out bytes.Buffer
	rc := &runCtx{out: &out, errOut: &bytes.Buffer{}, client: c, format: format}
	root := buildRootForTest(rc)
	root.SetArgs(append([]string{"issues", "relations"}, args...))
	err := root.Execute()
	return out.String(), err
}

func noServerCall(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no server call expected (%s %s)", r.Method, r.URL.Path)
	}
}

func dryRunPreview(t *testing.T, out string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got["dry_run"] != true {
		t.Errorf("dry_run=%v", got["dry_run"])
	}
	return got
}

func TestRelationsAdd_DryRun(t *testing.T) {
	out, err := runRelations(t, "json", noServerCall(t), "add", "5", "blocks", "12")
	if err != nil {
		t.Fatal(err)
	}
	got := dryRunPreview(t, out)
	if got["method"] != "POST" || got["path"] != "/issues/5/relations.json" {
		t.Errorf("preview wrong: %v", got)
	}
	body, _ := got["body"].(map[string]any)
	rel, _ := body["relation"].(map[string]any)
	if rel["issue_to_id"] != float64(12) || rel["relation_type"] != "blocks" {
		t.Errorf("relation=%v", rel)
	}
	if _, present := rel["delay"]; present {
		t.Errorf("delay must be omitted when not given: %v", rel)
	}
}

func TestRelationsAdd_DryRun_DelayZeroSent(t *testing.T) {
	out, err := runRelations(t, "json", noServerCall(t), "add", "5", "precedes", "12", "--delay", "0")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := dryRunPreview(t, out)["body"].(map[string]any)
	rel, _ := body["relation"].(map[string]any)
	if v, present := rel["delay"]; !present || v != float64(0) {
		t.Errorf("delay=%v present=%v, want 0", v, present)
	}
}

func TestRelationsAdd_Confirm_SendsPOST(t *testing.T) {
	var calls int32
	var gotBody []byte
	out, err := runRelations(t, "json", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != "POST" || r.URL.Path != "/issues/5/relations.json" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"relation":{"id":873,"issue_id":5,"issue_to_id":12,"relation_type":"precedes","delay":3}}`))
	}, "add", "5", "precedes", "12", "--delay", "3", "--confirm")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls=%d", calls)
	}
	if want := `{"relation":{"issue_to_id":12,"relation_type":"precedes","delay":3}}`; string(gotBody) != want {
		t.Errorf("body=%s, want %s", gotBody, want)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	rel, _ := got["relation"].(map[string]any)
	if rel["id"] != float64(873) || rel["delay"] != float64(3) {
		t.Errorf("output relation=%v", got)
	}
}

// Redmine stores inverse types flipped: "5 blocked 12" comes back as
// "12 blocks 5". The confirmation still reads from issue 5's side.
func TestRelationsAdd_Confirm_Markdown_InverseType(t *testing.T) {
	out, err := runRelations(t, "markdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"relation":{"id":873,"issue_id":12,"issue_to_id":5,"relation_type":"blocks","delay":null}}`))
	}, "add", "5", "blocked", "12", "--confirm")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Added relation 873: #5 blocked by #12.\n" {
		t.Errorf("out=%q", out)
	}
}

func TestRelationsAdd_RejectsBadArgs(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"add", "x", "relates", "12"}, `invalid issue id "x"`},
		{[]string{"add", "5", "relates", "y"}, `invalid issue id "y"`},
		{[]string{"add", "0", "relates", "12"}, `invalid issue id "0"`},
		{[]string{"add", "5", "blocker", "12"}, `unknown relation type "blocker" (want one of: relates, duplicates, duplicated, blocks, blocked, precedes, follows, copied_to, copied_from)`},
		{[]string{"add", "5", "relates", "12", "--delay", "2"}, "--delay is only valid for precedes and follows"},
		{[]string{"add", "5", "relates", "5"}, "an issue cannot be related to itself"},
	} {
		_, err := runRelations(t, "json", noServerCall(t), tc.args...)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v: err=%v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestRelationsRemove_DryRun_NoBody(t *testing.T) {
	out, err := runRelations(t, "json", noServerCall(t), "remove", "873")
	if err != nil {
		t.Fatal(err)
	}
	got := dryRunPreview(t, out)
	if got["method"] != "DELETE" || got["path"] != "/relations/873.json" {
		t.Errorf("preview wrong: %v", got)
	}
	if _, present := got["body"]; present {
		t.Errorf("DELETE preview must have no body: %v", got)
	}
}

func TestRelationsRemove_Confirm_SendsDELETE(t *testing.T) {
	var calls int32
	handler := func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != "DELETE" || r.URL.Path != "/relations/873.json" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}
	out, err := runRelations(t, "json", handler, "remove", "873", "--confirm")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got["removed"] != true || got["relation_id"] != float64(873) {
		t.Errorf("output=%v", got)
	}
	out, err = runRelations(t, "markdown", handler, "remove", "873", "--confirm")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Removed relation 873.\n" {
		t.Errorf("markdown out=%q", out)
	}
	if calls != 2 {
		t.Errorf("calls=%d", calls)
	}
}

func TestRelationsRemove_RejectsBadID(t *testing.T) {
	for _, id := range []string{"x", "0"} {
		_, err := runRelations(t, "json", noServerCall(t), "remove", id)
		if err == nil || !strings.Contains(err.Error(), `invalid relation id "`+id+`"`) {
			t.Errorf("remove %q: err=%v", id, err)
		}
	}
}

// The relation type in the server's response is shown verbatim for types
// the CLI does not know, so it must be stripped of terminal escapes.
func TestRelationsAdd_Confirm_Markdown_SanitizesServerType(t *testing.T) {
	out, err := runRelations(t, "markdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"relation":{"id":873,"issue_id":5,"issue_to_id":12,"relation_type":"evil\u001b]0;pwned\u0007"}}`))
	}, "add", "5", "relates", "12", "--confirm")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("terminal escapes not stripped: %q", out)
	}
}
