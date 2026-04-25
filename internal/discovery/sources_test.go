package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
)

// TestDiscoverer_DiscoverAll_CrossSourceDedup verifies that a repository
// surfaced by both the topic source AND the org source is inserted once
// per cycle, not twice. This is the contract for `discovered_known_repos`
// uniqueness when multi-source discovery is enabled (ISI-578 Stage A).
//
// Setup: one mock GitHub Search API server that always returns the same
// repo regardless of query. Discoverer is configured with one topic AND
// one org. Plan rev 5 ordering: topic source runs first, so the repo is
// counted in the topic Result and dropped from the org Result.
//
// Expected behavior:
//   - Both source steps execute (verified via callCount == 2)
//   - Topic Result.Repos contains the repo (winning source)
//   - Org Result.Repos is empty (deduplicated)
//   - Org Result counters (NewRepos, AfterFilters) are decremented
//     accordingly so unique-repo totals stay accurate.
func TestDiscoverer_DiscoverAll_CrossSourceDedup(t *testing.T) {
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		// Return the same single repo regardless of which query the
		// discoverer sends — exactly the cross-source overlap case.
		resp := map[string]interface{}{
			"total_count":        1,
			"incomplete_results": false,
			"items": []map[string]interface{}{
				{
					"owner":            map[string]string{"login": "kubernetes"},
					"name":             "kubernetes",
					"full_name":        "kubernetes/kubernetes",
					"description":      "Production-Grade Container Scheduling",
					"language":         "Go",
					"topics":           []string{"kubernetes", "containers"},
					"stargazers_count": 100000,
					"forks_count":      30000,
					"created_at":       time.Now().AddDate(-5, 0, 0).Format(time.RFC3339),
					"updated_at":       time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)

	store := state.NewStore("")

	cfg := Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		AutoTrackThreshold: 50.0,
		Sources: SourcesConfig{
			Orgs: OrgsSourceConfig{
				Enabled: true,
				Names:   []string{"kubernetes"},
			},
		},
	}
	d := NewDiscoverer(client, store, cfg)
	d.SetSearchThrottle(0) // keep test fast; throttle behavior is tested separately

	results, err := d.DiscoverAll(context.Background())
	if err != nil {
		t.Fatalf("DiscoverAll failed: %v", err)
	}

	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 Search API calls (topic + org), got %d", got)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results (one per source step), got %d", len(results))
	}

	// Topic step ran first — should contain the repo.
	topicResult := results[0]
	if topicResult.Topic != "kubernetes" {
		t.Errorf("expected first result.Topic=kubernetes, got %q", topicResult.Topic)
	}
	if len(topicResult.Repos) != 1 {
		t.Errorf("expected topic source to contain 1 repo, got %d", len(topicResult.Repos))
	}

	// Org step ran second — repo was already seen, must be dropped.
	orgResult := results[1]
	if orgResult.Topic != "org:kubernetes" {
		t.Errorf("expected second result.Topic=org:kubernetes, got %q", orgResult.Topic)
	}
	if len(orgResult.Repos) != 0 {
		t.Errorf("expected org source to be deduplicated empty, got %d repos: %+v",
			len(orgResult.Repos), orgResult.Repos)
	}
	if orgResult.NewRepos != 0 {
		t.Errorf("expected org source NewRepos=0 after dedup, got %d", orgResult.NewRepos)
	}
	if orgResult.AfterFilters != 0 {
		t.Errorf("expected org source AfterFilters=0 after dedup, got %d", orgResult.AfterFilters)
	}

	// TotalFound is pre-dedup — it represents what the API returned.
	// Both sources hit the API and got 1 hit each, so both should
	// report TotalFound=1 even though only one survives dedup.
	if topicResult.TotalFound != 1 || orgResult.TotalFound != 1 {
		t.Errorf("expected TotalFound=1 on both pre-dedup; got topic=%d org=%d",
			topicResult.TotalFound, orgResult.TotalFound)
	}
}

