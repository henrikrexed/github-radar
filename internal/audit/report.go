package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Render produces the full markdown report per plan §5 template. Output is
// stable: floats are formatted to one decimal place for share, two for
// score; rows are pre-sorted in Run.
func Render(r *Result) string {
	var b strings.Builder
	share := r.AggregateShare * 100
	gateLabel := "PASS ≤3%"
	if r.EscalateCritical {
		gateLabel = "FAIL >3% (escalate priority=critical)"
	}
	dateStr := r.GeneratedAt.Format("2006-01-02")
	fmt.Fprintf(&b, "# `<cat>/other` drift audit — %s\n\n", dateStr)
	fmt.Fprintf(&b, "**Corpus size:** %d repos\n", r.CorpusSize)
	fmt.Fprintf(&b, "**`<cat>/other` aggregate share:** %.1f%% (%d repos) [%s]\n\n", share, r.OtherCount, gateLabel)

	if r.EscalateCritical {
		// Plan §7: aggregate share > 3% gets a critical-priority marker
		// AND an @BigBoss mention. We render both so consumers (graders,
		// integration tests, humans) can pin the behavior.
		b.WriteString("> **priority=critical** — aggregate `other` share exceeds the 3% gate. @BigBoss\n\n")
	}

	b.WriteString("## Per-category breakdown\n")
	b.WriteString("| category | other count | % of category | clusters ≥5 |\n")
	b.WriteString("|----------|-------------|---------------|-------------|\n")
	for _, s := range r.PerCategory {
		clusters := "0"
		if len(s.ClustersGE5) > 0 {
			parts := make([]string, 0, len(s.ClustersGE5))
			for _, c := range s.ClustersGE5 {
				parts = append(parts, c.ProposedSub)
			}
			clusters = fmt.Sprintf("%d (%s)", len(s.ClustersGE5), strings.Join(parts, ", "))
		}
		fmt.Fprintf(&b, "| %s | %d | %.1f%% | %s |\n", s.Category, s.OtherCount, s.OtherShare*100, clusters)
	}
	b.WriteString("\n")

	b.WriteString("## Graduation proposals auto-filed\n")
	if len(r.AutoFiled) == 0 {
		b.WriteString("_No clusters reached the auto-file gate this run._\n\n")
	} else {
		for _, f := range r.AutoFiled {
			tokens := strings.Join(f.Cluster.MergedTokens, ", ")
			label := f.IssueIdentifier
			href := f.IssueIdentifier
			if strings.HasPrefix(href, "ISI-") {
				href = "/ISI/issues/" + href
			}
			fmt.Fprintf(&b, "- [%s](%s): `%s/%s` — %d repos, token overlap: %s\n",
				label, href, f.Cluster.Category, f.Cluster.ProposedSub, len(f.Cluster.Repos), tokens)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Watch list (2 ≤ cluster < 5, plus any auto-file failures)\n")
	if len(r.WatchList) == 0 {
		b.WriteString("_No watch entries this run._\n")
	} else {
		for _, w := range r.WatchList {
			fmt.Fprintf(&b, "- `%s/%s`: %d repos — %s\n", w.Category, w.ProposedSub, w.RepoCount, w.Reason)
		}
	}
	return b.String()
}

// PersistReport writes the rendered report to <reportDir>/<YYYY-MM>.md and
// returns the absolute path. Creates the directory if it does not exist.
func PersistReport(r *Result, reportDir string) (string, error) {
	if reportDir == "" {
		return "", fmt.Errorf("report dir is empty")
	}
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return "", fmt.Errorf("creating report dir %s: %w", reportDir, err)
	}
	stamp := r.GeneratedAt.Format("2006-01")
	path := filepath.Join(reportDir, stamp+".md")
	if err := os.WriteFile(path, []byte(Render(r)), 0o644); err != nil {
		return "", fmt.Errorf("writing report %s: %w", path, err)
	}
	return path, nil
}

// DefaultReportDir returns the XDG-aware default audits directory.
func DefaultReportDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "github-radar", "audits")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "data/audits"
	}
	return filepath.Join(home, ".local", "share", "github-radar", "audits")
}
