package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

// searchAllCap caps the total number of results returned in --all mode.
// Mirrors the soft cap used elsewhere; keeps a runaway query bounded.
const searchAllCap = 1000

func newSearchCmd(rc *runCtx) *cobra.Command {
	var (
		issues, wiki, projects, allTypes bool
		titlesOnly                       bool
		scope, project                   string
		limit, offset                    int
		all                              bool
	)

	cmd := &cobra.Command{
		Use:   "search <query>...",
		Short: "Search issues, wiki pages, and projects",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && offset > 0 {
				return fmt.Errorf("--all and --offset are mutually exclusive")
			}
			if limit < 0 || limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}
			if scope != "" {
				switch scope {
				case "all", "my_projects", "subprojects":
				default:
					return fmt.Errorf("--scope must be one of: all, my_projects, subprojects")
				}
			}

			query := strings.Join(args, " ")

			// --all-types is shorthand for setting all three explicitly.
			if allTypes {
				issues, wiki, projects = true, true, true
			}

			p := api.SearchParams{
				Q:          query,
				Limit:      limit,
				Offset:     offset,
				Issues:     issues,
				Wiki:       wiki,
				Projects:   projects,
				TitlesOnly: titlesOnly,
				Scope:      scope,
				ProjectID:  project,
			}
			if p.Limit == 0 {
				p.Limit = 25
			}

			ctx := rc.ctx()
			var collected []api.SearchResult
			for {
				res, err := rc.client.Search(ctx, p)
				if err != nil {
					return err
				}
				collected = append(collected, res.Results...)
				if !all || len(collected) >= res.TotalCount || len(res.Results) == 0 {
					break
				}
				if len(collected) >= searchAllCap {
					break
				}
				p.Offset += len(res.Results)
			}

			return renderSearch(rc, collected)
		},
	}

	cmd.Flags().BoolVar(&issues, "issues", false, "Include issues in search results")
	cmd.Flags().BoolVar(&wiki, "wiki", false, "Include wiki pages in search results")
	cmd.Flags().BoolVar(&projects, "projects", false, "Include projects in search results")
	cmd.Flags().BoolVar(&allTypes, "all-types", false, "Include issues, wiki pages, and projects")
	cmd.Flags().BoolVar(&titlesOnly, "titles-only", false, "Match only in titles")
	cmd.Flags().StringVar(&scope, "scope", "", "Search scope: all, my_projects, subprojects")
	cmd.Flags().StringVar(&project, "project", "", "Narrow search to a project (ID or identifier)")
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages (capped at 1000 results)")
	return cmd
}

func renderSearch(rc *runCtx, results []api.SearchResult) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(results))
		for i, r := range results {
			rows[i] = []string{
				r.Type,
				fmt.Sprintf("%d", r.ID),
				r.Title,
				r.Datetime,
			}
		}
		return output.MarkdownTable(rc.out, []string{"Type", "ID", "Title", "Datetime"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"results": results})
}
