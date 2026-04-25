package audit

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Clock is the audit-job clock dependency-injection seam (plan §6 dedup
// review Q3 Layer-2). Production passes time.Now; tests pass a fixed clock.
type Clock func() time.Time

// Filer is the contract the audit uses to file (or skip) graduation
// proposals. Production: PaperclipFiler. Tests: in-memory stub.
type Filer interface {
	// FindRecentDuplicate returns a non-empty issue identifier if the title
	// prefix has already been filed inside the dedup window relative to now.
	// Stubs decide via the response, not wall-clock — see plan §6 review Q3.
	FindRecentDuplicate(ctx context.Context, titlePrefix string, now time.Time) (string, error)
	// File posts a new graduation-proposal issue. Returns the new issue's
	// identifier. On non-recoverable error (after retry), returns FileError
	// so the caller can degrade to the report's Watch list per plan §6.1.
	File(ctx context.Context, p Proposal) (string, error)
}

// Proposal is the payload the Filer needs to either dedup or POST.
type Proposal struct {
	Category    string
	ProposedSub string
	Repos       []string
	Tokens      []string
	AvgConf     float64
	Score       float64
}

// FileError is returned by Filer.File on persistent failure (retry exhausted
// or non-retryable HTTP). The audit treats it as a degrade-to-watch signal
// rather than a fatal error per plan §6.1.
type FileError struct {
	StatusCode int
	Reason     string
}

func (e *FileError) Error() string {
	return fmt.Sprintf("auto-file failed: %d %s", e.StatusCode, e.Reason)
}

// Result is the outcome of one audit run.
type Result struct {
	GeneratedAt        time.Time
	CorpusSize         int
	OtherCount         int
	AggregateShare     float64 // 0–1
	EscalateCritical   bool
	PerCategory        []CategoryStats
	AutoFiled          []FiledRef // successfully auto-filed clusters
	WatchList          []WatchEntry
	AutoFileFailures   int
	ScannedRepos       int // includes empty-topics
	NowFunc            Clock
	parentTaxonomyHint string
}

// CategoryStats is one row of the per-category breakdown table.
type CategoryStats struct {
	Category    string
	OtherCount  int
	OtherShare  float64 // share within the category (other/total in cat)
	ClustersGE5 []Cluster
}

// FiledRef is a successfully filed graduation-proposal reference for the
// "Graduation proposals auto-filed" report section.
type FiledRef struct {
	IssueIdentifier string
	Cluster         Cluster
}

// WatchEntry is one row of the Watch list — clusters that did not auto-file
// (either too small / score-below-threshold / confidence-below-threshold,
// or the auto-file API call persistently failed per plan §6.1).
type WatchEntry struct {
	Category    string
	ProposedSub string
	RepoCount   int
	Reason      string // free-form label, e.g. "score 2.75 < 3.00" or "auto-file FAILED (503 Service Unavailable); file manually."
	Tokens      []string
	Repos       []string
}

// Options configures one audit run.
type Options struct {
	DB        *sql.DB
	DryRun    bool          // suppresses both Filer calls and file writes
	File      bool          // when true, persist report to disk
	ReportDir string        // defaults to ~/.local/share/github-radar/audits when File is true
	Filer     Filer         // required unless DryRun is set
	Now       Clock         // defaults to time.Now
	DedupWin  time.Duration // defaults to 60 days
	Logger    StructuredLogger
}

// StructuredLogger is the small subset of slog the audit uses. Defining it
// here keeps the package free of an internal/logging dependency, which keeps
// it unit-testable without booting the global logger.
type StructuredLogger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// Run executes the monthly drift audit per plan §3–§7.
//
// It is the single entrypoint used by the CLI and by integration tests. The
// returned Result is the in-memory artifact; the rendered markdown report
// is produced by Render(result) in report.go.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.DedupWin == 0 {
		opts.DedupWin = 60 * 24 * time.Hour
	}
	if opts.Logger == nil {
		opts.Logger = nopLogger{}
	}

	now := opts.Now()
	res := &Result{GeneratedAt: now, NowFunc: opts.Now}

	// Plan §2 / §7 query — drives BOTH numerator (other-bucket) and the
	// denominator scope. Plan §7 explicitly requires curated and inactive
	// repos to be excluded from BOTH.
	//
	// `excluded` and `is_curated_list` are independent flags. The plan
	// references is_curated_list and scan_status='active'; the on-disk
	// status column is named `status` (not `scan_status`). We use the
	// plan's semantic but the on-disk column name in SQL.
	denomQuery := `SELECT COUNT(*) FROM repos WHERE is_curated_list = 0 AND status = 'active'`
	if err := opts.DB.QueryRowContext(ctx, denomQuery).Scan(&res.CorpusSize); err != nil {
		return nil, fmt.Errorf("denominator query: %w", err)
	}

	otherQuery := `
		SELECT full_name, primary_category, topics, description, category_confidence
		FROM repos
		WHERE primary_subcategory = 'other'
		  AND is_curated_list = 0
		  AND status = 'active'`
	rows, err := opts.DB.QueryContext(ctx, otherQuery)
	if err != nil {
		return nil, fmt.Errorf("other-bucket query: %w", err)
	}
	defer rows.Close()

	var others []AuditRepo
	for rows.Next() {
		var r AuditRepo
		var topicsCSV, desc, primaryCat string
		if err := rows.Scan(&r.FullName, &primaryCat, &topicsCSV, &desc, &r.Confidence); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		r.PrimaryCategory = primaryCat
		r.Description = desc
		r.Topics = splitTopics(topicsCSV)
		others = append(others, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter rows: %w", err)
	}
	res.OtherCount = len(others)
	res.ScannedRepos = len(others)

	// Aggregate share. Avoid div-by-zero on an empty corpus.
	if res.CorpusSize > 0 {
		res.AggregateShare = float64(res.OtherCount) / float64(res.CorpusSize)
	}
	// Plan §7 — strictly greater than 3% (>; not ≥). 3.0% does NOT escalate.
	res.EscalateCritical = res.AggregateShare > 0.03

	// Cluster within each (cat) bucket. Empty-topic repos are excluded by
	// ClusterByCategory but already counted toward the aggregate share above.
	clustersByCat := ClusterByCategory(others)

	// Per-category counts (denominator within the cat = total active +
	// non-curated for that cat).
	perCatTotals, err := perCategoryTotals(ctx, opts.DB)
	if err != nil {
		return nil, err
	}
	perCatOther := map[string]int{}
	for _, r := range others {
		perCatOther[r.PrimaryCategory]++
	}
	cats := make([]string, 0, len(perCatTotals))
	for k := range perCatTotals {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		stat := CategoryStats{
			Category:   cat,
			OtherCount: perCatOther[cat],
		}
		if total := perCatTotals[cat]; total > 0 {
			stat.OtherShare = float64(perCatOther[cat]) / float64(total)
		}
		// Surface only auto-file-eligible clusters in the table; watch-list
		// (sub-threshold) clusters are listed in a separate report section.
		for _, c := range clustersByCat[cat] {
			if len(c.Repos) >= MinClusterSize {
				stat.ClustersGE5 = append(stat.ClustersGE5, c)
			}
		}
		res.PerCategory = append(res.PerCategory, stat)
	}

	// Auto-file or watch-list each cluster.
	for _, cat := range cats {
		for _, c := range clustersByCat[cat] {
			if !c.IsAutoFileEligible() {
				res.WatchList = append(res.WatchList, watchEntryFor(c, "score "+formatFloat(c.Score)+" / count "+fmt.Sprint(len(c.Repos))+" / avg_conf "+formatFloat(c.AvgConfidence)+" — sub-threshold"))
				continue
			}
			titlePrefix := proposalTitlePrefix(c.Category, c.ProposedSub)
			if !opts.DryRun && opts.Filer != nil {
				dup, err := opts.Filer.FindRecentDuplicate(ctx, titlePrefix, now)
				if err != nil {
					opts.Logger.Warn("audit.dedup.search_failed", "category", c.Category, "sub", c.ProposedSub, "err", err.Error())
					// On a search failure, treat as degrade-to-watch — same
					// posture as a POST failure (plan §6.1 spirit).
					res.AutoFileFailures++
					res.WatchList = append(res.WatchList, watchEntryFor(c, "auto-file FAILED (dedup search error: "+truncReason(err.Error())+"); file manually."))
					continue
				}
				if dup != "" {
					opts.Logger.Info("audit.autofile.skipped_dedup", "category", c.Category, "sub", c.ProposedSub, "existing", dup)
					continue
				}
				p := Proposal{
					Category:    c.Category,
					ProposedSub: c.ProposedSub,
					Repos:       c.Repos,
					Tokens:      c.MergedTokens,
					AvgConf:     c.AvgConfidence,
					Score:       c.Score,
				}
				id, err := opts.Filer.File(ctx, p)
				if err != nil {
					var fe *FileError
					reason := err.Error()
					code := 0
					if asFileErr(err, &fe) {
						code = fe.StatusCode
						reason = fe.Reason
					}
					opts.Logger.Warn("audit.autofile.failed", "category", c.Category, "sub", c.ProposedSub, "status", code, "reason", truncReason(reason))
					res.AutoFileFailures++
					failLabel := fmt.Sprintf("auto-file FAILED (%d %s); file manually.", code, truncReason(reason))
					res.WatchList = append(res.WatchList, watchEntryFor(c, failLabel))
					continue
				}
				res.AutoFiled = append(res.AutoFiled, FiledRef{IssueIdentifier: id, Cluster: c})
				continue
			}
			// DryRun or no Filer — record what *would* be filed but do not
			// surface as Watch (those are real reasons for not filing).
			res.AutoFiled = append(res.AutoFiled, FiledRef{IssueIdentifier: "(dry-run)", Cluster: c})
		}
	}

	return res, nil
}

