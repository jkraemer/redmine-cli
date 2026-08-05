package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newCategoriesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "categories", Short: "Issue category operations"}
	c.AddCommand(newCategoriesListCmd(rc))
	return c
}

func newCategoriesListCmd(rc *runCtx) *cobra.Command {
	var projectID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issue categories in a project (--project required)",
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := rc.client.ListCategories(rc.ctx(), projectID)
			if err != nil {
				return err
			}
			return renderCategories(rc, res)
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func renderCategories(rc *runCtx, res *api.ListCategoriesResult) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(res.IssueCategories))
		for i, c := range res.IssueCategories {
			assignee := ""
			if c.AssignedTo != nil {
				assignee = c.AssignedTo.Name
			}
			rows[i] = []string{fmt.Sprintf("%d", c.ID), c.Name, assignee}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Name", "Default Assignee"}, rows)
	}
	return output.JSON(rc.out, res)
}
