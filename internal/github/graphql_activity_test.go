package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/state"
)

// newTestStore returns a `*state.Store` backed by a temp file scoped to
// the test. Mirrors the inline pattern used by scanner_test.go but
// trimmed to one helper so the activity-fold tests stay readable.
func newTestStore(t *testing.T) *state.Store {
	t.Helper()
	tmpDir := t.TempDir()
	return state.NewStore(filepath.Join(tmpDir, "state.json"))
}

// fixedNow is a frozen "now" used for deterministic 7-day-window math
// in extractActivityFromNode unit tests.
var fixedNow = time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)

// TestExtractActivityFromNode_HappyPath verifies the parse-and-filter
// logic for the GraphQL activity sub-tree against a realistic node mix:
// some merges in-window, some out-of-window, an issue in-window, and a
// release.
func TestExtractActivityFromNode_HappyPath(t *testing.T) {
	inWindow := fixedNow.Add(-3 * 24 * time.Hour)
	outOfWindow := fixedNow.Add(-30 * 24 * time.Hour)

	node := &graphqlRepoNode{
		RecentMergedPRs: graphqlMergedPRsPage{
			Nodes: []graphqlMergedPR{
				{MergedAt: &inWindow},
				{MergedAt: &inWindow},
				{MergedAt: &outOfWindow},
				{MergedAt: nil}, // never merged
			},
		},
		RecentIssues: graphqlIssuesPage{
			Nodes: []graphqlIssueNode{
				{CreatedAt: inWindow},
				{CreatedAt: outOfWindow},
			},
		},
		MentionableUsers: graphqlTotalCount{TotalCount: 42},
		LatestReleases: graphqlReleasesResult{
			Nodes: []graphqlReleaseNode{
				{TagName: "v2.0.0", Name: "Release 2.0", PublishedAt: inWindow, URL: "https://example/v2"},
			},
		},
	}

	got, truncated := extractActivityFromNode(node, fixedNow)
	if got == nil {
		t.Fatal("expected non-nil ActivityMetrics")
	}
	if got.MergedPRs7d != 2 {
		t.Errorf("MergedPRs7d = %d, want 2", got.MergedPRs7d)
	}
	if got.NewIssues7d != 1 {
		t.Errorf("NewIssues7d = %d, want 1", got.NewIssues7d)
	}
	if got.Contributors != 42 {
		t.Errorf("Contributors = %d, want 42 (mentionableUsers proxy)", got.Contributors)
	}
	if got.LatestRelease == nil || got.LatestRelease.TagName != "v2.0.0" {
		t.Errorf("LatestRelease = %+v, want v2.0.0", got.LatestRelease)
	}
	if truncated {
		t.Errorf("truncated = true, want false (under 100-node page)")
	}
}

// TestExtractActivityFromNode_TruncatedPage exercises the saturation
// guard: when the merged-PR connection returns a full 100-node page AND
// the oldest node is still inside the 7-day window AND the page reports
// hasNextPage, the count is a lower bound and we must signal truncation
// so the caller can fall back to REST.
func TestExtractActivityFromNode_TruncatedPage(t *testing.T) {
	inWindow := fixedNow.Add(-2 * 24 * time.Hour)

	prs := make([]graphqlMergedPR, graphqlActivityNodePage)
	for i := range prs {
		t := inWindow
		prs[i] = graphqlMergedPR{MergedAt: &t}
	}
	node := &graphqlRepoNode{
		RecentMergedPRs: graphqlMergedPRsPage{
			Nodes:    prs,
			PageInfo: graphqlPageInfo{HasNextPage: true},
		},
	}
	got, truncated := extractActivityFromNode(node, fixedNow)
	if got == nil {
		t.Fatal("expected non-nil ActivityMetrics")
	}
	if got.MergedPRs7d != graphqlActivityNodePage {
		t.Errorf("MergedPRs7d = %d, want %d", got.MergedPRs7d, graphqlActivityNodePage)
	}
	if !truncated {
		t.Errorf("truncated = false, want true (full page + hasNextPage + oldest in-window)")
	}
}

