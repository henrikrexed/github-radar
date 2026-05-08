package metrics

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func buildGZippedEvents(events []ghArchiveEvent) ([]byte, error) {
	var buf []byte
	for _, evt := range events {
		data, err := json.Marshal(evt)
		if err != nil {
			return nil, err
		}
		buf = append(buf, data...)
		buf = append(buf, '\n')
	}
	return buf, nil
}

func makeGZippedArchive(events []ghArchiveEvent) ([]byte, error) {
	raw, err := buildGZippedEvents(events)
	if err != nil {
		return nil, err
	}

	var compressed []byte
	pr, pw := ioPipe()
	go func() {
		gz := gzip.NewWriter(pw)
		gz.Write(raw)
		gz.Close()
		pw.Close()
	}()
	buf := make([]byte, 4096)
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			compressed = append(compressed, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return compressed, nil
}

type pipeReader struct {
	ch chan []byte
}

type pipeWriter struct {
	ch chan []byte
}

func ioPipe() (*pipeReader, *pipeWriter) {
	ch := make(chan []byte, 1)
	return &pipeReader{ch: ch}, &pipeWriter{ch: ch}
}

func (r *pipeReader) Read(p []byte) (int, error) {
	data, ok := <-r.ch
	if !ok {
		return 0, fmt.Errorf("closed")
	}
	n := copy(p, data)
	return n, nil
}

func (w *pipeWriter) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	w.ch <- cp
	return len(p), nil
}

func (w *pipeWriter) Close() error {
	close(w.ch)
	return nil
}

func makeTestEvent(eventType, repoName, actorLogin string) ghArchiveEvent {
	return ghArchiveEvent{
		Type:  eventType,
		Repo:  struct{ Name string `json:"name"` }{Name: repoName},
		Actor: struct{ Login string `json:"login"` }{Login: actorLogin},
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
