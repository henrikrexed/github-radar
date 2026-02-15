package repository

import (
	"reflect"
	"testing"
)

func TestTracker_Add_WithCategories(t *testing.T) {
	tracker := NewTracker()
	repo := Repo{Owner: "kubernetes", Name: "kubernetes"}

	tracker.Add(repo, "cncf", "container-orchestration")

	all := tracker.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(all))
	}
	if all[0].Repo.Owner != "kubernetes" {
		t.Errorf("Owner = %q, want %q", all[0].Repo.Owner, "kubernetes")
	}
	if len(all[0].Categories) != 2 {
		t.Errorf("Categories len = %d, want 2", len(all[0].Categories))
	}
}

func TestTracker_Add_NoCategories(t *testing.T) {
	tracker := NewTracker()
	repo := Repo{Owner: "grafana", Name: "grafana"}

	tracker.Add(repo)

	all := tracker.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(all))
	}
	if len(all[0].Categories) != 1 {
		t.Errorf("Categories len = %d, want 1", len(all[0].Categories))
	}
	if all[0].Categories[0] != DefaultCategory {
		t.Errorf("Category = %q, want %q", all[0].Categories[0], DefaultCategory)
	}
}

func TestTracker_Add_MergesCategories(t *testing.T) {
	tracker := NewTracker()
	repo := Repo{Owner: "kubernetes", Name: "kubernetes"}

	tracker.Add(repo, "cncf")
	tracker.Add(repo, "container-orchestration")

	all := tracker.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 repo (merged), got %d", len(all))
	}
	if len(all[0].Categories) != 2 {
		t.Errorf("Categories len = %d, want 2", len(all[0].Categories))
	}
}

func TestTracker_Remove(t *testing.T) {
	tracker := NewTracker()
	repo := Repo{Owner: "kubernetes", Name: "kubernetes"}
	tracker.Add(repo, "cncf")

	removed := tracker.Remove(repo)

	if !removed {
		t.Error("expected Remove to return true")
	}
	if tracker.Count() != 0 {
		t.Errorf("Count = %d, want 0", tracker.Count())
	}
}

func TestTracker_Remove_NotFound(t *testing.T) {
	tracker := NewTracker()
	repo := Repo{Owner: "kubernetes", Name: "kubernetes"}

	removed := tracker.Remove(repo)

	if removed {
		t.Error("expected Remove to return false for non-existent repo")
	}
}

func TestTracker_ByCategory(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "cncf", "container-orchestration")
	tracker.Add(Repo{Owner: "prometheus", Name: "prometheus"}, "cncf", "monitoring")
	tracker.Add(Repo{Owner: "grafana", Name: "grafana"}, "monitoring")

	cncfRepos := tracker.ByCategory("cncf")
	if len(cncfRepos) != 2 {
		t.Errorf("CNCF repos = %d, want 2", len(cncfRepos))
	}

	monitoringRepos := tracker.ByCategory("monitoring")
	if len(monitoringRepos) != 2 {
		t.Errorf("Monitoring repos = %d, want 2", len(monitoringRepos))
	}

	containerRepos := tracker.ByCategory("container-orchestration")
	if len(containerRepos) != 1 {
		t.Errorf("Container repos = %d, want 1", len(containerRepos))
	}
}

func TestTracker_ByCategory_Empty(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "cncf")

	repos := tracker.ByCategory("unknown")
	if len(repos) != 0 {
		t.Errorf("Unknown category repos = %d, want 0", len(repos))
	}
}

func TestTracker_Categories(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "cncf", "container-orchestration")
	tracker.Add(Repo{Owner: "prometheus", Name: "prometheus"}, "cncf", "monitoring")

	categories := tracker.Categories()

	expected := []string{"cncf", "container-orchestration", "monitoring"}
	if !reflect.DeepEqual(categories, expected) {
		t.Errorf("Categories = %v, want %v", categories, expected)
	}
}