// TestExtractActivityFromNode_NotTruncatedWhenTailBeyondWindow covers
// the "page is full but the tail crosses the 7-day window" case: even
// though hasNextPage may be true, every PR beyond the in-window tail is
// older than 7 days, so the count is exact and we MUST NOT mark it
// truncated (or we would burn an unnecessary REST fallback).
func TestExtractActivityFromNode_NotTruncatedWhenTailBeyondWindow(t *testing.T) {
	inWindow := fixedNow.Add(-2 * 24 * time.Hour)
	outOfWindow := fixedNow.Add(-10 * 24 * time.Hour)

	prs := make([]graphqlMergedPR, graphqlActivityNodePage)
	// First half in window, last node out of window.
	for i := 0; i < graphqlActivityNodePage-1; i++ {
		t := inWindow
		prs[i] = graphqlMergedPR{MergedAt: &t}
	}
	tt := outOfWindow
	prs[graphqlActivityNodePage-1] = graphqlMergedPR{MergedAt: &tt}

	node := &graphqlRepoNode{
		RecentMergedPRs: graphqlMergedPRsPage{
			Nodes:    prs,
			PageInfo: graphqlPageInfo{HasNextPage: true},
		},
	}
	_, truncated := extractActivityFromNode(node, fixedNow)
	if truncated {
		t.Errorf("truncated = true, want false (oldest node in page is outside 7d window)")
	}
}

// TestBulkFetchMetadata_PopulatesActivity is the integration-shape
// assertion for T5b: after the GraphQL bulk fetch returns activity
// fields, RepoMetrics.Activity must be populated and ActivityTruncated
// must reflect the page-saturation state.
func TestBulkFetchMetadata_PopulatesActivity(t *testing.T) {
	mergedAt := time.Now().Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	createdAt := time.Now().Add(-1 * 24 * time.Hour).Format(time.RFC3339)
	publishedAt := time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "recentMergedPRs") {
			t.Errorf("query missing recentMergedPRs alias: %s", body)
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
			"data": {
				"r0": {
					"nameWithOwner": "a/x",
					"stargazerCount": 100,
					"forkCount": 5,
					"issues": {"totalCount": 2},
					"pullRequests": {"totalCount": 3},
					"primaryLanguage": {"name": "Go"},
					"repositoryTopics": {"nodes": []},
					"description": "hello",
					"recentMergedPRs": {
						"nodes": [{"mergedAt": %q}, {"mergedAt": %q}],
						"pageInfo": {"hasNextPage": false}
					},
					"recentIssues": {
						"nodes": [{"createdAt": %q}],
						"pageInfo": {"hasNextPage": false}
					},
					"mentionableUsers": {"totalCount": 17},
					"latestReleases": {
						"nodes": [{"tagName": "v1.2.3", "name": "Release 1.2.3", "publishedAt": %q, "url": "https://github.com/a/x/releases/tag/v1.2.3"}]
					}
				}
			}
		}`, mergedAt, mergedAt, createdAt, publishedAt)
	}))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)

	out, err := client.BulkFetchMetadata(context.Background(), []Repo{{Owner: "a", Name: "x"}})
	if err != nil {
		t.Fatalf("BulkFetchMetadata: %v", err)
	}

	m := out.Metrics["a/x"]
	if m == nil {
		t.Fatal("missing metrics for a/x")
	}
	if m.Activity == nil {
		t.Fatal("Activity is nil; expected populated from GraphQL")
	}
	if m.ActivityTruncated {
		t.Errorf("ActivityTruncated = true, want false (under page cap)")
	}
	if m.Activity.MergedPRs7d != 2 {
		t.Errorf("MergedPRs7d = %d, want 2", m.Activity.MergedPRs7d)
	}
	if m.Activity.NewIssues7d != 1 {
		t.Errorf("NewIssues7d = %d, want 1", m.Activity.NewIssues7d)
	}
	if m.Activity.Contributors != 17 {
		t.Errorf("Contributors = %d, want 17", m.Activity.Contributors)
	}
	if m.Activity.LatestRelease == nil || m.Activity.LatestRelease.TagName != "v1.2.3" {
		t.Errorf("LatestRelease = %+v", m.Activity.LatestRelease)
	}
}

// TestBulkFetchMetadata_FragmentDeclaresActivity asserts the query
// contains the activity sub-aliases. This is a guard against a future
// refactor accidentally dropping the activity fields and silently
// reverting T5b's HTTP-rate gain.
func TestBulkFetchMetadata_FragmentDeclaresActivity(t *testing.T) {
	q, _ := buildBulkQuery([]Repo{{Owner: "a", Name: "x"}})
	for _, marker := range []string{
		"recentMergedPRs:",
		"recentIssues:",
		"mentionableUsers(first: 1)",
		"latestReleases:",
	} {
		if !strings.Contains(q, marker) {
			t.Errorf("query missing %q (T5b fold-in regression?)", marker)
		}
	}
}

// activityCallCounter is a tiny stub used by the scanner-fold tests
// below to confirm whether the per-repo REST activity path was invoked.
type activityCallCounter struct {
	pulls, issues, contributors, releases int
}

func newActivityRESTServer(t *testing.T, c *activityCallCounter, graphqlData string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/graphql":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, graphqlData)
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			c.pulls++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]"))
		case strings.HasSuffix(r.URL.Path, "/issues"):
			c.issues++
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]"))
		case strings.HasSuffix(r.URL.Path, "/contributors"):
			c.contributors++
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			c.releases++
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

// happyGraphQLForRepo returns a stub GraphQL data string with one alias
// "r0" for owner/name; truncated controls the saturation flag mirroring
// the production parser's truncation rule (full page + hasNextPage +
// oldest-in-window).
func happyGraphQLForRepo(owner, name string, truncated bool) string {
	mergedAt := time.Now().Add(-2 * 24 * time.Hour).Format(time.RFC3339)
	createdAt := time.Now().Add(-1 * 24 * time.Hour).Format(time.RFC3339)

	prNodes := fmt.Sprintf(`[{"mergedAt": %q}]`, mergedAt)
	hasNext := false
	if truncated {
		// Build a 100-element node array, all in-window, with hasNextPage=true.
		var b strings.Builder
		b.WriteString("[")
		for i := 0; i < graphqlActivityNodePage; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"mergedAt": %q}`, mergedAt)
		}
		b.WriteString("]")
		prNodes = b.String()
		hasNext = true
	}

	return fmt.Sprintf(`{
		"data": {
			"r0": {
				"nameWithOwner": %q,
				"stargazerCount": 1,
				"forkCount": 0,
				"issues": {"totalCount": 0},
				"pullRequests": {"totalCount": 0},
				"primaryLanguage": null,
				"repositoryTopics": {"nodes": []},
				"description": "",
				"recentMergedPRs": {"nodes": %s, "pageInfo": {"hasNextPage": %t}},
				"recentIssues": {"nodes": [{"createdAt": %q}], "pageInfo": {"hasNextPage": false}},
				"mentionableUsers": {"totalCount": 5},
				"latestReleases": {"nodes": []}
			}
		}
	}`, owner+"/"+name, prNodes, hasNext, createdAt)
}

