package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

// ErrStatusNotAllowed is returned by the issues update preflight when the
// requested status is not among the transitions the user's role may make on
// the issue (include=allowed_statuses). Redmine would accept the PUT with
// HTTP 200 and silently keep the old status, so the CLI refuses before
// sending anything. exitCodeFor maps it to exit 1 on purpose: it is a
// validation-type failure, not a server refusal (4) and not a local policy
// mode (8).
var ErrStatusNotAllowed = errors.New("status transition not allowed by the project workflow")

// ErrUpdateNotApplied is returned after a confirmed issues update when the
// re-read issue shows that the server accepted the PUT but dropped one or
// more of the sent fields (e.g. a status the workflow forbids, a user who is
// not assignable). The success line is still printed, since notes and the
// remaining fields were saved. exitCodeFor maps it to exit 1.
var ErrUpdateNotApplied = errors.New("server accepted the update but did not apply")

// preflightStatus fetches the issue with include=allowed_statuses and
// returns ErrStatusNotAllowed when want is not an allowed transition. Servers
// that do not return allowed_statuses (Redmine < 5.0) skip the check.
func preflightStatus(rc *runCtx, id, want int) error {
	issue, err := rc.client.GetIssue(rc.ctx(), id, &api.GetIssueParams{Include: []string{"allowed_statuses"}})
	if err != nil {
		return err
	}
	if issue.AllowedStatuses == nil {
		return nil
	}
	for _, s := range issue.AllowedStatuses {
		if s.ID == want {
			return nil
		}
	}
	clean := output.SanitizeForTerminal
	current := fmt.Sprintf("%s (%d)", clean(issue.Status.Name), issue.Status.ID)
	if len(issue.AllowedStatuses) == 0 {
		return fmt.Errorf("%w: your role has no status transitions on issue #%d (current status: %s, tracker: %s)",
			ErrStatusNotAllowed, id, current, clean(issue.Tracker.Name))
	}
	allowed := make([]string, 0, len(issue.AllowedStatuses))
	for _, s := range issue.AllowedStatuses {
		allowed = append(allowed, fmt.Sprintf("%s (%d)", clean(s.Name), s.ID))
	}
	return fmt.Errorf("%w: status %d is not an allowed transition for issue #%d (current status: %s); allowed: %s",
		ErrStatusNotAllowed, want, id, current, strings.Join(allowed, ", "))
}

// fieldCheck is the outcome of comparing one sent field against the issue
// as the server returns it after the update.
type fieldCheck struct {
	Name      string // flag-style name: status, assignee, priority, ...
	Requested string // what was sent, for the warning
	Actual    string // what the server has now, for the warning and summary
	Applied   bool
	Hint      string // optional explanation appended to the warning
}

