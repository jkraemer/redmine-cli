//go:build live

// Package e2e runs the built redmine-cli binary against a live Redmine
// instance (see scripts/live/run.sh). No mocks: every test talks to the
// real server through the real binary.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	binPath string
	project string
)

func TestMain(m *testing.M) {
	if os.Getenv("REDMINE_URL") == "" || os.Getenv("REDMINE_API_KEY") == "" {
		fmt.Println("REDMINE_URL / REDMINE_API_KEY not set; skipping live suite")
		os.Exit(0)
	}
	project = os.Getenv("E2E_PROJECT")
	if project == "" {
		project = "e2e"
	}
	dir, err := os.MkdirTemp("", "redmine-cli-e2e")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	binPath = filepath.Join(dir, "redmine-cli")
	build := exec.Command("go", "build", "-o", binPath, "../cmd/redmine-cli")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building binary:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// run executes the binary with args plus -j, returning stdout, stderr, exit code.
func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, append(args, "-j")...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return out.String(), errOut.String(), code
}

// runJSON is run + assert exit 0 + unmarshal stdout into a map.
func runJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	out, errOut, code := run(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d\nstderr: %s\nstdout: %s", args, code, errOut, out)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v: not JSON: %v\n%s", args, err, out)
	}
	return m
}

// runWithEnv is run, plus environment overrides layered on top of the
// process environment (used by tests that need a bogus API key or
// read-only mode for a single invocation).
func runWithEnv(t *testing.T, env map[string]string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, append(args, "-j")...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running %v: %v", args, err)
	}
	return out.String(), errOut.String(), code
}

// uniqueSubject returns a subject string unique to this test run.
func uniqueSubject(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// jsonArray fetches m[key] and asserts it is a JSON array. A missing key or
// JSON null (Go's encoding/json marshals a nil slice as null, e.g. an
// "--all" list with zero results) is treated as an empty array rather than
// a failure — both mean "no items".
func jsonArray(t *testing.T, m map[string]any, key string) []any {
	t.Helper()
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("key %q is not an array: %v", key, v)
	}
	return arr
}

// containsField reports whether arr contains an object whose string field
// equals want.
func containsField(arr []any, field, want string) bool {
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		if s, _ := m[field].(string); s == want {
			return true
		}
	}
	return false
}

// containsID reports whether arr contains an object whose numeric "id"
// field equals id.
func containsID(arr []any, id int) bool {
	for _, el := range arr {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		if fid, ok := m["id"].(float64); ok && int(fid) == id {
			return true
		}
	}
	return false
}

// firstTrackerID returns the ID of the first tracker available on the
// server, used by tests that just need any valid tracker.
func firstTrackerID(t *testing.T) int {
	t.Helper()
	m := runJSON(t, "trackers", "list")
	arr := jsonArray(t, m, "trackers")
	if len(arr) == 0 {
		t.Fatalf("trackers list: empty")
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("trackers list: element is not an object: %v", arr[0])
	}
	id, ok := first["id"].(float64)
	if !ok {
		t.Fatalf("trackers list: no numeric id: %v", first)
	}
	return int(id)
}

// adminID looks up the currently authenticated user (the bootstrap admin)
// and returns its numeric ID.
func adminID(t *testing.T) int {
	t.Helper()
	m := runJSON(t, "users", "me")
	u, ok := m["user"].(map[string]any)
	if !ok {
		t.Fatalf("users me: no user object in %v", m)
	}
	if u["login"] != "admin" {
		t.Fatalf("users me: login=%v, want admin", u["login"])
	}
	id, ok := u["id"].(float64)
	if !ok {
		t.Fatalf("users me: no numeric id in %v", u)
	}
	return int(id)
}

// createIssue creates a confirmed issue with the given subject in the
// shared e2e project and returns its ID.
func createIssue(t *testing.T, subject string) int {
	t.Helper()
	trackerID := firstTrackerID(t)
	m := runJSON(t, "issues", "create",
		"--project", project,
		"--tracker", fmt.Sprintf("%d", trackerID),
		"--subject", subject,
		"--confirm")
	issue, ok := m["issue"].(map[string]any)
	if !ok {
		t.Fatalf("issues create: no issue in %v", m)
	}
	id, ok := issue["id"].(float64)
	if !ok {
		t.Fatalf("issues create: no numeric id in %v", issue)
	}
	return int(id)
}

