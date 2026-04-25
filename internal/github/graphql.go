package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// MaxGraphQLBatchSize is the maximum number of repos queried in a single
// GraphQL request. GitHub's GraphQL API has a soft node limit of 500,000
// per query; with ~6-8 nodes per repo fragment this bounds us at ~50-80.
// We pick 50 to stay well within the limit and match the plan spec.
const MaxGraphQLBatchSize = 50

// graphqlEndpoint is the path relative to the configured base URL.
const graphqlEndpoint = "/graphql"

// BulkFetchResult contains the outcome of a GraphQL bulk metadata fetch.
type BulkFetchResult struct {
	// Metrics is keyed by "owner/name".
	Metrics map[string]*RepoMetrics
	// NotFound contains repos whose alias resolved to null (deleted/renamed).
	NotFound []string
	// FailedBatches contains per-batch failures that survived the per-batch
	// retry budget. The outer loop continues past these so neighbouring
	// batches still persist; callers can inspect this slice to decide
	// whether to escalate or rely on the next refresh tick.
	FailedBatches []BatchFailure
	// QueryCount is the number of HTTP requests issued (one per batch).
	QueryCount int
}

// BatchFailure describes one batch that failed all retry attempts.
type BatchFailure struct {
	Start int   // inclusive index into the input slice
	End   int   // exclusive index into the input slice
	Err   error // underlying error from the per-batch HTTP/parse
}

// MaxConsecutiveBatchFailures bounds how many batches in a row may fail
// before BulkFetchMetadata aborts the sweep. This prevents a sustained
// outage from burning the entire retry budget on every batch in a 60-batch
// sweep at 3,000 repos.
const MaxConsecutiveBatchFailures = 3

// BulkFetchMetadata fetches repository metadata for the given repos in
// batches of up to MaxGraphQLBatchSize, using GraphQL with aliased fields.
//
// Each GraphQL request counts as ONE against the REST-equivalent rate
// limit bucket shared with REST (GitHub's v4 GraphQL rate limit uses a
// point-based system that is effectively 5000 points/hr for typical
// queries; one batched repo query ≈ 10-50 points).
//
// The hot path is metadata refresh (stars, forks, open issues, open PRs,
// primary language, topics, description). Activity metrics (commits,
// merged PRs, contributors) stay on REST.
func (c *Client) BulkFetchMetadata(ctx context.Context, repos []Repo) (*BulkFetchResult, error) {
	result := &BulkFetchResult{
		Metrics: make(map[string]*RepoMetrics, len(repos)),
	}

	if len(repos) == 0 {
		return result, nil
	}

	consecutiveFailures := 0
	for start := 0; start < len(repos); start += MaxGraphQLBatchSize {
		end := start + MaxGraphQLBatchSize
		if end > len(repos) {
			end = len(repos)
		}
		batch := repos[start:end]

		// Honour caller cancellation between batches so a late ctx
		// cancellation does not silently issue more requests.
		if err := ctx.Err(); err != nil {
			return result, err
		}

		err := c.bulkFetchBatch(ctx, batch, result)
		result.QueryCount++
		if err != nil {
			result.FailedBatches = append(result.FailedBatches, BatchFailure{
				Start: start,
				End:   end,
				Err:   fmt.Errorf("graphql batch [%d,%d): %w", start, end, err),
			})
			consecutiveFailures++
			if consecutiveFailures >= MaxConsecutiveBatchFailures {
				return result, fmt.Errorf(
					"graphql bulk fetch aborted after %d consecutive batch failures (last: %w)",
					consecutiveFailures, err,
				)
			}
			continue
		}
		consecutiveFailures = 0
	}

	return result, nil
}

// graphqlRepoFragment is the set of fields collected per repository.
// Keep this in sync with RepoMetrics; the fragment name is used below.
const graphqlRepoFragment = `fragment RepoFields on Repository {
  nameWithOwner
  stargazerCount
  forkCount
  issues(states: OPEN) { totalCount }
  pullRequests(states: OPEN) { totalCount }
  primaryLanguage { name }
  repositoryTopics(first: 20) { nodes { topic { name } } }
  description
}`

