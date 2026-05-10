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

// paginateAllPageSize is the internal page size used when --all is set on
// list commands. paginateAllCap is the maximum allowed total_count; if the
// server reports more than this many results, --all aborts with an error
// asking the caller to narrow their filters.
const (
	paginateAllPageSize = 100
	paginateAllCap      = 1000
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
		limit, offset                                 int
		all                                           bool
		includes                                      []string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List issues",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			if all {
				// In --all mode, ignore --limit/--offset and fetch
				// every page using a fixed internal page size.
				p.Limit = paginateAllPageSize
				p.Offset = 0
			} else if p.Limit == 0 {
				p.Limit = 25
			}

			ctx := rc.ctx()
			var collected []api.Issue
			for {
				res, err := rc.client.ListIssues(ctx, p)
				if err != nil {
					return err
				}
				if all && res.TotalCount > paginateAllCap {
					return fmt.Errorf("more than %d results (%d); narrow your filters or omit --all", paginateAllCap, res.TotalCount)
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
	cmd.Flags().BoolVar(&all, "all", false, "Fetch all pages (ignores --limit/--offset; capped at 1000 results)")
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
		cfStrs, attachStrs                                          []string
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
				ParentIssueID: parent,
				StartDate:     startDate,
				DueDate:       dueDate,
				DoneRatio:     done,
				CustomFields:  cfs,
			}
			if !confirm {
				payload.Uploads = attachDryRun(specs)
				body := map[string]any{"issue": payload}
				return renderDryRun(rc, "POST", "/issues.json", body, specs)
			}
			if err := preflightAttachSpecs(specs); err != nil {
				return err
			}
			refs, err := uploadAttachments(rc.ctx(), rc.client, specs)
			if err != nil {
				return err
			}
			payload.Uploads = refs
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
	cmd.Flags().StringSliceVar(&cfStrs, "cf", nil, "Custom field id=value (repeatable)")
	cmd.Flags().StringArrayVar(&attachStrs, "attach", nil, attachFlagHelp)
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually send the request (without this flag the command runs in dry-run mode)")
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
			if !confirm {
				payload.Uploads = attachDryRun(specs)
				body := map[string]any{"issue": payload}
				return renderDryRun(rc, "PUT", path, body, specs)
			}
			if err := preflightAttachSpecs(specs); err != nil {
				return err
			}
			refs, err := uploadAttachments(rc.ctx(), rc.client, specs)
			if err != nil {
				return err
			}
			payload.Uploads = refs
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
