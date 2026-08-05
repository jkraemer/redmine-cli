package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newQueriesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "queries", Short: "Saved query operations"}
	c.AddCommand(newQueriesListCmd(rc))
	return c
}

func newQueriesListCmd(rc *runCtx) *cobra.Command {
	var limit, offset int
	var all bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved queries visible to the authenticated user",
		Long: "Lists saved queries. The Redmine API does not expose a query's " +
			"entity type (issues, time entries, projects); only ID, name, " +
			"visibility, and project scope are returned. Global queries " +
			"(visible across projects) have an empty Project ID.",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := rc.ctx()
			collected, err := collectPages(limit, offset, all, func(limit, offset int) ([]api.Query, int, error) {
				res, err := rc.client.ListQueries(ctx, api.ListQueriesParams{
					Limit:  limit,
					Offset: offset,
				})
				if err != nil {
					return nil, 0, err
				}
				return res.Queries, res.TotalCount, nil
			})
			if err != nil {
				return err
			}
			return renderQueries(rc, collected)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages (ignores --limit/--offset; capped at 1000 results)")
	return cmd
}

func renderQueries(rc *runCtx, queries []api.Query) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(queries))
		for i, q := range queries {
			pid := ""
			if q.ProjectID != nil {
				pid = fmt.Sprintf("%d", *q.ProjectID)
			}
			rows[i] = []string{
				fmt.Sprintf("%d", q.ID),
				q.Name,
				fmt.Sprintf("%t", q.IsPublic),
				pid,
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Name", "Public", "Project ID"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"queries": queries})
}
