package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jkraemer/redmine-cli/internal/output"
)

func newWikiCmd(rc *runCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wiki",
		Short: "Manage project wiki pages",
	}
	cmd.AddCommand(newWikiListCmd(rc))
	cmd.AddCommand(newWikiGetCmd(rc))
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
