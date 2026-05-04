package commands

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

// dateRE validates YYYY-MM-DD strings used in --start-date / --due-date.
var dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func newIssuesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "issues", Short: "Issue operations"}
	c.AddCommand(newIssuesListCmd(rc))
	c.AddCommand(newIssuesGetCmd(rc))
	c.AddCommand(newIssuesCreateCmd(rc))
	return c
}

func newIssuesListCmd(rc *runCtx) *cobra.Command {
	var (
		project, status, assignee, updatedSince, sort string
		limit, offset                                 int
		all                                           bool
		includes                                      []string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all && offset > 0 {
				return fmt.Errorf("--all and --offset are mutually exclusive")
			}
			if limit < 0 || limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}

			p := api.ListIssuesParams{
				ProjectID:  project,
				StatusID:   status,
				AssignedTo: assignee,
				Sort:       sort,
				Limit:      limit,
				Offset:     offset,
				Include:    includes,
			}
			if updatedSince != "" {
				p.UpdatedOn = ">=" + updatedSince
			}
			if p.Limit == 0 {
				p.Limit = 25
			}

			ctx := rc.ctx()
			var collected []api.Issue
			for {
				res, err := rc.client.ListIssues(ctx, p)
				if err != nil {
					return err
				}
				collected = append(collected, res.Issues...)
				if !all || len(collected) >= res.TotalCount || len(res.Issues) == 0 {
					break
				}
				p.Offset += len(res.Issues)
			}
			return renderIssueList(rc, collected)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Filter by project identifier or ID")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status: open, closed, *, or numeric ID")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Filter by assignee user ID or 'me'")
	cmd.Flags().StringVar(&updatedSince, "updated-since", "", "Filter by updated_on >= YYYY-MM-DD")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort expression, e.g. updated_on:desc")
	cmd.Flags().IntVar(&limit, "limit", 25, "Max results per page (1-100)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages")
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Include extras: attachments, relations")
	return cmd
}

func newIssuesGetCmd(rc *runCtx) *cobra.Command {
	var includes []string
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a single issue by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid issue id %q", args[0])
			}
			issue, err := rc.client.GetIssue(rc.ctx(), id, &api.GetIssueParams{Include: includes})
			if err != nil {
				return err
			}
			return renderIssueDetail(rc, issue)
		},
	}
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Include extras: journals, attachments, relations, children")
	return cmd
}

func renderIssueList(rc *runCtx, issues []api.Issue) error {
	if rc.format == "markdown" {
		rows := make([][]string, len(issues))
		for i, is := range issues {
			rows[i] = []string{
				fmt.Sprintf("#%d", is.ID),
				is.Project.Name,
				is.Tracker.Name,
				is.Status.Name,
				is.Subject,
			}
		}
		return output.MarkdownTable(rc.out, []string{"ID", "Project", "Tracker", "Status", "Subject"}, rows)
	}
	return output.JSON(rc.out, map[string]any{"issues": issues})
}

func newIssuesCreateCmd(rc *runCtx) *cobra.Command {
	var (
		project, subject, description, assignee, startDate, dueDate string
		tracker, status, priority, parent, done                     int
		confirm                                                     bool
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new issue (requires --confirm to send)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if project == "" {
				return fmt.Errorf("--project is required")
			}
			if tracker <= 0 {
				return fmt.Errorf("--tracker is required")
			}
			if subject == "" {
				return fmt.Errorf("--subject is required")
			}
			if startDate != "" && !dateRE.MatchString(startDate) {
				return fmt.Errorf("--start-date must be YYYY-MM-DD")
			}
			if dueDate != "" && !dateRE.MatchString(dueDate) {
				return fmt.Errorf("--due-date must be YYYY-MM-DD")
			}
			if cmd.Flags().Changed("done") && (done < 0 || done > 100) {
				return fmt.Errorf("--done must be between 0 and 100")
			}

			payload := api.IssueCreate{
				ProjectID:     project,
				TrackerID:     tracker,
				Subject:       subject,
				Description:   description,
				StatusID:      status,
				PriorityID:    priority,
				AssignedToID:  assignee,
				ParentIssueID: parent,
				StartDate:     startDate,
				DueDate:       dueDate,
				DoneRatio:     done,
			}
			body := map[string]any{"issue": payload}
			if !confirm {
				return renderDryRun(rc, "POST", "/issues.json", body)
			}
			issue, err := rc.client.CreateIssue(rc.ctx(), payload)
			if err != nil {
				return err
			}
			return renderIssueDetail(rc, issue)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project identifier or ID (required)")
	cmd.Flags().IntVar(&tracker, "tracker", 0, "Tracker ID (required)")
	cmd.Flags().StringVar(&subject, "subject", "", "Subject (required)")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Assignee user ID or 'me'")
	cmd.Flags().IntVar(&status, "status", 0, "Status ID")
	cmd.Flags().IntVar(&priority, "priority", 0, "Priority ID")
	cmd.Flags().IntVar(&parent, "parent", 0, "Parent issue ID")
	cmd.Flags().StringVar(&startDate, "start-date", "", "Start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&dueDate, "due-date", "", "Due date (YYYY-MM-DD)")
	cmd.Flags().IntVar(&done, "done", 0, "Done ratio (0-100)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually send the request (without this flag the command runs in dry-run mode)")
	return cmd
}

func renderIssueDetail(rc *runCtx, is *api.Issue) error {
	if rc.format == "markdown" {
		var b strings.Builder
		fmt.Fprintf(&b, "# #%d %s\n\n", is.ID, is.Subject)
		fmt.Fprintf(&b, "- **Project:** %s\n", is.Project.Name)
		fmt.Fprintf(&b, "- **Tracker:** %s\n", is.Tracker.Name)
		fmt.Fprintf(&b, "- **Status:** %s\n", is.Status.Name)
		fmt.Fprintf(&b, "- **Priority:** %s\n", is.Priority.Name)
		fmt.Fprintf(&b, "- **Author:** %s\n", is.Author.Name)
		if is.AssignedTo != nil {
			fmt.Fprintf(&b, "- **Assigned to:** %s\n", is.AssignedTo.Name)
		}
		if is.DueDate != "" {
			fmt.Fprintf(&b, "- **Due:** %s\n", is.DueDate)
		}
		fmt.Fprintf(&b, "\n%s\n", is.Description)
		if len(is.Journals) > 0 {
			fmt.Fprintf(&b, "\n## Journals\n\n")
			for _, j := range is.Journals {
				if j.Notes == "" {
					continue
				}
				fmt.Fprintf(&b, "**%s** (%s):\n%s\n\n", j.User.Name, j.CreatedOn, j.Notes)
			}
		}
		_, err := fmt.Fprint(rc.out, b.String())
		return err
	}
	return output.JSON(rc.out, map[string]any{"issue": is})
}
