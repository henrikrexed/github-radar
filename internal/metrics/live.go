package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/scoring"
	"github.com/hrexed/github-radar/internal/state"
)

type LiveAPICollector struct {
	client     *github.Client
	collector  *github.Collector
	calculator *scoring.Calculator
	store      *state.Store
}

func NewLiveAPICollector(client *github.Client, store *state.Store, weights scoring.Weights) *LiveAPICollector {
	return &LiveAPICollector{
		client:     client,
		collector:  github.NewCollector(client),
		calculator: scoring.NewCalculator(weights),
		store:      store,
	}
}

func (l *LiveAPICollector) Collect(ctx context.Context, repos []RepoRef, window time.Duration) ([]CollectedMetrics, error) {
	results := make([]CollectedMetrics, 0, len(repos))
	var failures []string

	for _, ref := range repos {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		fullName := fmt.Sprintf("%s/%s", ref.Owner, ref.Name)
		prevState := l.store.GetRepoState(fullName)

		var cond *github.ConditionalInfo
		if prevState != nil {
			cond = &github.ConditionalInfo{
				ETag:         prevState.ETag,
				LastModified: prevState.LastModified,
			}
		}

		collResult := l.collector.CollectRepoConditional(ctx, ref.Owner, ref.Name, cond)
		if collResult.Error != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", fullName, collResult.Error))
			continue
		}

		if collResult.Skipped {
			if prevState != nil {
				results = append(results, stateToCollected(*prevState, ref))
			}
			continue
		}

		m := CollectedMetrics{
			Owner:       ref.Owner,
			Name:        ref.Name,
			CollectedAt: collResult.Collected,
		}

		if collResult.Metrics != nil {
			m.Stars = collResult.Metrics.Stars
			m.Forks = collResult.Metrics.Forks
			m.OpenIssues = collResult.Metrics.OpenIssues
			m.OpenPRs = collResult.Metrics.OpenPRs
		}

		if collResult.Activity != nil {
			m.Contributors = collResult.Activity.Contributors
			m.MergedPRs7d = collResult.Activity.MergedPRs7d
			m.NewIssues7d = collResult.Activity.NewIssues7d
			if collResult.Activity.LatestRelease != nil {
				m.LatestReleaseAt = collResult.Activity.LatestRelease.PublishedAt
			}
		}

		if prevState != nil && len(prevState.RecentReleaseDates) > 0 {
			m.ReleaseDates = append(m.ReleaseDates, prevState.RecentReleaseDates...)
		}
		if !m.LatestReleaseAt.IsZero() {
			if len(m.ReleaseDates) == 0 || !m.ReleaseDates[0].Equal(m.LatestReleaseAt) {
				m.ReleaseDates = append([]time.Time{m.LatestReleaseAt}, m.ReleaseDates...)
				if len(m.ReleaseDates) > 10 {
					m.ReleaseDates = m.ReleaseDates[:10]
				}
			}
		}

		if prevState != nil && !prevState.LastCollected.IsZero() {
			sm := scoring.RepoMetrics{
				Stars:              m.Stars,
				Forks:              m.Forks,
				Contributors:       m.Contributors,
				MergedPRs7d:        m.MergedPRs7d,
				NewIssues7d:        m.NewIssues7d,
				RecentReleaseDates: m.ReleaseDates,
				StarsPrev:          prevState.Stars,
				ForksPrev:          prevState.Forks,
				ContributorsPrev:   prevState.Contributors,
				DaysElapsed:        m.CollectedAt.Sub(prevState.LastCollected).Hours() / 24,
				PrevStarVelocity:   prevState.StarVelocity,
				Now:                m.CollectedAt,
			}
			vels := l.calculator.CalculateVelocities(sm)
			m.StarVelocity = vels.StarVelocity
			m.StarAcceleration = vels.StarAcceleration
			m.ForkVelocity = vels.ForkVelocity
			m.ReleaseCadence = vels.ReleaseCadence
			m.PRVelocity = vels.PRVelocity
			m.IssueVelocity = vels.IssueVelocity
			m.ContributorGrowth = vels.ContributorGrowth
			m.GrowthScore = l.calculator.CalculateRawScore(vels)
		}

		results = append(results, m)
	}

	if len(failures) > 0 && len(results) == 0 {
		return nil, fmt.Errorf("all repos failed: %v", failures)
	}

	return results, nil
}

func (l *LiveAPICollector) Client() *github.Client {
	return l.client
}

func stateToCollected(s state.RepoState, ref RepoRef) CollectedMetrics {
	return CollectedMetrics{
		Owner:             ref.Owner,
		Name:              ref.Name,
		Stars:             s.Stars,
		Forks:             s.Forks,
		Contributors:      s.Contributors,
		MergedPRs7d:       s.MergedPRs7d,
		NewIssues7d:       s.NewIssues7d,
		ReleaseDates:      s.RecentReleaseDates,
		CollectedAt:       s.LastCollected,
		StarVelocity:      s.StarVelocity,
		StarAcceleration:  s.StarAcceleration,
		ForkVelocity:      s.ForkVelocity,
		ReleaseCadence:    s.ReleaseCadence,
		PRVelocity:        s.PRVelocity,
		IssueVelocity:     s.IssueVelocity,
		ContributorGrowth: s.ContributorGrowth,
		GrowthScore:       s.GrowthScore,
	}
}
