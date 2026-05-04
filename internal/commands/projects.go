package commands

import "github.com/spf13/cobra"

func newProjectsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "projects", Short: "Project operations"}
	return c
}
