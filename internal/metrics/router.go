package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/logging"
	"github.com/hrexed/github-radar/internal/scoring"
	"github.com/hrexed/github-radar/internal/state"
)

type Router struct {
	live      *LiveAPICollector
	gharchive *HourlyArchiveCollector
	client    *github.Client
	store     *state.Store
	enabled   bool
	threshold float64
	exporter  *Exporter
}

type RouterConfig struct {
	GHArchiveEnabled     bool
	GHArchiveBaseURL     string
	GHArchiveTimeout     time.Duration
	FallbackThresholdPct float64
}

func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		GHArchiveEnabled:     false,
		GHArchiveBaseURL:     "https://data.gharchive.org",
		GHArchiveTimeout:     60 * time.Second,
		FallbackThresholdPct: 0.25,
	}
}

func NewRouter(client *github.Client, store *state.Store, weights scoring.Weights, cfg RouterConfig, exporter *Exporter) *Router {
	r := &Router{
		live:      NewLiveAPICollector(client, store, weights),
		client:    client,
		store:     store,
		enabled:   cfg.GHArchiveEnabled,
		threshold: cfg.FallbackThresholdPct,
		exporter:  exporter,
	}

	if r.threshold <= 0 || r.threshold >= 1 {
		r.threshold = 0.25
	}

	if r.enabled {
		r.gharchive = NewHourlyArchiveCollector(cfg.GHArchiveBaseURL, cfg.GHArchiveTimeout, exporter)
		logging.Info("collector router: gharchive fallback enabled",
			"threshold_pct", r.threshold,
			"base_url", cfg.GHArchiveBaseURL)
	}

	return r
}

func (r *Router) shouldFallback() bool {
	if !r.enabled {
		return false
	}
	if r.client == nil {
		return false
	}

	rl := r.client.RateLimitInfo()
	if rl.Limit == 0 {
		return false
	}

	headroom := float64(rl.Remaining) / float64(rl.Limit)
	return headroom < r.threshold
}

func (r *Router) Collect(ctx context.Context, repos []RepoRef, window time.Duration) ([]CollectedMetrics, error) {
	useFallback := r.shouldFallback()

	if r.exporter != nil {
		backend := "live"
		if useFallback {
			backend = "gharchive"
		}
		r.exporter.RecordCollectorActive(ctx, backend)
	}

	if useFallback {
		logging.Info("collector router: falling back to gharchive",
			"rate_limit_remaining", r.client.RateLimitInfo().Remaining,
			"rate_limit_limit", r.client.RateLimitInfo().Limit)

		if r.exporter != nil {
			r.exporter.RecordFallbackTrip(ctx)
		}

		return r.gharchive.Collect(ctx, repos, window)
	}

	return r.live.Collect(ctx, repos, window)
}

func (r *Router) LiveCollector() *LiveAPICollector {
	return r.live
}

func (r *Router) IsFallbackEnabled() bool {
	return r.enabled
}

// IsFallbackActive reports whether the router is currently routing to the
// gharchive backend (i.e. API headroom is below the threshold). Returns false
// when gharchive is disabled or when there is sufficient rate-limit headroom.
func (r *Router) IsFallbackActive() bool {
	return r.shouldFallback()
}

func (r *Router) Status() string {
	if !r.enabled {
		return "live_only"
	}
	if r.shouldFallback() {
		return "gharchive_active"
	}
	return "live_active"
}

func BuildRepoRefs(repos []struct{ Owner, Name string }) []RepoRef {
	refs := make([]RepoRef, len(repos))
	for i, r := range repos {
		refs[i] = RepoRef{Owner: r.Owner, Name: r.Name}
	}
	return refs
}

func BuildRepoRefsFromConfig(trackedRepos []struct {
	Repo string
}) []RepoRef {
	refs := make([]RepoRef, 0, len(trackedRepos))
	for _, t := range trackedRepos {
		parts := splitRepo(t.Repo)
		if len(parts) == 2 {
			refs = append(refs, RepoRef{Owner: parts[0], Name: parts[1]})
		}
	}
	return refs
}

