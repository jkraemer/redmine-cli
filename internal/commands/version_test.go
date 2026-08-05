package commands

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// version must work without any config/env — it is the one command users
// run before setting anything up.
func TestVersionCmd_RunsWithoutConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("REDMINE_URL", "")
	t.Setenv("REDMINE_API_KEY", "")
	t.Setenv("REDMINE_OAUTH_CLIENT_ID", "")
	var out bytes.Buffer
	root := Build(context.Background(), &out, &bytes.Buffer{})
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "redmine-cli dev") {
		t.Errorf("version output wrong:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "go1.") {
		t.Errorf("missing go runtime info:\n%s", out.String())
	}
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	root := Build(context.Background(), &out, &bytes.Buffer{})
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Errorf("--version output wrong:\n%s", out.String())
	}
}
