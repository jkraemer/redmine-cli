package commands

import (
	"encoding/json"
	"fmt"

	"github.com/jkraemer/redmine-cli/internal/output"
)

// renderDryRun emits a "would do this" preview to rc.out and returns nil.
// path is the API path portion (e.g. "/issues.json"); we don't have the
// base URL inside runCtx, so we just emit the path. Callers can
// reconstruct the full URL by joining with their configured base.
func renderDryRun(rc *runCtx, method, path string, body any) error {
	if rc.format == "markdown" {
		b, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(rc.out, "DRY RUN -- would %s %s with body:\n```json\n%s\n```\n(re-run with --confirm to send)\n", method, path, b)
		return err
	}
	return output.JSON(rc.out, map[string]any{
		"dry_run": true,
		"method":  method,
		"path":    path,
		"body":    body,
	})
}
