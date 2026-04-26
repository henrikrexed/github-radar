package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// AggregateShareThreshold is the strict-`>` 3.0% gate from plan §7. At
// exactly 3.0% the run does NOT escalate; > 3.0% triggers `priority=critical`.
const AggregateShareThreshold = 3.0

// CandidateRepo is the audit-time view of a `<cat>/other` row. Topics are
// live-fetched per ISI-743 (T3 dropped them from the persistence layer).
type CandidateRepo struct {
	FullName        string
	PrimaryCategory string
	Topics          []string
	Confidence      float64
}

// DataProvider is the audit's read port. The production implementation
// wraps internal/database.*DB; tests provide an in-memory stub. Defining
// a port here lets the audit unit tests run without seeding SQLite.
type DataProvider interface {
	// OtherDriftCandidates returns rows where `primary_subcategory='other'`,
	// `is_curated_list=0`, and `status='active'`. Topics MUST already be
	// populated (the production impl wraps the DB query and a topic
	// live-fetch via internal/github).
	OtherDriftCandidates(ctx context.Context) ([]CandidateRepo, error)

	// ActiveNonCuratedCount is the denominator from plan §7: count of
	// rows with `is_curated_list=0 AND status='active'`. Curated lists
	// and inactive repos are excluded from BOTH numerator and denominator
	// (I4).
	ActiveNonCuratedCount(ctx context.Context) (int, error)
}

// Mode controls auto-file behavior. DryRun renders a report and stdout
// only; File renders + persists + auto-files to Paperclip.
type Mode int

const (
	ModeDryRun Mode = iota
	ModeFile
)

// AuditOutput is the result of one Run() invocation.
type AuditOutput struct {
	Report             Report
	Filed              []FiledIssue
	WatchOnly          []ClusterEntry
	Escalated          bool   // aggregate share > 3.0%
	AggregateSharePct  float64
}

// FiledIssue records a successful auto-file POST.
type FiledIssue struct {
	Cluster      Cluster
	Identifier   string // e.g. "ISI-790"
	Subcategory  string // proposed subcat slug
}

// ClusterEntry is the watch-list shape: cluster + reason it didn't auto-file.
// Reason is one of: "size_below_threshold" (never happens — clusters
// emitted by ClusterRepos already meet size), "score_below_threshold",
// "avg_confidence_below_threshold", "deduped_within_60d", "auto_file_failed".
type ClusterEntry struct {
	Cluster     Cluster
	Subcategory string
	Reason      string
	Detail      string // human-readable reason (for the watch-list table)
}

// Auditor wires the orchestrator together. Construct with NewAuditor.
type Auditor struct {
	Data       DataProvider
	Filer      Filer
	Now        func() time.Time
	ReportsDir string // where to persist YYYY-MM.md (empty = don't persist)
	Logger     StructuredLogger
}

// NewAuditor returns an Auditor with sensible defaults.
func NewAuditor(d DataProvider, f Filer, reportsDir string) *Auditor {
	return &Auditor{
		Data:       d,
		Filer:      f,
		Now:        time.Now,
		ReportsDir: reportsDir,
	}
}

func (a *Auditor) log() StructuredLogger {
	if a.Logger == nil {
		return noopLogger{}
	}
	return a.Logger
}

