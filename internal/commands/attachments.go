package commands

import "github.com/spf13/cobra"

func newAttachmentsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Attachment operations"}
	return c
}
