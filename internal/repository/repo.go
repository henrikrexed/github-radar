// Package repository provides repository identifier parsing and management.
package repository

import (
	"fmt"
	"net/url"
	"strings"
)

// Repo represents a GitHub repository.
type Repo struct {
	Owner string
	Name  string
}

// FullName returns the repository in owner/repo format.
func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

// String returns the repository in owner/repo format.
func (r Repo) String() string {
	return r.FullName()
}

// ParseError represents a repository parsing error.
type ParseError struct {
	Input string
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	return fmt.Sprintf(`invalid repository identifier: %q
Expected formats:
  - owner/repo
  - https://github.com/owner/repo
  - github.com/owner/repo`, e.Input)
}

// Parse parses a repository identifier in various formats.
// Supported formats:
//   - owner/repo
//   - https://github.com/owner/repo
//   - http://github.com/owner/repo
//   - github.com/owner/repo
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo/
func Parse(input string) (Repo, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Repo{}, &ParseError{Input: input}
	}

	// Try to parse as URL first
	if strings.Contains(input, "github.com") {
		return parseGitHubURL(input)
	}

	// Try simple owner/repo format
	return parseOwnerRepo(input)
}

// parseGitHubURL parses a GitHub URL in various formats.
func parseGitHubURL(input string) (Repo, error) {
	// Add scheme if missing
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		input = "https://" + input
	}

	parsed, err := url.Parse(input)
	if err != nil {
		return Repo{}, &ParseError{Input: input}
	}

	// Must be github.com
	if parsed.Host != "github.com" {
		return Repo{}, &ParseError{Input: input}
	}

	// Extract path and clean it
	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	return parseOwnerRepo(path)
}

// parseOwnerRepo parses the owner/repo format.
func parseOwnerRepo(input string) (Repo, error) {
	// Remove any leading/trailing slashes
	input = strings.Trim(input, "/")

	parts := strings.Split(input, "/")
	if len(parts) != 2 {
		return Repo{}, &ParseError{Input: input}
	}

	owner := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])

	if owner == "" || name == "" {
		return Repo{}, &ParseError{Input: input}
	}

	// Basic validation: no special characters that would be invalid
	if strings.ContainsAny(owner, " \t\n") || strings.ContainsAny(name, " \t\n") {
		return Repo{}, &ParseError{Input: input}
	}

	return Repo{Owner: owner, Name: name}, nil
}

// MustParse parses a repository identifier and panics on error.
// Use only in tests or initialization code.
func MustParse(input string) Repo {
	repo, err := Parse(input)
	if err != nil {
		panic(err)
	}
	return repo
}