// bulkFetchBatch issues a single aliased GraphQL query for up to
// MaxGraphQLBatchSize repos, parses the JSON response, and merges results
// into `out`.
func (c *Client) bulkFetchBatch(ctx context.Context, batch []Repo, out *BulkFetchResult) error {
	query, aliasToRepo := buildBulkQuery(batch)

	reqBody, err := json.Marshal(map[string]string{"query": query})
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+graphqlEndpoint,
		bytes.NewReader(reqBody),
	)
	if err != nil {
		return fmt.Errorf("new graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github.v4+json")
	// DoWithRetry rewinds the request body on retry; the body is small
	// (one batched query) so re-buffering is cheap.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(reqBody)), nil
	}

	resp, err := c.DoWithRetry(req)
	if err != nil {
		c.notifyCall("graphql", "error")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		result := "error"
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			result = "rate_limited"
		}
		c.notifyCall("graphql", result)
		return &APIError{StatusCode: resp.StatusCode, Message: string(body)}
	}
	c.notifyCall("graphql", "ok")

	var envelope graphqlEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode graphql response: %w", err)
	}

	// Any top-level errors (e.g. rate limit, auth) are fatal for the batch.
	// Per-alias errors (NOT_FOUND) are embedded in `errors` but `data` is
	// still partially populated — we treat those as "not found" and
	// continue.
	if len(envelope.Errors) > 0 && len(envelope.Data) == 0 {
		return fmt.Errorf("graphql errors: %s", formatGraphQLErrors(envelope.Errors))
	}

	for alias, repo := range aliasToRepo {
		raw, ok := envelope.Data[alias]
		if !ok || bytes.Equal(raw, []byte("null")) {
			out.NotFound = append(out.NotFound, fmt.Sprintf("%s/%s", repo.Owner, repo.Name))
			continue
		}

		var node graphqlRepoNode
		if err := json.Unmarshal(raw, &node); err != nil {
			return fmt.Errorf("decode alias %s: %w", alias, err)
		}

		fullName := node.NameWithOwner
		if fullName == "" {
			fullName = fmt.Sprintf("%s/%s", repo.Owner, repo.Name)
		}

		metrics := &RepoMetrics{
			Owner:       repo.Owner,
			Name:        repo.Name,
			FullName:    fullName,
			Stars:       node.StargazerCount,
			Forks:       node.ForkCount,
			OpenIssues:  node.Issues.TotalCount,
			OpenPRs:     node.PullRequests.TotalCount,
			Description: node.Description,
		}
		if node.PrimaryLanguage != nil {
			metrics.Language = node.PrimaryLanguage.Name
		}
		for _, t := range node.RepositoryTopics.Nodes {
			metrics.Topics = append(metrics.Topics, t.Topic.Name)
		}

		out.Metrics[fullName] = metrics
	}

	return nil
}

// buildBulkQuery constructs a single GraphQL document that queries each
// repo under an alias like `r0`, `r1`, ... and returns the alias→repo
// mapping so the response can be demultiplexed.
func buildBulkQuery(batch []Repo) (string, map[string]Repo) {
	var sb strings.Builder
	aliasMap := make(map[string]Repo, len(batch))

	sb.WriteString("query {\n")
	for i, r := range batch {
		alias := fmt.Sprintf("r%d", i)
		aliasMap[alias] = r
		sb.WriteString(fmt.Sprintf(
			"  %s: repository(owner: %q, name: %q) { ...RepoFields }\n",
			alias, r.Owner, r.Name,
		))
	}
	sb.WriteString("}\n")
	sb.WriteString(graphqlRepoFragment)
	return sb.String(), aliasMap
}

// graphqlEnvelope matches GitHub's standard GraphQL response shape.
type graphqlEnvelope struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []graphqlError             `json:"errors"`
}

type graphqlError struct {
	Type    string   `json:"type"`
	Message string   `json:"message"`
	Path    []string `json:"path"`
}

func formatGraphQLErrors(errs []graphqlError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%s: %s", e.Type, e.Message))
	}
	return strings.Join(parts, "; ")
}

// graphqlRepoNode is the decoded shape of one aliased `repository` response.
type graphqlRepoNode struct {
	NameWithOwner    string              `json:"nameWithOwner"`
	StargazerCount   int                 `json:"stargazerCount"`
	ForkCount        int                 `json:"forkCount"`
	Issues           graphqlTotalCount   `json:"issues"`
	PullRequests     graphqlTotalCount   `json:"pullRequests"`
	PrimaryLanguage  *graphqlLanguage    `json:"primaryLanguage"`
	RepositoryTopics graphqlTopicResults `json:"repositoryTopics"`
	Description      string              `json:"description"`
}

type graphqlTotalCount struct {
	TotalCount int `json:"totalCount"`
}

type graphqlLanguage struct {
	Name string `json:"name"`
}

type graphqlTopicResults struct {
	Nodes []graphqlTopicNode `json:"nodes"`
}

type graphqlTopicNode struct {
	Topic struct {
		Name string `json:"name"`
	} `json:"topic"`
}
