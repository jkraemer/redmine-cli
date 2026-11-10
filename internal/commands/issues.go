package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
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
	c.AddCommand(newIssuesWatchersCmd(rc))
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
			// Checklists come from the redmine_checklists plugin's own
			// endpoint, not from Redmine's include param.
			serverIncludes, withChecklists := splitChecklistsInclude(includes)
			issue, err := rc.client.GetIssue(rc.ctx(), id, &api.GetIssueParams{Include: serverIncludes})
			if err != nil {
				return err
			}
			if withChecklists {
				if issue.Checklists, err = rc.client.ListChecklists(rc.ctx(), id); err != nil {
					return err
				}
			}
			return renderIssueDetail(rc, issue)
		},
	}
	cmd.Flags().StringSliceVar(&includes, "include", nil, "Include extras: journals, attachments, relations, children, changesets, watchers, allowed_statuses, checklists (requires the redmine_checklists plugin)")
	return cmd
}

// splitChecklistsInclude removes "checklists" from includes and reports
// whether it was present.
func splitChecklistsInclude(includes []string) ([]string, bool) {
	var rest []string
	found := false
	for _, inc := range includes {
		if inc == "checklists" {
			found = true
			continue
		}
		rest = append(rest, inc)
	}
	return rest, found
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
		watchers                                                              []int
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
				ProjectID:      project,
				TrackerID:      tracker,
				Subject:        subject,
				Description:    description,
				StatusID:       status,
				PriorityID:     priority,
				AssignedToID:   assignee,
				CategoryID:     category,
				ParentIssueID:  parent,
				StartDate:      startDate,
				DueDate:        dueDate,
				DoneRatio:      done,
				CustomFields:   cfs,
				WatcherUserIDs: watchers,
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
	cmd.Flags().IntSliceVar(&watchers, "watcher", nil, "Watcher user ID (repeatable)")
	cmd.Flags().StringSliceVar(&cfStrs, "cf", nil, "Custom field id=value (repeatable)")
	cmd.Flags().StringArrayVar(&attachStrs, "attach", nil, attachFlagHelp)
	addConfirmFlag(cmd, &confirm)
	return cmd
}