// TestDiscoverer_DiscoverAll_ThrottleAcrossSources verifies that the
// 2s Search API throttle from `bbf12fb` is enforced across topic, org,
// and language source boundaries — i.e. the throttle is shared, not
// per-source. This is what keeps total Search RPS under 30/min when
// all four discovery sources are active.
//
// Setup: 1 topic + 1 org + 1 language with one push-window = 3 calls
// total. With the throttle set to a non-zero value, total elapsed time
// must be at least 2 * throttle (gaps between calls 1→2 and 2→3). The
// first call carries no upfront wait, matching production semantics.
func TestDiscoverer_DiscoverAll_ThrottleAcrossSources(t *testing.T) {
	var (
		mu      sync.Mutex
		queries []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		// Decode q= so the test can assert query shape per source
		// without depending on raw URL ordering.
		q, _ := url.QueryUnescape(r.URL.Query().Get("q"))
		queries = append(queries, q)
		mu.Unlock()

		resp := map[string]interface{}{
			"total_count":        0,
			"incomplete_results": false,
			"items":              []map[string]interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)

	store := state.NewStore("")

	cfg := Config{
		Topics:             []string{"kubernetes"},
		MinStars:           100,
		AutoTrackThreshold: 50.0,
		Sources: SourcesConfig{
			Orgs: OrgsSourceConfig{
				Enabled: true,
				Names:   []string{"hashicorp"},
			},
			Languages: LanguagesSourceConfig{
				Enabled:         true,
				Names:           []string{"Rust"},
				PushWindowsDays: []int{7},
			},
		},
	}
	d := NewDiscoverer(client, store, cfg)

	// Use a small but non-zero throttle so the test verifies real
	// time gating without burning seconds on every CI run.
	const throttle = 50 * time.Millisecond
	d.SetSearchThrottle(throttle)

	start := time.Now()
	if _, err := d.DiscoverAll(context.Background()); err != nil {
		t.Fatalf("DiscoverAll failed: %v", err)
	}
	elapsed := time.Since(start)

	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()

	if len(got) != 3 {
		t.Fatalf("expected 3 Search API calls (topic + org + language), got %d: %v",
			len(got), got)
	}

	// Each query must show its source's distinguishing predicate.
	if !strings.Contains(got[0], "topic:kubernetes") {
		t.Errorf("call 1 expected topic:kubernetes, got %q", got[0])
	}
	if !strings.Contains(got[1], "org:hashicorp") {
		t.Errorf("call 2 expected org:hashicorp, got %q", got[1])
	}
	if !strings.Contains(got[2], "language:Rust") || !strings.Contains(got[2], "pushed:>=") {
		t.Errorf("call 3 expected language:Rust + pushed:>=, got %q", got[2])
	}

	// Throttle is honored across all source boundaries: 3 calls means
	// 2 inter-call gaps, each ≥ throttle. Allow 5ms slack for
	// scheduler/HTTP overhead variability.
	min := 2*throttle - 5*time.Millisecond
	if elapsed < min {
		t.Errorf("expected total elapsed ≥ %v (2 throttle gaps), got %v", min, elapsed)
	}
}

// TestDiscoverer_DiscoverAll_NoSourcesNoTopics verifies that when both
// the topic source and all extra sources are disabled, DiscoverAll
// returns nil without making any API calls. This preserves the
// pre-Stage-A "no topics → no work" semantics.
func TestDiscoverer_DiscoverAll_NoSourcesNoTopics(t *testing.T) {
	client, _ := github.NewClient("test-token")
	store := state.NewStore("")

	d := NewDiscoverer(client, store, Config{})
	d.SetSearchThrottle(0)

	results, err := d.DiscoverAll(context.Background())
	if err != nil {
		t.Fatalf("DiscoverAll failed: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results when no sources configured, got %v", results)
	}
}

// TestDiscoverer_DiscoverOrg_QueryShape verifies the exact query the
// org source emits (including MinStars override semantics).
func TestDiscoverer_DiscoverOrg_QueryShape(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = url.QueryUnescape(r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count":        0,
			"incomplete_results": false,
			"items":              []map[string]interface{}{},
		})
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	cfg := Config{
		MinStars:           500,
		AutoTrackThreshold: 50.0,
		Sources: SourcesConfig{
			Orgs: OrgsSourceConfig{
				Enabled:  true,
				MinStars: 100, // override
			},
		},
	}
	d := NewDiscoverer(client, store, cfg)
	d.SetSearchThrottle(0)

	if _, err := d.DiscoverOrg(context.Background(), "kubernetes"); err != nil {
		t.Fatalf("DiscoverOrg failed: %v", err)
	}

	if !strings.Contains(got, "org:kubernetes") {
		t.Errorf("expected query to contain org:kubernetes, got %q", got)
	}
	// Override should win over Discovery.MinStars=500.
	if !strings.Contains(got, "stars:>=100") {
		t.Errorf("expected stars:>=100 (override), got %q", got)
	}
}

// TestDiscoverer_DiscoverLanguage_QueryShape verifies the language-pivot
// query shape, including the pushed:>= window and that MinStars falls
// back to Discovery.MinStars when not overridden.
func TestDiscoverer_DiscoverLanguage_QueryShape(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = url.QueryUnescape(r.URL.Query().Get("q"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count":        0,
			"incomplete_results": false,
			"items":              []map[string]interface{}{},
		})
	}))
	defer server.Close()

	client, _ := github.NewClient("test-token")
	client.SetBaseURL(server.URL)
	store := state.NewStore("")

	cfg := Config{
		MinStars:           500, // fallback when language MinStars is zero
		AutoTrackThreshold: 50.0,
		Sources: SourcesConfig{
			Languages: LanguagesSourceConfig{
				Enabled: true,
			},
		},
	}
	d := NewDiscoverer(client, store, cfg)
	d.SetSearchThrottle(0)

	if _, err := d.DiscoverLanguage(context.Background(), "Rust", 30); err != nil {
		t.Fatalf("DiscoverLanguage failed: %v", err)
	}

	if !strings.Contains(got, "language:Rust") {
		t.Errorf("expected language:Rust, got %q", got)
	}
	if !strings.Contains(got, "stars:>=500") {
		t.Errorf("expected stars:>=500 (fallback to Discovery.MinStars), got %q", got)
	}
	if !strings.Contains(got, "pushed:>=") {
		t.Errorf("expected pushed:>=cutoff, got %q", got)
	}
}
