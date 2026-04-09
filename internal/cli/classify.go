package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hrexed/github-radar/internal/classification"
	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/logging"
)

// ClassifyCmd handles the classify command.
type ClassifyCmd struct {
	cli *CLI
}

// NewClassifyCmd creates a new classify command handler.
func NewClassifyCmd(cli *CLI) *ClassifyCmd {
	return &ClassifyCmd{cli: cli}
}

// Run executes the classify command, dispatching to subcommands when present.
func (c *ClassifyCmd) Run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "test":
			return c.runTest(args[1:])
		case "model":
			return c.runModel(args[1:])
		}
	}

	return c.runBatch(args)
}

// runBatch runs the batch classification flow (original classify behavior).
func (c *ClassifyCmd) runBatch(args []string) int {
	// Load config
	if err := c.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	cfg := c.cli.Config

	// Open database
	db, err := database.Open("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		return 1
	}
	defer db.Close()

	// Create GitHub client
	gh, err := github.NewClient(cfg.GitHub.Token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating GitHub client: %v\n", err)
		return 1
	}

	// Create Ollama client
	clsCfg := cfg.Classification
	ollama := classification.NewOllamaClient(
		clsCfg.OllamaEndpoint,
		clsCfg.Model,
		clsCfg.TimeoutMs,
		clsCfg.Categories,
	)

	// Dry-run mode: show repos that would be classified without calling LLM
	if c.cli.DryRun {
		return c.dryRun(db)
	}

	// Create pipeline and run classification
	pipeline := classification.NewPipeline(db, gh, ollama, clsCfg)
	ctx := context.Background()

	logging.Info("starting classification",
		"model", clsCfg.Model,
		"endpoint", clsCfg.OllamaEndpoint,
		"min_confidence", clsCfg.MinConfidence,
	)

	summary, err := pipeline.ClassifyAll(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during classification: %v\n", err)
		return 1
	}

	// Print summary
	c.printSummary(summary)

	if summary.Failed > 0 {
		return 1
	}
	return 0
}

// runTest classifies a single repository with verbose output for debugging.
// Does NOT save results to the database.
func (c *ClassifyCmd) runTest(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: github-radar classify test <owner/repo>\n")
		return 1
	}

	repoArg := args[0]
	parts := strings.SplitN(repoArg, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		fmt.Fprintf(os.Stderr, "Error: repository must be in owner/repo format\n")
		return 1
	}
	owner, repoName := parts[0], parts[1]

	// Load config
	if err := c.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	cfg := c.cli.Config
	clsCfg := cfg.Classification

	// Create GitHub client
	gh, err := github.NewClient(cfg.GitHub.Token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating GitHub client: %v\n", err)
		return 1
	}

	// Create Ollama client
	ollama := classification.NewOllamaClient(
		clsCfg.OllamaEndpoint,
		clsCfg.Model,
		clsCfg.TimeoutMs,
		clsCfg.Categories,
	)

	ctx := context.Background()
	start := time.Now()

	fmt.Printf("=== Classification Test: %s ===\n\n", repoArg)
	fmt.Printf("Model:    %s\n", clsCfg.Model)
	fmt.Printf("Endpoint: %s\n\n", clsCfg.OllamaEndpoint)

	// Fetch README
	fmt.Printf("Fetching README for %s/%s ...\n", owner, repoName)
	readmeResp, err := gh.GetReadme(ctx, owner, repoName, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching README: %v\n", err)
		return 1
	}

	var readmeContent string
	if readmeResp.Found {
		readmeContent = readmeResp.Content
		fmt.Printf("README found: %d characters\n", len(readmeContent))
	} else {
		fmt.Printf("README not found (repository has no README)\n")
	}

	readmeHash := classification.HashReadme(readmeContent)
	truncated := classification.TruncateReadme(readmeContent, clsCfg.MaxReadmeChars)
	if len(readmeContent) > clsCfg.MaxReadmeChars {
		fmt.Printf("Truncated to %d characters\n", clsCfg.MaxReadmeChars)
	}
	fmt.Printf("README SHA256: %s\n\n", readmeHash)

	// Build prompts
	systemPrompt, err := classification.BuildSystemPrompt(clsCfg.SystemPrompt, clsCfg.Categories)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building system prompt: %v\n", err)
		return 1
	}

	userPrompt, err := classification.BuildUserPrompt(clsCfg.UserPrompt, classification.PromptData{
		RepoName:    repoArg,
		Description: "", // not available without DB lookup
		Language:    "",
		Topics:      "",
		Stars:       0,
		StarTrend:   "unknown",
		Readme:      truncated,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error building user prompt: %v\n", err)
		return 1
	}

	fmt.Printf("--- System Prompt ---\n%s\n\n", systemPrompt)
	fmt.Printf("--- User Prompt ---\n%s\n\n", userPrompt)

	// Call Ollama
	fmt.Printf("Calling Ollama (%s) ...\n", clsCfg.Model)
	llmStart := time.Now()
	result, err := ollama.Classify(ctx, systemPrompt, userPrompt)
	llmDuration := time.Since(llmStart)
	totalDuration := time.Since(start)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error from Ollama: %v\n", err)
		return 1
	}

	// Print full verbose result
	fmt.Printf("\n=== Classification Result ===\n")
	fmt.Printf("Category:   %s\n", result.Category)
	fmt.Printf("Confidence: %.1f%%\n", result.Confidence*100)
	fmt.Printf("Reasoning:  %s\n", result.Reasoning)
	fmt.Printf("Model:      %s\n", clsCfg.Model)
	fmt.Printf("LLM time:   %s\n", llmDuration.Round(time.Millisecond))
	fmt.Printf("Total time: %s\n", totalDuration.Round(time.Millisecond))

	if result.Confidence < clsCfg.MinConfidence {
		fmt.Printf("\n⚠ Confidence %.1f%% is below threshold %.1f%% (would be marked needs_review)\n",
			result.Confidence*100, clsCfg.MinConfidence*100)
	}

	return 0
}

