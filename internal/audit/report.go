package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Report is the rendered markdown audit report. Persisted to
// `~/.local/share/github-radar/audits/YYYY-MM.md` in --file mode and
// printed to stdout in --dry-run.
type Report struct {
	Filename string // YYYY-MM.md
	Markdown string
}

type reportInputs struct {
	Now               time.Time
	CorpusSize        int
	OtherCount        int
	AggregateSharePct float64
	Escalated         bool
	PerCategory       []PerCategoryRow
	Filed             []FiledIssue
	WatchOnly         []ClusterEntry
}

func renderReport(in reportInputs) Report {
	year, month, day := in.Now.Date()
	filename := fmt.Sprintf("%04d-%02d.md", year, int(month))

	var b strings.Builder
	fmt.Fprintf(&b, "# `<cat>/other` drift audit — %04d-%02d-%02d\n\n", year, int(month), day)

	// Top-line summary — `[CRITICAL]` token if escalated, plan §7.
	gateLabel := "PASS ≤3%"
	if in.Escalated {
		gateLabel = "BREACH >3% [CRITICAL]"
	}
	fmt.Fprintf(&b, "**Corpus size:** %d repos  \n", in.CorpusSize)
	fmt.Fprintf(&b, "**`<cat>/other` aggregate share:** %.2f%% (%d repos) [%s]  \n",
		in.AggregateSharePct, in.OtherCount, gateLabel)
	if in.Escalated {
		b.WriteString("\n> @BigBoss — aggregate `<cat>/other` share exceeded the 3.0% threshold; report escalated to `priority=critical`.\n")
	}

	// Per-category breakdown.
	b.WriteString("\n## Per-category breakdown\n\n")
	if len(in.PerCategory) == 0 {
		b.WriteString("_No `<cat>/other` candidates._\n")
	} else {
		b.WriteString("| category | other count | clusters ≥5 |\n")
		b.WriteString("|----------|-------------|-------------|\n")
		// Already sorted alphabetically by category.
		for _, r := range in.PerCategory {
			fmt.Fprintf(&b, "| %s | %d | %d |\n", r.Category, r.OtherCount, r.NumClusters)
		}
	}

	// Auto-filed graduation proposals.
	b.WriteString("\n## Graduation proposals auto-filed\n\n")
	if len(in.Filed) == 0 {
		b.WriteString("_None this run._\n")
	} else {
		filed := append([]FiledIssue{}, in.Filed...)
		sort.Slice(filed, func(i, j int) bool {
			a, c := dominantCategory(filed[i].Cluster), dominantCategory(filed[j].Cluster)
			if a != c {
				return a < c
			}
			return filed[i].Subcategory < filed[j].Subcategory
		})
		for _, f := range filed {
			cat := dominantCategory(f.Cluster)
			fmt.Fprintf(&b, "- `%s/%s` — %d repos. Token overlap: %s. Issue: `%s`.\n",
				cat, f.Subcategory, len(f.Cluster.Repos), strings.Join(f.Cluster.Tokens, ", "), f.Identifier)
		}
	}

	// Watch list.
	b.WriteString("\n## Watch list\n\n")
	if len(in.WatchOnly) == 0 {
		b.WriteString("_None this run._\n")
	} else {
		watch := append([]ClusterEntry{}, in.WatchOnly...)
		sort.Slice(watch, func(i, j int) bool {
			a, c := dominantCategory(watch[i].Cluster), dominantCategory(watch[j].Cluster)
			if a != c {
				return a < c
			}
			return watch[i].Subcategory < watch[j].Subcategory
		})
		for _, w := range watch {
			cat := dominantCategory(w.Cluster)
			fmt.Fprintf(&b, "- `%s/%s` cluster on `%s`: %s\n",
				cat, w.Subcategory, strings.Join(w.Cluster.Tokens, ", "), w.Detail)
		}
	}

	b.WriteString("\n---\nFiled by `github-radar audit other-drift`. See [ISI-720](/ISI/issues/ISI-720) for the audit framework.\n")
	return Report{Filename: filename, Markdown: b.String()}
}

// persistReport writes the rendered markdown to ReportsDir/YYYY-MM.md,
// creating the directory if needed.
func persistReport(reportsDir string, now time.Time, r Report) error {
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", reportsDir, err)
	}
	path := filepath.Join(reportsDir, r.Filename)
	if err := os.WriteFile(path, []byte(r.Markdown), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
