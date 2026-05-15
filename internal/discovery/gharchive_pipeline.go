package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
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

// GHArchivePipelineHooks is the pipeline-level callback surface for
// observability ([ISI-1005]). Mirrors GHArchiveHooks in design: all
// hooks are optional; nil callbacks are no-ops. Keeping the metric SDK
// out of the discovery package keeps unit tests free of OTel setup.
//
// Hook emission points:
//   - OnCandidateAdmitted fires once per repo that passes the top-N +
//     activity-floor admission step (i.e., each entry from TopActiveRepos).
//     eventType is the dominant event type from the candidate's window
//     aggregate (empty when the aggregate has no per-type breakdown).
//   - OnDedupComplete fires once after the dedup pass completes, carrying
//     the total candidates admitted and how many were dropped as
//     already-tracked.
type GHArchivePipelineHooks struct {
	OnCandidateAdmitted func(eventType string)
	OnDedupComplete     func(total, dropped int)
}

// dominantEventType returns the event type with the highest count in
// perType, or "" when the map is empty or nil. Used to attribute the
// candidates_total counter to the event type that drove the candidate's
// admission.
func dominantEventType(perType map[string]int) string {
	if len(perType) == 0 {
		return ""
	}
	var best string
	var bestN int
	for t, n := range perType {
		if n > bestN || (n == bestN && t < best) {
			best = t
			bestN = n
		}
	}
	return best
}

// DiscoverFromGHArchive runs the gharchive event-stream discovery step.
// Returns nil (with no error) when:
//
//   - Sources.GHArchive.Enabled is false, OR
//   - SetGHArchiveSource has not been called (collector not wired).
//
// In both cases the daemon may have legitimate reasons to skip the
// step — config-disabled or collector still warming — and bubbling an
// error would noisily fail every DiscoverAll cycle.
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
		return nil, nil
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
	// MinStarsCacheTTL governs the per-repo stargazer cache used by
	// the pre-hydration prefilter (ISI-982). Zero falls back to the
	// package default so an operator who only sets MinStarsGate still
	// gets the bandwidth savings without having to learn a second
	// knob.
	cacheTTL := cfg.MinStarsCacheTTL
	if cacheTTL <= 0 {
		cacheTTL = DefaultGHArchiveMinStarsCacheTTL
	}
	now := time.Now()

	result := &Result{
		Topic:     "gharchive",
		StartTime: now,
		Repos:     []DiscoveredRepo{},
	}

	// Backpressure gate ([ISI-954]) — checked before TopActiveRepos so
	// a paused step does no work at all (no aggregate snapshot, no
	// REST hydration). When the gate is unset (older daemon paths,
	// unit tests not exercising backpressure) discovery runs unguarded.
	if d.ghArchiveBackpressure != nil {
		allow, reason, snap := d.ghArchiveBackpressure.CheckEntry()
		if !allow {
			d.log("info", "gharchive discovery paused by backpressure",
				"reason", reason,
				"queue_depth", snap.QueueDepth,
				"rate_limit_pct", snap.RateLimitPct,
				"daily_count", snap.DailyCount)
			result.EndTime = time.Now()
			return result, nil
		}
	}

	d.log("info", "Starting gharchive discovery",
		"top_n", topN, "activity_floor", floor,
		"min_stars_gate", cfg.MinStarsGate,
		"min_stars_cache_ttl", cacheTTL,
		"tracked_repos", d.ghArchive.TrackedRepoCount())

	// TopActiveRepos applies the activity floor and N cap. The
	// collector returns repos sorted by total events descending.
	candidates := d.ghArchive.TopActiveRepos(topN, floor)
	result.TotalFound = len(candidates)

	for _, act := range candidates {
		if d.ghArchivePipelineHooks.OnCandidateAdmitted != nil {
			d.ghArchivePipelineHooks.OnCandidateAdmitted(dominantEventType(act.PerEventType))
		}

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
			// failing the whole step.
			d.log("debug", "gharchive: skipping malformed repo name", "name", act.RepoName)
			continue
		}
		fullName := owner + "/" + name

		// Tracked-repo dedup — AC: "Dedup against currently tracked
		// repos (no duplicate ingest)." Stop here before paying the
		// REST hydration cost.
		if d.store.GetRepoState(fullName) != nil {
			result.AlreadyTracked++
			continue
		}

		// Exclusion list applies the same way as the search-API
		// sources.
		if d.isExcluded(fullName) {
			result.Excluded++
			continue
		}

		// Pre-hydration MinStarsGate prefilter (ISI-982). When the
		// gate is on AND we have a fresh cached observation showing
		// the repo below the gate, skip the REST call entirely.
		// Failing-safe: a missing or stale entry falls through to
		// the hydrate path so a previously-rejected repo that has
		// since gone viral isn't permanently shadowed.
		if cfg.MinStarsGate > 0 {
			if obs, ok := d.store.GetStarObservation(fullName); ok {
				age := now.Sub(obs.ObservedAt)
				if age >= 0 && age < cacheTTL && obs.Stars < cfg.MinStarsGate {
					result.MinStarsPrefiltered++
					d.log("debug", "gharchive: min_stars_gate prefilter hit",
						"repo", fullName,
						"cached_stars", obs.Stars,
						"gate", cfg.MinStarsGate,
						"age", age)
					continue
				}
			}
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

		// Cache the freshly-observed stargazer count so the next
		// cycle's prefilter can skip this repo when MinStarsGate is
		// in effect. Written unconditionally — even when the gate is
		// off — so flipping the gate later doesn't require a cold
		// cycle to start saving REST calls. Also written for repos
		// that the gate is about to reject, which is the whole point
		// of the cache.
		d.store.SetStarObservation(fullName, state.StarObservation{
			Stars:      metrics.Stars,
			ObservedAt: now,
		})

		// min_stars_gate — post-hydration enforcement. On cycle 2+
		// the prefilter above shoulders most of this work; this
		// branch handles cold-cache / stale-cache fallthroughs and
		// the always-on post-filter semantics.
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

		// Daily-cap counter ([ISI-954]). Counted after the candidate
		// is committed to the result so the metric tracks "candidates
		// shipped to the classifier" rather than "candidates the
		// gharchive aggregate proposed". A hard-cap trip mid-cycle
		// breaks the loop; the candidate that pushed us over is kept
		// (RecordEmission already counted it) so accounting stays
		// consistent on resume.
		if d.ghArchiveBackpressure != nil {
			if allow, reason := d.ghArchiveBackpressure.RecordEmission(); !allow {
				d.log("info", "gharchive discovery hit daily cap mid-cycle",
					"reason", reason,
					"emitted_this_cycle", result.NewRepos)
				break
			}
		}
	}

	d.normalizeScores(result)
	result.EndTime = time.Now()

	if d.ghArchivePipelineHooks.OnDedupComplete != nil && result.TotalFound > 0 {
		d.ghArchivePipelineHooks.OnDedupComplete(result.TotalFound, result.AlreadyTracked)
	}

	d.log("info", "gharchive discovery complete",
		"found", result.TotalFound,
		"after_filters", result.AfterFilters,
		"new", result.NewRepos,
		"already_tracked", result.AlreadyTracked,
		"excluded", result.Excluded,
		"min_stars_prefiltered", result.MinStarsPrefiltered)

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
