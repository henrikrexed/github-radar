package repository

import (
	"reflect"
	"testing"
)

func TestExclusionList_Add(t *testing.T) {
	el := NewExclusionList(nil)

	added := el.Add("owner/repo")
	if !added {
		t.Error("expected Add to return true for new pattern")
	}
	if el.Count() != 1 {
		t.Errorf("Count = %d, want 1", el.Count())
	}

	// Adding duplicate should return false
	added = el.Add("owner/repo")
	if added {
		t.Error("expected Add to return false for duplicate")
	}
	if el.Count() != 1 {
		t.Errorf("Count = %d, want 1 (no duplicate)", el.Count())
	}
}

func TestExclusionList_Add_Empty(t *testing.T) {
	el := NewExclusionList(nil)

	added := el.Add("")
	if added {
		t.Error("expected Add to return false for empty pattern")
	}

	added = el.Add("   ")
	if added {
		t.Error("expected Add to return false for whitespace pattern")
	}
}

func TestExclusionList_Remove(t *testing.T) {
	el := NewExclusionList([]string{"owner/repo", "org/*"})

	removed := el.Remove("owner/repo")
	if !removed {
		t.Error("expected Remove to return true")
	}
	if el.Count() != 1 {
		t.Errorf("Count = %d, want 1", el.Count())
	}

	// Removing non-existent should return false
	removed = el.Remove("nonexistent/repo")
	if removed {
		t.Error("expected Remove to return false for non-existent")
	}
}

func TestExclusionList_IsExcluded_ExactMatch(t *testing.T) {
	el := NewExclusionList([]string{"owner/repo"})

	if !el.IsExcluded("owner", "repo") {
		t.Error("expected owner/repo to be excluded")
	}
	if el.IsExcluded("owner", "other") {
		t.Error("expected owner/other to NOT be excluded")
	}
	if el.IsExcluded("other", "repo") {
		t.Error("expected other/repo to NOT be excluded")
	}
}

func TestExclusionList_IsExcluded_Wildcard(t *testing.T) {
	el := NewExclusionList([]string{"example-org/*"})

	// Should match any repo from example-org
	if !el.IsExcluded("example-org", "repo1") {
		t.Error("expected example-org/repo1 to be excluded")
	}
	if !el.IsExcluded("example-org", "repo2") {
		t.Error("expected example-org/repo2 to be excluded")
	}
	if !el.IsExcluded("example-org", "any-repo") {
		t.Error("expected example-org/any-repo to be excluded")
	}

	// Should NOT match other orgs
	if el.IsExcluded("other-org", "repo") {
		t.Error("expected other-org/repo to NOT be excluded")
	}
}

func TestExclusionList_IsExcluded_Mixed(t *testing.T) {
	el := NewExclusionList([]string{
		"exact/repo",
		"wildcard-org/*",
	})

	// Exact match
	if !el.IsExcluded("exact", "repo") {
		t.Error("expected exact/repo to be excluded")
	}

	// Wildcard match
	if !el.IsExcluded("wildcard-org", "any") {
		t.Error("expected wildcard-org/any to be excluded")
	}

	// Not excluded
	if el.IsExcluded("other", "repo") {
		t.Error("expected other/repo to NOT be excluded")
	}
}

func TestExclusionList_IsExcludedRepo(t *testing.T) {
	el := NewExclusionList([]string{"owner/repo"})

	repo := Repo{Owner: "owner", Name: "repo"}
	if !el.IsExcludedRepo(repo) {
		t.Error("expected repo to be excluded")
	}

	repo2 := Repo{Owner: "other", Name: "repo"}
	if el.IsExcludedRepo(repo2) {
		t.Error("expected repo2 to NOT be excluded")
	}
}

func TestExclusionList_Patterns(t *testing.T) {
	patterns := []string{"a/b", "c/*"}
	el := NewExclusionList(patterns)

	result := el.Patterns()
	if !reflect.DeepEqual(result, patterns) {
		t.Errorf("Patterns = %v, want %v", result, patterns)
	}

	// Ensure it's a copy
	result[0] = "modified"
	if el.patterns[0] == "modified" {
		t.Error("Patterns() should return a copy")
	}
}

func TestValidatePattern(t *testing.T) {
	tests := []struct {
		pattern string
		valid   bool
	}{
		{"owner/repo", true},
		{"org/*", true},
		{"my-org/*", true},
		{"owner_name/repo_name", true},
		{"owner.name/repo.name", true},
		{"", false},
		{"   ", false},
		{"noslash", false},
		{"/repo", false},
		{"owner/", false},
		{"a/b/c", false},
		{"/*", false},
		// Path traversal attacks
		{"../secret", false},
		{"owner/../etc", false},
		// Special character starts
		{"./current", false},
		{"/absolute", false},
		{"~/home", false},
		// Invalid characters
		{"-invalid/repo", false},
	}

	for _, tc := range tests {
		result := ValidatePattern(tc.pattern)
		if result != tc.valid {
			t.Errorf("ValidatePattern(%q) = %v, want %v", tc.pattern, result, tc.valid)
		}
	}
}

func TestExclusionList_Add_InvalidPattern(t *testing.T) {
	el := NewExclusionList(nil)

	// Invalid patterns should not be added
	if el.Add("invalid") {
		t.Error("Add should return false for invalid pattern")
	}
	if el.Add("../traversal") {
		t.Error("Add should return false for path traversal")
	}
	if el.Count() != 0 {
		t.Errorf("Count = %d, want 0 (no invalid patterns added)", el.Count())
	}
}

func TestExclusionList_CaseInsensitive(t *testing.T) {
	el := NewExclusionList([]string{"Owner/Repo", "MyOrg/*"})

	// Exact match should be case-insensitive
	if !el.IsExcluded("owner", "repo") {
		t.Error("IsExcluded should be case-insensitive for exact match")
	}
	if !el.IsExcluded("OWNER", "REPO") {
		t.Error("IsExcluded should be case-insensitive for exact match")
	}

	// Wildcard should be case-insensitive
	if !el.IsExcluded("myorg", "anything") {
		t.Error("IsExcluded should be case-insensitive for wildcard")
	}
	if !el.IsExcluded("MYORG", "anything") {
		t.Error("IsExcluded should be case-insensitive for wildcard")
	}
}

func TestIsWildcardPattern(t *testing.T) {
	tests := []struct {
		pattern    string
		isWildcard bool
	}{
		{"owner/*", true},
		{"my-org/*", true},
		{"/*", false}, // Too short - no owner
		{"owner/repo", false},
		{"", false},
	}

	for _, tc := range tests {
		result := IsWildcardPattern(tc.pattern)
		if result != tc.isWildcard {
			t.Errorf("IsWildcardPattern(%q) = %v, want %v", tc.pattern, result, tc.isWildcard)
		}
	}
}

func TestGetWildcardOwner(t *testing.T) {
	tests := []struct {
		pattern string
		owner   string
	}{
		{"owner/*", "owner"},
		{"my-org/*", "my-org"},
		{"owner/repo", ""},
		{"/*", ""},
	}

	for _, tc := range tests {
		result := GetWildcardOwner(tc.pattern)
		if result != tc.owner {
			t.Errorf("GetWildcardOwner(%q) = %q, want %q", tc.pattern, result, tc.owner)
		}
	}
}

func TestExclusionList_Empty(t *testing.T) {
	el := NewExclusionList(nil)

	if el.Count() != 0 {
		t.Errorf("Count = %d, want 0", el.Count())
	}
	if el.IsExcluded("any", "repo") {
		t.Error("empty list should not exclude anything")
	}
}