func TestTracker_Categories_Empty(t *testing.T) {
	tracker := NewTracker()

	categories := tracker.Categories()

	if len(categories) != 0 {
		t.Errorf("Categories len = %d, want 0", len(categories))
	}
}

func TestTracker_Count(t *testing.T) {
	tracker := NewTracker()

	if tracker.Count() != 0 {
		t.Errorf("Count = %d, want 0", tracker.Count())
	}

	tracker.Add(Repo{Owner: "a", Name: "a"})
	tracker.Add(Repo{Owner: "b", Name: "b"})

	if tracker.Count() != 2 {
		t.Errorf("Count = %d, want 2", tracker.Count())
	}
}

func TestTracker_Get(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "cncf")

	repo := tracker.Get("kubernetes", "kubernetes")
	if repo == nil {
		t.Fatal("expected repo to be found")
	}
	if repo.Repo.Owner != "kubernetes" {
		t.Errorf("Owner = %q, want %q", repo.Repo.Owner, "kubernetes")
	}
}

func TestTracker_Get_NotFound(t *testing.T) {
	tracker := NewTracker()

	repo := tracker.Get("unknown", "repo")
	if repo != nil {
		t.Error("expected nil for non-existent repo")
	}
}

func TestTracker_HasRepo(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"})

	if !tracker.HasRepo("kubernetes", "kubernetes") {
		t.Error("expected HasRepo to return true")
	}
	if tracker.HasRepo("unknown", "repo") {
		t.Error("expected HasRepo to return false for non-existent repo")
	}
}

func TestNormalizeCategories(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{nil, []string{DefaultCategory}},
		{[]string{}, []string{DefaultCategory}},
		{[]string{"cncf"}, []string{"cncf"}},
		{[]string{"a", "b"}, []string{"a", "b"}},
	}

	for _, tc := range tests {
		result := NormalizeCategories(tc.input)
		if !reflect.DeepEqual(result, tc.expected) {
			t.Errorf("NormalizeCategories(%v) = %v, want %v", tc.input, result, tc.expected)
		}
	}
}

func TestMergeCategories(t *testing.T) {
	tests := []struct {
		existing []string
		new      []string
		expected []string
	}{
		{[]string{"a"}, []string{"b"}, []string{"a", "b"}},
		{[]string{"a", "b"}, []string{"b", "c"}, []string{"a", "b", "c"}},
		{[]string{}, []string{"a"}, []string{"a"}},
		{[]string{"a"}, []string{}, []string{"a"}},
	}

	for _, tc := range tests {
		result := mergeCategories(tc.existing, tc.new)
		if !reflect.DeepEqual(result, tc.expected) {
			t.Errorf("mergeCategories(%v, %v) = %v, want %v", tc.existing, tc.new, result, tc.expected)
		}
	}
}

func TestTrackedRepository_MultipleCategories(t *testing.T) {
	tracker := NewTracker()
	repo := Repo{Owner: "kubernetes", Name: "kubernetes"}

	// Add with multiple categories at once
	tracker.Add(repo, "cncf", "container-orchestration", "graduated")

	tr := tracker.Get("kubernetes", "kubernetes")
	if tr == nil {
		t.Fatal("expected repo to be found")
	}
	if len(tr.Categories) != 3 {
		t.Errorf("Categories len = %d, want 3", len(tr.Categories))
	}

	// Repo should appear in all three category queries
	if len(tracker.ByCategory("cncf")) != 1 {
		t.Error("expected repo in cncf category")
	}
	if len(tracker.ByCategory("container-orchestration")) != 1 {
		t.Error("expected repo in container-orchestration category")
	}
	if len(tracker.ByCategory("graduated")) != 1 {
		t.Error("expected repo in graduated category")
	}
}