func splitRepo(fullName string) []string {
	for i := 0; i < len(fullName); i++ {
		if fullName[i] == '/' {
			return []string{fullName[:i], fullName[i+1:]}
		}
	}
	return nil
}

func UpdateStoreFromCollected(store *state.Store, results []CollectedMetrics, prevStates func(string) *state.RepoState) {
	for _, m := range results {
		fullName := m.Owner + "/" + m.Name
		prev := prevStates(fullName)

		if m.Partial && prev != nil {
			// (ISI-922) Partial collectors (e.g. gharchive fallback) produce
			// delta/derived metrics only.  Preserve the existing absolute
			// values so that dashboards do not drop to near-zero.
			newState := state.RepoState{
				Owner:                 m.Owner,
				Name:                  m.Name,
				Stars:                 prev.Stars,
				Forks:                 prev.Forks,
				Contributors:          prev.Contributors,
				MergedPRs7d:           prev.MergedPRs7d,
				NewIssues7d:           prev.NewIssues7d,
				LastCollected:         m.CollectedAt,
				StarVelocity:          m.StarVelocity,
				StarAcceleration:      prev.StarAcceleration,
				ForkVelocity:          m.ForkVelocity,
				ReleaseCadence:        prev.ReleaseCadence,
				PRVelocity:            m.PRVelocity,
				IssueVelocity:         m.IssueVelocity,
				ContributorGrowth:     prev.ContributorGrowth,
				GrowthScore:           prev.GrowthScore,
				NormalizedGrowthScore: prev.NormalizedGrowthScore,
				RecentReleaseDates:    prev.RecentReleaseDates,
				ETag:                  prev.ETag,
				LastModified:          prev.LastModified,
				StarsPrev:             prev.StarsPrev,
				ForksPrev:             prev.ForksPrev,
				ContributorsPrev:      prev.ContributorsPrev,
				LatestReleaseAt:       prev.LatestReleaseAt,
			}
			if len(m.ReleaseDates) > 0 {
				newState.RecentReleaseDates = m.ReleaseDates
			}
			if !m.LatestReleaseAt.IsZero() {
				newState.LatestReleaseAt = m.LatestReleaseAt
			}
			store.SetRepoState(fullName, newState)
			continue
		}

		newState := state.RepoState{
			Owner:              m.Owner,
			Name:               m.Name,
			Stars:              m.Stars,
			Forks:              m.Forks,
			Contributors:       m.Contributors,
			MergedPRs7d:        m.MergedPRs7d,
			NewIssues7d:        m.NewIssues7d,
			LastCollected:      m.CollectedAt,
			StarVelocity:       m.StarVelocity,
			StarAcceleration:   m.StarAcceleration,
			ForkVelocity:       m.ForkVelocity,
			ReleaseCadence:     m.ReleaseCadence,
			PRVelocity:         m.PRVelocity,
			IssueVelocity:      m.IssueVelocity,
			ContributorGrowth:  m.ContributorGrowth,
			GrowthScore:        m.GrowthScore,
			RecentReleaseDates: m.ReleaseDates,
		}

		if prev != nil {
			newState.ETag = prev.ETag
			newState.LastModified = prev.LastModified
			newState.StarsPrev = prev.Stars
			newState.ForksPrev = prev.Forks
			newState.ContributorsPrev = prev.Contributors
			if !prev.LatestReleaseAt.IsZero() {
				newState.LatestReleaseAt = prev.LatestReleaseAt
			}
			if len(prev.RecentReleaseDates) > 0 && len(newState.RecentReleaseDates) == 0 {
				newState.RecentReleaseDates = prev.RecentReleaseDates
			}
		}

		if !m.LatestReleaseAt.IsZero() {
			newState.LatestReleaseAt = m.LatestReleaseAt
		}

		store.SetRepoState(fullName, newState)
	}
}

func FormatRouterStatus(r *Router) string {
	return fmt.Sprintf("collector_router: backend=%s gharchive_enabled=%v",
		r.Status(), r.IsFallbackEnabled())
}
