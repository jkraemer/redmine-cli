// Client is a thin typed wrapper around the Redmine REST API. We do not use
// the oapi-codegen-generated client directly because (a) the spec covers
// many endpoints we do not need, (b) hand-rolling four endpoints is
// straightforward and avoids generated-code awkwardness for mixed-type
// fields. The generated code lives in internal/client/ for reference and
// future expansion.
package api

import (
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