func TestLoadFromConfig(t *testing.T) {
	repos := []TrackedRepoConfig{
		{Repo: "kubernetes/kubernetes", Categories: []string{"cncf", "container-orchestration"}},
		{Repo: "prometheus/prometheus", Categories: []string{"cncf", "monitoring"}},
		{Repo: "grafana/grafana", Categories: nil}, // Should get default category
	}

	tracker, errors := LoadFromConfig(repos)

	if len(errors) != 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	if tracker.Count() != 3 {
		t.Errorf("Count = %d, want 3", tracker.Count())
	}

	// Check categories
	k8s := tracker.Get("kubernetes", "kubernetes")
	if k8s == nil {
		t.Fatal("expected kubernetes repo to be found")
	}
	if len(k8s.Categories) != 2 {
		t.Errorf("kubernetes categories = %d, want 2", len(k8s.Categories))
	}

	grafana := tracker.Get("grafana", "grafana")
	if grafana == nil {
		t.Fatal("expected grafana repo to be found")
	}
	if len(grafana.Categories) != 1 {
		t.Errorf("grafana categories = %d, want 1", len(grafana.Categories))
	}
	if grafana.Categories[0] != DefaultCategory {
		t.Errorf("grafana category = %q, want %q", grafana.Categories[0], DefaultCategory)
	}
}

func TestLoadFromConfig_WithURLs(t *testing.T) {
	repos := []TrackedRepoConfig{
		{Repo: "https://github.com/kubernetes/kubernetes", Categories: []string{"cncf"}},
		{Repo: "github.com/prometheus/prometheus", Categories: []string{"monitoring"}},
	}

	tracker, errors := LoadFromConfig(repos)

	if len(errors) != 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	if tracker.Count() != 2 {
		t.Errorf("Count = %d, want 2", tracker.Count())
	}

	k8s := tracker.Get("kubernetes", "kubernetes")
	if k8s == nil {
		t.Fatal("expected kubernetes repo to be found")
	}
}

func TestLoadFromConfig_InvalidRepo(t *testing.T) {
	repos := []TrackedRepoConfig{
		{Repo: "kubernetes/kubernetes", Categories: []string{"cncf"}},
		{Repo: "invalid", Categories: []string{"bad"}}, // Invalid - no owner/repo
		{Repo: "prometheus/prometheus", Categories: []string{"monitoring"}},
	}

	tracker, errors := LoadFromConfig(repos)

	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(errors))
	}
	// Should still have the valid repos
	if tracker.Count() != 2 {
		t.Errorf("Count = %d, want 2 (valid repos only)", tracker.Count())
	}
}

func TestLoadFromConfig_Empty(t *testing.T) {
	tracker, errors := LoadFromConfig([]TrackedRepoConfig{})

	if len(errors) != 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	if tracker.Count() != 0 {
		t.Errorf("Count = %d, want 0", tracker.Count())
	}
}

func TestLoadFromConfigWithExclusions(t *testing.T) {
	repos := []TrackedRepoConfig{
		{Repo: "kubernetes/kubernetes", Categories: []string{"cncf"}},
		{Repo: "excluded/repo", Categories: []string{"test"}},
		{Repo: "prometheus/prometheus", Categories: []string{"monitoring"}},
	}

	exclusions := NewExclusionList([]string{"excluded/repo"})
	tracker, errors := LoadFromConfigWithExclusions(repos, exclusions)

	if len(errors) != 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	if tracker.Count() != 2 {
		t.Errorf("Count = %d, want 2 (excluded should be filtered)", tracker.Count())
	}
	if tracker.HasRepo("excluded", "repo") {
		t.Error("excluded repo should not be in tracker")
	}
}

