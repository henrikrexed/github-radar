package ghstub

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStub_REST_RoundTrip_AndConditionalGET(t *testing.T) {
	stub := New(Config{})
	defer stub.Close()

	ctx := context.Background()

	// Cycle 1: cold fetch -> 200 with ETag.
	resp1, err := http.Get(stub.URL() + "/repos/foo/bar")
	if err != nil {
		t.Fatalf("cycle1 get: %v", err)
	}
	resp1.Body.Close()
	if resp1.StatusCode != 200 {
		t.Fatalf("cycle1 status = %d", resp1.StatusCode)
	}
	etag := resp1.Header.Get("ETag")
	if etag == "" {
		t.Fatal("cycle1 missing ETag")
	}
	if rem := resp1.Header.Get("X-RateLimit-Remaining"); rem == "" {
		t.Error("missing X-RateLimit-Remaining")
	}

	// Cycle 2: send If-None-Match -> 304.
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, stub.URL()+"/repos/foo/bar", nil)
	req.Header.Set("If-None-Match", etag)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cycle2 get: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotModified {
		t.Errorf("cycle2 status = %d, want 304", resp2.StatusCode)
	}
	if got := stub.NotModifiedHits.Load(); got != 1 {
		t.Errorf("NotModifiedHits = %d, want 1", got)
	}
}

func TestStub_L3_ETagRotation_AlwaysReturns200(t *testing.T) {
	stub := New(Config{L3ETagRotation: true})
	defer stub.Close()

	resp1, _ := http.Get(stub.URL() + "/repos/foo/bar")
	resp1.Body.Close()
	etag1 := resp1.Header.Get("ETag")

	req, _ := http.NewRequest(http.MethodGet, stub.URL()+"/repos/foo/bar", nil)
	req.Header.Set("If-None-Match", etag1)
	resp2, _ := http.DefaultClient.Do(req)
	resp2.Body.Close()

	if resp2.StatusCode != 200 {
		t.Errorf("L3 cycle2 status = %d, want 200", resp2.StatusCode)
	}
	if etag1 == resp2.Header.Get("ETag") {
		t.Error("L3 ETag did not rotate")
	}
}

func TestStub_GraphQL_AllAliasesResolved(t *testing.T) {
	stub := New(Config{})
	defer stub.Close()

	body := strings.NewReader(`{"query":"query { r0: repository(owner: \"a\", name: \"b\") { ...RepoFields } r1: repository(owner: \"c\", name: \"d\") { ...RepoFields } }"}`)
	resp, err := http.Post(stub.URL()+"/graphql", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := stub.GraphQLCalls.Load(); got != 1 {
		t.Errorf("GraphQLCalls = %d, want 1", got)
	}
}

func TestStub_GraphQL_NullAliases_L6(t *testing.T) {
	stub := New(Config{GraphQLNullAliases: 1})
	defer stub.Close()

	body := strings.NewReader(`{"query":"query { r0: repository(owner: \"a\", name: \"b\") { ...RepoFields } r1: repository(owner: \"c\", name: \"d\") { ...RepoFields } }"}`)
	resp, err := http.Post(stub.URL()+"/graphql", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), `"r0":null`) {
		t.Errorf("expected r0 null, got %s", string(buf[:n]))
	}
	if !strings.Contains(string(buf[:n]), `"nameWithOwner":"c/d"`) {
		t.Errorf("expected r1 populated, got %s", string(buf[:n]))
	}
}

func TestStub_GraphQL_Transient502_L8(t *testing.T) {
	stub := New(Config{GraphQLTransient502BatchIndex: 1, GraphQLTransient502MaxFires: 1})
	defer stub.Close()

	body1 := strings.NewReader(`{"query":"query { r0: repository(owner: \"a\", name: \"b\") { ...RepoFields } }"}`)
	resp1, _ := http.Post(stub.URL()+"/graphql", "application/json", body1)
	resp1.Body.Close()
	if resp1.StatusCode != 200 {
		t.Errorf("batch 0 status = %d, want 200", resp1.StatusCode)
	}

	body2 := strings.NewReader(`{"query":"query { r0: repository(owner: \"a\", name: \"b\") { ...RepoFields } }"}`)
	resp2, _ := http.Post(stub.URL()+"/graphql", "application/json", body2)
	resp2.Body.Close()
	if resp2.StatusCode != 502 {
		t.Errorf("batch 1 status = %d, want 502", resp2.StatusCode)
	}

	// Third call should succeed since MaxFires=1.
	body3 := strings.NewReader(`{"query":"query { r0: repository(owner: \"a\", name: \"b\") { ...RepoFields } }"}`)
	resp3, _ := http.Post(stub.URL()+"/graphql", "application/json", body3)
	resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Errorf("batch 2 status = %d, want 200", resp3.StatusCode)
	}
}

func TestStub_PrimaryRateLimit_L5a(t *testing.T) {
	now := time.Now()
	clock := &simClock{now: now}
	stub := New(Config{
		Now:                      clock.Now,
		PrimaryRateLimitInjectAt: 30 * time.Minute,
	})
	defer stub.Close()

	// First request before the trigger fires normally.
	resp1, _ := http.Get(stub.URL() + "/repos/foo/bar")
	resp1.Body.Close()
	if resp1.StatusCode != 200 {
		t.Errorf("pre-inject status = %d", resp1.StatusCode)
	}

	// Advance clock past the trigger.
	clock.Advance(31 * time.Minute)

	// Next request should fire the 403 + remaining=0.
	resp2, _ := http.Get(stub.URL() + "/repos/foo/bar")
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("inject status = %d, want 403", resp2.StatusCode)
	}
	if rem := resp2.Header.Get("X-RateLimit-Remaining"); rem != "0" {
		t.Errorf("inject remaining = %q, want 0", rem)
	}

	// Third request should succeed (one-shot injector).
	resp3, _ := http.Get(stub.URL() + "/repos/foo/bar")
	resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Errorf("post-inject status = %d, want 200", resp3.StatusCode)
	}
}

func TestStub_SecondaryRateLimit_L5b(t *testing.T) {
	clock := &simClock{now: time.Now()}
	stub := New(Config{
		Now:                          clock.Now,
		SecondaryRateLimitInjectAt:   30 * time.Minute,
		SecondaryRateLimitRetryAfter: 60,
	})
	defer stub.Close()

	clock.Advance(31 * time.Minute)
	resp, _ := http.Get(stub.URL() + "/repos/foo/bar")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("inject status = %d, want 429", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra != "60" {
		t.Errorf("Retry-After = %q, want 60", ra)
	}
}

// simClock is a deterministic clock for the smoke tests.
type simClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *simClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *simClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}
