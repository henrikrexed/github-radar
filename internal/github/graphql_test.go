package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildBulkQuery_Aliases(t *testing.T) {
	batch := []Repo{
		{Owner: "a", Name: "x"},
		{Owner: "b", Name: "y"},
	}
	query, aliasMap := buildBulkQuery(batch)

	if !strings.Contains(query, `r0: repository(owner: "a", name: "x")`) {
		t.Errorf("missing r0 alias in query:\n%s", query)
	}
	if !strings.Contains(query, `r1: repository(owner: "b", name: "y")`) {
		t.Errorf("missing r1 alias in query:\n%s", query)
	}
	if !strings.Contains(query, "fragment RepoFields on Repository") {
		t.Errorf("missing RepoFields fragment:\n%s", query)
	}
	if aliasMap["r0"].Owner != "a" || aliasMap["r1"].Owner != "b" {
		t.Errorf("alias map mismatch: %+v", aliasMap)
	}
}

func TestBulkFetchMetadata_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `r0: repository`) {
			t.Errorf("body missing aliased query: %s", body)
		}

		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": {
				"r0": {
					"nameWithOwner": "a/x",
					"stargazerCount": 100,
					"forkCount": 5,
					"issues": {"totalCount": 2},
					"pullRequests": {"totalCount": 3},
					"primaryLanguage": {"name": "Go"},
					"repositoryTopics": {"nodes": [{"topic": {"name": "cli"}}]},
					"description": "hello"
				},
				"r1": null
			},
			"errors": [
				{"type": "NOT_FOUND", "message": "Could not resolve b/y", "path": ["r1"]}
			]
		}`))
	}))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)

	out, err := client.BulkFetchMetadata(context.Background(), []Repo{
		{Owner: "a", Name: "x"},
		{Owner: "b", Name: "y"},
	})
	if err != nil {
		t.Fatalf("BulkFetchMetadata: %v", err)
	}

	if len(out.Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(out.Metrics))
	}
	m := out.Metrics["a/x"]
	if m == nil {
		t.Fatal("missing metrics for a/x")
	}
	if m.Stars != 100 || m.Forks != 5 || m.OpenIssues != 2 || m.OpenPRs != 3 {
		t.Errorf("field mismatch: %+v", m)
	}
	if m.Language != "Go" {
		t.Errorf("Language = %s, want Go", m.Language)
	}
	if len(m.Topics) != 1 || m.Topics[0] != "cli" {
		t.Errorf("Topics = %v", m.Topics)
	}
	if len(out.NotFound) != 1 || out.NotFound[0] != "b/y" {
		t.Errorf("NotFound = %v", out.NotFound)
	}
	if out.QueryCount != 1 {
		t.Errorf("QueryCount = %d, want 1", out.QueryCount)
	}
}

func TestBulkFetchMetadata_Batching(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		// Extract alias count by counting "r<num>:" occurrences in the
		// query string.
		aliasCount := strings.Count(string(body), "repository(owner")
		if aliasCount > MaxGraphQLBatchSize {
			t.Errorf("batch %d exceeded limit: %d aliases", calls, aliasCount)
		}

		// Build a matching data map: every alias returns a tiny stub.
		data := map[string]interface{}{}
		for i := 0; i < aliasCount; i++ {
			data[`r`+itoa(i)] = map[string]interface{}{
				"nameWithOwner":    "",
				"stargazerCount":   i,
				"forkCount":        0,
				"issues":           map[string]int{"totalCount": 0},
				"pullRequests":     map[string]int{"totalCount": 0},
				"primaryLanguage":  nil,
				"repositoryTopics": map[string]interface{}{"nodes": []interface{}{}},
				"description":      "",
			}
		}
		payload, _ := json.Marshal(map[string]interface{}{"data": data})
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)

	// 51 repos → 2 batches (50, 1)
	repos := make([]Repo, 51)
	for i := range repos {
		repos[i] = Repo{Owner: "o", Name: "r" + itoa(i)}
	}
	out, err := client.BulkFetchMetadata(context.Background(), repos)
	if err != nil {
		t.Fatalf("BulkFetchMetadata: %v", err)
	}
	if out.QueryCount != 2 {
		t.Errorf("QueryCount = %d, want 2", out.QueryCount)
	}
	if calls != 2 {
		t.Errorf("server saw %d calls, want 2", calls)
	}
}

func TestBulkFetchMetadata_TopLevelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`))
	}))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)

	// One batch that fails — single failure no longer aborts the sweep,
	// but does surface in FailedBatches for caller observability.
	out, err := client.BulkFetchMetadata(context.Background(), []Repo{{Owner: "a", Name: "x"}})
	if err != nil {
		t.Fatalf("BulkFetchMetadata: unexpected top-level error %v", err)
	}
	if len(out.FailedBatches) != 1 {
		t.Fatalf("FailedBatches = %d, want 1", len(out.FailedBatches))
	}
	if !strings.Contains(out.FailedBatches[0].Err.Error(), "RATE_LIMITED") {
		t.Errorf("FailedBatches[0].Err = %v", out.FailedBatches[0].Err)
	}
	if out.FailedBatches[0].Start != 0 || out.FailedBatches[0].End != 1 {
		t.Errorf("FailedBatches[0] indices = [%d,%d), want [0,1)",
			out.FailedBatches[0].Start, out.FailedBatches[0].End)
	}
	if out.QueryCount != 1 {
		t.Errorf("QueryCount = %d, want 1", out.QueryCount)
	}
}