// runModel shows or sets the classification model.
// No args: prints the current model from config.
// With a model name: updates config, marks all classified repos as needs_reclassify.
func (c *ClassifyCmd) runModel(args []string) int {
	if err := c.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	cfg := c.cli.Config

	// No args: show current model
	if len(args) == 0 {
		fmt.Printf("Current classification model: %s\n", cfg.Classification.Model)
		return 0
	}

	newModel := args[0]
	oldModel := cfg.Classification.Model

	if newModel == oldModel {
		fmt.Printf("Model is already set to %s\n", oldModel)
		return 0
	}

	// Mark all classified repos as needs_reclassify (DB first, config second)
	db, err := database.Open("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		return 1
	}
	defer db.Close()

	count, err := db.MarkAllNeedsReclassify()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marking repos for reclassification: %v\n", err)
		return 1
	}

	// Update model in config and save
	cfg.Classification.Model = newModel
	if err := config.SaveToPath(c.cli.ConfigPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		return 1
	}

	fmt.Printf("Classification model changed: %s -> %s\n", oldModel, newModel)
	fmt.Printf("Queued %d repos for reclassification\n", count)
	return 0
}

// dryRun shows repos that would be classified without calling the LLM.
func (c *ClassifyCmd) dryRun(db *database.DB) int {
	repos, err := db.ReposNeedingClassification()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying repos: %v\n", err)
		return 1
	}

	if len(repos) == 0 {
		fmt.Println("No repositories need classification.")
		return 0
	}

	fmt.Printf("Dry run: %d repositories would be classified:\n\n", len(repos))
	for i, repo := range repos {
		status := repo.Status
		if status == "" {
			status = "pending"
		}
		category := repo.PrimaryCategory
		if category == "" {
			category = "unclassified"
		}
		fmt.Printf("  [%d/%d] %s (status: %s, category: %s)\n", i+1, len(repos), repo.FullName, status, category)
	}

	fmt.Printf("\nTotal: %d repos pending classification\n", len(repos))
	return 0
}

// printSummary prints the classification batch summary.
func (c *ClassifyCmd) printSummary(s *classification.Summary) {
	fmt.Fprintf(os.Stderr, "\n--- Classification Summary ---\n")
	fmt.Fprintf(os.Stderr, "Total:        %d\n", s.Total)
	fmt.Fprintf(os.Stderr, "Classified:   %d\n", s.Classified)
	fmt.Fprintf(os.Stderr, "Needs review: %d\n", s.NeedsReview)
	fmt.Fprintf(os.Stderr, "Skipped:      %d\n", s.Skipped)
	fmt.Fprintf(os.Stderr, "Failed:       %d\n", s.Failed)
	fmt.Fprintf(os.Stderr, "Duration:     %s\n", s.Duration.Round(time.Millisecond))
}