func TestLookups(t *testing.T) {
	t.Run("trackers", func(t *testing.T) {
		m := runJSON(t, "trackers", "list")
		if arr := jsonArray(t, m, "trackers"); len(arr) == 0 {
			t.Errorf("trackers list: empty")
		}
	})
	t.Run("statuses", func(t *testing.T) {
		m := runJSON(t, "statuses", "list")
		if arr := jsonArray(t, m, "issue_statuses"); len(arr) == 0 {
			t.Errorf("statuses list: empty")
		}
	})
	t.Run("priorities", func(t *testing.T) {
		m := runJSON(t, "priorities", "list")
		if arr := jsonArray(t, m, "issue_priorities"); len(arr) == 0 {
			t.Errorf("priorities list: empty")
		}
	})
	t.Run("time-activities", func(t *testing.T) {
		m := runJSON(t, "time-activities", "list")
		if arr := jsonArray(t, m, "time_entry_activities"); len(arr) == 0 {
			t.Errorf("time-activities list: empty")
		}
	})
	t.Run("custom-fields", func(t *testing.T) {
		m := runJSON(t, "custom-fields", "list")
		arr := jsonArray(t, m, "custom_fields")
		if len(arr) == 0 {
			t.Errorf("custom-fields list: empty")
		}
		if !containsField(arr, "name", "E2E Text") {
			t.Errorf("custom-fields list does not contain %q: %v", "E2E Text", arr)
		}
	})
	t.Run("categories", func(t *testing.T) {
		m := runJSON(t, "categories", "list", "--project", project)
		arr := jsonArray(t, m, "issue_categories")
		if len(arr) == 0 {
			t.Errorf("categories list: empty")
		}
		if !containsField(arr, "name", "E2E Cat") {
			t.Errorf("categories list does not contain %q: %v", "E2E Cat", arr)
		}
	})
}

func TestUsersMe(t *testing.T) {
	m := runJSON(t, "users", "me")
	u, ok := m["user"].(map[string]any)
	if !ok {
		t.Fatalf("users me: no user object in %v", m)
	}
	if u["login"] != "admin" {
		t.Errorf("login=%v, want admin", u["login"])
	}
	if _, ok := u["id"].(float64); !ok {
		t.Errorf("id is not numeric: %v", u["id"])
	}
}

func TestIssueLifecycle(t *testing.T) {
	subject := uniqueSubject("lifecycle")
	trackerID := firstTrackerID(t)

	cats := runJSON(t, "categories", "list", "--project", project)
	catArr := jsonArray(t, cats, "issue_categories")
	var categoryID int
	for _, el := range catArr {
		m, _ := el.(map[string]any)
		if m["name"] == "E2E Cat" {
			if id, ok := m["id"].(float64); ok {
				categoryID = int(id)
			}
		}
	}
	if categoryID == 0 {
		t.Fatalf("E2E Cat category not found: %v", catArr)
	}

	cfs := runJSON(t, "custom-fields", "list")
	cfArr := jsonArray(t, cfs, "custom_fields")
	var cfID int
	for _, el := range cfArr {
		m, _ := el.(map[string]any)
		if m["name"] == "E2E Text" {
			if id, ok := m["id"].(float64); ok {
				cfID = int(id)
			}
		}
	}
	if cfID == 0 {
		t.Fatalf("E2E Text custom field not found: %v", cfArr)
	}

	// Dry-run create must exit 0, report dry_run, and not create anything.
	dryOut := runJSON(t, "issues", "create",
		"--project", project,
		"--tracker", fmt.Sprintf("%d", trackerID),
		"--subject", subject)
	if dryOut["dry_run"] != true {
		t.Errorf("dry-run create: dry_run=%v, want true", dryOut["dry_run"])
	}
	preList := runJSON(t, "issues", "list", "--project", project, "--all")
	if containsField(jsonArray(t, preList, "issues"), "subject", subject) {
		t.Errorf("dry-run create leaked into issues list: %q", subject)
	}

	// Real create with category and custom field.
	createOut := runJSON(t, "issues", "create",
		"--project", project,
		"--tracker", fmt.Sprintf("%d", trackerID),
		"--subject", subject,
		"--category", fmt.Sprintf("%d", categoryID),
		"--cf", fmt.Sprintf("%d=hello", cfID),
		"--confirm")
	issue, ok := createOut["issue"].(map[string]any)
	if !ok {
		t.Fatalf("create: no issue in %v", createOut)
	}
	idF, ok := issue["id"].(float64)
	if !ok {
		t.Fatalf("create: no numeric id in %v", issue)
	}
	issueID := int(idF)
	issueIDStr := fmt.Sprintf("%d", issueID)

	// Get shows subject and category.
	getOut := runJSON(t, "issues", "get", issueIDStr)
	got, ok := getOut["issue"].(map[string]any)
	if !ok {
		t.Fatalf("get: no issue in %v", getOut)
	}
	if got["subject"] != subject {
		t.Errorf("get: subject=%v, want %q", got["subject"], subject)
	}
	cat, ok := got["category"].(map[string]any)
	if !ok || cat["name"] != "E2E Cat" {
		t.Errorf("get: category=%v, want object with name E2E Cat", got["category"])
	}
	cfArr2 := jsonArray(t, got, "custom_fields")
	foundCF := false
	for _, el := range cfArr2 {
		m, _ := el.(map[string]any)
		if idF, ok := m["id"].(float64); ok && int(idF) == cfID && m["value"] == "hello" {
			foundCF = true
		}
	}
	if !foundCF {
		t.Errorf("get: custom_fields=%v, want id %d value %q", got["custom_fields"], cfID, "hello")
	}

	// Add a note; it must show up under --include journals.
	note := "note-" + uniqueSubject("n")
	runJSON(t, "issues", "update", issueIDStr, "--notes", note, "--confirm")
	getJournals := runJSON(t, "issues", "get", issueIDStr, "--include", "journals")
	gj, ok := getJournals["issue"].(map[string]any)
	if !ok {
		t.Fatalf("get --include journals: no issue in %v", getJournals)
	}
	journals := jsonArray(t, gj, "journals")
	foundNote := false
	for _, j := range journals {
		jm, _ := j.(map[string]any)
		if jm["notes"] == note {
			foundNote = true
		}
	}
	if !foundNote {
		t.Errorf("journals do not contain note %q: %v", note, journals)
	}

	// Clear the category.
	runJSON(t, "issues", "update", issueIDStr, "--category", "", "--confirm")
	getCleared := runJSON(t, "issues", "get", issueIDStr)
	gc, ok := getCleared["issue"].(map[string]any)
	if !ok {
		t.Fatalf("get after clearing category: no issue in %v", getCleared)
	}
	if _, present := gc["category"]; present {
		t.Errorf("category still present after clearing: %v", gc["category"])
	}

	// The project's issue list contains the issue.
	postList := runJSON(t, "issues", "list", "--project", project, "--all")
	if !containsField(jsonArray(t, postList, "issues"), "subject", subject) {
		t.Errorf("issues list --project %s does not contain %q", project, subject)
	}
}