func newIssuesUpdateCmd(rc *runCtx) *cobra.Command {
	var (
		subject, description, assignee, category, notes, notesFile, startDate, dueDate string
		status, priority, done                                                         int
		confirm                                                                        bool
		cfStrs, attachStrs                                                             []string
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

			mutating := []string{"subject", "description", "status", "assignee", "category", "priority", "done", "notes", "notes-file", "start-date", "due-date", "cf", "attach"}
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
			if cmd.Flags().Changed("category") {
				payload.CategoryID = &category
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
	cmd.Flags().StringVar(&category, "category", "", "New category ID; pass \"\" to clear (see: categories list)")
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

// customFieldValueString renders a custom field's value for display.
// Values arrive as JSON scalars or, for multi-value fields, arrays;
// arrays are joined with ", ". Unset fields ("" or null) yield "".
func customFieldValueString(v any) string {
	switch vv := v.(type) {
	case nil:
		return ""
	case []any:
		parts := make([]string, 0, len(vv))
		for _, e := range vv {
			parts = append(parts, fmt.Sprintf("%v", e))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprintf("%v", vv)
	}
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
		if is.Category != nil {
			fmt.Fprintf(&b, "- **Category:** %s\n", clean(is.Category.Name))
		}
		fmt.Fprintf(&b, "- **Author:** %s\n", clean(is.Author.Name))
		if is.AssignedTo != nil {
			fmt.Fprintf(&b, "- **Assigned to:** %s\n", clean(is.AssignedTo.Name))
		}
		if is.Parent != nil {
			fmt.Fprintf(&b, "- **Parent:** #%d\n", is.Parent.ID)
		}
		if is.FixedVersion != nil {
			fmt.Fprintf(&b, "- **Target version:** %s\n", clean(is.FixedVersion.Name))
		}
		if is.StartDate != "" {
			fmt.Fprintf(&b, "- **Start:** %s\n", clean(is.StartDate))
		}
		if is.DueDate != "" {
			fmt.Fprintf(&b, "- **Due:** %s\n", clean(is.DueDate))
		}
		fmt.Fprintf(&b, "- **Done:** %d%%\n", is.DoneRatio)
		if h := hoursString(is.EstimatedHours, is.TotalEstimatedHours); h != "" {
			fmt.Fprintf(&b, "- **Estimated time:** %s\n", h)
		}
		if h := hoursString(is.SpentHours, is.TotalSpentHours); h != "" {
			fmt.Fprintf(&b, "- **Spent time:** %s\n", h)
		}
		if is.IsPrivate {
			fmt.Fprintf(&b, "- **Private:** yes\n")
		}
		if is.CreatedOn != "" {
			fmt.Fprintf(&b, "- **Created:** %s\n", clean(is.CreatedOn))
		}
		if is.UpdatedOn != "" {
			fmt.Fprintf(&b, "- **Updated:** %s\n", clean(is.UpdatedOn))
		}
		if is.ClosedOn != "" {
			fmt.Fprintf(&b, "- **Closed:** %s\n", clean(is.ClosedOn))
		}
		for _, cf := range is.CustomFields {
			if val := customFieldValueString(cf.Value); val != "" {
				fmt.Fprintf(&b, "- **%s:** %s\n", clean(cf.Name), clean(val))
			}
		}
		if len(is.AllowedStatuses) > 0 {
			names := make([]string, len(is.AllowedStatuses))
			for i, st := range is.AllowedStatuses {
				names[i] = clean(st.Name)
			}
			fmt.Fprintf(&b, "- **Allowed statuses:** %s\n", strings.Join(names, ", "))
		}
		fmt.Fprintf(&b, "\n%s\n", clean(is.Description))
		if len(is.Children) > 0 {
			fmt.Fprintf(&b, "\n## Subtasks\n\n")
			writeSubtasks(&b, is.Children, "")
		}
		if len(is.Relations) > 0 {
			fmt.Fprintf(&b, "\n## Relations\n\n")
			for _, r := range is.Relations {
				forward := r.IssueID == is.ID
				other := r.IssueToID
				if !forward {
					other = r.IssueID
				}
				fmt.Fprintf(&b, "- %s #%d", clean(relationLabel(r.RelationType, forward)), other)
				if r.Delay != nil {
					fmt.Fprintf(&b, " (delay: %d days)", *r.Delay)
				}
				b.WriteString("\n")
			}
		}
		if len(is.Checklists) > 0 {
			fmt.Fprintf(&b, "\n## Checklist\n\n")
			for _, item := range is.Checklists {
				mark := " "
				if item.IsDone {
					mark = "x"
				}
				fmt.Fprintf(&b, "- [%s] %s\n", mark, clean(item.Subject))
			}
		}
		if len(is.Journals) > 0 {
			fmt.Fprintf(&b, "\n## Journals\n\n")
			for _, j := range is.Journals {
				if j.Notes == "" {
					continue
				}
				private := ""
				if j.PrivateNotes {
					private = ", private"
				}
				fmt.Fprintf(&b, "**%s** (%s%s):\n%s\n\n", clean(j.User.Name), clean(j.CreatedOn), private, clean(j.Notes))
			}
		}
		if len(is.Changesets) > 0 {
			fmt.Fprintf(&b, "\n## Changesets\n\n")
			for _, cs := range is.Changesets {
				fmt.Fprintf(&b, "- %s", clean(cs.Revision))
				if cs.User != nil {
					fmt.Fprintf(&b, " by %s", clean(cs.User.Name))
				}
				if cs.CommittedOn != "" {
					fmt.Fprintf(&b, " (%s)", clean(cs.CommittedOn))
				}
				if cs.Comments != "" {
					fmt.Fprintf(&b, ": %s", clean(cs.Comments))
				}
				b.WriteString("\n")
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
		if len(is.Watchers) > 0 {
			fmt.Fprintf(&b, "\n## Watchers\n\n")
			for _, w := range is.Watchers {
				fmt.Fprintf(&b, "- %s\n", clean(w.Name))
			}
		}
		writeExtraFields(&b, is.Extra)
		_, err := fmt.Fprint(rc.out, b.String())
		return err
	}
	return output.JSON(rc.out, map[string]any{"issue": is})
}

// hoursString formats an issue's own and total hours as "2.5 h" or
// "2.5 h (total 4 h)" when the total (including subtasks) differs, rounded
// to two decimals like Redmine's UI. It returns "" when the hours are not
// set.
func hoursString(own, total *float64) string {
	if own == nil {
		return ""
	}
	s := formatHours(*own)
	if total != nil {
		if t := formatHours(*total); t != s {
			s += " (total " + t + ")"
		}
	}
	return s
}

// formatHours renders hours rounded to two decimals, without trailing zeros.
func formatHours(h float64) string {
	return strconv.FormatFloat(math.Round(h*100)/100, 'f', -1, 64) + " h"
}

// writeSubtasks writes an issue's children tree as a nested list.
func writeSubtasks(b *strings.Builder, children []api.IssueChild, indent string) {
	clean := output.SanitizeForTerminal
	for _, c := range children {
		fmt.Fprintf(b, "%s- #%d ", indent, c.ID)
		if c.Tracker != nil {
			fmt.Fprintf(b, "%s: ", clean(c.Tracker.Name))
		}
		fmt.Fprintf(b, "%s\n", clean(c.Subject))
		writeSubtasks(b, c.Children, indent+"  ")
	}
}

// relationLabels maps a Redmine relation type to its wording from the
// relation's source issue (forward) and from its target issue (inverse).
var relationLabels = map[string][2]string{
	"relates":    {"related to", "related to"},
	"duplicates": {"duplicates", "duplicated by"},
	"blocks":     {"blocks", "blocked by"},
	"precedes":   {"precedes", "follows"},
	"copied_to":  {"copied to", "copied from"},
}

// relationLabel words a relation from the viewed issue's side: forward
// when the viewed issue is the relation's source (issue_id).
func relationLabel(relationType string, forward bool) string {
	labels, ok := relationLabels[relationType]
	switch {
	case ok && forward:
		return labels[0]
	case ok:
		return labels[1]
	case forward:
		return relationType
	default:
		return relationType + " (inverse)"
	}
}

// writeExtraFields renders the non-standard issue keys (see api.Issue.Extra)
// as an "Other fields" list, sorted by key. Null and empty values are
// skipped.
func writeExtraFields(b *strings.Builder, extra map[string]json.RawMessage) {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		if val := extraValueString(extra[k]); val != "" {
			lines = append(lines, fmt.Sprintf("- **%s:** %s\n", output.SanitizeForTerminal(humanizeKey(k)), output.SanitizeForTerminal(val)))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## Other fields\n\n%s", strings.Join(lines, ""))
}

// humanizeKey turns a JSON key like "customer_company" into "Customer company".
func humanizeKey(k string) string {
	s := strings.ReplaceAll(k, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// extraValueString renders a raw JSON value for markdown: scalars as-is, an
// object with a "name" by its name, a list of those comma-separated, and
// anything else as compact JSON.
func extraValueString(raw json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	if s, ok := simpleValueString(v); ok {
		return s
	}
	if list, ok := v.([]any); ok {
		parts := make([]string, 0, len(list))
		for _, e := range list {
			s, ok := simpleValueString(e)
			if !ok {
				parts = nil
				break
			}
			parts = append(parts, s)
		}
		if parts != nil {
			return strings.Join(parts, ", ")
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return string(raw)
	}
	return compact.String()
}

// simpleValueString renders a scalar, or an object carrying a string
// "name", as text. It reports false for any other value.
func simpleValueString(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	case map[string]any:
		if name, ok := x["name"].(string); ok {
			return name, true
		}
	}
	return "", false
}
