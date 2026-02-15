package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/hrexed/github-radar/internal/discovery"
	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/logging"
	"github.com/hrexed/github-radar/internal/state"
)

// DiscoverCmd handles the discover command.
type DiscoverCmd struct {
	cli *CLI
}

// NewDiscoverCmd creates a new discover command handler.
func NewDiscoverCmd(cli *CLI) *DiscoverCmd {
	return &DiscoverCmd{cli: cli}
}

// Run executes the discover command.
func (d *DiscoverCmd) Run(args []string) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	var (
		topics    string
		minStars  int
		maxAge    int
		threshold float64
		autoTrack bool
		format    string
	)

	fs.StringVar(&topics, "topics", "", "Comma-separated topics to search (overrides config)")
	fs.IntVar(&minStars, "min-stars", 0, "Minimum stars filter (overrides config)")
	fs.IntVar(&maxAge, "max-age", 0, "Maximum repo age in days (overrides config)")
	fs.Float64Var(&threshold, "threshold", 0, "Auto-track threshold (overrides config)")
	fs.BoolVar(&autoTrack, "auto-track", false, "Automatically add discovered repos to tracking")
	fs.StringVar(&format, "format", "table", "Output format: table, json, csv")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Load config
	if err := d.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	cfg := d.cli.Config

	// Build discovery config from config file, with CLI overrides
	discoveryCfg := discovery.Config{
		Topics:             cfg.Discovery.Topics,
		MinStars:           cfg.Discovery.MinStars,
		MaxAgeDays:         cfg.Discovery.MaxAgeDays,
		AutoTrackThreshold: cfg.Discovery.AutoTrackThreshold,
		Exclusions:         cfg.Exclusions,
	}

	// Apply CLI overrides
	if topics != "" {
		discoveryCfg.Topics = strings.Split(topics, ",")
		for i := range discoveryCfg.Topics {
			discoveryCfg.Topics[i] = strings.TrimSpace(discoveryCfg.Topics[i])
		}
	}
	if minStars > 0 {
		discoveryCfg.MinStars = minStars
	}
	if maxAge > 0 {
		discoveryCfg.MaxAgeDays = maxAge
	}
	if threshold > 0 {
		discoveryCfg.AutoTrackThreshold = threshold
	}

	// Validate we have topics
	if len(discoveryCfg.Topics) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no topics configured. Use --topics or configure discovery.topics in config file\n")
		return 1
	}

	// Create GitHub client
	client, err := github.NewClient(cfg.GitHub.Token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating GitHub client: %v\n", err)
		return 1
	}

	// Create state store
	store := state.NewStore(state.DefaultStatePath)

	// Load existing state
	if err := store.Load(); err != nil {
		logging.Debug("state file not found, starting fresh", "path", store.Path())
	}

	// Load tracked repos from config into state (for AlreadyTracked detection)
	for _, tracked := range cfg.Repositories {
		parts := strings.SplitN(tracked.Repo, "/", 2)
		if len(parts) != 2 {
			logging.Warn("invalid repo format, skipping", "repo", tracked.Repo)
			continue
		}
		if store.GetRepoState(tracked.Repo) == nil {
			store.SetRepoState(tracked.Repo, state.RepoState{
				Owner: parts[0],
				Name:  parts[1],
			})
		}
	}

	// Create discoverer
	discoverer := discovery.NewDiscoverer(client, store, discoveryCfg)
	discoverer.SetLogger(func(level, msg string, args ...interface{}) {
		switch level {
		case "debug":
			logging.Debug(msg, args...)
		case "info":
			logging.Info(msg, args...)
		case "warn":
			logging.Warn(msg, args...)
		case "error":
			logging.Error(msg, args...)
		}
	})

	// Run discovery
	ctx := context.Background()
	results, err := discoverer.DiscoverAll(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during discovery: %v\n", err)
		return 1
	}

	// Print results
	totalNew := 0
	totalAutoTrack := 0
	for _, result := range results {
		totalNew += result.NewRepos
		totalAutoTrack += result.AutoTracked
		d.printResult(result, format)
	}

	// Auto-track if requested
	if autoTrack {
		for _, result := range results {
			tracked := discoverer.AutoTrack(result)
			for _, repo := range tracked {
				fmt.Printf("Auto-tracked: %s (score: %.1f)\n", repo.FullName, repo.NormalizedScore)
			}
		}

		// Save state
		if err := store.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save state: %v\n", err)
		}
	}

	// Print summary
	fmt.Printf("\nSummary: %d new repos discovered, %d eligible for auto-tracking\n", totalNew, totalAutoTrack)
	if autoTrack && totalAutoTrack > 0 {
		fmt.Printf("Note: Run 'github-radar list' to see tracked repos\n")
	}

	return 0
}

