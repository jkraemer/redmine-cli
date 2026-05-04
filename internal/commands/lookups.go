package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

// ----- users -----

func newUsersCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "users", Short: "User operations"}
	c.AddCommand(newUsersMeCmd(rc))
	c.AddCommand(newUsersListCmd(rc))
	return c
}

func newUsersMeCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "Show the currently authenticated user",
		RunE: func(_ *cobra.Command, _ []string) error {
			u, err := rc.client.GetCurrentUser(rc.ctx())
			if err != nil {
				return err
			}
			return renderUserMe(rc, u)
		},
	}
}

func renderUserMe(rc *runCtx, u *api.User) error {
	if rc.format == "markdown" {
		name := strings.TrimSpace(u.Firstname + " " + u.Lastname)
		rows := [][]string{
			{"id", fmt.Sprintf("%d", u.ID)},
			{"login", u.Login},
			{"name", name},
			{"mail", u.Mail},
		}
		return output.MarkdownTable(rc.out, []string{"Field", "Value"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"user": u})
}

func newUsersListCmd(rc *runCtx) *cobra.Command {
	var limit, offset int
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List users (admin only on most Redmine installs)",
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
				// In --all mode, ignore --limit/--offset and fetch
				// every page using a fixed internal page size.
				pageLimit = paginateAllPageSize
				pageOffset = 0
			}

			ctx := rc.ctx()
			var collected []api.User
			for {
				res, err := rc.client.ListUsers(ctx, api.ListUsersParams{
					Limit:  pageLimit,
					Offset: pageOffset,
				})
				if err != nil {
					return err
				}
				if all && res.TotalCount > paginateAllCap {
					return fmt.Errorf("more than %d results (%d); narrow your filters or omit --all", paginateAllCap, res.TotalCount)
				}
				collected = append(collected, res.Users...)
				if !all || len(collected) >= res.TotalCount || len(res.Users) == 0 {
					break
				}
				pageOffset += len(res.Users)
			}
			return renderUsersList(rc, collected)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages (ignores --limit/--offset; capped at 1000 results)")
	return cmd
}

func renderUsersList(rc *runCtx, users []api.User) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(users))
		for i, u := range users {
			name := strings.TrimSpace(u.Firstname + " " + u.Lastname)
			rows[i] = []string{
				fmt.Sprintf("%d", u.ID),
				u.Login,
				name,
				u.Mail,
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Login", "Name", "Mail"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"users": users})
}

// ----- trackers -----

func newTrackersCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "trackers", Short: "Tracker (issue type) operations"}
	c.AddCommand(newTrackersListCmd(rc))
	return c
}

func newTrackersListCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List trackers",
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := rc.client.ListTrackers(rc.ctx())
			if err != nil {
				return err
			}
			return renderTrackers(rc, res.Trackers)
		},
	}
}

func renderTrackers(rc *runCtx, trackers []api.Tracker) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(trackers))
		for i, t := range trackers {
			ds := ""
			if t.DefaultStatus != nil {
				ds = t.DefaultStatus.Name
			}
			rows[i] = []string{
				fmt.Sprintf("%d", t.ID),
				t.Name,
				ds,
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Name", "Default Status"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"trackers": trackers})
}

// ----- statuses -----

func newStatusesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "statuses", Short: "Issue status operations"}
	c.AddCommand(newStatusesListCmd(rc))
	return c
}

func newStatusesListCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List issue statuses",
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := rc.client.ListStatuses(rc.ctx())
			if err != nil {
				return err
			}
			return renderStatuses(rc, res.IssueStatuses)
		},
	}
}

func renderStatuses(rc *runCtx, statuses []api.Status) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(statuses))
		for i, s := range statuses {
			rows[i] = []string{
				fmt.Sprintf("%d", s.ID),
				s.Name,
				fmt.Sprintf("%t", s.IsClosed),
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Name", "Is Closed"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"issue_statuses": statuses})
}

// ----- priorities -----

func newPrioritiesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "priorities", Short: "Issue priority operations"}
	c.AddCommand(newPrioritiesListCmd(rc))
	return c
}

func newPrioritiesListCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List issue priorities",
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := rc.client.ListPriorities(rc.ctx())
			if err != nil {
				return err
			}
			return renderPriorities(rc, res.IssuePriorities)
		},
	}
}

func renderPriorities(rc *runCtx, priorities []api.Priority) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(priorities))
		for i, p := range priorities {
			rows[i] = []string{
				fmt.Sprintf("%d", p.ID),
				p.Name,
				fmt.Sprintf("%t", p.IsDefault),
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Name", "Is Default"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"issue_priorities": priorities})
}

// ----- time-activities -----

func newTimeActivitiesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "time-activities", Short: "Time-entry activity operations"}
	c.AddCommand(newTimeActivitiesListCmd(rc))
	return c
}

func newTimeActivitiesListCmd(rc *runCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List time-entry activities",
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := rc.client.ListActivities(rc.ctx())
			if err != nil {
				return err
			}
			return renderActivities(rc, res.TimeEntryActivities)
		},
	}
}

func renderActivities(rc *runCtx, activities []api.Activity) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(activities))
		for i, a := range activities {
			rows[i] = []string{
				fmt.Sprintf("%d", a.ID),
				a.Name,
				fmt.Sprintf("%t", a.IsDefault),
				fmt.Sprintf("%t", a.Active),
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Name", "Is Default", "Active"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"time_entry_activities": activities})
}
