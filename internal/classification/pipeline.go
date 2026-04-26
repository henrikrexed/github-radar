package classification

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/github"
)

// ErrClassificationAbortedOllama is returned by ClassifyAll when the Ollama
// circuit breaker trips mid-cycle. The daemon recognises this sentinel via
// errors.Is and emits radar.classification.run{result="aborted_ollama"} so
// the existing run-failed alert (ISI-775) doesn't conflate a code bug with a
// known infrastructure outage. See ISI-782.
var ErrClassificationAbortedOllama = errors.New("classification aborted: ollama unreachable")

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
	Total       int
	Classified  int
	NeedsReview int
	Skipped     int
	Failed      int
	Duration    time.Duration
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

// OllamaEndpoint returns the configured Ollama endpoint for tagging the
// radar.classification.ollama_reachable gauge. Returns "" when classification
// is not configured (no Ollama client). ISI-782.
func (p *Pipeline) OllamaEndpoint() string {
	if p.ollama == nil {
		return ""
	}
	return p.ollama.Endpoint()
}

// OllamaReachable returns (reachable, observed) for the most recent Classify
// call. observed=false means no call has yet completed and the daemon should
// skip emitting the gauge rather than fabricate a value. ISI-782.
func (p *Pipeline) OllamaReachable() (reachable, observed bool) {
	if p.ollama == nil {
		return false, false
	}
	return p.ollama.LastReachable()
}

// ClassifySingle classifies a single repository by fetching its README,
// building prompts, calling the LLM, and returning the result.
// It does NOT persist the result to the database — the caller decides whether to save.
//
// Description and topics are live-fetched from the GitHub API here instead of
// read from the database (ISI-744, folded into the v3 taxonomy migration).
// The scanner does not persist those fields because they were empty for 100%
// of repos in production; the GitHub API is the authoritative source and
// classification runs are rare enough that one extra `GET /repos/{owner}/{name}`
// per repo is acceptable.
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

	// Live-fetch description + topics from the GitHub API. If the fetch fails
	// we continue with empty values rather than aborting — the README is still
	// the main classifier signal, and matches prior behavior where DB columns
	// were effectively empty strings.
	var description, topics string
	if repoMeta, metaErr := p.gh.GetRepository(ctx, owner, name); metaErr == nil && repoMeta != nil {
		description = repoMeta.Description
		topics = strings.Join(repoMeta.Topics, ",")
	} else if metaErr != nil {
		log.Printf("[classification] WARNING: live-fetch description/topics failed for %s: %v", repo.FullName, metaErr)
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
		Description: description,
		Language:    repo.Language,
		Topics:      topics,
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

// CheckReadmeHashes checks all classified repos for README content changes.
// For each repo whose README hash has changed, it marks the repo as needs_reclassify
// via UpdateReadmeHash so it will be picked up by the next classification run.
// Returns the number of repos marked for reclassification.
func (p *Pipeline) CheckReadmeHashes(ctx context.Context) (int, error) {
	repos, err := p.db.ClassifiedRepos()
	if err != nil {
		return 0, fmt.Errorf("querying classified repos: %w", err)
	}

	if len(repos) == 0 {
		return 0, nil
	}

	fmt.Fprintf(os.Stderr, "Checking README hashes for %d classified repos...\n", len(repos))
	changed := 0

	for _, repo := range repos {
		select {
		case <-ctx.Done():
			return changed, ctx.Err()
		default:
		}

		owner, name := splitFullName(repo.FullName)
		readmeResp, err := p.gh.GetReadme(ctx, owner, name, "")
		if err != nil {
			log.Printf("[classification] WARNING: failed to fetch README for %s: %v", repo.FullName, err)
			continue
		}

		var readmeContent string
		if readmeResp.Found {
			readmeContent = readmeResp.Content
		}

		newHash := HashReadme(readmeContent)
		hashChanged, err := p.db.UpdateReadmeHash(repo.FullName, newHash)
		if err != nil {
			log.Printf("[classification] ERROR updating readme hash for %s: %v", repo.FullName, err)
			continue
		}

		if hashChanged {
			log.Printf("[classification] README changed for %s, marked needs_reclassify", repo.FullName)
			changed++
		}
	}

	if changed > 0 {
		fmt.Fprintf(os.Stderr, "Detected %d README changes, repos marked for reclassification.\n", changed)
	}
	return changed, nil
}

// ClassifyAll queries the DB for repos needing classification and classifies each one.
// It first checks all classified repos for README hash changes, marking changed ones
// as needs_reclassify so they are included in this classification run.
// Results are persisted to the database. Progress is written to stderr.
//
// Circuit-breaker (ISI-782): the Ollama breaker is reset at the start of every
// invocation so a recovered Ollama is re-tried promptly. If the breaker trips
// mid-batch, ClassifyAll emits a single ERROR log line and returns
// ErrClassificationAbortedOllama so the daemon can record
// result="aborted_ollama" instead of "failed" — Ollama down is an
// infra-class signal, not a code-class regression.
func (p *Pipeline) ClassifyAll(ctx context.Context) (*Summary, error) {
	start := time.Now()

	// Reset the Ollama circuit breaker so a recovered server is re-tried at
	// the top of every cycle (ISI-782). Otherwise a single outage would
	// permanently disable classification until the daemon was restarted.
	if p.ollama != nil {
		p.ollama.ResetCircuitBreaker()
	}

	// Pre-step: detect README changes in already-classified repos.
	if _, err := p.CheckReadmeHashes(ctx); err != nil {
		log.Printf("[classification] WARNING: readme hash check failed: %v", err)
		// Continue with classification even if hash check fails.
	}

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

		// ISI-782: bail early if the Ollama circuit breaker has tripped.
		// Without this we'd burn one short-circuited (sub-millisecond)
		// ClassifySingle call per remaining repo, which is fast but still
		// pollutes the failed counter with what is really one infra outage.
		if p.ollama != nil && p.ollama.CircuitBreakerOpen() {
			log.Printf("[classification] ERROR: classification aborted: ollama unreachable for %d repos in a row",
				p.ollama.BreakerThreshold())
			summary.Duration = time.Since(start)
			return summary, ErrClassificationAbortedOllama
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
			fmt.Fprintf(os.Stderr, " skipped - no README change\n")
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