// TestScanBulk_UsesGraphQLActivity_NoRESTFallback proves the integration
// gain from T5b: when GraphQL activity is present and not truncated,
// Scanner.ScanBulk consumes it directly and issues ZERO REST sub-calls
// — the heart of the HTTP-rate gate.
func TestScanBulk_UsesGraphQLActivity_NoRESTFallback(t *testing.T) {
	counter := &activityCallCounter{}
	server := newActivityRESTServer(t, counter, happyGraphQLForRepo("a", "x", false))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)

	store := newTestStore(t)
	scanner := NewScanner(client, store)
	scanner.collector.collectActivity = true

	res, err := scanner.ScanBulk(context.Background(), []Repo{{Owner: "a", Name: "x"}})
	if err != nil {
		t.Fatalf("ScanBulk: %v", err)
	}
	if res.Successful != 1 {
		t.Errorf("Successful = %d, want 1", res.Successful)
	}
	if counter.pulls+counter.issues+counter.contributors+counter.releases != 0 {
		t.Errorf("REST activity was called: pulls=%d issues=%d contributors=%d releases=%d (want 0)",
			counter.pulls, counter.issues, counter.contributors, counter.releases)
	}

	state := store.GetRepoState("a/x")
	if state == nil {
		t.Fatal("state for a/x was not persisted")
	}
	if state.MergedPRs7d != 1 {
		t.Errorf("MergedPRs7d = %d, want 1 (from GraphQL)", state.MergedPRs7d)
	}
	if state.Contributors != 5 {
		t.Errorf("Contributors = %d, want 5 (from mentionableUsers)", state.Contributors)
	}
}

// TestScanBulk_FallsBackToRESTWhenTruncated proves the fallback path:
// for very-high-velocity repos that saturate the 100-node page within
// the 7-day window, we MUST hit REST so the count is accurate even at
// the cost of 4 extra HTTP calls for that one repo.
func TestScanBulk_FallsBackToRESTWhenTruncated(t *testing.T) {
	counter := &activityCallCounter{}
	server := newActivityRESTServer(t, counter, happyGraphQLForRepo("a", "x", true))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)

	store := newTestStore(t)
	scanner := NewScanner(client, store)
	scanner.collector.collectActivity = true

	res, err := scanner.ScanBulk(context.Background(), []Repo{{Owner: "a", Name: "x"}})
	if err != nil {
		t.Fatalf("ScanBulk: %v", err)
	}
	if res.Successful != 1 {
		t.Errorf("Successful = %d, want 1", res.Successful)
	}
	// Truncation should have triggered all four REST sub-calls.
	if counter.pulls == 0 || counter.issues == 0 || counter.contributors == 0 || counter.releases == 0 {
		t.Errorf("expected REST fallback to fire all 4 sub-calls, got pulls=%d issues=%d contributors=%d releases=%d",
			counter.pulls, counter.issues, counter.contributors, counter.releases)
	}
}
