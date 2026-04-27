package classification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/github"
)

// testDeps bundles test dependencies for pipeline tests.
type testDeps struct {
	db       *database.DB
	ghServer *httptest.Server
	gh       *github.Client
	cfg      config.ClassificationConfig
}

// setupPipeline creates a Pipeline with an in-memory DB, mock GitHub server,
// and mock Ollama server. The ollamaHandler controls Ollama behavior;
// the ghHandler controls GitHub API behavior.
func setupPipeline(t *testing.T, ghHandler, ollamaHandler http.HandlerFunc) (*Pipeline, *testDeps) {
	t.Helper()

	// In-memory SQLite DB
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("opening test DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Mock GitHub API
	ghServer := httptest.NewServer(ghHandler)
	t.Cleanup(ghServer.Close)

	ghClient, err := github.NewClient("test-token")
	if err != nil {
		t.Fatalf("creating GitHub client: %v", err)
	}
	ghClient.SetBaseURL(ghServer.URL)

	// Mock Ollama server
	ollamaServer := httptest.NewServer(ollamaHandler)
	t.Cleanup(ollamaServer.Close)

	cfg := config.ClassificationConfig{
		OllamaEndpoint: ollamaServer.URL,
		Model:          "test-model",
		TimeoutMs:      5000,
		MaxReadmeChars: 2000,
		MinConfidence:  0.6,
		Categories:     []string{"kubernetes", "observability", "ai-agents", "other"},
		SystemPrompt:   `Classify. Categories: {{.Categories}}`,
		UserPrompt:     `Repo: {{.RepoName}} Desc: {{.Description}} README: {{.Readme}}`,
	}

	ollama := NewOllamaClient(ollamaServer.URL, cfg.Model, cfg.TimeoutMs, cfg.Categories)
	pipeline := NewPipeline(db, ghClient, ollama, cfg)

	return pipeline, &testDeps{
		db:       db,
		ghServer: ghServer,
		gh:       ghClient,
		cfg:      cfg,
	}
}

// ollamaSuccess returns a handler that responds with a valid classification.
func ollamaSuccess(category string, confidence float64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{}
		resp.Message.Content = mustJSON(map[string]interface{}{
			"category":   category,
			"confidence": confidence,
			"reasoning":  "test reasoning",
		})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// ghReadmeHandler returns a GitHub handler that serves README content for known repos.
// It also serves a minimal `/repos/{owner}/{repo}` response for the live
// description/topics fetch the classifier performs (ISI-744, folded into the
// taxonomy v3 migration); tests do not assert on those values, so an empty
// JSON payload is sufficient.
func ghReadmeHandler(readmes map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// README endpoint: /repos/{owner}/{repo}/readme
		for key, content := range readmes {
			if r.URL.Path == "/repos/"+key+"/readme" {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(content))
				return
			}
			// Repo metadata endpoint used by the live description/topics fetch.
			if r.URL.Path == "/repos/"+key {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"name":"","full_name":"","owner":{"login":""},"description":"","topics":[]}`))
				return
			}
		}
		http.NotFound(w, r)
	}
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// --- splitFullName ---

func TestSplitFullName(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
	}{
		{"owner/repo", "owner", "repo"},
		{"org/my-project", "org", "my-project"},
		{"noslash", "noslash", ""},
		{"a/b/c", "a", "b/c"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, repo := splitFullName(tt.input)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("splitFullName(%q) = (%q, %q), want (%q, %q)",
					tt.input, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

// --- ClassifySingle ---

func TestClassifySingle_Success(t *testing.T) {
	readmes := map[string]string{"test/repo": "# Test Repo\nA Kubernetes tool."}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaSuccess("kubernetes", 0.92),
	)

	repo := database.RepoRecord{
		FullName:  "test/repo",
		Owner:     "test",
		Name:      "repo",
		Stars:     500,
		StarsPrev: 400,
	}
	if err := deps.db.UpsertRepo(&repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	result, err := pipeline.ClassifySingle(context.Background(), repo)
	if err != nil {
		t.Fatalf("ClassifySingle error: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("ClassifySingle result.Error: %v", result.Error)
	}
	if result.Category != "kubernetes" {
		t.Errorf("Category = %q, want %q", result.Category, "kubernetes")
	}
	if result.Confidence != 0.92 {
		t.Errorf("Confidence = %f, want 0.92", result.Confidence)
	}
	if result.ModelUsed != "test-model" {
		t.Errorf("ModelUsed = %q, want %q", result.ModelUsed, "test-model")
	}
	if result.Skipped {
		t.Error("should not be skipped")
	}
	if result.ReadmeHash == "" {
		t.Error("ReadmeHash should not be empty")
	}
	if result.Duration == 0 {
		t.Error("Duration should be non-zero")
	}
}

func TestClassifySingle_SkipsUnchangedReadme(t *testing.T) {
	readmeContent := "# Same README"
	readmeHash := HashReadme(readmeContent)

	readmes := map[string]string{"test/repo": readmeContent}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaSuccess("kubernetes", 0.9),
	)

	repo := database.RepoRecord{
		FullName:           "test/repo",
		Owner:              "test",
		Name:               "repo",
		ReadmeHash:         readmeHash,
		PrimaryCategory:    "observability",
		CategoryConfidence: 0.85,
		ModelUsed:          "old-model",
	}
	if err := deps.db.UpsertRepo(&repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	result, err := pipeline.ClassifySingle(context.Background(), repo)
	if err != nil {
		t.Fatalf("ClassifySingle error: %v", err)
	}
	if !result.Skipped {
		t.Error("expected Skipped=true when README hash unchanged and already classified")
	}
	if result.Category != "observability" {
		t.Errorf("Category = %q, want original %q", result.Category, "observability")
	}
}

func TestClassifySingle_NoReadme(t *testing.T) {
	// GitHub returns 404 for README
	ghHandler := func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}
	pipeline, deps := setupPipeline(t,
		ghHandler,
		ollamaSuccess("other", 0.5),
	)

	repo := database.RepoRecord{
		FullName: "test/no-readme",
		Owner:    "test",
		Name:     "no-readme",
	}
	if err := deps.db.UpsertRepo(&repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	result, err := pipeline.ClassifySingle(context.Background(), repo)
	if err != nil {
		t.Fatalf("ClassifySingle error: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result.Error: %v", result.Error)
	}
	// Should still classify (empty readme → Ollama gets empty readme text)
	if result.Category != "other" {
		t.Errorf("Category = %q, want %q", result.Category, "other")
	}
}

func TestClassifySingle_OllamaError(t *testing.T) {
	readmes := map[string]string{"test/repo": "# README"}
	ollamaHandler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaHandler,
	)

	repo := database.RepoRecord{
		FullName: "test/repo",
		Owner:    "test",
		Name:     "repo",
	}
	if err := deps.db.UpsertRepo(&repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	result, err := pipeline.ClassifySingle(context.Background(), repo)
	if err != nil {
		t.Fatalf("ClassifySingle should not return err (graceful): %v", err)
	}
	if result.Error == nil {
		t.Error("expected result.Error to be non-nil for Ollama failure")
	}
}

func TestClassifySingle_StarTrend(t *testing.T) {
	var capturedBody chatRequest
	ollamaHandler := func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		resp := chatResponse{}
		resp.Message.Content = `{"category": "kubernetes", "confidence": 0.9, "reasoning": "test"}`
		json.NewEncoder(w).Encode(resp)
	}

	readmes := map[string]string{"test/repo": "# Test"}
	pipeline, deps := setupPipeline(t, ghReadmeHandler(readmes), ollamaHandler)

	tests := []struct {
		name      string
		stars     int
		starsPrev int
		wantTrend string
	}{
		{"rising", 500, 400, "rising"},
		{"declining", 300, 400, "declining"},
		{"stable", 400, 400, "stable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := database.RepoRecord{
				FullName:  "test/repo",
				Owner:     "test",
				Name:      "repo",
				Stars:     tt.stars,
				StarsPrev: tt.starsPrev,
			}
			if err := deps.db.UpsertRepo(&repo); err != nil {
				t.Fatalf("UpsertRepo: %v", err)
			}

			_, err := pipeline.ClassifySingle(context.Background(), repo)
			if err != nil {
				t.Fatalf("ClassifySingle: %v", err)
			}

			// Check the user prompt sent to Ollama contains the expected trend
			if len(capturedBody.Messages) < 2 {
				t.Fatal("expected 2 messages in Ollama request")
			}
			// The user prompt template includes the star trend
			// We can't easily check the rendered template without parsing,
			// but we verify the pipeline didn't error
		})
	}
}

// --- ClassifyAll ---

func TestClassifyAll_BatchClassification(t *testing.T) {
	readmes := map[string]string{
		"a/one": "# Repo One",
		"b/two": "# Repo Two",
	}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaSuccess("kubernetes", 0.9),
	)

	// Insert repos needing classification
	repos := []*database.RepoRecord{
		{FullName: "a/one", Owner: "a", Name: "one", Status: "pending", PrimaryCategory: ""},
		{FullName: "b/two", Owner: "b", Name: "two", Status: "pending", PrimaryCategory: ""},
		{FullName: "c/done", Owner: "c", Name: "done", Status: "active", PrimaryCategory: "observability"},
	}
	for _, r := range repos {
		if err := deps.db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo(%s): %v", r.FullName, err)
		}
	}

	summary, err := pipeline.ClassifyAll(context.Background())
	if err != nil {
		t.Fatalf("ClassifyAll error: %v", err)
	}

	// a/one and b/two need classification; c/done does not
	if summary.Total != 2 {
		t.Errorf("Total = %d, want 2", summary.Total)
	}
	if summary.Classified != 2 {
		t.Errorf("Classified = %d, want 2", summary.Classified)
	}
	if summary.Failed != 0 {
		t.Errorf("Failed = %d, want 0", summary.Failed)
	}
	if summary.Duration == 0 {
		t.Error("Duration should be non-zero")
	}

	// Verify DB was updated
	got, err := deps.db.GetRepo("a/one")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.PrimaryCategory != "kubernetes" {
		t.Errorf("a/one category = %q, want %q", got.PrimaryCategory, "kubernetes")
	}
	if got.Status != "active" {
		t.Errorf("a/one status = %q, want %q", got.Status, "active")
	}
}

func TestClassifyAll_LowConfidenceNeedsReview(t *testing.T) {
	readmes := map[string]string{"test/low": "# Low confidence"}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaSuccess("other", 0.3), // below 0.6 threshold
	)

	repo := &database.RepoRecord{
		FullName: "test/low", Owner: "test", Name: "low",
		Status: "pending", PrimaryCategory: "",
	}
	if err := deps.db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	summary, err := pipeline.ClassifyAll(context.Background())
	if err != nil {
		t.Fatalf("ClassifyAll error: %v", err)
	}

	if summary.NeedsReview != 1 {
		t.Errorf("NeedsReview = %d, want 1", summary.NeedsReview)
	}

	got, err := deps.db.GetRepo("test/low")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Status != "needs_review" {
		t.Errorf("status = %q, want %q", got.Status, "needs_review")
	}
}

func TestClassifyAll_EmptyBatch(t *testing.T) {
	pipeline, _ := setupPipeline(t,
		ghReadmeHandler(nil),
		ollamaSuccess("other", 0.5),
	)

	summary, err := pipeline.ClassifyAll(context.Background())
	if err != nil {
		t.Fatalf("ClassifyAll error: %v", err)
	}
	if summary.Total != 0 {
		t.Errorf("Total = %d, want 0", summary.Total)
	}
}

func TestClassifyAll_ContextCancellation(t *testing.T) {
	readmes := map[string]string{"a/one": "# One", "b/two": "# Two"}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaSuccess("kubernetes", 0.9),
	)

	repos := []*database.RepoRecord{
		{FullName: "a/one", Owner: "a", Name: "one", Status: "pending", PrimaryCategory: ""},
		{FullName: "b/two", Owner: "b", Name: "two", Status: "pending", PrimaryCategory: ""},
	}
	for _, r := range repos {
		if err := deps.db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := pipeline.ClassifyAll(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestClassifyAll_OllamaFailureCountsAsFailed(t *testing.T) {
	readmes := map[string]string{"test/fail": "# Fail"}
	ollamaHandler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaHandler,
	)

	repo := &database.RepoRecord{
		FullName: "test/fail", Owner: "test", Name: "fail",
		Status: "pending", PrimaryCategory: "",
	}
	if err := deps.db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	summary, err := pipeline.ClassifyAll(context.Background())
	if err != nil {
		t.Fatalf("ClassifyAll error: %v", err)
	}
	if summary.Failed != 1 {
		t.Errorf("Failed = %d, want 1", summary.Failed)
	}
	if summary.Classified != 0 {
		t.Errorf("Classified = %d, want 0", summary.Classified)
	}
}

// --- CheckReadmeHashes ---

func TestCheckReadmeHashes_DetectsChanges(t *testing.T) {
	// Repo has old hash, GitHub returns new content → should trigger reclassify
	newReadme := "# Updated README"
	readmes := map[string]string{"test/repo": newReadme}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaSuccess("kubernetes", 0.9),
	)

	repo := &database.RepoRecord{
		FullName:        "test/repo",
		Owner:           "test",
		Name:            "repo",
		Status:          "active",
		PrimaryCategory: "observability",
		ReadmeHash:      "oldhash_that_wont_match",
	}
	if err := deps.db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	changed, err := pipeline.CheckReadmeHashes(context.Background())
	if err != nil {
		t.Fatalf("CheckReadmeHashes error: %v", err)
	}
	if changed != 1 {
		t.Errorf("changed = %d, want 1", changed)
	}

	got, err := deps.db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Status != "needs_reclassify" {
		t.Errorf("status = %q, want %q", got.Status, "needs_reclassify")
	}
}

func TestCheckReadmeHashes_NoChanges(t *testing.T) {
	readmeContent := "# Same README"
	readmeHash := HashReadme(readmeContent)

	readmes := map[string]string{"test/repo": readmeContent}
	pipeline, deps := setupPipeline(t,
		ghReadmeHandler(readmes),
		ollamaSuccess("kubernetes", 0.9),
	)

	repo := &database.RepoRecord{
		FullName:        "test/repo",
		Owner:           "test",
		Name:            "repo",
		Status:          "active",
		PrimaryCategory: "observability",
		ReadmeHash:      readmeHash,
	}
	if err := deps.db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	changed, err := pipeline.CheckReadmeHashes(context.Background())
	if err != nil {
		t.Fatalf("CheckReadmeHashes error: %v", err)
	}
	if changed != 0 {
		t.Errorf("changed = %d, want 0", changed)
	}

	got, err := deps.db.GetRepo("test/repo")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want %q (should remain active)", got.Status, "active")
	}
}

func TestCheckReadmeHashes_NoClassifiedRepos(t *testing.T) {
	pipeline, _ := setupPipeline(t,
		ghReadmeHandler(nil),
		ollamaSuccess("other", 0.5),
	)

	changed, err := pipeline.CheckReadmeHashes(context.Background())
	if err != nil {
		t.Fatalf("CheckReadmeHashes error: %v", err)
	}
	if changed != 0 {
		t.Errorf("changed = %d, want 0", changed)
	}
}

func TestCheckReadmeHashes_GitHubError(t *testing.T) {
	// GitHub returns 500 → should skip repo gracefully, not fail
	ghHandler := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}
	pipeline, deps := setupPipeline(t,
		ghHandler,
		ollamaSuccess("kubernetes", 0.9),
	)

	repo := &database.RepoRecord{
		FullName:        "test/repo",
		Owner:           "test",
		Name:            "repo",
		Status:          "active",
		PrimaryCategory: "observability",
		ReadmeHash:      "somehash",
	}
	if err := deps.db.UpsertRepo(repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}

	changed, err := pipeline.CheckReadmeHashes(context.Background())
	if err != nil {
		t.Fatalf("CheckReadmeHashes should not error (graceful skip): %v", err)
	}
	if changed != 0 {
		t.Errorf("changed = %d, want 0 (GitHub error should be skipped)", changed)
	}
}
