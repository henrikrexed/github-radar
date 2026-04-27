package audit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestFiler returns a PaperclipFiler pointed at the supplied test
// server with an aggressive retry delay (so retry tests don't sleep 2s
// on each call) and a deterministic Now.
func newTestFiler(t *testing.T, srv *httptest.Server, now time.Time) *PaperclipFiler {
	t.Helper()
	f := NewPaperclipFiler(srv.URL, "co-1", "proj-1", "test-key", "agent-x")
	f.HTTP = srv.Client()
	f.Now = func() time.Time { return now }
	return f
}

// withFastRetry sets RetryDelay to ~zero for the duration of a test.
func withFastRetry(t *testing.T) {
	t.Helper()
	old := RetryDelay
	RetryDelay = 1 * time.Millisecond
	t.Cleanup(func() { RetryDelay = old })
}

// TestPaperclipFiler_Retries5xx — plan §6.1: one retry on 5xx.
// First response is 503; second is 201. Filer must succeed and the
// server must have seen exactly 2 attempts.
func TestPaperclipFiler_Retries5xx(t *testing.T) {
	withFastRetry(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{ID: "uuid-1", Identifier: "ISI-900"})
	}))
	defer srv.Close()

	f := newTestFiler(t, srv, time.Now())
	id, err := f.File(context.Background(), GraduationDraft{
		Category: "ai", ProposedSubcat: "quantum-ml",
		Cluster: Cluster{Tokens: []string{"quantum-computing"}, Repos: fiveRepos(0.8)},
	})
	if err != nil {
		t.Fatalf("File err: %v", err)
	}
	if id != "ISI-900" {
		t.Errorf("identifier: got %q, want ISI-900", id)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("attempts: got %d, want 2 (1 retry per plan §6.1)", got)
	}
}

// TestPaperclipFiler_RetryOn429 — plan §6.1: retry on 429.
func TestPaperclipFiler_RetryOn429(t *testing.T) {
	withFastRetry(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{Identifier: "ISI-901"})
	}))
	defer srv.Close()

	f := newTestFiler(t, srv, time.Now())
	if _, err := f.File(context.Background(), GraduationDraft{
		Category: "ai", ProposedSubcat: "x",
		Cluster: Cluster{Tokens: []string{"x"}, Repos: fiveRepos(0.8)},
	}); err != nil {
		t.Fatalf("File err: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("attempts: got %d, want 2 (429 must retry)", got)
	}
}

// TestPaperclipFiler_NoRetryOn4xx — plan §6.1: 4xx other than 429 is
// terminal. Must NOT retry; must return APIError.
func TestPaperclipFiler_NoRetryOn4xx(t *testing.T) {
	withFastRetry(t)

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	f := newTestFiler(t, srv, time.Now())
	_, err := f.File(context.Background(), GraduationDraft{
		Category: "ai", ProposedSubcat: "x",
		Cluster: Cluster{Tokens: []string{"x"}, Repos: fiveRepos(0.8)},
	})
	if err == nil {
		t.Fatal("expected APIError on 400, got nil")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err type: got %T, want *APIError", err)
	}
	if apiErr.Status != http.StatusBadRequest {
		t.Errorf("APIError.Status: got %d, want 400", apiErr.Status)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("attempts: got %d, want 1 (4xx must NOT retry)", got)
	}
}

// TestPaperclipFiler_DedupSearchPattern — plan §6 dedup with the 60-day
// window. Layer-2 (clock-injection) test: with Now fixed, search returns
// an issue created 30d ago (within window) → must dedup; search returns
// an issue created 90d ago (outside window) → must NOT dedup.
func TestPaperclipFiler_DedupSearchPattern(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	titlePrefix := "Subcat graduation proposal: ai/quantum-ml"

	cases := []struct {
		name      string
		createdAt time.Time
		wantDedup bool
	}{
		{"within_60d", now.Add(-30 * 24 * time.Hour), true},
		{"outside_60d", now.Add(-90 * 24 * time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method: got %s, want GET", r.Method)
				}
				if !strings.Contains(r.URL.RawQuery, "q=") {
					t.Errorf("missing q= in query: %s", r.URL.RawQuery)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]searchIssue{
					{
						ID:         "uuid-1",
						Identifier: "ISI-700",
						Title:      titlePrefix,
						Status:     "todo",
						CreatedAt:  tc.createdAt.Format(time.RFC3339),
					},
				})
			}))
			defer srv.Close()

			f := newTestFiler(t, srv, now)
			got, err := f.AlreadyFiledRecently(context.Background(), titlePrefix)
			if err != nil {
				t.Fatalf("AlreadyFiledRecently err: %v", err)
			}
			if got != tc.wantDedup {
				t.Errorf("dedup: got %v, want %v", got, tc.wantDedup)
			}
		})
	}
}

