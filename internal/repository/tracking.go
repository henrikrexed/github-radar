// Package repository provides repository identifier parsing and tracking management.
package repository

import (
	"sort"
	"sync"
)

// DefaultCategory is used when no category is specified for a repository.
const DefaultCategory = "default"

// TrackedRepository represents a repository with its tracking metadata.
type TrackedRepository struct {
	Repo       Repo
	Categories []string
}

// Tracker manages tracked repositories and their categories.
// It is safe for concurrent use.
type Tracker struct {
	mu    sync.RWMutex
	repos []TrackedRepository
}

// NewTracker creates a new repository tracker.
func NewTracker() *Tracker {
	return &Tracker{
		repos: []TrackedRepository{},
	}
}

// Add adds a repository with the specified categories.
// If no categories are provided, the repository is added to the default category.
func (t *Tracker) Add(repo Repo, categories ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(categories) == 0 {
		categories = []string{DefaultCategory}
	}

	// Check if repo already exists
	for i, tr := range t.repos {
		if equalsCaseInsensitive(tr.Repo.Owner, repo.Owner) &&
			equalsCaseInsensitive(tr.Repo.Name, repo.Name) {
			// Merge categories
			t.repos[i].Categories = mergeCategories(tr.Categories, categories)
			return
		}
	}

	// Add new repo
	t.repos = append(t.repos, TrackedRepository{
		Repo:       repo,
		Categories: categories,
	})
}

// Remove removes a repository from all categories.
// Returns true if the repository was found and removed.
func (t *Tracker) Remove(repo Repo) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, tr := range t.repos {
		if equalsCaseInsensitive(tr.Repo.Owner, repo.Owner) &&
			equalsCaseInsensitive(tr.Repo.Name, repo.Name) {
			t.repos = append(t.repos[:i], t.repos[i+1:]...)
			return true
		}
	}
	return false
}

// All returns all tracked repositories.
func (t *Tracker) All() []TrackedRepository {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]TrackedRepository, len(t.repos))
	for i, tr := range t.repos {
		result[i] = TrackedRepository{
			Repo:       tr.Repo,
			Categories: copyStrings(tr.Categories),
		}
	}
	return result
}

// ByCategory returns all repositories in the specified category.
func (t *Tracker) ByCategory(category string) []TrackedRepository {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var result []TrackedRepository
	for _, tr := range t.repos {
		if containsCategory(tr.Categories, category) {
			result = append(result, TrackedRepository{
				Repo:       tr.Repo,
				Categories: copyStrings(tr.Categories),
			})
		}
	}
	return result
}

// Categories returns all unique categories across all tracked repositories.
func (t *Tracker) Categories() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	categorySet := make(map[string]struct{})
	for _, tr := range t.repos {
		for _, cat := range tr.Categories {
			categorySet[cat] = struct{}{}
		}
	}

	categories := make([]string, 0, len(categorySet))
	for cat := range categorySet {
		categories = append(categories, cat)
	}
	sort.Strings(categories)
	return categories
}

// Count returns the total number of tracked repositories.
func (t *Tracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return len(t.repos)
}

// Get returns a copy of the tracked repository with the given owner/name.
// Returns nil if not found.
func (t *Tracker) Get(owner, name string) *TrackedRepository {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, tr := range t.repos {
		if equalsCaseInsensitive(tr.Repo.Owner, owner) &&
			equalsCaseInsensitive(tr.Repo.Name, name) {
			// Return a copy to prevent external mutation
			return &TrackedRepository{
				Repo:       tr.Repo,
				Categories: copyStrings(tr.Categories),
			}
		}
	}
	return nil
}

// HasRepo checks if a repository is being tracked.
func (t *Tracker) HasRepo(owner, name string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, tr := range t.repos {
		if equalsCaseInsensitive(tr.Repo.Owner, owner) &&
			equalsCaseInsensitive(tr.Repo.Name, name) {
			return true
		}
	}
	return false
}

