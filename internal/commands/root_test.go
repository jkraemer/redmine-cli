package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// buildRootForTest returns a minimal root command with the runCtx pre-populated.
// It bypasses PersistentPreRunE so tests can inject *api.Client directly without
// needing real config or environment variables.
func buildRootForTest(rc *runCtx) *cobra.Command {
	root := &cobra.Command{Use: "redmine-cli", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(rc.out)
	root.SetErr(rc.errOut)
	root.PersistentPreRunE = func(_ *cobra.Command, _ []string) error { return nil }
	root.AddCommand(newProjectsCmd(rc))
	root.AddCommand(newIssuesCmd(rc))
	root.AddCommand(newAttachmentsCmd(rc))
	return root
}

func TestRoot_AgentHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(&out, &errOut)
	root.SetArgs([]string{"--agent", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if got["command"] != "redmine-cli" {
		t.Errorf("command=%v", got["command"])
	}
}

func TestRoot_Help_ListsSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(&out, &errOut)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	combined := out.String() + errOut.String()
	for _, want := range []string{"projects", "issues", "attachments"} {
		if !strings.Contains(combined, want) {
			t.Errorf("help missing %q\n%s", want, combined)
		}
	}
}