// TestPaperclipFiler_SearchEnvelopeShape — search response may come back
// as a bare array or a {"issues": [...]} envelope; the parser must
// accept both.
func TestPaperclipFiler_SearchEnvelopeShape(t *testing.T) {
	now := time.Now()
	prefix := "Subcat graduation proposal: ai/foo"
	for _, shape := range []string{"array", "envelope"} {
		t.Run(shape, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if shape == "array" {
					_ = json.NewEncoder(w).Encode([]searchIssue{})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"issues": []searchIssue{}})
				}
			}))
			defer srv.Close()
			f := newTestFiler(t, srv, now)
			got, err := f.AlreadyFiledRecently(context.Background(), prefix)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got {
				t.Error("empty search must not dedup")
			}
		})
	}
}

// TestGraduationDraft_U5_PostBodyContract — plan §6 + T9.2 review §1: the
// auto-file POST body must contain the repo list, the token rationale,
// and a draft config-PR snippet. Pin via regex on the rendered body so
// future drift surfaces in the test.
func TestGraduationDraft_U5_PostBodyContract(t *testing.T) {
	d := GraduationDraft{
		Category: "ai", ProposedSubcat: "quantum-ml",
		Cluster: Cluster{
			Tokens: []string{"quantum-computing", "variational-algorithms"},
			Repos: []Repo{
				{FullName: "alice/q-engine", Confidence: 0.85},
				{FullName: "bob/qml-toolkit", Confidence: 0.78},
				{FullName: "carol/q-circuits", Confidence: 0.91},
				{FullName: "dan/quantum-sim", Confidence: 0.65},
				{FullName: "eve/qaoa-solver", Confidence: 0.74},
			},
		},
		AggregateShare: 2.4,
	}

	if got, want := d.Title(), "Subcat graduation proposal: ai/quantum-ml"; got != want {
		t.Errorf("Title: got %q, want %q", got, want)
	}

	body := d.Body()

	// Repo list — every full_name must appear with its confidence.
	for _, r := range d.Cluster.Repos {
		if !strings.Contains(body, r.FullName) {
			t.Errorf("body missing repo %q", r.FullName)
		}
	}

	// Token rationale.
	for _, tok := range d.Cluster.Tokens {
		if !strings.Contains(body, tok) {
			t.Errorf("body missing token %q in rationale", tok)
		}
	}

	// Draft config-PR snippet — YAML block with `categories:` then
	// `<cat>:` then `subcategories:` then `- <subcat>` (regex tolerant
	// of whitespace).
	yamlRe := regexp.MustCompile(
		"(?s)```yaml.*?categories:.*?ai:.*?subcategories:.*?-\\s+quantum-ml.*?```",
	)
	if !yamlRe.MatchString(body) {
		t.Errorf("body missing draft taxonomy YAML snippet:\n%s", body)
	}

	// Aggregate share line.
	if !strings.Contains(body, "2.40%") && !strings.Contains(body, "2.4%") {
		t.Errorf("body missing aggregate share rendering (got: %q)", body)
	}
}

// TestPaperclipFiler_SendsAuthAndPayload — sanity-check that the POST
// payload conforms to the create-issue API contract (status=todo,
// priority=medium, projectId set, parentId set, assigneeAgentId set,
// Authorization header present).
func TestPaperclipFiler_SendsAuthAndPayload(t *testing.T) {
	withFastRetry(t)

	var seen createIssueRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &seen)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createIssueResponse{Identifier: "ISI-999"})
	}))
	defer srv.Close()

	f := newTestFiler(t, srv, time.Now())
	if _, err := f.File(context.Background(), GraduationDraft{
		Category: "ai", ProposedSubcat: "x",
		Cluster:       Cluster{Tokens: []string{"x"}, Repos: fiveRepos(0.7)},
		ParentIssueID: "audit-parent-id",
	}); err != nil {
		t.Fatalf("File err: %v", err)
	}

	if auth != "Bearer test-key" {
		t.Errorf("Authorization header: got %q, want %q", auth, "Bearer test-key")
	}
	if seen.Status != "todo" {
		t.Errorf("status: got %q, want todo", seen.Status)
	}
	if seen.Priority != "medium" {
		t.Errorf("priority: got %q, want medium", seen.Priority)
	}
	if seen.ParentID != "audit-parent-id" {
		t.Errorf("parentId: got %q, want audit-parent-id", seen.ParentID)
	}
	if seen.ProjectID != "proj-1" {
		t.Errorf("projectId: got %q, want proj-1", seen.ProjectID)
	}
	if seen.AssigneeAgentID != "agent-x" {
		t.Errorf("assigneeAgentId: got %q, want agent-x", seen.AssigneeAgentID)
	}
}

// fiveRepos is a small helper that builds a 5-repo cluster body for tests
// where the cluster contents don't matter (retry/auth/payload tests).
func fiveRepos(conf float64) []Repo {
	out := make([]Repo, 5)
	for i := range out {
		out[i] = Repo{FullName: "owner/repo" + itoa(i+1), Confidence: conf}
	}
	return out
}
