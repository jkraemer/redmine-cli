package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

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
