package github

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// ActivityMetrics contains recent activity data for a repository.
type ActivityMetrics struct {
	MergedPRs7d   int          // PRs merged in the last 7 days
	NewIssues7d   int          // Issues opened in the last 7 days
	Contributors  int          // Total contributor count
	LatestRelease *ReleaseInfo // Latest release info (nil if no releases)
}

// ReleaseInfo contains information about a GitHub release.
type ReleaseInfo struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	URL         string    `json:"html_url"`
}

// pullRequestResponse represents a PR from the GitHub API.
type pullRequestResponse struct {
	Number   int        `json:"number"`
	State    string     `json:"state"`
	MergedAt *time.Time `json:"merged_at"`
}

// issueResponse represents an issue from the GitHub API.
type issueResponse struct {
	Number          int       `json:"number"`
	CreatedAt       time.Time `json:"created_at"`
	PullRequestInfo *struct{} `json:"pull_request,omitempty"` // Non-nil if this is a PR
}

// contributorResponse represents a contributor from the GitHub API.
type contributorResponse struct {
	Login string `json:"login"`
}

// releaseResponse represents a release from the GitHub API.
type releaseResponse struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// GetMergedPRsCount returns the number of PRs merged in the last 7 days.
func (c *Client) GetMergedPRsCount(ctx context.Context, owner, repo string) (int, error) {
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	count := 0
	page := 1

	for {
		path := fmt.Sprintf("/repos/%s/%s/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=%d",
			owner, repo, page)

		var prs []pullRequestResponse
		if err := c.GetJSON(ctx, path, &prs); err != nil {
			return 0, fmt.Errorf("fetching merged PRs for %s/%s: %w", owner, repo, err)
		}

		if len(prs) == 0 {
			break
		}

		recentInPage := 0
		for _, pr := range prs {
			if pr.MergedAt != nil && pr.MergedAt.After(sevenDaysAgo) {
				count++
				recentInPage++
			}
		}

		// If no recent PRs found in this page, all remaining pages will be older too
		// (since results are sorted by updated desc)
		if recentInPage == 0 {
			break
		}

		page++
		// Safety limit to prevent infinite loops
		if page > 10 {
			break
		}
	}

	return count, nil
}

// GetRecentIssuesCount returns the number of issues opened in the last 7 days.
// Note: GitHub's issues endpoint includes PRs, so we filter them out.
func (c *Client) GetRecentIssuesCount(ctx context.Context, owner, repo string) (int, error) {
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	since := sevenDaysAgo.Format(time.RFC3339)

	path := fmt.Sprintf("/repos/%s/%s/issues?state=all&since=%s&per_page=100", owner, repo, since)

	var issues []issueResponse
	if err := c.GetJSON(ctx, path, &issues); err != nil {
		return 0, fmt.Errorf("fetching recent issues for %s/%s: %w", owner, repo, err)
	}

	count := 0
	for _, issue := range issues {
		// Skip pull requests (GitHub includes them in issues endpoint)
		if issue.PullRequestInfo != nil {
			continue
		}
		// Only count issues created in our window
		if issue.CreatedAt.After(sevenDaysAgo) {
			count++
		}
	}

	return count, nil
}

