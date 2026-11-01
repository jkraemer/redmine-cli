package commands

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/api"
	"github.com/jkraemer/redmine-cli/internal/output"
)

func newWikiCmd(rc *runCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wiki",
		Short: "Manage project wiki pages",
	}
	cmd.AddCommand(newWikiListCmd(rc))
	cmd.AddCommand(newWikiGetCmd(rc))
	cmd.AddCommand(newWikiPutCmd(rc))
	return cmd
}

func newWikiListCmd(rc *runCtx) *cobra.Command {
	var projectID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List wiki pages in a project (--project required)",
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := rc.client.ListWikiPages(rc.ctx(), projectID)
			if err != nil {
				return err
			}
			if rc.format == "markdown" {
				headers := []string{"Title", "Version", "Updated"}
				rows := make([][]string, len(res.WikiPages))
				for i, p := range res.WikiPages {
					rows[i] = []string{p.Title, fmt.Sprintf("%d", p.Version), p.UpdatedOn}
				}
				return output.MarkdownTable(rc.out, headers, rows)
			}
			return output.JSON(rc.out, res)
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newWikiGetCmd(rc *runCtx) *cobra.Command {
	var projectID, outPath string
	var raw bool

	cmd := &cobra.Command{
		Use:   "get <title>",
		Short: "Get a wiki page by title (--project required)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if raw && outPath != "" && outPath != "-" {
				return fmt.Errorf("--raw and --out <path> are mutually exclusive (use --out - for stdout)")
			}
			page, err := rc.client.GetWikiPage(rc.ctx(), projectID, args[0])
			if err != nil {
				return err
			}
			if raw || outPath == "-" {
				// Page source only, byte for byte: no sanitizing, no added
				// or trimmed newline, no line-ending normalisation, so the
				// output can be edited and sent back with wiki put as is.
				_, err := io.WriteString(rc.out, page.Text)
				return err
			}
			if outPath != "" {
				return writeWikiPageSource(rc, page, outPath)
			}
			if rc.format == "markdown" {
				// Server-controlled strings reach a terminal; strip ANSI/control
				// bytes that a malicious wiki editor could embed.
				clean := output.SanitizeForTerminal
				fmt.Fprintf(rc.out, "# %s\n\n", clean(page.Title))
				fmt.Fprintf(rc.out, "**Author:** %s  \n", clean(page.Author.Name))
				fmt.Fprintf(rc.out, "**Version:** %d  \n", page.Version)
				fmt.Fprintf(rc.out, "**Updated:** %s\n\n", clean(page.UpdatedOn))
				fmt.Fprintf(rc.out, "%s\n", clean(page.Text))
				if len(page.Attachments) > 0 {
					fmt.Fprintln(rc.out, "\n## Attachments")
					headers := []string{"ID", "Filename", "Size"}
					rows := make([][]string, len(page.Attachments))
					for i, a := range page.Attachments {
						rows[i] = []string{fmt.Sprintf("%d", a.ID), a.Filename, fmt.Sprintf("%d", a.Filesize)}
					}
					return output.MarkdownTable(rc.out, headers, rows)
				}
				return nil
			}
			return output.JSON(rc.out, page)
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().BoolVar(&raw, "raw", false, "Print only the page source, bytes untouched (no header, no trailing newline added); for editing and sending back with wiki put")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "Write the page source to this file (bytes untouched) and print the absolute path; - for stdout (same as --raw)")
	return cmd
}

// writeWikiPageSource writes page.Text to dest byte for byte and reports the
// absolute path, mirroring attachments download -o. A failed close can mean
// a failed final flush, so it is not reported as success.
func writeWikiPageSource(rc *runCtx, page *api.WikiPage, dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(f, page.Text); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		abs = dest
	}
	if rc.format == "markdown" {
		_, err := fmt.Fprintf(rc.out, "Saved wiki page %q (version %d) to `%s`\n", output.SanitizeForTerminal(page.Title), page.Version, abs)
		return err
	}
	return output.JSON(rc.out, map[string]any{
		"title":   page.Title,
		"version": page.Version,
		"path":    abs,
	})
}

func newWikiPutCmd(rc *runCtx) *cobra.Command {
	var projectID, text, textFile, comments string
	var version int
	var confirm bool
	var attachStrs []string

	cmd := &cobra.Command{
		Use:   "put <title>",
		Short: "Create or update a wiki page (--project required; dry-run unless --confirm)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := args[0]

			if cmd.Flags().Changed("text") && cmd.Flags().Changed("text-file") {
				return fmt.Errorf("--text and --text-file are mutually exclusive")
			}
			if !cmd.Flags().Changed("text") && !cmd.Flags().Changed("text-file") {
				return fmt.Errorf("one of --text or --text-file is required")
			}
			if cmd.Flags().Changed("text-file") {
				data, err := os.ReadFile(textFile)
				if err != nil {
					return fmt.Errorf("reading --text-file %q: %w", textFile, err)
				}
				text = string(data)
			}

			specs, err := parseAttachSpecs(attachStrs)
			if err != nil {
				return err
			}

			payload := api.WikiPageWrite{Text: text, Comments: comments}
			if cmd.Flags().Changed("version") {
				payload.Version = version
			}
			// Escape like the client does, so the dry-run preview shows the
			// path that would actually be requested.
			path := fmt.Sprintf("/projects/%s/wiki/%s.json", url.PathEscape(projectID), url.PathEscape(title))

			if err := applyAttachments(rc.ctx(), rc.client, specs, &payload.Uploads, confirm); err != nil {
				return err
			}
			if !confirm {
				body := map[string]any{"wiki_page": payload}
				return renderDryRun(rc, "PUT", path, body, specs)
			}

			page, err := rc.client.PutWikiPage(rc.ctx(), projectID, title, payload)
			if err != nil {
				// Redmine answers 409 when --version is older than the page's
				// current version (someone saved in between). Keep the
				// *api.Error wrapped so exitCodeFor still sees it; 409 maps to
				// 1 (validation-type), and no other exit code fits better.
				var apiErr *api.Error
				if errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict && payload.Version > 0 {
					return fmt.Errorf("wiki page %q has been modified on the server since version %d (409 Conflict); run wiki get to fetch the current version and retry with --version: %w", title, payload.Version, err)
				}
				return err
			}
			if rc.format == "markdown" {
				if page != nil {
					fmt.Fprintf(rc.out, "Wiki page %q saved (version %d).\n", output.SanitizeForTerminal(page.Title), page.Version)
				} else {
					fmt.Fprintf(rc.out, "Wiki page %q saved.\n", output.SanitizeForTerminal(title))
				}
				return nil
			}
			return output.JSON(rc.out, page)
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&text, "text", "", "Page content (Textile markup)")
	cmd.Flags().StringVar(&textFile, "text-file", "", "Read page content from file (mutually exclusive with --text)")
	cmd.Flags().StringVar(&comments, "comments", "", "Edit summary / comment")
	cmd.Flags().IntVar(&version, "version", 0, "Expected current page version (from wiki get) for optimistic locking; the server answers 409 Conflict if the page changed since")
	cmd.Flags().StringArrayVar(&attachStrs, "attach", nil, attachFlagHelp)
	addConfirmFlag(cmd, &confirm)
	return cmd
}