func TestLoadFromConfigWithExclusions_Wildcard(t *testing.T) {
	repos := []TrackedRepoConfig{
		{Repo: "kubernetes/kubernetes", Categories: []string{"cncf"}},
		{Repo: "bad-org/repo1", Categories: []string{"test"}},
		{Repo: "bad-org/repo2", Categories: []string{"test"}},
	}

	exclusions := NewExclusionList([]string{"bad-org/*"})
	tracker, errors := LoadFromConfigWithExclusions(repos, exclusions)

	if len(errors) != 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	if tracker.Count() != 1 {
		t.Errorf("Count = %d, want 1 (all bad-org should be filtered)", tracker.Count())
	}
	if tracker.HasRepo("bad-org", "repo1") || tracker.HasRepo("bad-org", "repo2") {
		t.Error("bad-org repos should not be in tracker")
	}
}

func TestLoadFromConfigWithExclusions_NilExclusions(t *testing.T) {
	repos := []TrackedRepoConfig{
		{Repo: "kubernetes/kubernetes", Categories: []string{"cncf"}},
	}

	tracker, errors := LoadFromConfigWithExclusions(repos, nil)

	if len(errors) != 0 {
		t.Errorf("unexpected errors: %v", errors)
	}
	if tracker.Count() != 1 {
		t.Errorf("Count = %d, want 1", tracker.Count())
	}
}

func TestTracker_FilterExcluded(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "cncf")
	tracker.Add(Repo{Owner: "excluded", Name: "repo"}, "test")
	tracker.Add(Repo{Owner: "prometheus", Name: "prometheus"}, "monitoring")

	exclusions := NewExclusionList([]string{"excluded/repo"})
	filtered := tracker.FilterExcluded(exclusions)

	// Original tracker unchanged
	if tracker.Count() != 3 {
		t.Errorf("original Count = %d, want 3", tracker.Count())
	}

	// Filtered tracker has exclusion removed
	if filtered.Count() != 2 {
		t.Errorf("filtered Count = %d, want 2", filtered.Count())
	}
	if filtered.HasRepo("excluded", "repo") {
		t.Error("excluded repo should not be in filtered tracker")
	}
}

func TestTracker_FilterExcluded_NilExclusions(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "cncf")

	filtered := tracker.FilterExcluded(nil)

	// Should return a new tracker with same contents (consistent behavior)
	if filtered.Count() != tracker.Count() {
		t.Errorf("filtered Count = %d, want %d", filtered.Count(), tracker.Count())
	}
	if !filtered.HasRepo("kubernetes", "kubernetes") {
		t.Error("filtered should contain kubernetes repo")
	}
}

func TestTracker_CaseInsensitive(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "Kubernetes", Name: "Kubernetes"}, "cncf")

	// Should find with different case
	if !tracker.HasRepo("kubernetes", "kubernetes") {
		t.Error("HasRepo should be case-insensitive")
	}
	if !tracker.HasRepo("KUBERNETES", "KUBERNETES") {
		t.Error("HasRepo should be case-insensitive")
	}

	// Get should also work
	tr := tracker.Get("kubernetes", "kubernetes")
	if tr == nil {
		t.Error("Get should be case-insensitive")
	}

	// Adding same repo with different case should merge
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "container")
	if tracker.Count() != 1 {
		t.Errorf("Count = %d, want 1 (should merge case-insensitive)", tracker.Count())
	}

	// Remove should work with different case
	removed := tracker.Remove(Repo{Owner: "KUBERNETES", Name: "KUBERNETES"})
	if !removed {
		t.Error("Remove should be case-insensitive")
	}
}

func TestTracker_GetReturnsACopy(t *testing.T) {
	tracker := NewTracker()
	tracker.Add(Repo{Owner: "kubernetes", Name: "kubernetes"}, "cncf")

	// Get a reference
	tr := tracker.Get("kubernetes", "kubernetes")
	if tr == nil {
		t.Fatal("expected repo to be found")
	}

	// Modify the returned copy
	tr.Categories = append(tr.Categories, "modified")

	// Original should be unchanged
	original := tracker.Get("kubernetes", "kubernetes")
	for _, cat := range original.Categories {
		if cat == "modified" {
			t.Error("modifying returned copy should not affect original")
		}
	}
}
