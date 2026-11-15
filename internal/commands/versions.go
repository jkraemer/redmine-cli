package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newVersionsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "versions", Short: "Project version (target version) operations"}
	c.AddCommand(newVersionsListCmd(rc))
	return c
}

func newVersionsListCmd(rc *runCtx) *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List versions available to a project, including shared ones (--project required)",
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := rc.client.ListVersions(rc.ctx(), projectID)
			if err != nil {
				return err
			}
			return renderVersions(rc, res)
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func renderVersions(rc *runCtx, res *api.ListVersionsResult) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(res.Versions))
		for i, v := range res.Versions {
			rows[i] = []string{fmt.Sprintf("%d", v.ID), v.Name, v.Status, v.DueDate, v.Project.Name}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Name", "Status", "Due Date", "Project"}, rows)
	}
	return output.JSON(rc.out, res)
}
