package commands

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

// parseCustomFields converts repeated --cf "id=value" strings into typed
// CustomFieldValue entries. It rejects malformed inputs and duplicate IDs.
func parseCustomFields(specs []string) ([]api.CustomFieldValue, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make([]api.CustomFieldValue, 0, len(specs))
	seen := make(map[int]bool, len(specs))
	for _, s := range specs {
		parts := strings.SplitN(s, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("--cf %q: expected id=value", s)
		}
		idStr := parts[0]
		if idStr == "" {
			return nil, fmt.Errorf("--cf %q: id is empty", s)
		}
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return nil, fmt.Errorf("--cf %q: id %q is not an integer", s, idStr)
		}
		if id <= 0 {
			return nil, fmt.Errorf("--cf %q: id must be positive", s)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate --cf id %d", id)
		}
		seen[id] = true
		out = append(out, api.CustomFieldValue{ID: id, Value: parts[1]})
	}
	return out, nil
}

// dateRE validates YYYY-MM-DD strings used in --start-date / --due-date.
var dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func newIssuesCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "issues", Short: "Issue operations"}
	c.AddCommand(newIssuesListCmd(rc))
	c.AddCommand(newIssuesGetCmd(rc))
	c.AddCommand(newIssuesCreateCmd(rc))
	c.AddCommand(newIssuesUpdateCmd(rc))
	return c
}

