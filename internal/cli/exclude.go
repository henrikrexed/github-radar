// Package cli provides CLI command implementations for github-radar.
package cli

import (
	"fmt"
	"os"

	"github.com/hrexed/github-radar/internal/logging"
	"github.com/hrexed/github-radar/internal/repository"
)

// ExcludeCmd handles exclusion management commands.
type ExcludeCmd struct {
	cli *CLI
}

// NewExcludeCmd creates a new ExcludeCmd instance.
func NewExcludeCmd(cli *CLI) *ExcludeCmd {
	return &ExcludeCmd{cli: cli}
}

// Run executes the exclude subcommand.
func (e *ExcludeCmd) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: action required (add, remove, list)\n")
		fmt.Fprintf(os.Stderr, "Usage: github-radar exclude <add|remove|list> [pattern]\n")
		return 1
	}

	action := args[0]
	remaining := args[1:]

	switch action {
	case "add":
		return e.Add(remaining)
	case "remove":
		return e.Remove(remaining)
	case "list":
		return e.List()
	default:
		fmt.Fprintf(os.Stderr, "Unknown exclude action: %s\n", action)
		fmt.Fprintf(os.Stderr, "Usage: github-radar exclude <add|remove|list> [pattern]\n")
		return 1
	}
}

// Add adds a pattern to the exclusion list.
func (e *ExcludeCmd) Add(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: pattern required\n")
		fmt.Fprintf(os.Stderr, "Usage: github-radar exclude add <owner/repo|owner/*>\n")
		return 1
	}

	pattern := args[0]

	// Validate pattern
	if !repository.ValidatePattern(pattern) {
		fmt.Fprintf(os.Stderr, "Error: invalid exclusion pattern: %q\n", pattern)
		fmt.Fprintf(os.Stderr, "Valid formats:\n")
		fmt.Fprintf(os.Stderr, "  - owner/repo (exact match)\n")
		fmt.Fprintf(os.Stderr, "  - owner/* (wildcard, entire org)\n")
		return 1
	}

	logging.Info("adding exclusion pattern",
		"pattern", pattern,
	)

	// TODO: Actually persist to config (requires config write support - Epic 3)

	fmt.Printf("Added exclusion: %s\n", pattern)
	fmt.Println("Note: Changes are not persisted yet. Add to config file to persist.")
	return 0
}

// Remove removes a pattern from the exclusion list.
func (e *ExcludeCmd) Remove(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Error: pattern required\n")
		fmt.Fprintf(os.Stderr, "Usage: github-radar exclude remove <pattern>\n")
		return 1
	}

	pattern := args[0]

	logging.Info("removing exclusion pattern",
		"pattern", pattern,
	)

	// TODO: Actually remove from config (requires config write support - Epic 3)

	fmt.Printf("Removed exclusion: %s\n", pattern)
	fmt.Println("Note: Changes are not persisted yet. Update config file to persist.")
	return 0
}

// List lists all exclusion patterns.
func (e *ExcludeCmd) List() int {
	// Load config to get exclusions
	if err := e.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	if e.cli.Config == nil {
		fmt.Println("No exclusions configured")
		return 0
	}

	exclusions := e.cli.Config.Exclusions
	if len(exclusions) == 0 {
		fmt.Println("No exclusions configured")
		return 0
	}

	fmt.Println("Exclusion patterns:")
	for _, pattern := range exclusions {
		// Indicate pattern type using repository package helper
		if repository.IsWildcardPattern(pattern) {
			fmt.Printf("  %s (wildcard)\n", pattern)
		} else {
			fmt.Printf("  %s (exact)\n", pattern)
		}
	}
	fmt.Printf("\nTotal: %d exclusions\n", len(exclusions))

	return 0
}
