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
	if err := Render(&buf, root, false, false); err != nil {
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
	if err := Render(&buf, list, false, false); err != nil {
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

func TestRender_ExampleOutput(t *testing.T) {
	root := &cobra.Command{Use: "redmine-cli"}
	get := &cobra.Command{
		Use:   "get",
		Short: "Get a thing",
		Annotations: map[string]string{
			"agent-example-output": `{"thing": {"id": 1}}`,
		},
	}
	plain := &cobra.Command{Use: "list", Short: "List things"}
	root.AddCommand(get, plain)

	var buf bytes.Buffer
	if err := Render(&buf, get, false, false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got["example_output"] != `{"thing": {"id": 1}}` {
		t.Errorf("example_output=%v", got["example_output"])
	}

	buf.Reset()
	if err := Render(&buf, plain, false, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "example_output") {
		t.Errorf("example_output must be omitted when no annotation is set:\n%s", buf.String())
	}
}
