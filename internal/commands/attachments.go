package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

// attachSpec is one parsed --attach value. Both forms (bare path and
// JSON object) decode into this shape.
type attachSpec struct {
	Path        string `json:"path"`
	Filename    string `json:"filename,omitempty"`
	Description string `json:"description,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

// filenameOrBase returns spec.Filename if set, else the basename of spec.Path.
// This is the name we send to the server and surface in dry-run previews.
func filenameOrBase(spec attachSpec) string {
	if spec.Filename != "" {
		return spec.Filename
	}
	return filepath.Base(spec.Path)
}

// parseAttachSpecs converts raw --attach values into attachSpecs.
// A value whose first non-whitespace byte is '{' is parsed as JSON;
// any other value is treated as a bare file path.
func parseAttachSpecs(values []string) ([]attachSpec, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]attachSpec, 0, len(values))
	for i, v := range values {
		trimmed := strings.TrimLeft(v, " \t\r\n")
		var spec attachSpec
		if strings.HasPrefix(trimmed, "{") {
			dec := json.NewDecoder(strings.NewReader(trimmed))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&spec); err != nil {
				return nil, fmt.Errorf("--attach %d: invalid JSON: %w", i+1, err)
			}
		} else {
			spec = attachSpec{Path: trimmed}
		}
		if spec.Path == "" {
			return nil, fmt.Errorf("--attach %d: path is required", i+1)
		}
		out = append(out, spec)
	}
	return out, nil
}

// preflightAttachSpecs stat-checks every spec before any HTTP call so a
// half-uploaded batch never happens. Aggregates all failures into a single
// error so the caller can fix everything in one pass. Each spec must point
// to an existing, regular, readable file.
func preflightAttachSpecs(specs []attachSpec) error {
	if len(specs) == 0 {
		return nil
	}
	var errs []error
	for i, spec := range specs {
		if err := checkAttachReadable(spec); err != nil {
			errs = append(errs, fmt.Errorf("--attach %d (%s): %w", i+1, spec.Path, err))
		}
	}
	return errors.Join(errs...)
}

// checkAttachReadable verifies a single spec's path resolves to an existing,
// regular, readable file. Errors are unwrapped; the caller adds context.
func checkAttachReadable(spec attachSpec) error {
	info, err := os.Stat(spec.Path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	f, err := os.Open(spec.Path)
	if err != nil {
		return err
	}
	return f.Close()
}

// uploadAttachments opens each file and streams it to client.UploadFile,
// returning the UploadRefs ready to embed in a create/update payload.
// Aborts on the first upload error, naming which file (index/total) failed.
// Does NOT pre-flight; call preflightAttachSpecs first.
func uploadAttachments(ctx context.Context, c *api.Client, specs []attachSpec) ([]api.UploadRef, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	refs := make([]api.UploadRef, 0, len(specs))
	for i, spec := range specs {
		name := filenameOrBase(spec)
		up, err := uploadOne(ctx, c, spec, name)
		if err != nil {
			return nil, fmt.Errorf("attach %d/%d (%s): %w", i+1, len(specs), spec.Path, err)
		}
		refs = append(refs, api.UploadRef{
			Token:       up.Token,
			Filename:    name,
			Description: spec.Description,
			ContentType: spec.ContentType,
		})
	}
	return refs, nil
}

// uploadOne opens spec.Path, sends it to the server, and closes the file
// regardless of outcome. Factored out so the deferred close fires exactly
// once per upload without leaking if a later iteration fails.
func uploadOne(ctx context.Context, c *api.Client, spec attachSpec, name string) (*api.Upload, error) {
	f, err := os.Open(spec.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return c.UploadFile(ctx, f, name, spec.ContentType)
}

// attachDryRun builds placeholder UploadRefs for dry-run output so the
// rendered create/update body is shaped exactly like the real one. The
// token uses the form "<UPLOAD-TOKEN-FOR-{filename}>".
func attachDryRun(specs []attachSpec) []api.UploadRef {
	if len(specs) == 0 {
		return nil
	}
	out := make([]api.UploadRef, 0, len(specs))
	for _, spec := range specs {
		name := filenameOrBase(spec)
		out = append(out, api.UploadRef{
			Token:       fmt.Sprintf("<UPLOAD-TOKEN-FOR-%s>", name),
			Filename:    name,
			Description: spec.Description,
			ContentType: spec.ContentType,
		})
	}
	return out
}

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
