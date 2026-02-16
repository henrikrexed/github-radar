// Package cli provides CLI command implementations for github-radar.
package cli

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/hrexed/github-radar/internal/logging"
	"github.com/hrexed/github-radar/internal/repository"
)

// RepoCmd handles repository management commands.
type RepoCmd struct {
	cli *CLI
}

// NewRepoCmd creates a new RepoCmd instance.
func NewRepoCmd(cli *CLI) *RepoCmd {
	return &RepoCmd{cli: cli}
}

// reorderFlags moves flag arguments before positional arguments so that
// Go's flag package can parse them. For example:
// ["cilium/cilium", "--category", "cncf"] -> ["--category", "cncf", "cilium/cilium"]
func reorderFlags(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			// If this flag expects a value (not a boolean flag), grab the next arg too
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	return append(flags, positional...)
}

// Add adds a repository to tracking.
func (r *RepoCmd) Add(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	category := fs.String("category", "", "Category for the repository")

	if err := fs.Parse(reorderFlags(args)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintf(os.Stderr, "Error: repository argument required\n")
		fmt.Fprintf(os.Stderr, "Usage: github-radar add <owner/repo> [--category <name>]\n")
		return 1
	}

	repoArg := remaining[0]

	// Parse the repository identifier
	repo, err := repository.Parse(repoArg)
	if err != nil {
		if parseErr, ok := err.(*repository.ParseError); ok {
			fmt.Fprintf(os.Stderr, "Error: %s\n", parseErr.Error())
		} else {
			fmt.Fprintf(os.Stderr, "Error: invalid repository: %v\n", err)
		}
		return 1
	}

	// Determine category
	categories := []string{}
	if *category != "" {
		categories = append(categories, *category)
	}
	categories = repository.NormalizeCategories(categories)

	// Log the action
	logging.Info("adding repository",
		logging.Repo(repo.Owner, repo.Name)...,
	)
	logging.Debug("repository details",
		"categories", categories,
	)

	// TODO: In the future, validate repo exists via GitHub API (Epic 3)
	// TODO: Persist to state file (Epic 3)

	// Display success with warning about persistence
	if *category != "" {
		fmt.Printf("Added %s to category: %s\n", repo.FullName(), *category)
	} else {
		fmt.Printf("Added %s to category: %s\n", repo.FullName(), repository.DefaultCategory)
	}
	fmt.Println("Note: Changes are not persisted yet. Add to config file to persist.")

	return 0
}

// Remove removes a repository from tracking.
func (r *RepoCmd) Remove(args []string) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	keepState := fs.Bool("keep-state", false, "Preserve state data for the repository")

	if err := fs.Parse(reorderFlags(args)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintf(os.Stderr, "Error: repository argument required\n")
		fmt.Fprintf(os.Stderr, "Usage: github-radar remove <owner/repo> [--keep-state]\n")
		return 1
	}

	repoArg := remaining[0]

	// Parse the repository identifier
	repo, err := repository.Parse(repoArg)
	if err != nil {
		if parseErr, ok := err.(*repository.ParseError); ok {
			fmt.Fprintf(os.Stderr, "Error: %s\n", parseErr.Error())
		} else {
			fmt.Fprintf(os.Stderr, "Error: invalid repository: %v\n", err)
		}
		return 1
	}

	// Log the action
	logging.Info("removing repository",
		logging.Repo(repo.Owner, repo.Name)...,
	)
	if *keepState {
		logging.Debug("keeping state data", "keep_state", true)
	}

	// TODO: Actually remove from tracking (requires state management - Epic 3)

	fmt.Printf("Removed %s from tracking\n", repo.FullName())
	if *keepState {
		fmt.Println("State data preserved")
	}
	fmt.Println("Note: Changes are not persisted yet. Update config file to persist.")

	return 0
}

// List lists all tracked repositories.
func (r *RepoCmd) List(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	category := fs.String("category", "", "Filter by category")
	format := fs.String("format", "table", "Output format (table, json, csv)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Log the action
	logging.Debug("listing repositories",
		"category", *category,
		"format", *format,
	)

	// Load config to get repositories
	if err := r.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	if r.cli.Config == nil {
		fmt.Println("No repositories configured")
		return 0
	}

	// Convert config repos to TrackedRepoConfig
	configRepos := make([]repository.TrackedRepoConfig, len(r.cli.Config.Repositories))
	for i, tr := range r.cli.Config.Repositories {
		configRepos[i] = repository.TrackedRepoConfig{
			Repo:       tr.Repo,
			Categories: tr.Categories,
		}
	}

	tracker, errors := repository.LoadFromConfig(configRepos)
	if len(errors) > 0 {
		for _, err := range errors {
			logging.Warn("failed to parse repository", logging.Err(err)...)
		}
	}

	// Get repos to display
	var repos []repository.TrackedRepository
	if *category != "" {
		repos = tracker.ByCategory(*category)
	} else {
		repos = tracker.All()
	}

	if len(repos) == 0 {
		if *category != "" {
			fmt.Printf("No repositories in category: %s\n", *category)
		} else {
			fmt.Println("No repositories tracked")
		}
		return 0
	}

	// Output based on format
	switch *format {
	case "json":
		return r.listJSON(repos)
	case "csv":
		return r.listCSV(repos)
	default:
		return r.listTable(repos)
	}
}

// listTable outputs repos in table format.
func (r *RepoCmd) listTable(repos []repository.TrackedRepository) int {
	fmt.Printf("%-40s %s\n", "REPOSITORY", "CATEGORIES")
	fmt.Printf("%-40s %s\n", "----------", "----------")
	for _, tr := range repos {
		categories := ""
		for i, cat := range tr.Categories {
			if i > 0 {
				categories += ", "
			}
			categories += cat
		}
		fmt.Printf("%-40s %s\n", tr.Repo.FullName(), categories)
	}
	fmt.Printf("\nTotal: %d repositories\n", len(repos))
	return 0
}

// repoJSON is the JSON representation of a tracked repository.
type repoJSON struct {
	Repo       string   `json:"repo"`
	Owner      string   `json:"owner"`
	Name       string   `json:"name"`
	Categories []string `json:"categories"`
}

// listJSON outputs repos in JSON format using encoding/json.
func (r *RepoCmd) listJSON(repos []repository.TrackedRepository) int {
	output := make([]repoJSON, len(repos))
	for i, tr := range repos {
		output[i] = repoJSON{
			Repo:       tr.Repo.FullName(),
			Owner:      tr.Repo.Owner,
			Name:       tr.Repo.Name,
			Categories: tr.Categories,
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		return 1
	}
	return 0
}

// listCSV outputs repos in CSV format using encoding/csv.
func (r *RepoCmd) listCSV(repos []repository.TrackedRepository) int {
	writer := csv.NewWriter(os.Stdout)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{"repo", "owner", "name", "categories"}); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing CSV header: %v\n", err)
		return 1
	}

	// Write rows
	for _, tr := range repos {
		// Join categories with semicolon (CSV field separator is comma)
		categories := strings.Join(tr.Categories, ";")
		record := []string{tr.Repo.FullName(), tr.Repo.Owner, tr.Repo.Name, categories}
		if err := writer.Write(record); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing CSV row: %v\n", err)
			return 1
		}
	}

	return 0
}
