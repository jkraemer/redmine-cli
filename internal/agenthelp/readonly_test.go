package agenthelp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func writeCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "create",
		Short:       "Create an issue",
		Annotations: map[string]string{"write": "true"},
	}
}

func renderJSON(t *testing.T, cmd *cobra.Command, readOnly bool) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := Render(&buf, cmd, readOnly); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	return got
}

func TestRender_ReadOnly_MarksWriteCommand(t *testing.T) {
	got := renderJSON(t, writeCmd(), true)
	if got["read_only"] != true {
		t.Errorf("read_only=%v, want true", got["read_only"])
	}
	if got["blocked"] != true {
		t.Errorf("blocked=%v, want true", got["blocked"])
	}
	notes, _ := got["notes"].([]any)
	joined := ""
	for _, n := range notes {
		joined += n.(string)
	}
	if !strings.Contains(joined, "read-only") || !strings.Contains(joined, "blocked") {
		t.Errorf("notes do not mention read-only block: %v", notes)
	}
}

func TestRender_NotReadOnly_NoMarks(t *testing.T) {
	got := renderJSON(t, writeCmd(), false)
	if _, ok := got["read_only"]; ok {
		t.Errorf("read_only should be omitted when false, got %v", got["read_only"])
	}
	if _, ok := got["blocked"]; ok {
		t.Errorf("blocked should be omitted when not read-only, got %v", got["blocked"])
	}
	if _, ok := got["notes"]; ok {
		t.Errorf("notes should be absent, got %v", got["notes"])
	}
}

func TestRender_ReadOnly_NonWriteCommandNotBlocked(t *testing.T) {
	read := &cobra.Command{Use: "list", Short: "List issues"}
	got := renderJSON(t, read, true)
	if got["read_only"] != true {
		t.Errorf("read_only=%v, want true", got["read_only"])
	}
	if _, ok := got["blocked"]; ok {
		t.Errorf("a read command must not be blocked, got %v", got["blocked"])
	}
}

func TestRender_ReadOnly_MarksWriteSubcommands(t *testing.T) {
	issues := &cobra.Command{Use: "issues", Short: "Issue ops"}
	issues.AddCommand(writeCmd())                                        // create: write
	issues.AddCommand(&cobra.Command{Use: "list", Short: "List issues"}) // read
	got := renderJSON(t, issues, true)

	subs, _ := got["subcommands"].([]any)
	if len(subs) != 2 {
		t.Fatalf("subcommands=%d, want 2", len(subs))
	}
	blocked := map[string]bool{}
	for _, s := range subs {
		m := s.(map[string]any)
		name, _ := m["name"].(string)
		blocked[name] = m["blocked"] == true
	}
	if !blocked["create"] {
		t.Errorf("write subcommand 'create' should be marked blocked")
	}
	if blocked["list"] {
		t.Errorf("read subcommand 'list' must not be marked blocked")
	}
}
