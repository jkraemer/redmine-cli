# Redmine Wiki Functionality Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Implement `redmine-cli wiki list` and `redmine-cli wiki get` functionality to allow users to list and view wiki pages.

**Architecture:** 
- Add Wiki-related data structures to `internal/api/types.go`.
- Add `ListWikiPages` and `GetWikiPage` methods to the `internal/api/Client`.
- Implement a new `wiki` command in `internal/commands/wiki.go` using the Cobra library.
- Register the `wiki` command in `internal/commands/root.go`.

**Tech Stack:** Go, Cobra CLI library.

---

### Task 1: Add Wiki Types to internal/api/types.go

**Objective:** Define the data structures for Wiki pages as returned by the Redmine API.

**Files:**
- Modify: `internal/api/types.go`

**Step 1: Add types**
Add `WikiPageSummary` (for listings) and `WikiPage` (for full content).

```go
// WikiPageSummary is a single entry in a project's wiki index.
type WikiPageSummary struct {
	Title     string `json:"title"`
	Version   int    `json:"version"`
	CreatedOn string `json:"created_on"`
	UpdatedOn string `json:"updated_on"`
}

// WikiPage is the full content of a wiki page.
type WikiPage struct {
	Title       string       `json:"title"`
	Text        string       `json:"text"`
	Version     int          `json:"version"`
	Author      IDName       `json:"author"`
	Comments    string       `json:"comments,omitempty"`
	CreatedOn   string       `json:"created_on"`
	UpdatedOn   string       `json:"updated_on"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// ListWikiPagesResult is the response from /projects/{id}/wiki/index.json.
type ListWikiPagesResult struct {
	WikiPages []WikiPageSummary `json:"wiki_pages"`
}
```

**Step 2: Verify compilation**
Run: `go build ./internal/api`

---

### Task 2: Add Wiki methods to internal/api/client.go

**Objective:** Implement methods to fetch wiki index and specific wiki pages.

**Files:**
- Modify: `internal/api/client.go`

**Step 1: Add ListWikiPages and GetWikiPage**

```go
// ListWikiPages fetches the wiki index for a project.
func (c *Client) ListWikiPages(ctx context.Context, projectID string) (*ListWikiPagesResult, error) {
	var res ListWikiPagesResult
	path := fmt.Sprintf("/projects/%s/wiki/index.json", url.PathEscape(projectID))
	if err := c.doJSON(ctx, path, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetWikiPage fetches a single wiki page by title.
func (c *Client) GetWikiPage(ctx context.Context, projectID, title string) (*WikiPage, error) {
	var wrapper struct {
		WikiPage WikiPage `json:"wiki_page"`
	}
	path := fmt.Sprintf("/projects/%s/wiki/%s.json", url.PathEscape(projectID), url.PathEscape(title))
	q := url.Values{}
	q.Set("include", "attachments")
	if err := c.doJSON(ctx, path, q, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.WikiPage, nil
}
```

**Step 2: Verify compilation**
Run: `go build ./internal/api`

---

### Task 3: Implement wiki command in internal/commands/wiki.go

**Objective:** Create the CLI command tree for `redmine-cli wiki`.

**Files:**
- Create: `internal/commands/wiki.go`

**Step 1: Implementation**
Implement `wiki list` and `wiki get`.

```go
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
		Short: "List wiki pages in a project",
		RunE: func(cmd *cobra.Command, args []string) error {
			if projectID == "" {
				return fmt.Errorf("project ID/identifier is required")
			}
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
				rows[i] = []string{
					p.Title,
					fmt.Sprintf("%d", p.Version),
					p.UpdatedOn,
				}
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
		Short: "Get a wiki page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := args[0]
			if projectID == "" {
				return fmt.Errorf("project ID/identifier is required for title-based lookup")
			}

			page, err := rc.client.GetWikiPage(rc.ctx(), projectID, title)
			if err != nil {
				return err
			}

			if rc.format == "json" {
				return output.JSON(rc.out, page)
			}

			// Markdown view
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
					rows[i] = []string{
						fmt.Sprintf("%d", a.ID),
						a.Filename,
						fmt.Sprintf("%d", a.Filesize),
					}
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
```

---

### Task 4: Register wiki command in internal/commands/root.go

**Objective:** Wire the new wiki command into the root CLI.

**Files:**
- Modify: `internal/commands/root.go`

**Step 1: Add wiki command**
Find `root.AddCommand` calls and add `root.AddCommand(newWikiCmd(rc))`.

---

### Task 5: Build and Test

**Objective:** Build the binary and verify the new commands.

**Step 1: Build**
Run: `make build`

**Step 2: Test**
Verify help: `./redmine-cli wiki --help`
Verify list: `./redmine-cli wiki list --project <your-project>`
Verify get: `./redmine-cli wiki get Wiki --project <your-project>`
