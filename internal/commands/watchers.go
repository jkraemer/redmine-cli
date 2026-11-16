package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/output"
)

func newIssuesWatchersCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "watchers", Short: "Manage issue watchers"}
	c.AddCommand(newWatchersAddCmd(rc))
	c.AddCommand(newWatchersRemoveCmd(rc))
	return c
}

// watcherArgs parses the <issue-id> <user-id> argument pair shared by
// add and remove.
func watcherArgs(args []string) (issueID, userID int, err error) {
	if issueID, err = positiveID(args[0], "issue"); err != nil {
		return 0, 0, err
	}
	if userID, err = positiveID(args[1], "user"); err != nil {
		return 0, 0, err
	}
	return issueID, userID, nil
}

func newWatchersAddCmd(rc *runCtx) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "add <issue-id> <user-id>",
		Short: "Add a watcher to an issue (requires --confirm to send)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			issueID, userID, err := watcherArgs(args)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/issues/%d/watchers.json", issueID)
			if !confirm {
				return renderDryRun(rc, "POST", path, map[string]any{"user_id": userID}, nil)
			}
			if err := rc.client.AddWatcher(rc.ctx(), issueID, userID); err != nil {
				return err
			}
			if rc.format == "markdown" {
				_, err := fmt.Fprintf(rc.out, "Added user %d as watcher on issue #%d.\n", userID, issueID)
				return err
			}
			return output.JSON(rc.out, map[string]any{"added": true, "issue_id": issueID, "user_id": userID})
		},
	}
	addConfirmFlag(cmd, &confirm)
	return cmd
}

func newWatchersRemoveCmd(rc *runCtx) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "remove <issue-id> <user-id>",
		Short: "Remove a watcher from an issue (requires --confirm to send)",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			issueID, userID, err := watcherArgs(args)
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/issues/%d/watchers/%d.json", issueID, userID)
			if !confirm {
				return renderDryRun(rc, "DELETE", path, nil, nil)
			}
			if err := rc.client.RemoveWatcher(rc.ctx(), issueID, userID); err != nil {
				return err
			}
			if rc.format == "markdown" {
				_, err := fmt.Fprintf(rc.out, "Removed user %d from watchers of issue #%d.\n", userID, issueID)
				return err
			}
			return output.JSON(rc.out, map[string]any{"removed": true, "issue_id": issueID, "user_id": userID})
		},
	}
	addConfirmFlag(cmd, &confirm)
	return cmd
}
