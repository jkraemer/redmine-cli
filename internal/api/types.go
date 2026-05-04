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

// IssueCreate is the payload for POST /issues.json. The CLI wraps this in
// {"issue": ...} when sending the request.
type IssueCreate struct {
	ProjectID     string `json:"project_id"`
	TrackerID     int    `json:"tracker_id"`
	Subject       string `json:"subject"`
	Description   string `json:"description,omitempty"`
	StatusID      int    `json:"status_id,omitempty"`
	PriorityID    int    `json:"priority_id,omitempty"`
	AssignedToID  string `json:"assigned_to_id,omitempty"`
	ParentIssueID int    `json:"parent_issue_id,omitempty"`
	StartDate     string `json:"start_date,omitempty"`
	DueDate       string `json:"due_date,omitempty"`
	DoneRatio     int    `json:"done_ratio,omitempty"`
}

// IssueUpdate is the payload for PUT /issues/{id}.json. All fields are
// optional; at least one must be set. Pointer fields let callers
// distinguish "not set" (omit from JSON) from "explicitly empty" (send
// "" to clear the field). Redmine treats missing fields as unchanged.
type IssueUpdate struct {
	Subject      *string `json:"subject,omitempty"`
	Description  *string `json:"description,omitempty"`
	StatusID     *int    `json:"status_id,omitempty"`
	PriorityID   *int    `json:"priority_id,omitempty"`
	AssignedToID *string `json:"assigned_to_id,omitempty"`
	DoneRatio    *int    `json:"done_ratio,omitempty"`
	StartDate    *string `json:"start_date,omitempty"`
	DueDate      *string `json:"due_date,omitempty"`
	Notes        *string `json:"notes,omitempty"`
}

// TimeEntry is a logged time entry returned by the API.
type TimeEntry struct {
	ID       int     `json:"id"`
	Hours    float64 `json:"hours"`
	Activity IDName  `json:"activity"`
	Issue    *struct {
		ID int `json:"id"`
	} `json:"issue,omitempty"`
	Project   IDName `json:"project"`
	User      IDName `json:"user"`
	SpentOn   string `json:"spent_on"`
	Comments  string `json:"comments,omitempty"`
	CreatedOn string `json:"created_on"`
}

// TimeEntryCreate is the payload for POST /time_entries.json. Either
// IssueID or ProjectID should be set, not both.
type TimeEntryCreate struct {
	IssueID    int     `json:"issue_id,omitempty"`
	ProjectID  string  `json:"project_id,omitempty"`
	Hours      float64 `json:"hours"`
	ActivityID int     `json:"activity_id"`
	SpentOn    string  `json:"spent_on,omitempty"`
	Comments   string  `json:"comments,omitempty"`
}