// printResult prints a single discovery result.
func (d *DiscoverCmd) printResult(result *discovery.Result, format string) {
	switch format {
	case "json":
		d.printJSON(result)
	case "csv":
		d.printCSV(result)
	default:
		d.printTable(result)
	}
}

// printTable prints results in table format.
func (d *DiscoverCmd) printTable(result *discovery.Result) {
	fmt.Printf("\n=== Topic: %s ===\n", result.Topic)
	fmt.Printf("Found: %d | After filters: %d | New: %d | Auto-track eligible: %d\n\n",
		result.TotalFound, result.AfterFilters, result.NewRepos, result.AutoTracked)

	if len(result.Repos) == 0 {
		fmt.Println("No repositories match criteria.")
		return
	}

	// Sort by normalized score descending
	repos := make([]discovery.DiscoveredRepo, len(result.Repos))
	copy(repos, result.Repos)
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].NormalizedScore > repos[j].NormalizedScore
	})

	// Print header
	fmt.Printf("%-40s %8s %8s %8s %s\n", "Repository", "Stars", "Score", "Status", "Description")
	fmt.Println(strings.Repeat("-", 100))

	for _, repo := range repos {
		status := ""
		if repo.AlreadyTracked {
			status = "[tracked]"
		} else if repo.Excluded {
			status = "[excluded]"
		} else if repo.ShouldAutoTrack {
			status = "[auto-track]"
		} else {
			status = "[new]"
		}

		desc := repo.Description
		if len(desc) > 30 {
			desc = desc[:27] + "..."
		}

		fmt.Printf("%-40s %8d %8.1f %11s %s\n",
			truncate(repo.FullName, 40),
			repo.Stars,
			repo.NormalizedScore,
			status,
			desc,
		)
	}
}

// jsonResultOutput represents the JSON output structure for discovery results.
type jsonResultOutput struct {
	Topic        string           `json:"topic"`
	TotalFound   int              `json:"total_found"`
	AfterFilters int              `json:"after_filters"`
	New          int              `json:"new"`
	AutoTrack    int              `json:"auto_track"`
	Repos        []jsonRepoOutput `json:"repos"`
}

// jsonRepoOutput represents a single repo in JSON output.
type jsonRepoOutput struct {
	Name      string  `json:"name"`
	Stars     int     `json:"stars"`
	Score     float64 `json:"score"`
	AutoTrack bool    `json:"auto_track"`
	Tracked   bool    `json:"tracked"`
}

// printJSON prints results in JSON format.
func (d *DiscoverCmd) printJSON(result *discovery.Result) {
	output := jsonResultOutput{
		Topic:        result.Topic,
		TotalFound:   result.TotalFound,
		AfterFilters: result.AfterFilters,
		New:          result.NewRepos,
		AutoTrack:    result.AutoTracked,
		Repos:        make([]jsonRepoOutput, len(result.Repos)),
	}

	for i, repo := range result.Repos {
		output.Repos[i] = jsonRepoOutput{
			Name:      repo.FullName,
			Stars:     repo.Stars,
			Score:     repo.NormalizedScore,
			AutoTrack: repo.ShouldAutoTrack,
			Tracked:   repo.AlreadyTracked,
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.Encode(output)
}

// printCSV prints results in CSV format.
func (d *DiscoverCmd) printCSV(result *discovery.Result) {
	fmt.Println("topic,repository,stars,forks,score,auto_track,already_tracked,excluded")
	for _, repo := range result.Repos {
		fmt.Printf("%s,%s,%d,%d,%.1f,%t,%t,%t\n",
			result.Topic,
			repo.FullName,
			repo.Stars,
			repo.Forks,
			repo.NormalizedScore,
			repo.ShouldAutoTrack,
			repo.AlreadyTracked,
			repo.Excluded,
		)
	}
}

// truncate shortens a string to max length.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