// verifyUpdate compares every field sent in payload with the issue as
// re-read from the server. Notes are not compared: they land in a journal
// and are not part of the issue body. The "me" assignee is resolved via
// GetCurrentUser only when it was sent; if that lookup fails the assignee
// check is skipped with a warning rather than reported as not applied.
func verifyUpdate(rc *runCtx, payload api.IssueUpdate, fresh *api.Issue) []fieldCheck {
	var checks []fieldCheck
	clean := output.SanitizeForTerminal
	idName := func(v *api.IDName) string {
		if v == nil {
			return "none"
		}
		return fmt.Sprintf("%s (%d)", clean(v.Name), v.ID)
	}
	dateOrNone := func(s string) string {
		if s == "" {
			return "none"
		}
		return s
	}

	if payload.Subject != nil {
		checks = append(checks, fieldCheck{
			Name: "subject", Requested: strconv.Quote(*payload.Subject),
			Actual: strconv.Quote(clean(fresh.Subject)), Applied: sameText(fresh.Subject, *payload.Subject),
		})
	}
	if payload.Description != nil {
		checks = append(checks, fieldCheck{
			Name: "description", Requested: fmt.Sprintf("%d bytes", len(*payload.Description)),
			Actual: fmt.Sprintf("%d bytes", len(fresh.Description)), Applied: sameText(fresh.Description, *payload.Description),
		})
	}
	if payload.StatusID != nil {
		checks = append(checks, fieldCheck{
			Name: "status", Requested: strconv.Itoa(*payload.StatusID),
			Actual:  fmt.Sprintf("%s (%d)", clean(fresh.Status.Name), fresh.Status.ID),
			Applied: fresh.Status.ID == *payload.StatusID,
			Hint:    "your role may not be allowed to make this transition in the project workflow",
		})
	}
	if payload.AssignedToID != nil {
		want := *payload.AssignedToID
		check := fieldCheck{Name: "assignee", Requested: want, Actual: idName(fresh.AssignedTo),
			Hint: "the user may not be assignable in this project"}
		switch want {
		case "":
			check.Applied = fresh.AssignedTo == nil
			check.Requested = "none"
			checks = append(checks, check)
		case "me":
			me, err := rc.client.GetCurrentUser(rc.ctx())
			if err != nil {
				fmt.Fprintf(rc.errOut, "warning: could not verify assignee: resolving 'me' failed: %v\n", err)
			} else {
				check.Requested = fmt.Sprintf("me (%d)", me.ID)
				check.Applied = fresh.AssignedTo != nil && fresh.AssignedTo.ID == me.ID
				checks = append(checks, check)
			}
		default:
			id, err := strconv.Atoi(want)
			check.Applied = err == nil && fresh.AssignedTo != nil && fresh.AssignedTo.ID == id
			checks = append(checks, check)
		}
	}
	if payload.PriorityID != nil {
		checks = append(checks, fieldCheck{
			Name: "priority", Requested: strconv.Itoa(*payload.PriorityID),
			Actual:  fmt.Sprintf("%s (%d)", clean(fresh.Priority.Name), fresh.Priority.ID),
			Applied: fresh.Priority.ID == *payload.PriorityID,
		})
	}
	if payload.CategoryID != nil {
		want := *payload.CategoryID
		check := fieldCheck{Name: "category", Requested: want, Actual: idName(fresh.Category)}
		if want == "" {
			check.Requested = "none"
			check.Applied = fresh.Category == nil
		} else {
			id, err := strconv.Atoi(want)
			check.Applied = err == nil && fresh.Category != nil && fresh.Category.ID == id
		}
		checks = append(checks, check)
	}
	if payload.DoneRatio != nil {
		checks = append(checks, fieldCheck{
			Name: "done", Requested: fmt.Sprintf("%d%%", *payload.DoneRatio),
			Actual: fmt.Sprintf("%d%%", fresh.DoneRatio), Applied: fresh.DoneRatio == *payload.DoneRatio,
		})
	}
	if payload.StartDate != nil {
		checks = append(checks, fieldCheck{
			Name: "start", Requested: dateOrNone(*payload.StartDate),
			Actual: dateOrNone(clean(fresh.StartDate)), Applied: fresh.StartDate == *payload.StartDate,
		})
	}
	if payload.DueDate != nil {
		checks = append(checks, fieldCheck{
			Name: "due", Requested: dateOrNone(*payload.DueDate),
			Actual: dateOrNone(clean(fresh.DueDate)), Applied: fresh.DueDate == *payload.DueDate,
		})
	}
	for _, cf := range payload.CustomFields {
		want := customFieldValueString(cf.Value)
		check := fieldCheck{Name: fmt.Sprintf("cf %d", cf.ID), Requested: strconv.Quote(want), Actual: "(absent)"}
		for _, got := range fresh.CustomFields {
			if got.ID != cf.ID {
				continue
			}
			if got.Name != "" {
				check.Name = clean(got.Name)
			}
			actual := customFieldValueString(got.Value)
			check.Actual = strconv.Quote(clean(actual))
			check.Applied = actual == want
			break
		}
		checks = append(checks, check)
	}
	return checks
}

// sameText compares free-text fields tolerantly: Redmine strips surrounding
// whitespace and may normalize CRLF to LF on save, and neither is a dropped
// field.
func sameText(got, want string) bool {
	norm := func(s string) string {
		return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	}
	return norm(got) == norm(want)
}

// reportUpdate prints the success line (markdown) or JSON document for a
// confirmed update plus one stderr warning per field that did not take, and
// returns ErrUpdateNotApplied when any field was dropped.
func reportUpdate(rc *runCtx, id int, fresh *api.Issue, checks []fieldCheck) error {
	var notApplied []string
	for _, c := range checks {
		if c.Applied {
			continue
		}
		notApplied = append(notApplied, c.Name)
		msg := fmt.Sprintf("warning: %s was not applied by the server: requested %s, issue has %s", c.Name, c.Requested, c.Actual)
		if c.Hint != "" {
			msg += "; " + c.Hint
		}
		fmt.Fprintln(rc.errOut, msg)
	}

	if rc.format == "markdown" {
		parts := make([]string, 0, len(checks))
		for _, c := range checks {
			parts = append(parts, c.Name+": "+c.Actual)
		}
		if len(parts) == 0 {
			fmt.Fprintf(rc.out, "Updated issue #%d.\n", id)
		} else {
			fmt.Fprintf(rc.out, "Updated issue #%d (%s).\n", id, strings.Join(parts, ", "))
		}
	} else {
		doc := map[string]any{"updated": true, "id": id, "issue": fresh}
		if len(notApplied) > 0 {
			doc["not_applied"] = notApplied
		}
		if err := output.JSON(rc.out, doc); err != nil {
			return err
		}
	}
	if len(notApplied) > 0 {
		return fmt.Errorf("%w: %s", ErrUpdateNotApplied, strings.Join(notApplied, ", "))
	}
	return nil
}
