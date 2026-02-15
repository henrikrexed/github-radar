# Story 5.3: Export Repository Metrics

Status: done

## Story

As the **system**,
I want to export all collected repo metrics as OTel gauges,
So that they appear in the observability backend.

## Acceptance Criteria

1. **Given** collected metrics for each repository
   **When** metrics are recorded
   **Then** all metrics use `github.repo.*` namespace

2. **Given** metrics are recorded
   **When** exported
   **Then** each metric includes dimensions: repo_owner, repo_name, category, language (FR41)

3. **Given** collected data
   **When** metrics are recorded
   **Then** metrics include: stars, forks, issues, prs, growth_score, velocities

## Tasks / Subtasks

- [x] Task 1: Create gauge instruments for all metrics
- [x] Task 2: Implement RecordRepoMetrics method
- [x] Task 3: Add common attributes (owner, name, language, category)
- [x] Task 4: Write tests

## Completion Notes

Metrics exported:
- github.repo.stars
- github.repo.forks
- github.repo.open_issues
- github.repo.open_prs
- github.repo.contributors
- github.repo.growth_score
- github.repo.growth_score_normalized
- github.repo.star_velocity
- github.repo.star_acceleration
- github.repo.pr_velocity
- github.repo.issue_velocity
- github.repo.contributor_growth

## Dev Agent Record

### Agent Model Used

Claude Opus 4.5

### File List

- internal/metrics/exporter.go (RepoMetrics, RecordRepoMetrics)
- internal/metrics/exporter_test.go
