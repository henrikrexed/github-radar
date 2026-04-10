package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// ReadmeResponse holds the result of a README fetch.
type ReadmeResponse struct {
	// Content is the raw README text. Empty if not found or not modified.
	Content string
	// ETag is the new ETag from the response for future conditional requests.
	ETag string
	// NotModified is true when the server returned 304 (content unchanged).
	NotModified bool
	// Found is false when the repository has no README (404).
	Found bool
}

// GetReadme fetches the decoded README content for a repository.
// Uses GET /repos/{owner}/{repo}/readme with Accept: application/vnd.github.raw
// to get the raw text content (GitHub decodes base64 server-side).
//
// Supports conditional GET: pass a previous ETag to avoid re-downloading
// unchanged content. Pass empty string to skip conditional request.
//
// Returns:
//   - 200: Content + new ETag, Found=true
//   - 304: NotModified=true, Found=true (content unchanged since etag)
//   - 404: Found=false (repo has no README)
func (c *Client) GetReadme(ctx context.Context, owner, repo, etag string) (*ReadmeResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/readme", owner, repo)

	req, err := c.newRequest(ctx, http.MethodGet, path)
	if err != nil {
		return nil, fmt.Errorf("creating readme request: %w", err)
	}

	// Request raw content so GitHub decodes base64 for us.
	req.Header.Set("Accept", "application/vnd.github.raw")

	// Conditional GET with ETag.
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching readme for %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	newETag := resp.Header.Get("ETag")

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading readme body for %s/%s: %w", owner, repo, err)
		}
		return &ReadmeResponse{
			Content: string(body),
			ETag:    newETag,
			Found:   true,
		}, nil

	case http.StatusNotModified:
		return &ReadmeResponse{
			ETag:        newETag,
			NotModified: true,
			Found:       true,
		}, nil

	case http.StatusNotFound:
		return &ReadmeResponse{Found: false}, nil

	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("unexpected status %d fetching readme for %s/%s: %s",
			resp.StatusCode, owner, repo, string(body))
	}
}
