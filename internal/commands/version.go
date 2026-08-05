package commands

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata, overridden at release time via
//
//	-ldflags "-X github.com/jkraemer/redmine-cli/internal/commands.buildVersion=…"
//
// (same for buildCommit and buildDate; see .goreleaser.yaml).
var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

func newVersionCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version and build information",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(rc.out, "redmine-cli %s (commit %s, built %s, %s %s/%s)\n",
				buildVersion, buildCommit, buildDate,
				runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return err
		},
	}
}