// GetContributorCount returns the total number of contributors to a repository.
func (c *Client) GetContributorCount(ctx context.Context, owner, repo string) (int, error) {
	// GitHub's contributors endpoint is paginated
	// For efficiency, we request only 1 per page and check the Link header
	path := fmt.Sprintf("/repos/%s/%s/contributors?per_page=1&anon=false", owner, repo)

	resp, err := c.Get(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("fetching contributors for %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		// Repository has no contributors (empty repo)
		return 0, nil
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d for contributors", resp.StatusCode)
	}

	// Try to get count from Link header (last page number)
	// If that fails, fall back to counting all contributors
	count := c.parseLastPageFromLink(resp.Header.Get("Link"))
	if count > 0 {
		return count, nil
	}

	// Fall back: fetch all contributors and count
	path = fmt.Sprintf("/repos/%s/%s/contributors?per_page=100&anon=false", owner, repo)
	var contributors []contributorResponse
	if err := c.GetJSON(ctx, path, &contributors); err != nil {
		return 0, err
	}

	return len(contributors), nil
}

// linkLastPageRegex matches the last page from a Link header.
var linkLastPageRegex = regexp.MustCompile(`<[^>]*[?&]page=(\d+)[^>]*>;\s*rel="last"`)

// parseLastPageFromLink extracts the last page number from a Link header.
// Returns 0 if unable to parse.
func (c *Client) parseLastPageFromLink(link string) int {
	if link == "" {
		return 0
	}

	// Parse Link header format: <url?page=X>; rel="last"
	matches := linkLastPageRegex.FindStringSubmatch(link)
	if len(matches) >= 2 {
		if page, err := strconv.Atoi(matches[1]); err == nil {
			return page
		}
	}
	return 0
}

// GetLatestRelease returns information about the latest release.
// Returns nil if the repository has no releases.
func (c *Client) GetLatestRelease(ctx context.Context, owner, repo string) (*ReleaseInfo, error) {
	path := fmt.Sprintf("/repos/%s/%s/releases/latest", owner, repo)

	resp, err := c.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetching latest release for %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	// 404 means no releases
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for latest release", resp.StatusCode)
	}

	var release releaseResponse
	if err := decodeJSON(resp.Body, &release); err != nil {
		return nil, fmt.Errorf("decoding release response: %w", err)
	}

	return &ReleaseInfo{
		TagName:     release.TagName,
		Name:        release.Name,
		PublishedAt: release.PublishedAt,
		URL:         release.HTMLURL,
	}, nil
}

// ActivityError represents partial failures when collecting activity metrics.
type ActivityError struct {
	PRError          error
	IssueError       error
	ContributorError error
	ReleaseError     error
}

func (e *ActivityError) Error() string {
	var parts []string
	if e.PRError != nil {
		parts = append(parts, fmt.Sprintf("PRs: %v", e.PRError))
	}
	if e.IssueError != nil {
		parts = append(parts, fmt.Sprintf("issues: %v", e.IssueError))
	}
	if e.ContributorError != nil {
		parts = append(parts, fmt.Sprintf("contributors: %v", e.ContributorError))
	}
	if e.ReleaseError != nil {
		parts = append(parts, fmt.Sprintf("releases: %v", e.ReleaseError))
	}
	if len(parts) == 0 {
		return "no errors"
	}
	return fmt.Sprintf("partial activity errors: %v", parts)
}

// HasErrors returns true if any errors occurred.
func (e *ActivityError) HasErrors() bool {
	return e.PRError != nil || e.IssueError != nil || e.ContributorError != nil || e.ReleaseError != nil
}

// GetActivityMetrics collects all activity metrics for a repository.
// Returns partial results with an ActivityError if some metrics fail to collect.
func (c *Client) GetActivityMetrics(ctx context.Context, owner, repo string) (*ActivityMetrics, error) {
	metrics := &ActivityMetrics{}
	var actErr ActivityError

	// Collect merged PRs
	mergedPRs, err := c.GetMergedPRsCount(ctx, owner, repo)
	if err != nil {
		actErr.PRError = err
		mergedPRs = 0
	}
	metrics.MergedPRs7d = mergedPRs

	// Collect recent issues
	newIssues, err := c.GetRecentIssuesCount(ctx, owner, repo)
	if err != nil {
		actErr.IssueError = err
		newIssues = 0
	}
	metrics.NewIssues7d = newIssues

	// Collect contributor count
	contributors, err := c.GetContributorCount(ctx, owner, repo)
	if err != nil {
		actErr.ContributorError = err
		contributors = 0
	}
	metrics.Contributors = contributors

	// Collect latest release
	release, err := c.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		actErr.ReleaseError = err
		release = nil
	}
	metrics.LatestRelease = release

	if actErr.HasErrors() {
		return metrics, &actErr
	}
	return metrics, nil
}
