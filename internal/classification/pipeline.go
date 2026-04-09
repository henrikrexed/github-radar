package classification

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/github"
)

// Result holds classification output for one repo.
type Result struct {
	Category   string
	Confidence float64
	Reasoning  string
	ModelUsed  string
	ReadmeHash string
	Duration   time.Duration
	Skipped    bool  // true if README unchanged
	Error      error // non-nil if classification failed
}

// Summary holds batch classification results.
type Summary struct {
	Total        int
	Classified   int
	NeedsReview  int
	Skipped      int
	Failed       int
	Duration     time.Duration
}

// Pipeline orchestrates LLM-based repository classification.
type Pipeline struct {
	db     *database.DB
	gh     *github.Client
	ollama *OllamaClient
	cfg    config.ClassificationConfig
}

// NewPipeline creates a classification pipeline with the given dependencies.
func NewPipeline(db *database.DB, gh *github.Client, ollama *OllamaClient, cfg config.ClassificationConfig) *Pipeline {
	return &Pipeline{
		db:     db,
		gh:     gh,
		ollama: ollama,
		cfg:    cfg,
	}
}

// ClassifySingle classifies a single repository by fetching its README,
// building prompts, calling the LLM, and returning the result.
// It does NOT persist the result to the database — the caller decides whether to save.
func (p *Pipeline) ClassifySingle(ctx context.Context, repo database.RepoRecord) (*Result, error) {
	start := time.Now()

	// Fetch README via GitHub API.
	owner, name := splitFullName(repo.FullName)
	readmeResp, err := p.gh.GetReadme(ctx, owner, name, "")
	if err != nil {
		return &Result{
			ModelUsed: p.ollama.Model(),
			Duration:  time.Since(start),
			Error:     fmt.Errorf("fetching readme: %w", err),
		}, nil
	}

	var readmeContent string
	if readmeResp.Found {
		readmeContent = readmeResp.Content
	}

	readmeHash := HashReadme(readmeContent)

	// Skip if README hash is unchanged and repo already classified.
	if repo.ReadmeHash != "" && repo.ReadmeHash == readmeHash && repo.PrimaryCategory != "" {
		return &Result{
			Category:   repo.PrimaryCategory,
			Confidence: repo.CategoryConfidence,
			ModelUsed:  repo.ModelUsed,
			ReadmeHash: readmeHash,
			Duration:   time.Since(start),
			Skipped:    true,
		}, nil
	}

	truncated := TruncateReadme(readmeContent, p.cfg.MaxReadmeChars)

	// Build prompts.
	systemPrompt, err := BuildSystemPrompt(p.cfg.SystemPrompt, p.cfg.Categories)
	if err != nil {
		return nil, fmt.Errorf("building system prompt: %w", err)
	}

	starTrend := "stable"
	if repo.Stars > repo.StarsPrev {
		starTrend = "rising"
	} else if repo.Stars < repo.StarsPrev {
		starTrend = "declining"
	}

	userPrompt, err := BuildUserPrompt(p.cfg.UserPrompt, PromptData{
		RepoName:    repo.FullName,
		Description: repo.Description,
		Language:    repo.Language,
		Topics:      repo.Topics,
		Stars:       repo.Stars,
		StarTrend:   starTrend,
		Readme:      truncated,
	})
	if err != nil {
		return nil, fmt.Errorf("building user prompt: %w", err)
	}

	// Call Ollama LLM.
	llmResult, err := p.ollama.Classify(ctx, systemPrompt, userPrompt)
	if err != nil {
		return &Result{
			ModelUsed:  p.ollama.Model(),
			ReadmeHash: readmeHash,
			Duration:   time.Since(start),
			Error:      fmt.Errorf("ollama classify: %w", err),
		}, nil
	}

	return &Result{
		Category:   llmResult.Category,
		Confidence: llmResult.Confidence,
		Reasoning:  llmResult.Reasoning,
		ModelUsed:  p.ollama.Model(),
		ReadmeHash: readmeHash,
		Duration:   time.Since(start),
	}, nil
}

// ClassifyAll queries the DB for repos needing classification and classifies each one.
// Results are persisted to the database. Progress is written to stderr.
func (p *Pipeline) ClassifyAll(ctx context.Context) (*Summary, error) {
	start := time.Now()

	repos, err := p.db.ReposNeedingClassification()
	if err != nil {
		return nil, fmt.Errorf("querying repos needing classification: %w", err)
	}

	summary := &Summary{Total: len(repos)}

	for i, repo := range repos {
		select {
		case <-ctx.Done():
			summary.Duration = time.Since(start)
			return summary, ctx.Err()
		default:
		}

		fmt.Fprintf(os.Stderr, "[%d/%d] Classifying %s ...", i+1, summary.Total, repo.FullName)

		result, err := p.ClassifySingle(ctx, repo)
		if err != nil {
			log.Printf("[classification] ERROR classifying %s: %v", repo.FullName, err)
			summary.Failed++
			continue
		}

		if result.Error != nil {
			log.Printf("[classification] ERROR classifying %s: %v", repo.FullName, result.Error)
			summary.Failed++
			continue
		}

		if result.Skipped {
			fmt.Fprintf(os.Stderr, " skipped (unchanged)\n")
			summary.Skipped++
			continue
		}

		// Persist result using the hash from ClassifySingle (no double fetch).
		// Pass MinConfidence so the DB layer can set needs_review for low-confidence results.
		if err := p.db.UpdateClassification(
			repo.FullName, result.Category, result.Confidence, result.ReadmeHash, result.ModelUsed, p.cfg.MinConfidence,
		); err != nil {
			log.Printf("[classification] ERROR saving classification for %s: %v", repo.FullName, err)
			summary.Failed++
			continue
		}

		if result.Confidence < p.cfg.MinConfidence {
			fmt.Fprintf(os.Stderr, " %s (%.0f%% < %.0f%% threshold → needs_review) [%s]\n",
				result.Category, result.Confidence*100, p.cfg.MinConfidence*100, result.Duration.Round(time.Millisecond))
			summary.NeedsReview++
		} else {
			fmt.Fprintf(os.Stderr, " %s (%.0f%%) [%s]\n",
				result.Category, result.Confidence*100, result.Duration.Round(time.Millisecond))
			summary.Classified++
		}
	}

	summary.Duration = time.Since(start)
	return summary, nil
}

// splitFullName splits "owner/repo" into owner and repo parts.
func splitFullName(fullName string) (string, string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return fullName, ""
	}
	return parts[0], parts[1]
}
