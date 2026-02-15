package github

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// SearchResult represents a repository from search results.
type SearchResult struct {
	Owner       string
	Name        string
	FullName    string
	Description string
	Language    string
	Topics      []string
	Stars       int
	Forks       int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// searchResponse represents the GitHub search API response.
type searchResponse struct {
	TotalCount        int                `json:"total_count"`
	IncompleteResults bool               `json:"incomplete_results"`
	Items             []searchResultItem `json:"items"`
}

// searchResultItem represents a single repository in search results.
type searchResultItem struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name            string   `json:"name"`
	FullName        string   `json:"full_name"`
	Description     string   `json:"description"`
	Language        string   `json:"language"`
	Topics          []string `json:"topics"`
	StargazersCount int      `json:"stargazers_count"`
	ForksCount      int      `json:"forks_count"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

// SearchRepositories searches for repositories matching the given query.
// The query uses GitHub's search syntax: https://docs.github.com/en/search-github/searching-on-github/searching-for-repositories
// sort can be: stars, forks, help-wanted-issues, updated (empty for best match)
// order can be: desc, asc (default: desc)
// perPage limits results (max 100)
func (c *Client) SearchRepositories(ctx context.Context, query, sort, order string, perPage int) ([]SearchResult, error) {
	if perPage <= 0 {
		perPage = 30
	}
	if perPage > 100 {
		perPage = 100
	}

	// Build the search URL with query parameters
	params := url.Values{}
	params.Set("q", query)
	if sort != "" {
		params.Set("sort", sort)
	}
	if order != "" {
		params.Set("order", order)
	}
	params.Set("per_page", fmt.Sprintf("%d", perPage))

	path := "/search/repositories?" + params.Encode()

	var resp searchResponse
	if err := c.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("searching repositories: %w", err)
	}

	results := make([]SearchResult, 0, len(resp.Items))
	for _, item := range resp.Items {
		createdAt, _ := time.Parse(time.RFC3339, item.CreatedAt)
		updatedAt, _ := time.Parse(time.RFC3339, item.UpdatedAt)

		results = append(results, SearchResult{
			Owner:       item.Owner.Login,
			Name:        item.Name,
			FullName:    item.FullName,
			Description: item.Description,
			Language:    item.Language,
			Topics:      item.Topics,
			Stars:       item.StargazersCount,
			Forks:       item.ForksCount,
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		})
	}

	return results, nil
}
