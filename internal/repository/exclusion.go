// Package repository provides repository identifier parsing and tracking management.
package repository

import (
	"strings"
	"sync"
)

// ExclusionList manages repository exclusion patterns.
// It is safe for concurrent use.
type ExclusionList struct {
	mu       sync.RWMutex
	patterns []string
}

// NewExclusionList creates a new exclusion list from patterns.
func NewExclusionList(patterns []string) *ExclusionList {
	return &ExclusionList{
		patterns: patterns,
	}
}

// Patterns returns all exclusion patterns.
func (e *ExclusionList) Patterns() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]string, len(e.patterns))
	copy(result, e.patterns)
	return result
}

// Add adds a pattern to the exclusion list.
// Returns true if the pattern was added, false if it already exists or is invalid.
func (e *ExclusionList) Add(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if !ValidatePattern(pattern) {
		return false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if already exists
	for _, p := range e.patterns {
		if p == pattern {
			return false
		}
	}

	e.patterns = append(e.patterns, pattern)
	return true
}

// Remove removes a pattern from the exclusion list.
// Returns true if the pattern was removed, false if it wasn't found.
func (e *ExclusionList) Remove(pattern string) bool {
	pattern = strings.TrimSpace(pattern)

	e.mu.Lock()
	defer e.mu.Unlock()

	for i, p := range e.patterns {
		if p == pattern {
			e.patterns = append(e.patterns[:i], e.patterns[i+1:]...)
			return true
		}
	}
	return false
}

// IsExcluded checks if a repository matches any exclusion pattern.
// Supports exact matches (owner/repo) and wildcard patterns (owner/*).
func (e *ExclusionList) IsExcluded(owner, name string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	fullName := owner + "/" + name

	for _, pattern := range e.patterns {
		if matchesPattern(pattern, fullName, owner) {
			return true
		}
	}
	return false
}

// IsExcludedRepo checks if a Repo matches any exclusion pattern.
func (e *ExclusionList) IsExcludedRepo(repo Repo) bool {
	return e.IsExcluded(repo.Owner, repo.Name)
}

// Count returns the number of exclusion patterns.
func (e *ExclusionList) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return len(e.patterns)
}

// WildcardSuffix is the suffix used for wildcard patterns.
const WildcardSuffix = "/*"

// IsWildcardPattern checks if a pattern is a wildcard pattern.
func IsWildcardPattern(pattern string) bool {
	return len(pattern) > len(WildcardSuffix) && strings.HasSuffix(pattern, WildcardSuffix)
}

// GetWildcardOwner extracts the owner from a wildcard pattern.
// Returns empty string if not a wildcard pattern.
func GetWildcardOwner(pattern string) string {
	if !IsWildcardPattern(pattern) {
		return ""
	}
	return pattern[:len(pattern)-len(WildcardSuffix)]
}

// matchesPattern checks if a repo matches an exclusion pattern.
// Supports:
//   - Exact match: "owner/repo"
//   - Wildcard: "owner/*" matches all repos from that owner
func matchesPattern(pattern, fullName, owner string) bool {
	// Handle wildcard pattern
	if wildcardOwner := GetWildcardOwner(pattern); wildcardOwner != "" {
		return equalsCaseInsensitive(owner, wildcardOwner)
	}

	// Exact match (case-insensitive for GitHub compatibility)
	return equalsCaseInsensitive(pattern, fullName)
}

// ValidatePattern checks if an exclusion pattern is valid.
// Valid patterns are:
//   - owner/repo (exact match)
//   - owner/* (wildcard)
//
// Invalid patterns include:
//   - Empty strings
//   - Patterns with path traversal (../)
//   - Patterns starting with special characters
func ValidatePattern(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}

	// Reject path traversal attempts
	if strings.Contains(pattern, "..") {
		return false
	}

	// Reject patterns starting with special characters
	if pattern[0] == '.' || pattern[0] == '/' || pattern[0] == '~' {
		return false
	}

	// Wildcard pattern
	if IsWildcardPattern(pattern) {
		owner := GetWildcardOwner(pattern)
		return owner != "" && !strings.Contains(owner, "/") && isValidIdentifier(owner)
	}

	// Should be owner/repo format
	parts := strings.Split(pattern, "/")
	if len(parts) != 2 {
		return false
	}

	return isValidIdentifier(parts[0]) && isValidIdentifier(parts[1])
}

// isValidIdentifier checks if a string is a valid GitHub owner or repo name.
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	// GitHub allows alphanumeric, hyphens, underscores
	// Must not start with hyphen
	if s[0] == '-' {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
