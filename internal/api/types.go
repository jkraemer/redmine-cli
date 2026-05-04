// Package api provides a thin typed Redmine HTTP client.
package api

// Project is a Redmine project (subset used by the CLI).
type Project struct {
	ID         int    `json:"id"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Status     int    `json:"status,omitempty"`
}

type listProjectsResponse struct {
	Projects   []Project `json:"projects"`
	TotalCount int       `json:"total_count"`
	Offset     int       `json:"offset"`
	Limit      int       `json:"limit"`
}

// ListProjectsResult is what we return to callers.
type ListProjectsResult struct {
	Projects   []Project `json:"projects"`
	TotalCount int       `json:"total_count"`
	Offset     int       `json:"offset"`
	Limit      int       `json:"limit"`
}

// IDName is a common shape for nested {id,name} fields in Redmine.
type IDName struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Issue is a Redmine issue (subset used by the CLI).
type Issue struct {
	ID          int     `json:"id"`
	Subject     string  `json:"subject"`
	Description string  `json:"description,omitempty"`
	Project     IDName  `json:"project"`
	Tracker     IDName  `json:"tracker"`
	Status      IDName  `json:"status"`
	Priority    IDName  `json:"priority"`
	Author      IDName  `json:"author"`
	AssignedTo  *IDName `json:"assigned_to,omitempty"`
	StartDate   string  `json:"start_date,omitempty"`
	DueDate     string  `json:"due_date,omitempty"`
	DoneRatio   int     `json:"done_ratio,omitempty"`
	CreatedOn   string  `json:"created_on,omitempty"`
	UpdatedOn   string  `json:"updated_on,omitempty"`

	Journals    []Journal    `json:"journals,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Journal is one entry in an issue's history.
type Journal struct {
	ID        int    `json:"id"`
	User      IDName `json:"user"`
	Notes     string `json:"notes,omitempty"`
	CreatedOn string `json:"created_on,omitempty"`
	Details   []any  `json:"details,omitempty"`
}

// Attachment metadata.
type Attachment struct {
	ID          int    `json:"id"`
	Filename    string `json:"filename"`
	Filesize    int64  `json:"filesize"`
	ContentType string `json:"content_type,omitempty"`
	ContentURL  string `json:"content_url"`
	Description string `json:"description,omitempty"`
	Author      IDName `json:"author"`
	CreatedOn   string `json:"created_on"`
}

// ListIssuesParams holds query params for /issues.json.
type ListIssuesParams struct {
	ProjectID  string
	StatusID   string // "open", "closed", "*", or numeric
	AssignedTo string // user id or "me"
	UpdatedOn  string // operator-style filter, e.g. ">=2026-01-01"
	Limit      int    // default 25, max 100
	Offset     int
	Sort       string
	Include    []string // attachments, relations
}

// ListIssuesResult holds the listing payload.
type ListIssuesResult struct {
	Issues     []Issue `json:"issues"`
	TotalCount int     `json:"total_count"`
	Offset     int     `json:"offset"`
	Limit      int     `json:"limit"`
}

// ListProjectsParams holds query params for /projects.json.
type ListProjectsParams struct {
	Limit  int
	Offset int
}

// GetIssueParams covers the include flags accepted by /issues/{id}.json.
type GetIssueParams struct {
	Include []string // journals, attachments, relations, children
}
