// Package commands contains the cobra command tree for redmine-cli.
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/agenthelp"
	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/config"
)

// runCtx holds context shared by all commands.
type runCtx struct {
	out, errOut io.Writer
	client      *api.Client
	format      string // "json" or "markdown"
	agentHelp   bool
}

// Build constructs the root command with all subcommands wired in.
// out and errOut are used in place of os.Stdout/os.Stderr (testable).
func Build(out, errOut io.Writer) *cobra.Command {
	rc := &runCtx{out: out, errOut: errOut}

	root := &cobra.Command{
		Use:           "redmine-cli",
		Short:         "Agent-friendly CLI for the Redmine/Planio API",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.PersistentFlags().StringVarP(&rc.format, "format", "f", "", "Output format: json or markdown (default from config)")
	root.PersistentFlags().BoolVar(&rc.agentHelp, "agent", false, "When combined with --help, emit machine-readable JSON")

	// Intercept --agent --help at any level by overriding HelpFunc.
	// Cobra propagates the help func to children that have not set their
	// own; setting it here on the root is sufficient for `--help` at any
	// nesting level.
	helpFunc := func(cmd *cobra.Command, _ []string) {
		if rc.agentHelp {
			_ = agenthelp.Render(rc.out, cmd)
			return
		}
		// fall back to default help on stderr
		_ = cmd.Usage()
	}
	root.SetHelpFunc(helpFunc)

	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		// Skip client init for help-only invocations.
		if cmd.Name() == "help" || cmd.Name() == "redmine-cli" {
			return nil
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if rc.format == "" {
			rc.format = cfg.DefaultFormat
		}
		rc.client = api.New(cfg.URL, cfg.APIKey, http.DefaultClient)
		return nil
	}

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

// Execute runs the root command and exits with the appropriate code.
func Execute() {
	root := Build(os.Stdout, os.Stderr)
	err := root.Execute()
	if err == nil {
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(exitCodeFor(err))
}

func exitCodeFor(err error) int {
	if errors.Is(err, config.ErrMissingURL) || errors.Is(err, config.ErrMissingAPIKey) {
		return 3
	}
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case 401:
			return 3
		case 403:
			return 4
		case 404:
			return 2
		case 429:
			return 5
		}
		if apiErr.Status >= 500 {
			return 7
		}
	}
	// network-style errors: detect by url.Error type
	type netErr interface{ Timeout() bool }
	var ne netErr
	if errors.As(err, &ne) {
		return 6
	}
	return 1
}

// ctx returns a context for subcommands.
func (rc *runCtx) ctx() context.Context {
	return context.Background()
}
