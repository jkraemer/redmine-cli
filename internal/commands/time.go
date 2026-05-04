package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newTimeCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "time", Short: "Time entry operations"}
	c.AddCommand(newTimeLogCmd(rc))
	return c
}

func newTimeLogCmd(rc *runCtx) *cobra.Command {
	var (
		hours              float64
		activity, issue    int
		project, date, msg string
		confirm            bool
	)
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Log time against an issue or project (requires --confirm to send)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if hours <= 0 {
				return fmt.Errorf("--hours must be > 0")
			}
			if activity <= 0 {
				return fmt.Errorf("--activity is required")
			}
			hasIssue := cmd.Flags().Changed("issue")
			hasProject := cmd.Flags().Changed("project")
			if hasIssue == hasProject {
				return fmt.Errorf("exactly one of --issue or --project is required")
			}
			if date != "" && !dateRE.MatchString(date) {
				return fmt.Errorf("--date must be YYYY-MM-DD")
			}

			payload := api.TimeEntryCreate{
				Hours:      hours,
				ActivityID: activity,
				SpentOn:    date,
				Comments:   msg,
			}
			if hasIssue {
				payload.IssueID = issue
			}
			if hasProject {
				payload.ProjectID = project
			}

			body := map[string]any{"time_entry": payload}
			if !confirm {
				return renderDryRun(rc, "POST", "/time_entries.json", body)
			}
			te, err := rc.client.LogTime(rc.ctx(), payload)
			if err != nil {
				return err
			}
			return renderTimeEntry(rc, te)
		},
	}
	cmd.Flags().Float64Var(&hours, "hours", 0, "Hours spent (required, > 0)")
	cmd.Flags().IntVar(&activity, "activity", 0, "Activity ID (required)")
	cmd.Flags().IntVar(&issue, "issue", 0, "Issue ID (mutually exclusive with --project)")
	cmd.Flags().StringVar(&project, "project", "", "Project identifier or ID (mutually exclusive with --issue)")
	cmd.Flags().StringVar(&date, "date", "", "Date the time was spent (YYYY-MM-DD, default today)")
	cmd.Flags().StringVar(&msg, "comments", "", "Comments")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually send the request (without this flag the command runs in dry-run mode)")
	return cmd
}

func renderTimeEntry(rc *runCtx, te *api.TimeEntry) error {
	if rc.format == "markdown" {
		_, err := fmt.Fprintf(rc.out, "Logged %.2f hours (entry #%d) on %s.\n", te.Hours, te.ID, te.SpentOn)
		return err
	}
	return output.JSON(rc.out, map[string]any{"time_entry": te})
}