// G1 — a transient 5xx in batch 2 must NOT abort batches 3..N.
// Stub returns 502 on the second batch only; with DoWithRetry the per-batch
// retries will exhaust and the outer loop must continue.
func TestBulkFetchMetadata_BatchFailure_ContinuesPastBadBatch(t *testing.T) {
	var calls int32
	var bodies [][]byte
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		idx := atomic.AddInt32(&calls, 1)
		mu.Unlock()

		// Decode the JSON envelope to inspect the inner GraphQL document.
		var env struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(body, &env)

		// Batch identity is owner-prefixed in our fixture: bank-A is batch 1,
		// bank-B is batch 2 (must fail), bank-C is batch 3.
		if strings.Contains(env.Query, `owner: "B"`) {
			// Always 502 — exhaust per-batch retries.
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("bad gateway"))
			return
		}
		_ = idx

		// Build a happy-path response: every alias gets a tiny stub.
		aliasCount := strings.Count(env.Query, "repository(owner")
		data := map[string]interface{}{}
		for i := 0; i < aliasCount; i++ {
			data["r"+itoa(i)] = map[string]interface{}{
				"nameWithOwner":    "",
				"stargazerCount":   0,
				"forkCount":        0,
				"issues":           map[string]int{"totalCount": 0},
				"pullRequests":     map[string]int{"totalCount": 0},
				"primaryLanguage":  nil,
				"repositoryTopics": map[string]interface{}{"nodes": []interface{}{}},
				"description":      "",
			}
		}
		payload, _ := json.Marshal(map[string]interface{}{"data": data})
		w.WriteHeader(http.StatusOK)
		w.Write(payload)
	}))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)
	// Tight retry knob so the test does not block on real backoff.
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 2,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   2 * time.Millisecond,
	})

	// Three batches: A (50), B (50, fails), C (5). 105 repos total.
	repos := make([]Repo, 0, 105)
	for i := 0; i < 50; i++ {
		repos = append(repos, Repo{Owner: "A", Name: "r" + itoa(i)})
	}
	for i := 0; i < 50; i++ {
		repos = append(repos, Repo{Owner: "B", Name: "r" + itoa(i)})
	}
	for i := 0; i < 5; i++ {
		repos = append(repos, Repo{Owner: "C", Name: "r" + itoa(i)})
	}

	out, err := client.BulkFetchMetadata(context.Background(), repos)
	if err != nil {
		t.Fatalf("BulkFetchMetadata returned top-level error: %v", err)
	}
	if out.QueryCount != 3 {
		t.Errorf("QueryCount = %d, want 3 (one per batch, retries collapse to one slot)", out.QueryCount)
	}
	if len(out.FailedBatches) != 1 {
		t.Fatalf("FailedBatches = %d, want 1", len(out.FailedBatches))
	}
	if out.FailedBatches[0].Start != 50 || out.FailedBatches[0].End != 100 {
		t.Errorf("FailedBatches[0] = [%d,%d), want [50,100)",
			out.FailedBatches[0].Start, out.FailedBatches[0].End)
	}
	// Batches A and C must have surfaced their slugs in Metrics.
	if _, ok := out.Metrics["A/r0"]; !ok {
		t.Errorf("Metrics missing A/r0 — first batch should have persisted")
	}
	if _, ok := out.Metrics["C/r0"]; !ok {
		t.Errorf("Metrics missing C/r0 — third batch should have persisted past failed batch B")
	}
	// And nothing from the failed batch B.
	for k := range out.Metrics {
		if strings.HasPrefix(k, "B/") {
			t.Errorf("Metrics unexpectedly contains failed-batch entry %q", k)
		}
	}
}

