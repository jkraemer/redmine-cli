package agenthelp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRender_RootCommand(t *testing.T) {
	root := &cobra.Command{
		Use:   "redmine-cli",
		Short: "CLI for Redmine",
	}
	root.PersistentFlags().String("format", "json", "Output format")
	sub := &cobra.Command{
		Use:   "issues",
		Short: "Issue operations",
	}
	sub.AddCommand(&cobra.Command{Use: "list", Short: "List issues"})
	root.AddCommand(sub)

	var buf bytes.Buffer
	if err := Render(&buf, root, false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got["command"] != "redmine-cli" {
		t.Errorf("command=%v", got["command"])
	}
	subs, _ := got["subcommands"].([]any)
	if len(subs) != 1 {
		t.Fatalf("subcommands=%d", len(subs))
	}
	if !strings.Contains(buf.String(), "issues") {
		t.Error("missing issues sub")
	}
}

func TestRender_Subcommand(t *testing.T) {
	root := &cobra.Command{Use: "redmine-cli"}
	issues := &cobra.Command{Use: "issues", Short: "Issue ops"}
	list := &cobra.Command{Use: "list", Short: "List issues"}
	list.Flags().Int("limit", 25, "max results")
	issues.AddCommand(list)
	root.AddCommand(issues)

	var buf bytes.Buffer
	if err := Render(&buf, list, false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(buf.Bytes(), &got)
	if got["path"] != "redmine-cli issues list" {
		t.Errorf("path=%v", got["path"])
	}
	flags, _ := got["flags"].([]any)
	if len(flags) == 0 {
		t.Error("expected flags")
	}
}