// mergeCategories combines two category lists, removing duplicates.
func mergeCategories(existing, new []string) []string {
	categorySet := make(map[string]struct{})
	for _, cat := range existing {
		categorySet[cat] = struct{}{}
	}
	for _, cat := range new {
		categorySet[cat] = struct{}{}
	}

	merged := make([]string, 0, len(categorySet))
	for cat := range categorySet {
		merged = append(merged, cat)
	}
	sort.Strings(merged)
	return merged
}

// containsCategory checks if a category exists in the list.
func containsCategory(categories []string, target string) bool {
	for _, cat := range categories {
		if cat == target {
			return true
		}
	}
	return false
}

// copyStrings returns a copy of a string slice.
func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	result := make([]string, len(s))
	copy(result, s)
	return result
}

// equalsCaseInsensitive compares two strings case-insensitively.
// GitHub treats owner/repo names as case-insensitive.
func equalsCaseInsensitive(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// NormalizeCategories ensures the category list is not empty.
// Returns [DefaultCategory] if the input is empty.
func NormalizeCategories(categories []string) []string {
	if len(categories) == 0 {
		return []string{DefaultCategory}
	}
	return categories
}

// TrackedRepoConfig represents a repository entry from configuration.
// This intentionally duplicates config.TrackedRepo to avoid circular imports
// between the repository and config packages. The config package depends on
// repository for parsing, so repository cannot import config.
type TrackedRepoConfig struct {
	Repo       string
	Categories []string
}

// LoadFromConfig creates a Tracker and populates it from configuration.
// Each repo string is parsed and added with its categories.
// Returns the tracker and any parsing errors that occurred.
func LoadFromConfig(repos []TrackedRepoConfig) (*Tracker, []error) {
	tracker := NewTracker()
	var errors []error

	for _, tr := range repos {
		repo, err := Parse(tr.Repo)
		if err != nil {
			errors = append(errors, err)
			continue
		}
		categories := NormalizeCategories(tr.Categories)
		tracker.Add(repo, categories...)
	}

	return tracker, errors
}

// LoadFromConfigWithExclusions creates a Tracker, filtering out excluded repos.
// Excluded repos are silently skipped.
func LoadFromConfigWithExclusions(repos []TrackedRepoConfig, exclusions *ExclusionList) (*Tracker, []error) {
	tracker := NewTracker()
	var errors []error

	for _, tr := range repos {
		repo, err := Parse(tr.Repo)
		if err != nil {
			errors = append(errors, err)
			continue
		}

		// Skip if excluded
		if exclusions != nil && exclusions.IsExcludedRepo(repo) {
			continue
		}

		categories := NormalizeCategories(tr.Categories)
		tracker.Add(repo, categories...)
	}

	return tracker, errors
}

// FilterExcluded returns a new Tracker with excluded repos removed.
// Always returns a new Tracker instance for consistency.
func (t *Tracker) FilterExcluded(exclusions *ExclusionList) *Tracker {
	t.mu.RLock()
	defer t.mu.RUnlock()

	filtered := NewTracker()
	for _, tr := range t.repos {
		if exclusions == nil || !exclusions.IsExcludedRepo(tr.Repo) {
			filtered.addWithoutLock(tr.Repo, tr.Categories...)
		}
	}
	return filtered
}

// addWithoutLock adds a repository without acquiring the lock.
// Used internally when the lock is already held.
func (t *Tracker) addWithoutLock(repo Repo, categories ...string) {
	if len(categories) == 0 {
		categories = []string{DefaultCategory}
	}

	for i, tr := range t.repos {
		if equalsCaseInsensitive(tr.Repo.Owner, repo.Owner) &&
			equalsCaseInsensitive(tr.Repo.Name, repo.Name) {
			t.repos[i].Categories = mergeCategories(tr.Categories, categories)
			return
		}
	}

	t.repos = append(t.repos, TrackedRepository{
		Repo:       repo,
		Categories: categories,
	})
}
