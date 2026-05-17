// Package commands contains the cobra command tree for redmine-cli.
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/agenthelp"
	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/auth"
	"github.com/jkraemer/redmine-cli/internal/config"
)

// runCtx holds context shared by all commands.
type runCtx struct {
	out, errOut io.Writer
	client      *api.Client
	format      string // "json" or "markdown"
	agentHelp   bool
	configPath  string
	// parentCtx is the cancellable context for the whole CLI run. The
	// production wiring derives it from os.Interrupt/SIGTERM in Execute,
	// so Ctrl-C cancels any in-flight HTTP request. Tests may set their
	// own context for cancellation tests.
	parentCtx context.Context
}

// Build constructs the root command with all subcommands wired in.
// ctx is the parent context for the whole run; it is propagated to every
// HTTP call so a Ctrl-C / SIGTERM in Execute() cancels in-flight requests.
// out and errOut are used in place of os.Stdout/os.Stderr (testable).
func Build(ctx context.Context, out, errOut io.Writer) *cobra.Command {
	rc := &runCtx{out: out, errOut: errOut, parentCtx: ctx}

	root := &cobra.Command{
		Use:           "redmine-cli",
		Short:         "Agent-friendly CLI for the Redmine/Planio API",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.PersistentFlags().StringVarP(&rc.format, "format", "f", "", "Output format: json or markdown (default from config)")
	root.PersistentFlags().StringVarP(&rc.configPath, "config", "c", "",
		"Path to config file")
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
		// Auth subcommands manage the token themselves; skip client init.
		if cmd.Parent() != nil && cmd.Parent().Name() == "auth" {
			return nil
		}
		cfg, err := config.Load(rc.configPath)
		if err != nil {
			return err
		}
		for _, w := range cfg.Warnings {
			fmt.Fprintln(rc.errOut, "warning:", w)
		}
		if rc.format == "" {
			rc.format = cfg.DefaultFormat
		}
		if cfg.AuthMethod() == "oauth" {
			tok := cfg.Token
			if tok == nil {
				return fmt.Errorf("not authenticated — run: redmine-cli auth login")
			}
			if tok.Expired() && tok.RefreshToken != "" {
				priorScope := tok.Scope
				refreshed, err := auth.RefreshWithScope(cfg.URL, cfg.OAuthClientID, cfg.OAuthClientSecret, tok.RefreshToken, priorScope)
				if err != nil {
					return fmt.Errorf("token refresh failed: %w (run: redmine-cli auth login)", err)
				}
				if err := cfg.SaveToken(refreshed); err != nil {
					return err
				}
				tok = refreshed
			}
			rc.client = api.NewWithToken(cfg.URL, tok.AccessToken, nil)
		} else {
			rc.client = api.New(cfg.URL, cfg.APIKey, nil)
		}
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
	root.AddCommand(newWikiCmd(rc))
	root.AddCommand(newQueriesCmd(rc))
	root.AddCommand(newAuthCmd(rc))

	return root
}

// Execute runs the root command and exits with the appropriate code.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := Build(ctx, os.Stdout, os.Stderr)
	err := root.ExecuteContext(ctx)
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

// ctx returns the cancellable run context shared by all subcommands.
// It falls back to context.Background only when nothing was wired (defensive
// guard for older test code paths).
func (rc *runCtx) ctx() context.Context {
	if rc.parentCtx == nil {
		return context.Background()
	}
	return rc.parentCtx
}
