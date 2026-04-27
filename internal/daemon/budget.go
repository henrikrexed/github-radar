package daemon

import (
	"context"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/metrics"
)

// apiObserver bridges github.Client telemetry events to the metrics
// exporter. It is installed via Client.SetAPIObserver when the metrics
// exporter is active.
//
// The exporter is allowed to be nil (dry-run mode): the bridge is a
// no-op in that case so the GitHub client does not need conditional
// logic at the call site.
type apiObserver struct {
	ctx      context.Context
	exporter *metrics.Exporter
}

func newAPIObserver(ctx context.Context, exp *metrics.Exporter) *apiObserver {
	return &apiObserver{ctx: ctx, exporter: exp}
}

// ObserveCall emits a counter increment for this HTTP round-trip.
func (o *apiObserver) ObserveCall(resource, result string) {
	if o == nil || o.exporter == nil {
		return
	}
	o.exporter.RecordAPICall(o.ctx, resource, result)
}

// ObserveRateLimit emits the rate-limit gauge set. GitHub sends these
// headers on every response so this gets called per-request.
func (o *apiObserver) ObserveRateLimit(limit, remaining int, resetAt time.Time) {
	if o == nil || o.exporter == nil {
		return
	}
	o.exporter.RecordRateLimit(o.ctx, metrics.RateLimitSnapshot{
		Limit:     limit,
		Remaining: remaining,
		ResetAt:   resetAt,
	})
}

// tierConfigFromYAML maps the YAML-surface TieringConfig into the
// refresh-tier classifier config, filling zero values from
// github.DefaultTierConfig.
func tierConfigFromYAML(t config.TieringConfig) github.TierConfig {
	cfg := github.DefaultTierConfig()
	if t.HotN > 0 {
		cfg.HotN = t.HotN
	}
	if t.WarmN > 0 {
		cfg.WarmN = t.WarmN
	}
	if t.NewRepoWindowHours > 0 {
		cfg.NewRepoWindow = time.Duration(t.NewRepoWindowHours) * time.Hour
	}
	if t.HotIntervalMin > 0 {
		cfg.HotInterval = time.Duration(t.HotIntervalMin) * time.Minute
	}
	if t.WarmIntervalMin > 0 {
		cfg.WarmInterval = time.Duration(t.WarmIntervalMin) * time.Minute
	}
	if t.ColdIntervalMin > 0 {
		cfg.ColdInterval = time.Duration(t.ColdIntervalMin) * time.Minute
	}
	if t.NewIntervalMin > 0 {
		cfg.NewInterval = time.Duration(t.NewIntervalMin) * time.Minute
	}
	return cfg
}

// buildTierCandidates extracts the minimum per-repo inputs the tier
// classifier needs, merging database "first_seen_at" (authoritative
// for new-repo promotion) with the live in-memory store.
func (d *Daemon) buildTierCandidates() []github.TierCandidate {
	all := d.store.AllRepoStates()
	out := make([]github.TierCandidate, 0, len(all))

	for fullName, rs := range all {
		cand := github.TierCandidate{
			FullName:        fullName,
			GrowthScore:     rs.GrowthScore,
			LastCollectedAt: rs.LastCollected,
		}
		if d.db != nil {
			if rec, err := d.db.GetRepo(fullName); err == nil && rec != nil {
				if rec.FirstSeenAt != "" {
					if ts, err := time.Parse(time.RFC3339, rec.FirstSeenAt); err == nil {
						cand.FirstSeenAt = ts
					}
				}
			}
		}
		out = append(out, cand)
	}
	return out
}

// publishTierHistogram emits the OTel gauge `github.api.refresh_tier.repos`
// per tier bucket so dashboards can visualise where repos sit.
func (d *Daemon) publishTierHistogram(hist github.TierHistogram) {
	if d.exporter == nil {
		return
	}
	d.exporter.RecordRefreshTierHistogram(d.ctx, map[string]int{
		"hot":  hist.Hot,
		"warm": hist.Warm,
		"cold": hist.Cold,
		"new":  hist.New,
	})
}

// publishRateLimit emits a fresh rate-limit snapshot even when no API
// calls are in flight. Useful after idle ticker wakeups so dashboards
// keep a current value.
func (d *Daemon) publishRateLimit() {
	if d.exporter == nil || d.client == nil {
		return
	}
	rl := d.client.RateLimitInfo()
	d.exporter.RecordRateLimit(d.ctx, metrics.RateLimitSnapshot{
		Limit:     rl.Limit,
		Remaining: rl.Remaining,
		ResetAt:   rl.Reset,
	})
}
