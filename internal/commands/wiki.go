package commands

import (
	"fmt"
	"os"

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
			if rc.format == "json" {
				return output.JSON(rc.out, res)
			}
			headers := []string{"Title", "Version", "Updated"}
			rows := make([][]string, len(res.WikiPages))
			for i, p := range res.WikiPages {
				rows[i] = []string{p.Title, fmt.Sprintf("%d", p.Version), p.UpdatedOn}
			}
			return output.MarkdownTable(rc.out, headers, rows)
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newWikiGetCmd(rc *runCtx) *cobra.Command {
	var projectID string

	cmd := &cobra.Command{
		Use:   "get <title>",
		Short: "Get a wiki page by title (--project required)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			page, err := rc.client.GetWikiPage(rc.ctx(), projectID, args[0])
			if err != nil {
				return err
			}
			if rc.format == "json" {
				return output.JSON(rc.out, page)
			}
			fmt.Fprintf(rc.out, "# %s\n\n", page.Title)
			fmt.Fprintf(rc.out, "**Author:** %s  \n", page.Author.Name)
			fmt.Fprintf(rc.out, "**Version:** %d  \n", page.Version)
			fmt.Fprintf(rc.out, "**Updated:** %s\n\n", page.UpdatedOn)
			fmt.Fprintf(rc.out, "%s\n", page.Text)
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
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

func newWikiPutCmd(rc *runCtx) *cobra.Command {
	var projectID, text, textFile, comments string
	var confirm bool

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

			payload := api.WikiPageWrite{Text: text, Comments: comments}
			body := map[string]any{"wiki_page": payload}
			path := fmt.Sprintf("/projects/%s/wiki/%s.json", projectID, title)

			if !confirm {
				return renderDryRun(rc, "PUT", path, body)
			}

			page, err := rc.client.PutWikiPage(rc.ctx(), projectID, title, payload)
			if err != nil {
				return err
			}
			if rc.format == "json" {
				return output.JSON(rc.out, page)
			}
			if page != nil {
				fmt.Fprintf(rc.out, "Wiki page %q saved (version %d).\n", page.Title, page.Version)
			} else {
				fmt.Fprintf(rc.out, "Wiki page %q saved.\n", title)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&projectID, "project", "p", "", "Project ID or identifier (required)")
	_ = cmd.MarkFlagRequired("project")
	cmd.Flags().StringVar(&text, "text", "", "Page content (Textile markup)")
	cmd.Flags().StringVar(&textFile, "text-file", "", "Read page content from file (mutually exclusive with --text)")
	cmd.Flags().StringVar(&comments, "comments", "", "Edit summary / comment")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually send the request (without this flag the command runs in dry-run mode)")
	return cmd
}