// G1 — sustained outage hits the consecutive-failure cap.
func TestBulkFetchMetadata_AbortsAfterMaxConsecutiveFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 1,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   2 * time.Millisecond,
	})

	// 5 batches of 50 — should abort after MaxConsecutiveBatchFailures.
	repos := make([]Repo, 5*MaxGraphQLBatchSize)
	for i := range repos {
		repos[i] = Repo{Owner: "o", Name: "r" + itoa(i)}
	}

	out, err := client.BulkFetchMetadata(context.Background(), repos)
	if err == nil {
		t.Fatal("expected abort error after sustained failure")
	}
	if !strings.Contains(err.Error(), "consecutive batch failures") {
		t.Errorf("error message = %v, want consecutive-failure abort", err)
	}
	if len(out.FailedBatches) != MaxConsecutiveBatchFailures {
		t.Errorf("FailedBatches = %d, want %d (abort cap)",
			len(out.FailedBatches), MaxConsecutiveBatchFailures)
	}
}

// G3 — replay safety: server sees identical batch body on retry.
func TestBulkFetchMetadata_RetryReplaysBody(t *testing.T) {
	var bodies []string
	var mu sync.Mutex
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		count := atomic.AddInt32(&calls, 1)
		if count == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		// On retry, return a happy-path response for r0.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"r0":{"nameWithOwner":"a/x","stargazerCount":1,"forkCount":0,"issues":{"totalCount":0},"pullRequests":{"totalCount":0},"primaryLanguage":null,"repositoryTopics":{"nodes":[]},"description":""}}}`))
	}))
	defer server.Close()

	client, _ := NewClient("tkn")
	client.SetBaseURL(server.URL)
	client.SetRetryConfig(RetryConfig{
		MaxRetries: 2,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   2 * time.Millisecond,
	})

	out, err := client.BulkFetchMetadata(context.Background(), []Repo{{Owner: "a", Name: "x"}})
	if err != nil {
		t.Fatalf("BulkFetchMetadata: %v", err)
	}
	if len(out.Metrics) != 1 || out.Metrics["a/x"] == nil {
		t.Fatalf("expected metrics for a/x after retry, got %+v", out.Metrics)
	}
	if calls < 2 {
		t.Fatalf("expected ≥2 server calls (retry), got %d", calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) < 2 {
		t.Fatalf("expected server to see ≥2 bodies, got %d", len(bodies))
	}
	if bodies[0] != bodies[1] {
		t.Errorf("retry body differs from original — GetBody is not replaying:\n0: %s\n1: %s",
			bodies[0], bodies[1])
	}
	var env struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(bodies[1]), &env); err != nil {
		t.Fatalf("decoding replayed body: %v", err)
	}
	if !strings.Contains(env.Query, `r0: repository(owner: "a", name: "x")`) {
		t.Errorf("replayed body missing query: %s", env.Query)
	}
}

// itoa is a small helper to avoid importing strconv just for tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
