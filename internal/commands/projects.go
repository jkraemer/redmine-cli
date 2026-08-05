package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newProjectsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "projects", Short: "Project operations"}
	c.AddCommand(newProjectsListCmd(rc))
	return c
}

func newProjectsListCmd(rc *runCtx) *cobra.Command {
	var limit, offset int
	var all bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := rc.ctx()
			collected, err := collectPages(limit, offset, all, func(limit, offset int) ([]api.Project, int, error) {
				res, err := rc.client.ListProjects(ctx, api.ListProjectsParams{
					Limit:  limit,
					Offset: offset,
				})
				if err != nil {
					return nil, 0, err
				}
				return res.Projects, res.TotalCount, nil
			})
			if err != nil {
				return err
			}
			return renderProjects(rc, collected)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages (ignores --limit/--offset; capped at 1000 results)")
	return cmd
}

func renderProjects(rc *runCtx, projects []api.Project) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(projects))
		for i, p := range projects {
			rows[i] = []string{fmt.Sprintf("%d", p.ID), p.Identifier, p.Name}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Identifier", "Name"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"projects": projects})
}
