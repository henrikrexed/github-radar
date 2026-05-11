package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/hrexed/github-radar/internal/github"
)

// gharchive_pipeline.go implements Story 2 of the Path C epic
// ([ISI-952]): wires the gharchive collector built in [ISI-951] into
// the existing discovery pipeline. The collector itself does the
// heavy lifting (top-N selector, sliding window, activity floor); this
// file is the glue that:
//
//  1. Reads top-N candidates from the collector via TopActiveRepos.
//  2. Dedups against state.Store (already-tracked repos).
//  3. Hydrates each candidate via the core REST API so downstream
//     classifier code sees the same DiscoveredRepo shape it gets
//     from the existing topic/org/language sources.
//  4. Optionally filters by min_stars (off by default — gharchive
//     event volume is the sole signal during initial soak).
//
// The flow runs synchronously inside DiscoverAll. Cross-source dedup
// in DiscoverAll keeps topic/org/language hits ahead of gharchive, so
// a repo already surfaced by a more specific source is not double-
// counted here.

// DiscoverFromGHArchive runs the gharchive event-stream discovery step.
// Returns an empty, non-nil *Result (with Topic="gharchive") and a nil
// error when:
//
//   - Sources.GHArchive.Enabled is false, OR
//   - SetGHArchiveSource has not been called (collector not wired).
//
// In both cases the daemon may have legitimate reasons to skip the
// step — config-disabled or collector still warming — and bubbling an
// error would noisily fail every DiscoverAll cycle. We return a
// zero-value Result rather than nil so callers can rely on a
// non-nil pointer and uniformly read counters/Repos without a nil
// guard ([ISI-965] M1).
//
// AC mapping ([ISI-952]):
//
//   - Top-N + activity floor: TopActiveRepos(TopN, ActivityFloor).
//   - Dedup: state.Store.GetRepoState skip.
//   - Classifier handoff: returns *Result with []DiscoveredRepo —
//     same shape as DiscoverTopic / DiscoverOrg / DiscoverLanguage.
//   - min_stars gate: Sources.GHArchive.MinStarsGate (0 = off).
func (d *Discoverer) DiscoverFromGHArchive(ctx context.Context) (*Result, error) {
	if !d.config.Sources.GHArchive.Enabled || d.ghArchive == nil {
		// M1 ([ISI-965]): non-nil empty Result on disabled/unwired
		// early return so downstream code doesn't need a nil guard.
		return &Result{Topic: "gharchive", Repos: []DiscoveredRepo{}}, nil
	}

	cfg := d.config.Sources.GHArchive
	topN := cfg.TopN
	if topN <= 0 {
		topN = DefaultGHArchiveTopN
	}
	floor := cfg.ActivityFloor
	if floor <= 0 {
		floor = DefaultGHArchiveActivityFloor
	}

	result := &Result{
		Topic:     "gharchive",
		StartTime: time.Now(),
		Repos:     []DiscoveredRepo{},
	}

	d.log("info", "Starting gharchive discovery",
		"top_n", topN, "activity_floor", floor,
		"min_stars_gate", cfg.MinStarsGate,
		"tracked_repos", d.ghArchive.TrackedRepoCount())

	// TopActiveRepos applies the activity floor and N cap. The
	// collector returns repos sorted by total events descending.
	candidates := d.ghArchive.TopActiveRepos(topN, floor)
	result.TotalFound = len(candidates)

	for _, act := range candidates {
		select {
		case <-ctx.Done():
			result.EndTime = time.Now()
			return result, ctx.Err()
		default:
		}

		owner, name, ok := splitRepoName(act.RepoName)
		if !ok {
			// gharchive should always produce "owner/name", but be
			// defensive against malformed entries rather than
			// failing the whole step. M6 ([ISI-965]): log at warn
			// and surface a counter so a sustained feed regression
			// is observable (debug logs are typically dropped in
			// prod).
			result.Malformed++
			d.log("warn", "gharchive: skipping malformed repo name", "name", act.RepoName)
			continue
		}
		fullName := owner + "/" + name

		// Tracked-repo dedup — AC: "Dedup against currently tracked
		// repos (no duplicate ingest)." Stop here before paying the
		// REST hydration cost. M2 ([ISI-965]): keep this early-skip
		// (architect call) — do NOT mirror the topic-source pattern of
		// appending tracked repos to the result. Per
		// [ISI-953#comment-e28b713a] the REST hydration budget is
		// the constraint, and early-skip preserves it.
		if d.store.GetRepoState(fullName) != nil {
			result.AlreadyTracked++
			result.AlreadyTrackedSkipped++
			continue
		}

		// Exclusion list applies the same way as the search-API
		// sources.
		if d.isExcluded(fullName) {
			result.Excluded++
			continue
		}

		// Hydrate via core REST API (separate quota from Search
		// API). Errors here are per-repo and shouldn't fail the
		// whole gharchive step — log and skip the repo.
		metrics, err := d.client.GetRepository(ctx, owner, name)
		if err != nil {
			d.log("debug", "gharchive: hydration failed; skipping",
				"repo", fullName, "error", err)
			continue
		}

		// M5 ([ISI-965]): defensive skip when hydration returns a
		// metrics shape that can't yield a usable identifier. We
		// need at least FullName, or both Owner+Name to reconstruct
		// it (see buildDiscoveredFromGHArchive). Anything else points
		// at a GitHub-side response anomaly we shouldn't propagate
		// downstream — count it as Malformed and move on.
		if metrics.FullName == "" && (metrics.Owner == "" || metrics.Name == "") {
			result.Malformed++
			d.log("warn", "gharchive: skipping repo with unidentifiable metrics",
				"repo", fullName)
			continue
		}

		// min_stars_gate — optional post-hydration star floor, off by
		// default. M4 ([ISI-965]): the original "emergency throttle"
		// framing implied a pre-hydration prefilter that doesn't
		// exist — this check runs AFTER GetRepository so it can't
		// shed REST load. It only filters the post-hydration result
		// set. A true prefilter would need stars cached in
		// state.Store from prior cycles; deferred to a follow-up
		// issue per architect ruling in the parent description.
		if cfg.MinStarsGate > 0 && metrics.Stars < cfg.MinStarsGate {
			continue
		}

		discovered := buildDiscoveredFromGHArchive(metrics, act)

		// MaxAgeDays from the parent Config still applies as a
		// safety net for hobby/inactive repos that bursted once.
		// We deliberately bypass passesFilters' MinStars check —
		// gharchive's raison d'être is to escape the star ceiling
		// — so the per-field checks we actually want are inlined.
		//
		// Skip MaxAgeDays when UpdatedAt is unknown: the gharchive
		// sliding window is itself a recency floor, so "always
		// pass" is the safe degradation path. RepoMetrics doesn't
		// surface UpdatedAt yet (see inline TODO in
		// buildDiscoveredFromGHArchive); when that follow-up lands,
		// the IsZero guard becomes a no-op and the filter activates.
		if d.config.MaxAgeDays > 0 && !discovered.UpdatedAt.IsZero() {
			ageDays := time.Since(discovered.UpdatedAt).Hours() / 24
			if ageDays > float64(d.config.MaxAgeDays) {
				continue
			}
		}

		result.AfterFilters++
		result.NewRepos++
		if discovered.NormalizedScore >= d.config.AutoTrackThreshold {
			discovered.ShouldAutoTrack = true
			result.AutoTracked++
		}
		result.Repos = append(result.Repos, discovered)
	}

	d.normalizeScores(result)
	result.EndTime = time.Now()

	d.log("info", "gharchive discovery complete",
		"found", result.TotalFound,
		"after_filters", result.AfterFilters,
		"new", result.NewRepos,
		"already_tracked", result.AlreadyTracked,
		"already_tracked_skipped", result.AlreadyTrackedSkipped,
		"excluded", result.Excluded,
		"malformed", result.Malformed)

	return result, nil
}

