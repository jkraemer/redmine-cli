package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
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
	root.AddCommand(newTimeCmd(rc))
	root.AddCommand(newUsersCmd(rc))
	root.AddCommand(newTrackersCmd(rc))
	root.AddCommand(newStatusesCmd(rc))
	root.AddCommand(newPrioritiesCmd(rc))
	root.AddCommand(newTimeActivitiesCmd(rc))
	root.AddCommand(newSearchCmd(rc))
	return root
}

func TestRoot_AgentHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
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

// M2: a parent context stored on runCtx must reach the HTTP layer, so
// cancelling it (e.g. on Ctrl-C) interrupts in-flight requests.
func TestRunCtx_CancelsInFlightHTTP(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-block:
		}
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rc := &runCtx{
		out:       &bytes.Buffer{},
		errOut:    &bytes.Buffer{},
		client:    api.New(srv.URL, "k", srv.Client()),
		format:    "json",
		parentCtx: ctx,
	}
	root := buildRootForTest(rc)
	root.SetArgs([]string{"projects", "list"})

	done := make(chan error, 1)
	go func() { done <- root.Execute() }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("server never received request")
	}
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Errorf("expected error after context cancel, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("command did not return after context cancel")
	}
}

func TestRoot_Help_ListsSubcommands(t *testing.T) {
	var out, errOut bytes.Buffer
	root := Build(context.Background(), &out, &errOut)
	root.SetArgs([]string{"--help"})
	_ = root.Execute()
	combined := out.String() + errOut.String()
	for _, want := range []string{"projects", "issues", "attachments"} {
		if !strings.Contains(combined, want) {
			t.Errorf("help missing %q\n%s", want, combined)
		}
	}
}
