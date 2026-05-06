// Client is a thin typed wrapper around the Redmine REST API. We do not use
// the oapi-codegen-generated client directly because (a) the spec covers
// many endpoints we do not need, (b) hand-rolling four endpoints is
// straightforward and avoids generated-code awkwardness for mixed-type
// fields. The generated code lives in internal/client/ for reference and
// future expansion.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client is the Redmine HTTP client.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New creates a Client. If httpClient is nil, http.DefaultClient is used.
func New(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    httpClient,
	}
}

const maxBodyExcerpt = 512

func (c *Client) do(ctx context.Context, method, path string, q url.Values) (*http.Response, error) {
	full := c.baseURL + path
	if q != nil && len(q) > 0 {
		full += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Redmine-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyExcerpt))
		_ = resp.Body.Close()
		return nil, &Error{Status: resp.StatusCode, Body: string(body), URL: full}
	}
	return resp, nil
}

func (c *Client) doJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.do(ctx, "GET", path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// doWriteJSON sends method to path with payload as a JSON body and decodes
// any 2xx response body into out. On non-2xx responses returns an *Error.
// On 204 No Content (or otherwise empty body), out is left untouched and
// nil is returned. If out is nil, the response body is discarded.
func (c *Client) doWriteJSON(ctx context.Context, method, path string, payload, out any) error {
	full := c.baseURL + path
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode payload: %w", err)
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return err
	}
	req.Header.Set("X-Redmine-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		excerpt, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyExcerpt))
		return &Error{Status: resp.StatusCode, Body: string(excerpt), URL: full}
	}
	if resp.StatusCode == http.StatusNoContent || out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	// Some servers may legitimately return an empty body on 200/201.
	// Peek at the body to decide whether to decode.
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ListProjects lists projects.
func (c *Client) ListProjects(ctx context.Context, p ListProjectsParams) (*ListProjectsResult, error) {
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	var raw listProjectsResponse
	if err := c.doJSON(ctx, "/projects.json", q, &raw); err != nil {
		return nil, err
	}
	return &ListProjectsResult{
		Projects:   raw.Projects,
		TotalCount: raw.TotalCount,
		Offset:     raw.Offset,
		Limit:      raw.Limit,
	}, nil
}

// ListIssues lists issues with the given filter params.
func (c *Client) ListIssues(ctx context.Context, p ListIssuesParams) (*ListIssuesResult, error) {
	q := url.Values{}
	if p.ProjectID != "" {
		q.Set("project_id", p.ProjectID)
	}
	if p.StatusID != "" {
		q.Set("status_id", p.StatusID)
	}
	if p.AssignedTo != "" {
		q.Set("assigned_to_id", p.AssignedTo)
	}
	if p.UpdatedOn != "" {
		q.Set("updated_on", p.UpdatedOn)
	}
	if p.Sort != "" {
		q.Set("sort", p.Sort)
	}
	if len(p.Include) > 0 {
		q.Set("include", strings.Join(p.Include, ","))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	var res ListIssuesResult
	if err := c.doJSON(ctx, "/issues.json", q, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetIssue fetches a single issue with optional includes.
func (c *Client) GetIssue(ctx context.Context, id int, p *GetIssueParams) (*Issue, error) {
	q := url.Values{}
	if p != nil && len(p.Include) > 0 {
		q.Set("include", strings.Join(p.Include, ","))
	}
	var wrapper struct {
		Issue Issue `json:"issue"`
	}
	if err := c.doJSON(ctx, fmt.Sprintf("/issues/%d.json", id), q, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.Issue, nil
}

// GetAttachment fetches metadata for an attachment and returns an open
// reader for the file content. The caller must close the body.
func (c *Client) GetAttachment(ctx context.Context, id int) (*Attachment, io.ReadCloser, error) {
	var wrapper struct {
		Attachment Attachment `json:"attachment"`
	}
	if err := c.doJSON(ctx, fmt.Sprintf("/attachments/%d.json", id), nil, &wrapper); err != nil {
		return nil, nil, err
	}
	if wrapper.Attachment.ContentURL == "" {
		return nil, nil, fmt.Errorf("attachment %d: no content_url returned", id)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", wrapper.Attachment.ContentURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-Redmine-API-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyExcerpt))
		_ = resp.Body.Close()
		return nil, nil, &Error{Status: resp.StatusCode, Body: string(body), URL: wrapper.Attachment.ContentURL}
	}
	return &wrapper.Attachment, resp.Body, nil
}

// CreateIssue posts a new issue. The payload is wrapped in
// {"issue": ...} on the wire and the {"issue": ...} response is unwrapped.
func (c *Client) CreateIssue(ctx context.Context, p IssueCreate) (*Issue, error) {
	in := struct {
		Issue IssueCreate `json:"issue"`
	}{Issue: p}
	var out struct {
		Issue Issue `json:"issue"`
	}
	if err := c.doWriteJSON(ctx, "POST", "/issues.json", in, &out); err != nil {
		return nil, err
	}
	return &out.Issue, nil
}

// UpdateIssue PUTs to /issues/{id}.json. Redmine returns 204 No Content
// on success, so this method returns nil on success.
func (c *Client) UpdateIssue(ctx context.Context, id int, p IssueUpdate) error {
	in := struct {
		Issue IssueUpdate `json:"issue"`
	}{Issue: p}
	return c.doWriteJSON(ctx, "PUT", fmt.Sprintf("/issues/%d.json", id), in, nil)
}

// GetCurrentUser fetches /users/current.json.
func (c *Client) GetCurrentUser(ctx context.Context) (*User, error) {
	var wrapper struct {
		User User `json:"user"`
	}
	if err := c.doJSON(ctx, "/users/current.json", nil, &wrapper); err != nil {
		return nil, err
	}
	return &wrapper.User, nil
}

// ListUsers lists users via /users.json. On most installs this is admin-only.
func (c *Client) ListUsers(ctx context.Context, p ListUsersParams) (*ListUsersResult, error) {
	q := url.Values{}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	var res ListUsersResult
	if err := c.doJSON(ctx, "/users.json", q, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListTrackers lists trackers via /trackers.json.
func (c *Client) ListTrackers(ctx context.Context) (*ListTrackersResult, error) {
	var res ListTrackersResult
	if err := c.doJSON(ctx, "/trackers.json", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListStatuses lists issue statuses via /issue_statuses.json.
func (c *Client) ListStatuses(ctx context.Context) (*ListStatusesResult, error) {
	var res ListStatusesResult
	if err := c.doJSON(ctx, "/issue_statuses.json", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListPriorities lists issue priorities via /enumerations/issue_priorities.json.
func (c *Client) ListPriorities(ctx context.Context) (*ListPrioritiesResult, error) {
	var res ListPrioritiesResult
	if err := c.doJSON(ctx, "/enumerations/issue_priorities.json", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListActivities lists time-entry activities via /enumerations/time_entry_activities.json.
func (c *Client) ListActivities(ctx context.Context) (*ListActivitiesResult, error) {
	var res ListActivitiesResult
	if err := c.doJSON(ctx, "/enumerations/time_entry_activities.json", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// LogTime posts a new time entry. The payload is wrapped in
// {"time_entry": ...} on the wire and the response is unwrapped.
func (c *Client) LogTime(ctx context.Context, p TimeEntryCreate) (*TimeEntry, error) {
	in := struct {
		TimeEntry TimeEntryCreate `json:"time_entry"`
	}{TimeEntry: p}
	var out struct {
		TimeEntry TimeEntry `json:"time_entry"`
	}
	if err := c.doWriteJSON(ctx, "POST", "/time_entries.json", in, &out); err != nil {
		return nil, err
	}
	return &out.TimeEntry, nil
}

// Search queries /search.json. Filter flags (issues, wiki, projects,
// titles_only) are only sent when set, so callers can let the server
// apply its defaults by leaving them off.
func (c *Client) Search(ctx context.Context, p SearchParams) (*SearchResults, error) {
	q := url.Values{}
	q.Set("q", p.Q)
	if p.Issues {
		q.Set("issues", "1")
	}
	if p.Wiki {
		q.Set("wiki_pages", "1")
	}
	if p.Projects {
		q.Set("projects", "1")
	}
	if p.TitlesOnly {
		q.Set("titles_only", "1")
	}
	if p.Scope != "" {
		q.Set("scope", p.Scope)
	}
	if p.ProjectID != "" {
		q.Set("project_id", p.ProjectID)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", strconv.Itoa(p.Offset))
	}
	var res SearchResults
	if err := c.doJSON(ctx, "/search.json", q, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListWikiPages fetches the wiki index for a project via /projects/{id}/wiki/index.json.
func (c *Client) ListWikiPages(ctx context.Context, projectID string) (*ListWikiPagesResult, error) {
	var res ListWikiPagesResult
	path := fmt.Sprintf("/projects/%s/wiki/index.json", url.PathEscape(projectID))
	if err := c.doJSON(ctx, path, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetWikiPage fetches a single wiki page by title via /projects/{id}/wiki/{title}.json.
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

// PutWikiPage creates or updates a wiki page via PUT (Redmine uses PUT for both).
// Returns the current page content after the write, or nil on 204 No Content.
func (c *Client) PutWikiPage(ctx context.Context, projectID, title string, p WikiPageWrite) (*WikiPage, error) {
	in := struct {
		WikiPage WikiPageWrite `json:"wiki_page"`
	}{WikiPage: p}
	var out struct {
		WikiPage WikiPage `json:"wiki_page"`
	}
	path := fmt.Sprintf("/projects/%s/wiki/%s.json", url.PathEscape(projectID), url.PathEscape(title))
	if err := c.doWriteJSON(ctx, "PUT", path, in, &out); err != nil {
		return nil, err
	}
	if out.WikiPage.Title == "" {
		// Redmine returned 204 No Content — fetch the page to return current state.
		return c.GetWikiPage(ctx, projectID, title)
	}
	return &out.WikiPage, nil
}
