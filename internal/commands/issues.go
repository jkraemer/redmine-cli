package commands

import "github.com/spf13/cobra"

func newIssuesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "issues", Short: "Issue operations"}
	return c
}
