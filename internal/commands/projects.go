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
			if all && offset > 0 {
				return fmt.Errorf("--all and --offset are mutually exclusive")
			}
			if limit < 0 || limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}
			ctx := rc.ctx()

			var collected []api.Project
			pageLimit := limit
			if pageLimit == 0 {
				pageLimit = 25
			}
			pageOffset := offset
			for {
				res, err := rc.client.ListProjects(ctx, api.ListProjectsParams{
					Limit:  pageLimit,
					Offset: pageOffset,
				})
				if err != nil {
					return err
				}
				collected = append(collected, res.Projects...)
				if !all || len(collected) >= res.TotalCount || len(res.Projects) == 0 {
					break
				}
				pageOffset += len(res.Projects)
			}

			return renderProjects(rc, collected)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages")
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