func perCategoryTotals(ctx context.Context, db *sql.DB) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT primary_category, COUNT(*) FROM repos WHERE is_curated_list = 0 AND status = 'active' GROUP BY primary_category`)
	if err != nil {
		return nil, fmt.Errorf("per-category totals: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var cat string
		var n int
		if err := rows.Scan(&cat, &n); err != nil {
			return nil, err
		}
		out[cat] = n
	}
	return out, rows.Err()
}

func splitTopics(csv string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func proposalTitlePrefix(category, sub string) string {
	return fmt.Sprintf("Subcat graduation proposal: %s/%s", category, sub)
}

func watchEntryFor(c Cluster, reason string) WatchEntry {
	return WatchEntry{
		Category:    c.Category,
		ProposedSub: c.ProposedSub,
		RepoCount:   len(c.Repos),
		Reason:      reason,
		Tokens:      c.MergedTokens,
		Repos:       c.Repos,
	}
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}

func truncReason(s string) string {
	const max = 256
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// asFileErr is a thin errors.As wrapper that avoids importing errors at the
// call site. It returns true if err is (or wraps) a *FileError and stores it.
func asFileErr(err error, dst **FileError) bool {
	for cur := err; cur != nil; {
		if fe, ok := cur.(*FileError); ok {
			*dst = fe
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := cur.(unwrapper)
		if !ok {
			return false
		}
		cur = u.Unwrap()
	}
	return false
}
