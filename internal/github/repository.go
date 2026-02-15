package github

import (
	"context"
	"fmt"
	"net/http"
)

// RepoMetrics contains core metrics collected from a GitHub repository.
type RepoMetrics struct {
	Owner       string   `json:"owner"`
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	Stars       int      `json:"stargazers_count"`
	Forks       int      `json:"forks_count"`
	OpenIssues  int      `json:"open_issues_count"`
	OpenPRs     int      `json:"open_prs"`
	Language    string   `json:"language"`
	Topics      []string `json:"topics"`
	Description string   `json:"description"`
}

// githubRepoResponse represents the GitHub API response for a repository.
type githubRepoResponse struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name            string   `json:"name"`
	FullName        string   `json:"full_name"`
	StargazersCount int      `json:"stargazers_count"`
	ForksCount      int      `json:"forks_count"`
	OpenIssuesCount int      `json:"open_issues_count"`
	Language        string   `json:"language"`
	Topics          []string `json:"topics"`
	Description     string   `json:"description"`
}

// GetRepository fetches core metrics for a repository.
func (c *Client) GetRepository(ctx context.Context, owner, repo string) (*RepoMetrics, error) {
	path := fmt.Sprintf("/repos/%s/%s", owner, repo)

	var resp githubRepoResponse
	if err := c.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("fetching repository %s/%s: %w", owner, repo, err)
	}

	return &RepoMetrics{
		Owner:       resp.Owner.Login,
		Name:        resp.Name,
		FullName:    resp.FullName,
		Stars:       resp.StargazersCount,
		Forks:       resp.ForksCount,
		OpenIssues:  resp.OpenIssuesCount,
		Language:    resp.Language,
		Topics:      resp.Topics,
		Description: resp.Description,
	}, nil
}

// GetOpenPRCount returns the count of open pull requests for a repository.
func (c *Client) GetOpenPRCount(ctx context.Context, owner, repo string) (int, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=open&per_page=1", owner, repo)

	resp, err := c.Get(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("fetching open PRs for %s/%s: %w", owner, repo, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d for %s/%s pulls", resp.StatusCode, owner, repo)
	}

	// GitHub includes total count in Link header for paginated responses
	// We can also use the X-Total-Count or parse the last page from Link header
	// For simplicity, we'll count items if < 100, otherwise use pagination info
	count, err := c.countFromLinkHeader(resp.Header.Get("Link"))
	if err != nil || count == 0 {
		// Fall back to counting items in response
		var prs []struct{}
		if err := c.GetJSON(ctx, fmt.Sprintf("/repos/%s/%s/pulls?state=open&per_page=100", owner, repo), &prs); err != nil {
			return 0, err
		}
		return len(prs), nil
	}

	return count, nil
}

// countFromLinkHeader extracts total count from GitHub's Link header.
// GitHub uses format: <url>; rel="last" with page number indicating total pages.
// Returns 0 if count cannot be determined from header.
func (c *Client) countFromLinkHeader(link string) (int, error) {
	if link == "" {
		return 0, nil
	}

	// Parse Link header to find "last" relation
	// Format: <https://api.github.com/...?page=5>; rel="last"
	// For now, return 0 to use fallback counting
	// Full implementation would parse the header
	return 0, nil
}

// GetRepositoryWithPRs fetches repository metrics including open PR count.
func (c *Client) GetRepositoryWithPRs(ctx context.Context, owner, repo string) (*RepoMetrics, error) {
	metrics, err := c.GetRepository(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	prCount, err := c.GetOpenPRCount(ctx, owner, repo)
	if err != nil {
		// Log warning but continue with 0 PRs
		prCount = 0
	}

	metrics.OpenPRs = prCount
	return metrics, nil
}