func TestWatchers(t *testing.T) {
	issueID := createIssue(t, uniqueSubject("watchers"))
	issueIDStr := fmt.Sprintf("%d", issueID)
	uid := adminID(t)
	uidStr := fmt.Sprintf("%d", uid)

	runJSON(t, "issues", "watchers", "add", issueIDStr, uidStr, "--confirm")

	afterAdd := runJSON(t, "issues", "get", issueIDStr, "--include", "watchers")
	issue, ok := afterAdd["issue"].(map[string]any)
	if !ok {
		t.Fatalf("get --include watchers: no issue in %v", afterAdd)
	}
	watchers := jsonArray(t, issue, "watchers")
	if !containsID(watchers, uid) {
		t.Errorf("watchers after add: %v, want to contain user %d", watchers, uid)
	}

	runJSON(t, "issues", "watchers", "remove", issueIDStr, uidStr, "--confirm")

	afterRemove := runJSON(t, "issues", "get", issueIDStr, "--include", "watchers")
	issue2, ok := afterRemove["issue"].(map[string]any)
	if !ok {
		t.Fatalf("get --include watchers (after remove): no issue in %v", afterRemove)
	}
	if w, present := issue2["watchers"]; present {
		if arr, ok := w.([]any); !ok || len(arr) != 0 {
			t.Errorf("watchers after remove: %v, want empty or absent", w)
		}
	}
}

func TestAttachments(t *testing.T) {
	issueID := createIssue(t, uniqueSubject("attach"))
	issueIDStr := fmt.Sprintf("%d", issueID)

	dir := t.TempDir()
	src := filepath.Join(dir, "payload.bin")
	data := []byte("hello e2e attachment\x00\x01\x02\xff\xfe binary bytes\n")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatalf("writing source file: %v", err)
	}

	runJSON(t, "issues", "update", issueIDStr, "--attach", src, "--confirm")

	getOut := runJSON(t, "issues", "get", issueIDStr, "--include", "attachments")
	issue, ok := getOut["issue"].(map[string]any)
	if !ok {
		t.Fatalf("get --include attachments: no issue in %v", getOut)
	}
	attachments := jsonArray(t, issue, "attachments")
	var attID int
	for _, el := range attachments {
		m, _ := el.(map[string]any)
		if m["filename"] == "payload.bin" {
			if id, ok := m["id"].(float64); ok {
				attID = int(id)
			}
		}
	}
	if attID == 0 {
		t.Fatalf("attachment payload.bin not found in %v", attachments)
	}

	dest := filepath.Join(dir, "downloaded.bin")
	dlOut := runJSON(t, "attachments", "download", fmt.Sprintf("%d", attID), "-o", dest)
	if dlOut["path"] != dest {
		t.Errorf("download path=%v, want %v", dlOut["path"], dest)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("downloaded bytes differ:\ngot:  %x\nwant: %x", got, data)
	}
}

