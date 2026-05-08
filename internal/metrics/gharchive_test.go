package metrics

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func makeGZippedArchive(events []ghArchiveEvent) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, evt := range events {
		if err := enc.Encode(evt); err != nil {
			return nil, fmt.Errorf("encoding event: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("closing gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

func makeTestEvent(eventType, repoName, actorLogin string) ghArchiveEvent {
	return ghArchiveEvent{
		Type: eventType,
		Repo: struct {
			Name string `json:"name"`
		}{Name: repoName},
		Actor: struct {
			Login string `json:"login"`
		}{Login: actorLogin},
	}
}

func TestHourlyArchiveCollector_BasicEventCounting(t *testing.T) {
	events := []ghArchiveEvent{
		makeTestEvent("WatchEvent", "kubernetes/kubernetes", "user1"),
		makeTestEvent("WatchEvent", "kubernetes/kubernetes", "user2"),
		makeTestEvent("WatchEvent", "other/repo", "user3"),
		makeTestEvent("ForkEvent", "kubernetes/kubernetes", "user4"),
	}

	archive, err := makeGZippedArchive(events)
	if err != nil {
		t.Fatalf("makeGZippedArchive: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer ts.Close()

	collector := NewHourlyArchiveCollector(ts.URL, 10*time.Second, nil)

	repos := []RepoRef{{Owner: "kubernetes", Name: "kubernetes"}}
	results, err := collector.Collect(context.Background(), repos, 1*time.Hour)
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	r := results[0]
	if r.Stars != 2 {
		t.Errorf("Stars = %d, want 2", r.Stars)
	}
	if r.Forks != 1 {
		t.Errorf("Forks = %d, want 1", r.Forks)
	}
}

func TestHourlyArchiveCollector_MergedPRs(t *testing.T) {
	prPayload := pullRequestPayload{
		Action: "closed",
		Number: 42,
		PullRequest: struct {
			Merged   bool   `json:"merged"`
			MergedAt string `json:"merged_at"`
			State    string `json:"state"`
		}{Merged: true, MergedAt: time.Now().Format(time.RFC3339), State: "closed"},
	}
	events := []ghArchiveEvent{
		makeTestEvent("PullRequestEvent", "test/repo", "user1"),
	}
	events[0].Payload, _ = json.Marshal(prPayload)

	archive, _ := makeGZippedArchive(events)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer ts.Close()

	collector := NewHourlyArchiveCollector(ts.URL, 10*time.Second, nil)
	repos := []RepoRef{{Owner: "test", Name: "repo"}}
	results, err := collector.Collect(context.Background(), repos, 1*time.Hour)
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	found := false
	for _, r := range results {
		if r.Owner == "test" && r.Name == "repo" {
			found = true
			if r.PRVelocity <= 0 {
				t.Errorf("PRVelocity = %v, want > 0", r.PRVelocity)
			}
		}
	}
	if !found {
		t.Error("test/repo not found in results")
	}
}

func TestHourlyArchiveCollector_IssuesEvent(t *testing.T) {
	issuePayload := issuesPayload{Action: "opened", Number: 10}
	events := []ghArchiveEvent{
		makeTestEvent("IssuesEvent", "test/repo", "user1"),
	}
	events[0].Payload, _ = json.Marshal(issuePayload)

	archive, _ := makeGZippedArchive(events)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer ts.Close()

	collector := NewHourlyArchiveCollector(ts.URL, 10*time.Second, nil)
	repos := []RepoRef{{Owner: "test", Name: "repo"}}
	results, err := collector.Collect(context.Background(), repos, 1*time.Hour)
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	if results[0].IssueVelocity <= 0 {
		t.Errorf("IssueVelocity = %v, want > 0", results[0].IssueVelocity)
	}
}

func TestHourlyArchiveCollector_ReleaseEvent(t *testing.T) {
	releasePayload := releasePayload{
		Release: struct {
			PublishedAt string `json:"published_at"`
			TagName     string `json:"tag_name"`
		}{PublishedAt: time.Now().Format(time.RFC3339), TagName: "v1.0.0"},
		Action: "published",
	}
	events := []ghArchiveEvent{
		makeTestEvent("ReleaseEvent", "test/repo", "user1"),
	}
	events[0].Payload, _ = json.Marshal(releasePayload)

	archive, _ := makeGZippedArchive(events)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer ts.Close()

	collector := NewHourlyArchiveCollector(ts.URL, 10*time.Second, nil)
	repos := []RepoRef{{Owner: "test", Name: "repo"}}
	results, err := collector.Collect(context.Background(), repos, 1*time.Hour)
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	if len(results[0].ReleaseDates) != 1 {
		t.Errorf("ReleaseDates len = %d, want 1", len(results[0].ReleaseDates))
	}
}

func TestHourlyArchiveCollector_EmptyRepos(t *testing.T) {
	collector := NewHourlyArchiveCollector("https://data.gharchive.org", 10*time.Second, nil)
	results, err := collector.Collect(context.Background(), nil, 1*time.Hour)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty repos")
	}
}

func TestHourlyArchiveCollector_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	collector := NewHourlyArchiveCollector(ts.URL, 10*time.Second, nil)
	repos := []RepoRef{{Owner: "test", Name: "repo"}}
	results, err := collector.Collect(context.Background(), repos, 1*time.Hour)
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	if results[0].Stars != 0 {
		t.Errorf("Stars = %d, want 0 (server error should produce zero counts)", results[0].Stars)
	}
}

func TestHourlyArchiveCollector_Filtering(t *testing.T) {
	events := []ghArchiveEvent{
		makeTestEvent("WatchEvent", "kubernetes/kubernetes", "user1"),
		makeTestEvent("WatchEvent", "untracked/repo", "user2"),
		makeTestEvent("PushEvent", "kubernetes/kubernetes", "user3"),
	}

	archive, _ := makeGZippedArchive(events)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer ts.Close()

	collector := NewHourlyArchiveCollector(ts.URL, 10*time.Second, nil)
	repos := []RepoRef{{Owner: "kubernetes", Name: "kubernetes"}}
	results, err := collector.Collect(context.Background(), repos, 1*time.Hour)
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	if results[0].Stars != 1 {
		t.Errorf("Stars = %d, want 1 (only kubernetes/kubernetes WatchEvent)", results[0].Stars)
	}
}

func TestHourlyArchiveCollector_VelocityCalculations(t *testing.T) {
	events := []ghArchiveEvent{
		makeTestEvent("WatchEvent", "test/repo", "user1"),
		makeTestEvent("WatchEvent", "test/repo", "user2"),
		makeTestEvent("ForkEvent", "test/repo", "user3"),
	}

	archive, _ := makeGZippedArchive(events)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer ts.Close()

	collector := NewHourlyArchiveCollector(ts.URL, 10*time.Second, nil)
	repos := []RepoRef{{Owner: "test", Name: "repo"}}
	results, err := collector.Collect(context.Background(), repos, 24*time.Hour)
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	r := results[0]
	if r.StarVelocity <= 0 {
		t.Errorf("StarVelocity = %v, want > 0", r.StarVelocity)
	}
	if r.ForkVelocity <= 0 {
		t.Errorf("ForkVelocity = %v, want > 0", r.ForkVelocity)
	}
}

func TestNewHourlyArchiveCollector_Defaults(t *testing.T) {
	c := NewHourlyArchiveCollector("", 0, nil)
	if c.baseURL != "https://data.gharchive.org" {
		t.Errorf("baseURL = %v, want https://data.gharchive.org", c.baseURL)
	}
	if c.httpClient.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", c.httpClient.Timeout)
	}
}
