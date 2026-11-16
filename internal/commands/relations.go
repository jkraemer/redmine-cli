package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

// relationTypes are the relation types Redmine accepts on create. The
// inverse ones (duplicated, blocked, follows, copied_from) are stored
// flipped by Redmine.
var relationTypes = []string{"relates", "duplicates", "duplicated", "blocks", "blocked", "precedes", "follows", "copied_to", "copied_from"}

func newIssuesRelationsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "relations", Short: "Manage issue relations (list them with: issues get <id> --include relations)"}
	c.AddCommand(newRelationsAddCmd(rc))
	c.AddCommand(newRelationsRemoveCmd(rc))
	return c
}

func newRelationsAddCmd(rc *runCtx) *cobra.Command {
	var (
		delay   int
		confirm bool
	)
	cmd := &cobra.Command{
		Use:   "add <issue-id> <type> <other-issue-id>",
		Short: "Relate an issue to another one (requires --confirm to send)",
		Long: "Relate an issue to another one, e.g. `add 5 blocks 12`. Types: " +
			strings.Join(relationTypes, ", ") + ". --delay (days) applies to precedes/follows.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			issueID, err := positiveID(args[0], "issue")
			if err != nil {
				return err
			}
			relType := args[1]
			otherID, err := positiveID(args[2], "issue")
			if err != nil {
				return err
			}
			if !slices.Contains(relationTypes, relType) {
				return fmt.Errorf("unknown relation type %q (want one of: %s)", relType, strings.Join(relationTypes, ", "))
			}
			if issueID == otherID {
				return fmt.Errorf("an issue cannot be related to itself")
			}
			payload := api.RelationCreate{IssueToID: otherID, RelationType: relType}
			if cmd.Flags().Changed("delay") {
				if relType != "precedes" && relType != "follows" {
					return fmt.Errorf("--delay is only valid for precedes and follows")
				}
				payload.Delay = &delay
			}
			path := fmt.Sprintf("/issues/%d/relations.json", issueID)
			if !confirm {
				return renderDryRun(rc, "POST", path, map[string]any{"relation": payload}, nil)
			}
			rel, err := rc.client.CreateRelation(rc.ctx(), issueID, payload)
			if err != nil {
				return err
			}
			if rc.format == "markdown" {
				label, other := relationFromSide(*rel, issueID)
				_, err := fmt.Fprintf(rc.out, "Added relation %d: #%d %s #%d.\n", rel.ID, issueID, output.SanitizeForTerminal(label), other)
				return err
			}
			return output.JSON(rc.out, map[string]any{"relation": rel})
		},
	}
	cmd.Flags().IntVar(&delay, "delay", 0, "Delay in days (precedes/follows only)")
	addConfirmFlag(cmd, &confirm)
	return cmd
}

func newRelationsRemoveCmd(rc *runCtx) *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "remove <relation-id>",
		Short: "Remove an issue relation by its ID (requires --confirm to send)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			relationID, err := positiveID(args[0], "relation")
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/relations/%d.json", relationID)
			if !confirm {
				return renderDryRun(rc, "DELETE", path, nil, nil)
			}
			if err := rc.client.DeleteRelation(rc.ctx(), relationID); err != nil {
				return err
			}
			if rc.format == "markdown" {
				_, err := fmt.Fprintf(rc.out, "Removed relation %d.\n", relationID)
				return err
			}
			return output.JSON(rc.out, map[string]any{"removed": true, "relation_id": relationID})
		},
	}
	addConfirmFlag(cmd, &confirm)
	return cmd
}
