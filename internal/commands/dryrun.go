package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/output"
)

// confirmFlagHelp is the standard help text for the --confirm flag shared by
// every write command.
const confirmFlagHelp = "Actually send the request (without this flag the command runs in dry-run mode)"

// addConfirmFlag wires the standard --confirm flag onto a write command and
// marks it with the "write" annotation. The annotation is the single source of
// truth for "this command mutates the server": --agent --help reads it to flag
// write commands as blocked in read-only mode. Keeping the flag and annotation
// together means a new write command picks up both at once.
func addConfirmFlag(cmd *cobra.Command, confirm *bool) {
	cmd.Flags().BoolVar(confirm, "confirm", false, confirmFlagHelp)
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["write"] = "true"
}

// renderDryRun emits a "would do this" preview to rc.out and returns nil.
// path is the API path portion (e.g. "/issues.json"); we don't have the
// base URL inside runCtx, so we just emit the path. Callers can
// reconstruct the full URL by joining with their configured base.
//
// wouldUpload, when non-empty, surfaces the local files that would be
// streamed to /uploads.json before the create/update is sent. When nil
// or empty the output is byte-identical to a renderer that doesn't know
// about uploads.
func renderDryRun(rc *runCtx, method, path string, body any, wouldUpload []attachSpec) error {
	if rc.format == "markdown" {
		var b strings.Builder
		if len(wouldUpload) > 0 {
			fmt.Fprintf(&b, "Would upload %d file(s):\n", len(wouldUpload))
			for i, spec := range wouldUpload {
				fmt.Fprintf(&b, "  %d. %s%s\n", i+1, spec.Path, attachMetaParens(spec))
			}
		}
		var raw bytes.Buffer
		enc := json.NewEncoder(&raw)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return err
		}
		// json.Encoder.Encode appends a trailing newline; trim it so the
		// closing ``` sits flush against the last line of JSON, matching
		// the previous json.MarshalIndent behavior.
		footer := "(re-run with --confirm to send)"
		if rc.readOnly {
			footer = "(read-only mode is active — --confirm is disabled; unset REDMINE_READ_ONLY / read_only to enable writes)"
		}
		fmt.Fprintf(&b, "DRY RUN -- would %s %s with body:\n```json\n%s\n```\n%s\n", method, path, strings.TrimRight(raw.String(), "\n"), footer)
		_, err := fmt.Fprint(rc.out, b.String())
		return err
	}
	payload := map[string]any{
		"dry_run": true,
		"method":  method,
		"path":    path,
		"body":    body,
	}
	if rc.readOnly {
		payload["read_only"] = true
	}
	if len(wouldUpload) > 0 {
		payload["would_upload"] = wouldUpload
	}
	return output.JSON(rc.out, payload)
}

// attachMetaParens renders the "(filename=…, content_type=…, description=…)"
// suffix for one entry of the markdown "Would upload …" list. Empty fields
// are omitted; an entry always has at least a filename (the basename) so the
// parenthetical is never empty.
func attachMetaParens(spec attachSpec) string {
	var parts []string
	if name := filenameOrBase(spec); name != "" {
		parts = append(parts, "filename="+name)
	}
	if spec.ContentType != "" {
		parts = append(parts, "content_type="+spec.ContentType)
	}
	if spec.Description != "" {
		parts = append(parts, "description="+spec.Description)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  (" + strings.Join(parts, ", ") + ")"
}
