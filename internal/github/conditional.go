package github

import (
	"context"
	"fmt"
	"net/http"
)

// ConditionalInfo contains ETag and Last-Modified for conditional requests.
type ConditionalInfo struct {
	ETag         string
	LastModified string
}

// ConditionalResponse wraps a response with cache information.
type ConditionalResponse struct {
	// NotModified is true if the server returned 304
	NotModified bool

	// Response is the HTTP response (nil if NotModified)
	Response *http.Response

	// Info contains the ETag/Last-Modified from the response
	Info ConditionalInfo
}

// GetConditional performs a GET request with conditional headers.
// If cond is provided and the resource hasn't changed, returns NotModified=true.
func (c *Client) GetConditional(ctx context.Context, path string, cond *ConditionalInfo) (*ConditionalResponse, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}

	// Add conditional headers if provided
	if cond != nil {
		if cond.ETag != "" {
			req.Header.Set("If-None-Match", cond.ETag)
		}
		if cond.LastModified != "" {
			req.Header.Set("If-Modified-Since", cond.LastModified)
		}
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}

	result := &ConditionalResponse{
		Info: ConditionalInfo{
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		},
	}

	// Handle 304 Not Modified
	if resp.StatusCode == http.StatusNotModified {
		resp.Body.Close()
		result.NotModified = true
		return result, nil
	}

	result.Response = resp
	return result, nil
}

// GetRepositoryConditional fetches repository with conditional request support.
// Returns (metrics, notModified, newInfo, error).
func (c *Client) GetRepositoryConditional(ctx context.Context, owner, repo string, cond *ConditionalInfo) (*RepoMetrics, bool, *ConditionalInfo, error) {
	path := fmt.Sprintf("/repos/%s/%s", owner, repo)

	condResp, err := c.GetConditional(ctx, path, cond)
	if err != nil {
		return nil, false, nil, fmt.Errorf("fetching repository %s/%s: %w", owner, repo, err)
	}

	// Return early if not modified
	if condResp.NotModified {
		return nil, true, &condResp.Info, nil
	}

	defer condResp.Response.Body.Close()

	if condResp.Response.StatusCode != http.StatusOK {
		return nil, false, nil, fmt.Errorf("unexpected status %d for %s/%s",
			condResp.Response.StatusCode, owner, repo)
	}

	var resp githubRepoResponse
	if err := decodeJSON(condResp.Response.Body, &resp); err != nil {
		return nil, false, nil, fmt.Errorf("decoding response: %w", err)
	}

	metrics := &RepoMetrics{
		Owner:       resp.Owner.Login,
		Name:        resp.Name,
		FullName:    resp.FullName,
		Stars:       resp.StargazersCount,
		Forks:       resp.ForksCount,
		OpenIssues:  resp.OpenIssuesCount,
		Language:    resp.Language,
		Topics:      resp.Topics,
		Description: resp.Description,
	}

	return metrics, false, &condResp.Info, nil
}