// buildDiscoveredFromGHArchive converts a hydrated REST repo + the
// collector's activity snapshot into a DiscoveredRepo.
//
// Scoring divergence from the search-API path: the existing
// processRepo derives GrowthScore from a stars-velocity heuristic
// (avg stars/day × 7 vs current stars). For gharchive candidates,
// event volume IS the velocity signal — using the heuristic on a
// gharchive-discovered repo would discard the very signal that
// surfaced it. We seed GrowthScore from TotalEvents directly and let
// d.normalizeScores apply the same per-result NormalizedScore pass
// that the other sources use.
//
// CreatedAt/UpdatedAt are unavailable on RepoMetrics today, so age-
// based filtering in DiscoverFromGHArchive is best-effort. Extending
// RepoMetrics to carry timestamps is a small follow-up — tracked
// inline TODO below — but it does not block AC for Story 2.
func buildDiscoveredFromGHArchive(metrics *github.RepoMetrics, act GHArchiveRepoActivity) DiscoveredRepo {
	owner := metrics.Owner
	name := metrics.Name
	fullName := metrics.FullName
	if fullName == "" {
		fullName = owner + "/" + name
	}

	return DiscoveredRepo{
		Owner:       owner,
		Name:        name,
		FullName:    fullName,
		Description: metrics.Description,
		Language:    metrics.Language,
		Topics:      metrics.Topics,
		Stars:       metrics.Stars,
		Forks:       metrics.Forks,
		// TODO(ISI-952 follow-up): RepoMetrics doesn't expose
		// CreatedAt/UpdatedAt yet. Extending the GraphQL/REST
		// fragments to surface them is a tiny change but lives
		// outside Story 2's AC. Leaving zero values until then —
		// MaxAgeDays filter degrades to "always pass" which is
		// safe (gharchive's window is itself a recency floor).
		GrowthScore: float64(act.TotalEvents),
	}
}

// splitRepoName parses a "owner/name" string. Returns ok=false on
// malformed input (zero or multiple slashes, empty halves).
func splitRepoName(s string) (owner, name string, ok bool) {
	idx := strings.IndexByte(s, '/')
	if idx <= 0 || idx == len(s)-1 {
		return "", "", false
	}
	owner, name = s[:idx], s[idx+1:]
	if strings.IndexByte(name, '/') >= 0 {
		return "", "", false
	}
	return owner, name, true
}
