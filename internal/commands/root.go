// Package commands contains the cobra command tree for redmine-cli.
package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
	readOnly    bool
	configPath  string
	// parentCtx is the cancellable context for the whole CLI run. The
	// production wiring derives it from os.Interrupt/SIGTERM in Execute,
	// so Ctrl-C cancels any in-flight HTTP request. Tests may set their
	// own context for cancellation tests.
	parentCtx context.Context
}

// ErrReadOnly is returned when a write is attempted while the CLI is in
// read-only mode (REDMINE_READ_ONLY env or read_only config). The command is
// refused before any server call; exitCodeFor maps it to exit code 8.
var ErrReadOnly = errors.New("read-only mode is enabled; refusing to send a write")

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
	root.PersistentFlags().BoolP("markdown", "m", false, "Shorthand for --format markdown")
	root.PersistentFlags().BoolP("json", "j", false, "Shorthand for --format json")
	root.PersistentFlags().StringVarP(&rc.configPath, "config", "c", "",
		"Path to config file")
	root.PersistentFlags().BoolVar(&rc.agentHelp, "agent", false, "When combined with --help, emit machine-readable JSON")

	// Intercept --agent --help at any level by overriding HelpFunc.
	// Cobra propagates the help func to children that have not set their
	// own; setting it here on the root is sufficient for `--help` at any
	// nesting level.
	helpFunc := func(cmd *cobra.Command, _ []string) {
		if rc.agentHelp {
			_ = agenthelp.Render(rc.out, cmd, config.ReadOnly(rc.configPath))
			return
		}
		// Fall back to default-style help on stderr: cobra's help template
		// shows Long (or Short) above the usage block, and overriding
		// HelpFunc bypasses that template.
		desc := cmd.Long
		if desc == "" {
			desc = cmd.Short
		}
		if desc = strings.TrimSpace(desc); desc != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s\n\n", desc)
		}
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
		format, err := resolveFormat(cmd.Flags(), cfg.DefaultFormat)
		if err != nil {
			return err
		}
		rc.format = format
		rc.readOnly = cfg.ReadOnly
		// In read-only mode, refuse any confirmed write before the client is
		// built — so no token refresh, attachment upload, or API call runs.
		// The --confirm value is the universal write signal; read commands
		// have no such flag, so GetBool returns its zero value and they pass.
		if rc.readOnly {
			if confirm, _ := cmd.Flags().GetBool("confirm"); confirm {
				return fmt.Errorf("%w: %s", ErrReadOnly, cmd.CommandPath())
			}
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
	root.AddCommand(newCategoriesCmd(rc))
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

// resolveFormat determines the output format from the mutually exclusive
// format flags. -m and -j are shorthands for --format markdown and
// --format json; at most one of --format, -m, or -j may be set. When none is
// set, def (the config default) is returned.
func resolveFormat(flags *pflag.FlagSet, def string) (string, error) {
	var set []string
	if flags.Changed("format") {
		set = append(set, "--format")
	}
	if flags.Changed("markdown") {
		set = append(set, "-m")
	}
	if flags.Changed("json") {
		set = append(set, "-j")
	}
	if len(set) > 1 {
		return "", fmt.Errorf("conflicting output format flags (%s): use only one of --format, -m, -j", strings.Join(set, ", "))
	}
	switch {
	case flags.Changed("markdown"):
		return "markdown", nil
	case flags.Changed("json"):
		return "json", nil
	case flags.Changed("format"):
		v, _ := flags.GetString("format")
		if v != "json" && v != "markdown" {
			return "", fmt.Errorf("invalid format %q: must be json or markdown", v)
		}
		return v, nil
	default:
		// def comes from config/env; an empty value means "no default
		// configured" and is left for the caller's fallback.
		if def != "" && def != "json" && def != "markdown" {
			return "", fmt.Errorf("invalid default_format %q: must be json or markdown", def)
		}
		return def, nil
	}
}

func exitCodeFor(err error) int {
	if errors.Is(err, ErrReadOnly) {
		return 8
	}
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