// Run executes the audit per plan §3–§7. Returns the rendered report and
// the auto-file/watch outcomes. In ModeDryRun the report is rendered but
// NOT persisted and NO Paperclip POSTs are made.
//
// Errors returned here are programmer/data-source errors (DB failure).
// Per plan §6.1, Paperclip API failures are degraded to watch-list
// entries — the audit run still returns successfully.
func (a *Auditor) Run(ctx context.Context, mode Mode, parentIssueID string) (*AuditOutput, error) {
	candidates, err := a.Data.OtherDriftCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	denom, err := a.Data.ActiveNonCuratedCount(ctx)
	if err != nil {
		return nil, fmt.Errorf("query denominator: %w", err)
	}

	// Aggregate share computation (plan §7) — numerator is the candidate
	// count (post primary_subcategory='other' / is_curated_list=0 /
	// status='active' filter); denominator is active+non-curated rows.
	aggSharePct := 0.0
	if denom > 0 {
		aggSharePct = float64(len(candidates)) * 100.0 / float64(denom)
	}
	escalated := aggSharePct > AggregateShareThreshold // strict >, plan §7 boundary

	// Cluster the candidates. Empty-topics repos are auto-excluded by
	// ClusterRepos (§3 fallback) but still counted in `candidates` length
	// for the aggregate share above (I4 + U1).
	repos := make([]Repo, 0, len(candidates))
	for _, c := range candidates {
		repos = append(repos, Repo{
			FullName: c.FullName, PrimaryCategory: c.PrimaryCategory,
			Topics: c.Topics, Confidence: c.Confidence,
		})
	}
	clusters := ClusterRepos(repos)

	// Per-category breakdown for the report (counts of `<cat>/other` per
	// category, plus the count of clusters ≥ MinClusterSize).
	perCategory := buildPerCategory(candidates, clusters)

	out := &AuditOutput{
		AggregateSharePct: aggSharePct,
		Escalated:         escalated,
	}

	for _, c := range clusters {
		subcat := proposeSubcategoryName(c)
		category := dominantCategory(c)
		draft := GraduationDraft{
			Category:       category,
			ProposedSubcat: subcat,
			Cluster:        c,
			AggregateShare: aggSharePct,
			ParentIssueID:  parentIssueID,
		}

		// Auto-file qualification (plan §3 step 5 + §6).
		if !c.QualifiesForAutoFile() {
			out.WatchOnly = append(out.WatchOnly, ClusterEntry{
				Cluster:     c,
				Subcategory: subcat,
				Reason:      reasonForNoAutoFile(c),
				Detail:      detailForNoAutoFile(c),
			})
			continue
		}

		// Dry-run: don't dedup, don't post — surface as "would-file".
		if mode == ModeDryRun {
			out.Filed = append(out.Filed, FiledIssue{Cluster: c, Identifier: "(dry-run)", Subcategory: subcat})
			continue
		}

		// Dedup before posting (plan §6).
		if a.Filer != nil {
			already, derr := a.Filer.AlreadyFiledRecently(ctx, draft.Title())
			if derr != nil {
				a.log().Warn("dedup search failed; proceeding with file attempt", "err", derr.Error())
				// Don't suppress the file on dedup-search failure — the
				// downstream API would 409 if dup, which we surface as a
				// watch entry per §6.1.
			} else if already {
				out.WatchOnly = append(out.WatchOnly, ClusterEntry{
					Cluster:     c,
					Subcategory: subcat,
					Reason:      "deduped_within_60d",
					Detail:      fmt.Sprintf("auto-file SKIPPED (already filed within %d days)", int(DedupWindow/(24*time.Hour))),
				})
				continue
			}
		}

		// File. On persistent failure, plan §6.1: degrade to watch list,
		// continue the run, exit 0.
		if a.Filer != nil {
			id, ferr := a.Filer.File(ctx, draft)
			if ferr != nil {
				a.log().Error("audit.autofile.failed",
					"cluster", strings.Join(c.Tokens, ","),
					"category", category,
					"err", ferr.Error())
				out.WatchOnly = append(out.WatchOnly, ClusterEntry{
					Cluster:     c,
					Subcategory: subcat,
					Reason:      "auto_file_failed",
					Detail:      fmt.Sprintf("auto-file FAILED (%s); file manually.", ferr.Error()),
				})
				continue
			}
			out.Filed = append(out.Filed, FiledIssue{Cluster: c, Identifier: id, Subcategory: subcat})
		}
	}

	// Render report.
	out.Report = renderReport(reportInputs{
		Now:               a.Now(),
		CorpusSize:        denom,
		OtherCount:        len(candidates),
		AggregateSharePct: aggSharePct,
		Escalated:         escalated,
		PerCategory:       perCategory,
		Filed:             out.Filed,
		WatchOnly:         out.WatchOnly,
	})

	if mode == ModeFile && a.ReportsDir != "" {
		if err := persistReport(a.ReportsDir, a.Now(), out.Report); err != nil {
			a.log().Warn("audit.report.persist_failed", "err", err.Error())
		}
	}

	return out, nil
}

// proposeSubcategoryName turns the cluster tokens into a proposed
// subcategory slug. Joins all tokens with `-` and lowercases.
func proposeSubcategoryName(c Cluster) string {
	if len(c.Tokens) == 0 {
		return "unknown"
	}
	parts := make([]string, 0, len(c.Tokens))
	for _, t := range c.Tokens {
		parts = append(parts, strings.ReplaceAll(strings.ToLower(strings.TrimSpace(t)), " ", "-"))
	}
	return strings.Join(parts, "-")
}

// dominantCategory returns the most common primary_category in the cluster.
// In practice all repos in a `<cat>/other` cluster share the same primary
// category, but this is robust to mixed input.
func dominantCategory(c Cluster) string {
	counts := map[string]int{}
	for _, r := range c.Repos {
		counts[r.PrimaryCategory]++
	}
	best, bestN := "", -1
	for k, n := range counts {
		if n > bestN || (n == bestN && k < best) {
			best, bestN = k, n
		}
	}
	if best == "" {
		return "unknown"
	}
	return best
}

func reasonForNoAutoFile(c Cluster) string {
	switch {
	case len(c.Repos) < MinClusterSize:
		return "size_below_threshold"
	case c.AvgConfidence() < MinAvgConfidence:
		return "avg_confidence_below_threshold"
	case c.Score() < MinClusterScore:
		return "score_below_threshold"
	}
	return "qualifies"
}

func detailForNoAutoFile(c Cluster) string {
	switch {
	case len(c.Repos) < MinClusterSize:
		return fmt.Sprintf("%d repos — too small to graduate, monitor next month.", len(c.Repos))
	case c.AvgConfidence() < MinAvgConfidence:
		return fmt.Sprintf("%d repos but avg_confidence %.2f < %.2f — too uncertain to graduate.", len(c.Repos), c.AvgConfidence(), MinAvgConfidence)
	case c.Score() < MinClusterScore:
		return fmt.Sprintf("%d repos, score %.2f < %.2f — surface only.", len(c.Repos), c.Score(), MinClusterScore)
	}
	return ""
}

// PerCategoryRow is one row of the report's per-category breakdown.
type PerCategoryRow struct {
	Category    string
	OtherCount  int     // count of <cat>/other candidates in this category
	PercentOf   float64 // (OtherCount / total in this category) * 100; left blank if denominator unknown
	NumClusters int     // count of clusters in this category that meet MinClusterSize
}

func buildPerCategory(candidates []CandidateRepo, clusters []Cluster) []PerCategoryRow {
	otherByCat := map[string]int{}
	for _, c := range candidates {
		otherByCat[c.PrimaryCategory]++
	}
	clusterByCat := map[string]int{}
	for _, c := range clusters {
		clusterByCat[dominantCategory(c)]++
	}

	rows := make([]PerCategoryRow, 0, len(otherByCat))
	for cat, n := range otherByCat {
		rows = append(rows, PerCategoryRow{
			Category:    cat,
			OtherCount:  n,
			NumClusters: clusterByCat[cat],
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Category < rows[j].Category })
	return rows
}