func newIssuesListCmd(rc *runCtx) *cobra.Command {
	var (
		project, status, assignee, updatedSince, sort string
		limit, offset, queryID                        int
		all                                           bool
		includes                                      []string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p := api.ListIssuesParams{
				ProjectID:  project,
				StatusID:   status,
				AssignedTo: assignee,
				Sort:       sort,
				Include:    includes,
				QueryID:    queryID,
			}
			if updatedSince != "" {
				p.UpdatedOn = ">=" + updatedSince
			}
			ctx := rc.ctx()
			collected, err := collectPages(limit, offset, all, func(limit, offset int) ([]api.Issue, int, error) {
				p.Limit = limit
				p.Offset = offset
				res, err := rc.client.ListIssues(ctx, p)
				if err != nil {
					return nil, 0, err
				}
				return res.Issues, res.TotalCount, nil
			})
			if err != nil {
				return err
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
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages (ignores --limit/--offset; capped at 1000 results)")
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Include extras: attachments, relations")
	cmd.Flags().IntVar(&queryID, "query-id", 0, "Apply saved query ID; other filters are ignored by the server when set")
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
		project, subject, description, assignee, category, startDate, dueDate string
		tracker, status, priority, parent, done                               int
		confirm                                                               bool
		cfStrs, attachStrs                                                    []string
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
			cfs, err := parseCustomFields(cfStrs)
			if err != nil {
				return err
			}
			specs, err := parseAttachSpecs(attachStrs)
			if err != nil {
				return err
			}

			payload := api.IssueCreate{
				ProjectID:     project,
				TrackerID:     tracker,
				Subject:       subject,
				Description:   description,
				StatusID:      status,
				PriorityID:    priority,
				AssignedToID:  assignee,
				CategoryID:    category,
				ParentIssueID: parent,
				StartDate:     startDate,
				DueDate:       dueDate,
				DoneRatio:     done,
				CustomFields:  cfs,
			}
			if err := applyAttachments(rc.ctx(), rc.client, specs, &payload.Uploads, confirm); err != nil {
				return err
			}
			if !confirm {
				body := map[string]any{"issue": payload}
				return renderDryRun(rc, "POST", "/issues.json", body, specs)
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
	cmd.Flags().StringVar(&category, "category", "", "Issue category ID (see: categories list)")
	cmd.Flags().IntVar(&status, "status", 0, "Status ID")
	cmd.Flags().IntVar(&priority, "priority", 0, "Priority ID")
	cmd.Flags().IntVar(&parent, "parent", 0, "Parent issue ID")
	cmd.Flags().StringVar(&startDate, "start-date", "", "Start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&dueDate, "due-date", "", "Due date (YYYY-MM-DD)")
	cmd.Flags().IntVar(&done, "done", 0, "Done ratio (0-100)")
	cmd.Flags().StringSliceVar(&cfStrs, "cf", nil, "Custom field id=value (repeatable)")
	cmd.Flags().StringArrayVar(&attachStrs, "attach", nil, attachFlagHelp)
	addConfirmFlag(cmd, &confirm)
	return cmd
}

func newIssuesUpdateCmd(rc *runCtx) *cobra.Command {
	var (
		subject, description, assignee, notes, notesFile, startDate, dueDate string
		status, priority, done                                               int
		confirm                                                              bool
		cfStrs, attachStrs                                                   []string
	)
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an existing issue (requires --confirm to send)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid issue id %q", args[0])
			}

			mutating := []string{"subject", "description", "status", "assignee", "priority", "done", "notes", "notes-file", "start-date", "due-date", "cf", "attach"}
			anySet := false
			for _, n := range mutating {
				if cmd.Flags().Changed(n) {
					anySet = true
					break
				}
			}
			if !anySet {
				return fmt.Errorf("at least one of %s must be provided", strings.Join(mutating, ", "))
			}

			if cmd.Flags().Changed("notes") && cmd.Flags().Changed("notes-file") {
				return fmt.Errorf("--notes and --notes-file are mutually exclusive")
			}
			if cmd.Flags().Changed("notes-file") {
				data, err := os.ReadFile(notesFile)
				if err != nil {
					return fmt.Errorf("reading --notes-file %q: %w", notesFile, err)
				}
				notes = string(data)
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
			cfs, err := parseCustomFields(cfStrs)
			if err != nil {
				return err
			}
			specs, err := parseAttachSpecs(attachStrs)
			if err != nil {
				return err
			}

			var payload api.IssueUpdate
			if cmd.Flags().Changed("subject") {
				payload.Subject = &subject
			}
			if cmd.Flags().Changed("description") {
				payload.Description = &description
			}
			if cmd.Flags().Changed("status") {
				payload.StatusID = &status
			}
			if cmd.Flags().Changed("priority") {
				payload.PriorityID = &priority
			}
			if cmd.Flags().Changed("assignee") {
				payload.AssignedToID = &assignee
			}
			if cmd.Flags().Changed("done") {
				payload.DoneRatio = &done
			}
			if cmd.Flags().Changed("notes") || cmd.Flags().Changed("notes-file") {
				payload.Notes = &notes
			}
			if cmd.Flags().Changed("start-date") {
				payload.StartDate = &startDate
			}
			if cmd.Flags().Changed("due-date") {
				payload.DueDate = &dueDate
			}
			if len(cfs) > 0 {
				payload.CustomFields = cfs
			}

			path := fmt.Sprintf("/issues/%d.json", id)
			if err := applyAttachments(rc.ctx(), rc.client, specs, &payload.Uploads, confirm); err != nil {
				return err
			}
			if !confirm {
				body := map[string]any{"issue": payload}
				return renderDryRun(rc, "PUT", path, body, specs)
			}
			if err := rc.client.UpdateIssue(rc.ctx(), id, payload); err != nil {
				return err
			}
			// Redmine returns 204 on update; emit a small confirmation.
			if rc.format == "markdown" {
				_, err := fmt.Fprintf(rc.out, "Updated issue #%d.\n", id)
				return err
			}
			return output.JSON(rc.out, map[string]any{"updated": true, "id": id})
		},
	}
	cmd.Flags().StringVar(&subject, "subject", "", "New subject")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().IntVar(&status, "status", 0, "New status ID")
	cmd.Flags().StringVar(&assignee, "assignee", "", "New assignee user ID or 'me'")
	cmd.Flags().IntVar(&priority, "priority", 0, "New priority ID")
	cmd.Flags().IntVar(&done, "done", 0, "Done ratio (0-100)")
	cmd.Flags().StringVar(&notes, "notes", "", "Add a journal note")
	cmd.Flags().StringVar(&notesFile, "notes-file", "", "Read journal note from file (mutually exclusive with --notes)")
	cmd.Flags().StringVar(&startDate, "start-date", "", "New start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&dueDate, "due-date", "", "New due date (YYYY-MM-DD)")
	cmd.Flags().StringSliceVar(&cfStrs, "cf", nil, "Custom field id=value (repeatable)")
	cmd.Flags().StringArrayVar(&attachStrs, "attach", nil, attachFlagHelp)
	addConfirmFlag(cmd, &confirm)
	return cmd
}

func renderIssueDetail(rc *runCtx, is *api.Issue) error {
	if rc.format == "markdown" {
		// Every value below is server-controlled and can contain attacker-
		// chosen ANSI escapes (OSC 52 clipboard write, screen clear, hyperlink
		// spoofing). Strip control bytes before they reach the terminal.
		clean := output.SanitizeForTerminal
		var b strings.Builder
		fmt.Fprintf(&b, "# #%d %s\n\n", is.ID, clean(is.Subject))
		fmt.Fprintf(&b, "- **Project:** %s\n", clean(is.Project.Name))
		fmt.Fprintf(&b, "- **Tracker:** %s\n", clean(is.Tracker.Name))
		fmt.Fprintf(&b, "- **Status:** %s\n", clean(is.Status.Name))
		fmt.Fprintf(&b, "- **Priority:** %s\n", clean(is.Priority.Name))
		fmt.Fprintf(&b, "- **Author:** %s\n", clean(is.Author.Name))
		if is.AssignedTo != nil {
			fmt.Fprintf(&b, "- **Assigned to:** %s\n", clean(is.AssignedTo.Name))
		}
		if is.DueDate != "" {
			fmt.Fprintf(&b, "- **Due:** %s\n", clean(is.DueDate))
		}
		fmt.Fprintf(&b, "\n%s\n", clean(is.Description))
		if len(is.Journals) > 0 {
			fmt.Fprintf(&b, "\n## Journals\n\n")
			for _, j := range is.Journals {
				if j.Notes == "" {
					continue
				}
				fmt.Fprintf(&b, "**%s** (%s):\n%s\n\n", clean(j.User.Name), clean(j.CreatedOn), clean(j.Notes))
			}
		}
		if len(is.Attachments) > 0 {
			fmt.Fprintf(&b, "\n## Attachments\n\n")
			for _, a := range is.Attachments {
				fmt.Fprintf(&b, "- **#%d** %s", a.ID, clean(a.Filename))
				if a.Description != "" {
					fmt.Fprintf(&b, " — %s", clean(a.Description))
				}
				fmt.Fprintf(&b, " (%s)\n", clean(a.ContentURL))
			}
		}
		_, err := fmt.Fprint(rc.out, b.String())
		return err
	}
	return output.JSON(rc.out, map[string]any{"issue": is})
}
