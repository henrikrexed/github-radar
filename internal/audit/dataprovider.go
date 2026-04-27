package audit

import (
	"context"
	"fmt"

	"github.com/hrexed/github-radar/internal/database"
)

// TopicsFetcher abstracts the live topic-fetch call that the audit
// performs per candidate repo. Per ISI-743 decision, topics are not
// persisted in the post-T3 schema (ISI-744 dropped them) — the audit
// fetches them at runtime against the GitHub API.
type TopicsFetcher interface {
	FetchTopics(ctx context.Context, fullName string) ([]string, error)
}

// DBDataProvider is the production DataProvider. It reads the
// `<cat>/other` candidate list from SQLite and live-fetches topics for
// each candidate via the TopicsFetcher.
type DBDataProvider struct {
	DB     *database.DB
	Topics TopicsFetcher

	// IgnoreFetchErrors controls how per-repo topic-fetch failures are
	// handled. true = treat as empty topics (skip from clustering, count
	// in denominator); false = abort the audit run. Default false to
	// surface unexpected GitHub outages — the daemon's existing rate-
	// limit handling means transient failures should be rare.
	IgnoreFetchErrors bool
}

// OtherDriftCandidates implements DataProvider.
func (p *DBDataProvider) OtherDriftCandidates(ctx context.Context) ([]CandidateRepo, error) {
	rows, err := p.DB.AuditOtherDriftCandidates()
	if err != nil {
		return nil, err
	}

	out := make([]CandidateRepo, 0, len(rows))
	for _, r := range rows {
		topics, ferr := p.Topics.FetchTopics(ctx, r.FullName)
		if ferr != nil {
			if !p.IgnoreFetchErrors {
				return nil, fmt.Errorf("fetch topics for %s: %w", r.FullName, ferr)
			}
			topics = nil
		}
		out = append(out, CandidateRepo{
			FullName:        r.FullName,
			PrimaryCategory: r.PrimaryCategory,
			Confidence:      r.Confidence,
			Topics:          topics,
		})
	}
	return out, nil
}

// ActiveNonCuratedCount implements DataProvider.
func (p *DBDataProvider) ActiveNonCuratedCount(ctx context.Context) (int, error) {
	return p.DB.AuditActiveNonCuratedCount()
}
