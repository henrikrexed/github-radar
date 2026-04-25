package github

import (
	"sort"
	"time"
)

// RefreshTier classifies a repository by how often it should be polled.
// New repos (just discovered) are always promoted regardless of rank so
// their growth signal can be established quickly.
type RefreshTier int

const (
	TierNew  RefreshTier = iota // first 48h after discovery
	TierHot                     // top N by growth_score
	TierWarm                    // next M
	TierCold                    // tail
)

// String returns a stable label used as the OTel attribute value.
func (t RefreshTier) String() string {
	switch t {
	case TierNew:
		return "new"
	case TierHot:
		return "hot"
	case TierWarm:
		return "warm"
	case TierCold:
		return "cold"
	default:
		return "unknown"
	}
}

// DefaultTierConfig returns the cadence tiering specified in the
// [ISI-709 plan](§5 API budget): top 500 @ 1h, next 1500 @ 4h, tail @
// 12h, new repos (<48h) @ 1h regardless of rank.
func DefaultTierConfig() TierConfig {
	return TierConfig{
		HotN:          500,
		WarmN:         1500,
		NewRepoWindow: 48 * time.Hour,
		HotInterval:   1 * time.Hour,
		WarmInterval:  4 * time.Hour,
		ColdInterval:  12 * time.Hour,
		NewInterval:   1 * time.Hour,
	}
}

// TierConfig parameterises the tiering algorithm.
type TierConfig struct {
	// HotN is how many top-ranked repos receive the hot interval.
	HotN int
	// WarmN is how many after the hot tier receive the warm interval.
	WarmN int
	// NewRepoWindow is how long a repo stays in TierNew after first_seen_at.
	NewRepoWindow time.Duration

	HotInterval  time.Duration
	WarmInterval time.Duration
	ColdInterval time.Duration
	NewInterval  time.Duration
}

// TierCandidate is the minimum data the classifier needs from a repo.
// We accept this narrow view so the tier package does not depend on
// `database` (which would cause an import cycle).
type TierCandidate struct {
	FullName        string
	GrowthScore     float64
	FirstSeenAt     time.Time
	LastCollectedAt time.Time
}

// TierAssignment is the output of ClassifyTier for one repo.
type TierAssignment struct {
	FullName  string
	Tier      RefreshTier
	Interval  time.Duration
	DueAt     time.Time // LastCollectedAt + Interval (or zero-time if never collected)
	IsDue     bool      // DueAt <= now
}

// ClassifyAll assigns every candidate to a tier using the configured
// rank thresholds and returns the list sorted hot→warm→cold→new.
// Candidates are ranked by GrowthScore descending; ties are broken by
// FullName for stability.
func ClassifyAll(candidates []TierCandidate, now time.Time, cfg TierConfig) []TierAssignment {
	ranked := make([]TierCandidate, len(candidates))
	copy(ranked, candidates)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].GrowthScore != ranked[j].GrowthScore {
			return ranked[i].GrowthScore > ranked[j].GrowthScore
		}
		return ranked[i].FullName < ranked[j].FullName
	})

	out := make([]TierAssignment, len(ranked))
	for i, c := range ranked {
		tier := tierForRank(i, cfg)

		// New-repo promotion overrides the rank-based tier.
		if !c.FirstSeenAt.IsZero() && now.Sub(c.FirstSeenAt) < cfg.NewRepoWindow {
			tier = TierNew
		}

		interval := intervalFor(tier, cfg)
		var due time.Time
		if !c.LastCollectedAt.IsZero() {
			due = c.LastCollectedAt.Add(interval)
		}

		out[i] = TierAssignment{
			FullName: c.FullName,
			Tier:     tier,
			Interval: interval,
			DueAt:    due,
			IsDue:    c.LastCollectedAt.IsZero() || !due.After(now),
		}
	}

	return out
}

// DueRepos filters ClassifyAll output to repos that should be polled now.
// The result is ordered hot → warm → cold → new (new last so the batch
// builder keeps recent discoveries contiguous for the fast tier).
func DueRepos(assignments []TierAssignment) []TierAssignment {
	due := make([]TierAssignment, 0, len(assignments))
	for _, a := range assignments {
		if a.IsDue {
			due = append(due, a)
		}
	}
	sort.SliceStable(due, func(i, j int) bool {
		return due[i].Tier < due[j].Tier
	})
	return due
}

// TierHistogram counts assignments per tier.
type TierHistogram struct {
	New  int
	Hot  int
	Warm int
	Cold int
}

// Count returns how many repos are in each tier bucket.
func Count(assignments []TierAssignment) TierHistogram {
	var h TierHistogram
	for _, a := range assignments {
		switch a.Tier {
		case TierNew:
			h.New++
		case TierHot:
			h.Hot++
		case TierWarm:
			h.Warm++
		case TierCold:
			h.Cold++
		}
	}
	return h
}

func tierForRank(rank int, cfg TierConfig) RefreshTier {
	switch {
	case rank < cfg.HotN:
		return TierHot
	case rank < cfg.HotN+cfg.WarmN:
		return TierWarm
	default:
		return TierCold
	}
}

func intervalFor(tier RefreshTier, cfg TierConfig) time.Duration {
	switch tier {
	case TierNew:
		return cfg.NewInterval
	case TierHot:
		return cfg.HotInterval
	case TierWarm:
		return cfg.WarmInterval
	case TierCold:
		return cfg.ColdInterval
	default:
		return cfg.ColdInterval
	}
}