func TestWiki(t *testing.T) {
	out1 := runJSON(t, "wiki", "put", "E2ETestPage", "--project", project, "--text", "h1. live\n\nfirst version", "--confirm")
	v1, ok := out1["version"].(float64)
	if !ok {
		t.Fatalf("wiki put: no numeric version in %v", out1)
	}

	getOut := runJSON(t, "wiki", "get", "E2ETestPage", "--project", project)
	if !strings.Contains(fmt.Sprintf("%v", getOut["text"]), "first version") {
		t.Errorf("wiki get: text=%v, want to contain %q", getOut["text"], "first version")
	}

	out2 := runJSON(t, "wiki", "put", "E2ETestPage", "--project", project, "--text", "h1. live\n\nsecond version", "--confirm")
	v2, ok := out2["version"].(float64)
	if !ok {
		t.Fatalf("second wiki put: no numeric version in %v", out2)
	}
	if v2 <= v1 {
		t.Errorf("second put did not bump version: v1=%v v2=%v", v1, v2)
	}
}

func TestTimeLog(t *testing.T) {
	issueID := createIssue(t, uniqueSubject("timelog"))

	acts := runJSON(t, "time-activities", "list")
	actArr := jsonArray(t, acts, "time_entry_activities")
	if len(actArr) == 0 {
		t.Fatalf("time-activities list: empty")
	}
	first, ok := actArr[0].(map[string]any)
	if !ok {
		t.Fatalf("time-activities list: element is not an object: %v", actArr[0])
	}
	activityIDF, ok := first["id"].(float64)
	if !ok {
		t.Fatalf("time-activities list: no numeric id: %v", first)
	}

	out := runJSON(t, "time", "log",
		"--hours", "0.25",
		"--activity", fmt.Sprintf("%d", int(activityIDF)),
		"--issue", fmt.Sprintf("%d", issueID),
		"--confirm")
	te, ok := out["time_entry"].(map[string]any)
	if !ok {
		t.Fatalf("time log: no time_entry in %v", out)
	}
	if _, ok := te["id"].(float64); !ok {
		t.Errorf("time log: no numeric id in %v", te)
	}
	if te["hours"] != 0.25 {
		t.Errorf("time log: hours=%v, want 0.25", te["hours"])
	}
}

func TestSearch(t *testing.T) {
	token := fmt.Sprintf("searchtok%d", time.Now().UnixNano())
	createIssue(t, token)

	var m map[string]any
	var found bool
	for attempt := 1; attempt <= 3; attempt++ {
		m = runJSON(t, "search", token, "--issues", "--titles-only")
		results := jsonArray(t, m, "results")
		for _, el := range results {
			rm, _ := el.(map[string]any)
			if title, _ := rm["title"].(string); strings.Contains(title, token) {
				found = true
			}
		}
		if found {
			break
		}
		if attempt < 3 {
			time.Sleep(2 * time.Second)
		}
	}
	if !found {
		t.Errorf("search %q: token not found in results: %v", token, m["results"])
	}
}

func TestExitCodes(t *testing.T) {
	t.Run("not-found", func(t *testing.T) {
		_, _, code := run(t, "issues", "get", "999999999")
		if code != 2 {
			t.Errorf("exit code=%d, want 2", code)
		}
	})

	t.Run("bad-auth", func(t *testing.T) {
		_, _, code := runWithEnv(t, map[string]string{"REDMINE_API_KEY": "bogus"}, "users", "me")
		if code != 3 {
			t.Errorf("exit code=%d, want 3", code)
		}
	})

	t.Run("read-only", func(t *testing.T) {
		issueID := createIssue(t, uniqueSubject("exitcodes"))
		_, errOut, code := runWithEnv(t, map[string]string{"REDMINE_READ_ONLY": "true"},
			"issues", "update", fmt.Sprintf("%d", issueID), "--subject", "x", "--confirm")
		if code != 8 {
			t.Errorf("exit code=%d, want 8", code)
		}
		if !strings.Contains(errOut, "read-only") {
			t.Errorf("stderr does not mention read-only: %q", errOut)
		}
	})
}
