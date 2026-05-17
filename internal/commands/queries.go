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
			if limit < 0 || limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}

			pageLimit := limit
			if pageLimit == 0 {
				pageLimit = 25
			}
			pageOffset := offset
			if all {
				pageLimit = paginateAllPageSize
				pageOffset = 0
			}

			ctx := rc.ctx()
			var collected []api.Query
			for {
				res, err := rc.client.ListQueries(ctx, api.ListQueriesParams{
					Limit:  pageLimit,
					Offset: pageOffset,
				})
				if err != nil {
					return err
				}
				if all && res.TotalCount > paginateAllCap {
					return fmt.Errorf("more than %d results (%d); narrow your filters or omit --all", paginateAllCap, res.TotalCount)
				}
				collected = append(collected, res.Queries...)
				if !all || len(collected) >= res.TotalCount || len(res.Queries) == 0 {
					break
				}
				pageOffset += len(res.Queries)
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
