package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/output"
)

// sanitizeAttachmentFilename reduces a server-supplied filename to a single
// path component safe to append to a chosen output directory. It strips any
// directory parts and rejects names that don't carry a usable basename
// (empty, ".", ".."). The Redmine server is the source of meta.Filename, so
// we treat it as untrusted: a malicious or compromised server otherwise
// could direct writes outside of $TMPDIR/redmine-cli.
func sanitizeAttachmentFilename(name string) (string, error) {
	base := filepath.Base(filepath.Clean(name))
	if base == "" || base == "." || base == ".." || base == string(filepath.Separator) {
		return "", fmt.Errorf("attachment filename %q is not a usable name", name)
	}
	return base, nil
}

func newAttachmentsCmd(rc *runCtx) *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Attachment operations"}
	c.AddCommand(newAttachmentsDownloadCmd(rc))
	return c
}

func newAttachmentsDownloadCmd(rc *runCtx) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download an attachment to a local file (prints the path)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid attachment id %q", args[0])
			}
			meta, body, err := rc.client.GetAttachment(rc.ctx(), id)
			if err != nil {
				return err
			}
			defer body.Close()

			dest := outPath
			if dest == "" {
				safeName, err := sanitizeAttachmentFilename(meta.Filename)
				if err != nil {
					return err
				}
				dir := filepath.Join(os.TempDir(), "redmine-cli")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				dest = filepath.Join(dir, fmt.Sprintf("%d-%s", meta.ID, safeName))
			}

			f, err := os.Create(dest)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(f, body); err != nil {
				return err
			}
			abs, err := filepath.Abs(dest)
			if err != nil {
				abs = dest
			}

			if rc.format == "markdown" {
				_, err := fmt.Fprintf(rc.out, "Saved attachment %d to `%s`\n", meta.ID, abs)
				return err
			}
			return output.JSON(rc.out, map[string]any{
				"id":           meta.ID,
				"filename":     meta.Filename,
				"path":         abs,
				"content_type": meta.ContentType,
				"size":         meta.Filesize,
			})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Output path (default: $TMPDIR/redmine-cli/<id>-<filename>)")
	return cmd
}
