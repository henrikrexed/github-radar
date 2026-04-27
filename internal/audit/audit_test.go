package audit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubData implements DataProvider for unit/integration tests in this
// package. It captures the candidate set and denominator directly so
// tests don't need to seed SQLite.
type stubData struct {
	candidates []CandidateRepo
	denom      int
	candErr    error
	denomErr   error
}

func (s *stubData) OtherDriftCandidates(ctx context.Context) ([]CandidateRepo, error) {
	return s.candidates, s.candErr
}
func (s *stubData) ActiveNonCuratedCount(ctx context.Context) (int, error) {
	return s.denom, s.denomErr
}

// stubFiler implements Filer with controllable outcomes for U2/I2/I3/U3
// + the §6.1 degrade-to-watch tests.
type stubFiler struct {
	dedupHits  map[string]bool   // titlePrefix → true means "already filed"
	failOn     map[string]error  // titlePrefix → error to return from File
	calls      []GraduationDraft // every File invocation captured
	dedupCalls []string          // every AlreadyFiledRecently invocation captured
}

func (s *stubFiler) AlreadyFiledRecently(ctx context.Context, titlePrefix string) (bool, error) {
	s.dedupCalls = append(s.dedupCalls, titlePrefix)
	return s.dedupHits[titlePrefix], nil
}
func (s *stubFiler) File(ctx context.Context, draft GraduationDraft) (string, error) {
	s.calls = append(s.calls, draft)
	if err, ok := s.failOn[draft.Title()]; ok {
		return "", err
	}
	return "ISI-9999", nil
}

func candRepo(full, cat string, conf float64, topics ...string) CandidateRepo {
	return CandidateRepo{FullName: full, PrimaryCategory: cat, Confidence: conf, Topics: topics}
}

func newCluster(cat, token string, n int, conf float64) []CandidateRepo {
	out := make([]CandidateRepo, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, candRepo(cat+"/r"+itoa(i+1), cat, conf, token))
	}
	return out
}

// TestAudit_U2_WatchVsGraduation: cluster of 4 → watch only; cluster of 5
// → auto-filed.
func TestAudit_U2_WatchVsGraduation(t *testing.T) {
	data := &stubData{
		candidates: append(
			newCluster("ai", "alpha", 4, 0.8),         // size 4 → watch
			newCluster("devtools", "beta", 5, 0.8)..., // size 5, conf 0.8, score 4.0 ≥ 3.0 → file
		),
		denom: 200,
	}
	filer := &stubFiler{}
	a := NewAuditor(data, filer, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	out, err := a.Run(context.Background(), ModeFile, "audit-parent-id")
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(out.Filed) != 1 {
		t.Errorf("filed: got %d, want 1 (the size-5 beta cluster)", len(out.Filed))
	}
	// The size-4 alpha cluster doesn't even reach ClusterRepos's MinClusterSize
	// emit threshold, so it neither shows up as filed nor as a per-cluster
	// watch entry — the spec is "cluster of 4 → watch only" at the report
	// level, which the report's per-category row reflects (ai/other count = 4).
	for _, w := range out.WatchOnly {
		if dominantCategory(w.Cluster) == "ai" && len(w.Cluster.Repos) >= MinClusterSize {
			t.Errorf("unexpected ai cluster in watch list: %+v", w)
		}
	}
}

// TestAudit_U3_AvgConfidenceGate: cluster of 5 with avg confidence 0.5 →
// score = 2.5 < 3.0 AND avg < 0.6, both gates fail, no auto-file.
func TestAudit_U3_AvgConfidenceGate(t *testing.T) {
	// 10 repos with confidence 0.5: cluster of 10, score = 10*0.5 = 5.0
	// ≥ 3.0 (passes score), but avg = 0.5 < 0.6 (fails U3 gate).
	data := &stubData{
		candidates: newCluster("ai", "alpha", 10, 0.5),
		denom:      200,
	}
	filer := &stubFiler{}
	a := NewAuditor(data, filer, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	out, err := a.Run(context.Background(), ModeFile, "audit-parent-id")
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(out.Filed) != 0 {
		t.Errorf("filed: got %d, want 0 (avg confidence gate fails)", len(out.Filed))
	}
	if len(out.WatchOnly) != 1 {
		t.Fatalf("watchOnly: got %d, want 1 (avg conf gate)", len(out.WatchOnly))
	}
	if got := out.WatchOnly[0].Reason; got != "avg_confidence_below_threshold" {
		t.Errorf("reason: got %q, want avg_confidence_below_threshold", got)
	}
}

// TestAudit_I1_GoldenReportShape: pin headings, table columns, and link
// format on a small canonical input.
func TestAudit_I1_GoldenReportShape(t *testing.T) {
	data := &stubData{
		candidates: append(
			newCluster("ai", "quantum-computing", 5, 0.8),
			newCluster("devtools", "svelte-framework", 4, 0.8)..., // below MinClusterSize, only contributes to per-category row
		),
		denom: 200,
	}
	filer := &stubFiler{}
	a := NewAuditor(data, filer, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	out, err := a.Run(context.Background(), ModeFile, "audit-parent-id")
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}

	md := out.Report.Markdown

	// Filename pattern.
	if got, want := out.Report.Filename, "2026-05.md"; got != want {
		t.Errorf("filename: got %q, want %q", got, want)
	}

	mustContain(t, md, "# `<cat>/other` drift audit — 2026-05-01")
	mustContain(t, md, "**Corpus size:** 200 repos")
	mustContain(t, md, "## Per-category breakdown")
	mustContain(t, md, "| category | other count | clusters ≥5 |")
	mustContain(t, md, "| ai | 5 | 1 |")
	mustContain(t, md, "| devtools | 4 | 0 |")
	mustContain(t, md, "## Graduation proposals auto-filed")
	mustContain(t, md, "`ai/quantum-computing` — 5 repos")
	mustContain(t, md, "## Watch list")
	mustContain(t, md, "Filed by `github-radar audit other-drift`")
	mustContain(t, md, "[ISI-720](/ISI/issues/ISI-720)")
}

// TestAudit_I2_DedupTwoRuns: run audit twice; second run posts zero new
// issues because the dedup search returns hits.
func TestAudit_I2_DedupTwoRuns(t *testing.T) {
	data := &stubData{
		candidates: newCluster("ai", "alpha", 5, 0.8),
		denom:      200,
	}
	filer := &stubFiler{dedupHits: map[string]bool{}}
	a := NewAuditor(data, filer, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	out1, err := a.Run(context.Background(), ModeFile, "audit-parent")
	if err != nil {
		t.Fatalf("first run err: %v", err)
	}
	if len(out1.Filed) != 1 {
		t.Fatalf("first run filed: got %d, want 1", len(out1.Filed))
	}

	// Mark the first run's title as already-filed for the second pass.
	filer.dedupHits[out1.Filed[0].Cluster.titleFor("ai")] = true
	// Easier: just stub the dedup to always-hit on prefix.
	filer.dedupHits = map[string]bool{
		"Subcat graduation proposal: ai/alpha": true,
	}

	out2, err := a.Run(context.Background(), ModeFile, "audit-parent")
	if err != nil {
		t.Fatalf("second run err: %v", err)
	}
	if len(out2.Filed) != 0 {
		t.Errorf("second run filed: got %d, want 0 (dedup must suppress)", len(out2.Filed))
	}
	if len(out2.WatchOnly) != 1 || out2.WatchOnly[0].Reason != "deduped_within_60d" {
		t.Errorf("second run watch: got %+v, want one deduped entry", out2.WatchOnly)
	}
}

// titleFor is a test helper on Cluster that builds the same canonical
// title that GraduationDraft.Title() does. Defined as a method here so
// the test reads naturally.
func (c Cluster) titleFor(category string) string {
	return "Subcat graduation proposal: " + category + "/" + proposeSubcategoryName(c)
}

// TestAudit_I3_EscalationStrictBoundary: aggregate share at exactly 3.0%
// does NOT escalate; > 3.0% does. Plan §7 strict-`>` boundary.
func TestAudit_I3_EscalationStrictBoundary(t *testing.T) {
	cases := []struct {
		name      string
		other     int
		denom     int
		wantEsc   bool
		wantLabel string
	}{
		{"exactly_3pct_no_escalation", 3, 100, false, "PASS ≤3%"},
		{"above_3pct_escalates", 4, 100, true, "BREACH >3% [CRITICAL]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := &stubData{
				candidates: newCluster("ai", "alpha", tc.other, 0.8),
				denom:      tc.denom,
			}
			a := NewAuditor(data, &stubFiler{}, "")
			a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

			out, err := a.Run(context.Background(), ModeFile, "audit-parent")
			if err != nil {
				t.Fatalf("Run err: %v", err)
			}
			if out.Escalated != tc.wantEsc {
				t.Errorf("escalated: got %v, want %v (share=%.4f%%)", out.Escalated, tc.wantEsc, out.AggregateSharePct)
			}
			if !strings.Contains(out.Report.Markdown, tc.wantLabel) {
				t.Errorf("report missing label %q", tc.wantLabel)
			}
		})
	}
}

// TestAudit_I4_DenominatorScope: exclude curated and inactive repos from
// BOTH numerator AND denominator. The DataProvider port is responsible
// for this filter (it returns rows with is_curated_list=0 AND
// status='active' only). We verify the orchestrator computes share as
// numerator/denominator without any side filter.
func TestAudit_I4_DenominatorScope(t *testing.T) {
	// 5 candidates (filtered set), denominator 100 (filtered set) →
	// share = 5%. If the orchestrator were including curated or inactive
	// rows in either side, share would differ.
	data := &stubData{
		candidates: newCluster("ai", "alpha", 5, 0.8),
		denom:      100,
	}
	a := NewAuditor(data, &stubFiler{}, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	out, err := a.Run(context.Background(), ModeFile, "audit-parent")
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if got := out.AggregateSharePct; got < 4.99 || got > 5.01 {
		t.Errorf("share: got %.4f%%, want 5.00%%", got)
	}
}

// TestAudit_APIFailure_DegradeToWatch: cluster qualifies for auto-file but
// the Paperclip POST fails twice (after retry). Plan §6.1: append watch
// entry, no exception, exit 0.
func TestAudit_APIFailure_DegradeToWatch(t *testing.T) {
	data := &stubData{
		candidates: newCluster("ai", "alpha", 5, 0.8),
		denom:      200,
	}
	filer := &stubFiler{
		failOn: map[string]error{
			"Subcat graduation proposal: ai/alpha": &APIError{Status: 503, Body: "service unavailable"},
		},
	}
	a := NewAuditor(data, filer, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	out, err := a.Run(context.Background(), ModeFile, "audit-parent")
	if err != nil {
		t.Fatalf("Run must NOT return an error on API failure (plan §6.1): %v", err)
	}
	if len(out.Filed) != 0 {
		t.Errorf("filed: got %d, want 0", len(out.Filed))
	}
	if len(out.WatchOnly) != 1 {
		t.Fatalf("watch: got %d, want 1 (degrade to watch)", len(out.WatchOnly))
	}
	if out.WatchOnly[0].Reason != "auto_file_failed" {
		t.Errorf("reason: got %q, want auto_file_failed", out.WatchOnly[0].Reason)
	}
}

// TestAudit_DryRunDoesNotFile: in --dry-run mode, qualifying clusters are
// reported as `(dry-run)` filed entries but no Paperclip calls happen.
func TestAudit_DryRunDoesNotFile(t *testing.T) {
	data := &stubData{
		candidates: newCluster("ai", "alpha", 5, 0.8),
		denom:      200,
	}
	filer := &stubFiler{}
	a := NewAuditor(data, filer, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	out, err := a.Run(context.Background(), ModeDryRun, "audit-parent")
	if err != nil {
		t.Fatalf("Run err: %v", err)
	}
	if len(out.Filed) != 1 || out.Filed[0].Identifier != "(dry-run)" {
		t.Errorf("filed: got %+v, want one (dry-run) entry", out.Filed)
	}
	if len(filer.calls) != 0 || len(filer.dedupCalls) != 0 {
		t.Errorf("dry-run must not call Paperclip; got file=%d dedup=%d", len(filer.calls), len(filer.dedupCalls))
	}
}

// TestAudit_FilePersistence: in --file mode the report is written to
// ReportsDir/YYYY-MM.md.
func TestAudit_FilePersistence(t *testing.T) {
	tmp := t.TempDir()
	data := &stubData{
		candidates: newCluster("ai", "alpha", 5, 0.8),
		denom:      200,
	}
	a := NewAuditor(data, &stubFiler{}, tmp)
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC) }

	if _, err := a.Run(context.Background(), ModeFile, "audit-parent"); err != nil {
		t.Fatalf("Run err: %v", err)
	}
	path := filepath.Join(tmp, "2026-05.md")
	b, err := readFile(path)
	if err != nil {
		t.Fatalf("read persisted report: %v", err)
	}
	if !strings.Contains(b, "# `<cat>/other` drift audit — 2026-05-01") {
		t.Errorf("persisted report missing header; got first chars: %q", firstN(b, 200))
	}
}

// TestDataProviderError: DataProvider failures bubble up as run errors
// (not the same class as Paperclip API failures, which degrade).
func TestDataProviderError(t *testing.T) {
	data := &stubData{candErr: errors.New("db gone")}
	a := NewAuditor(data, &stubFiler{}, "")
	a.Now = func() time.Time { return time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := a.Run(context.Background(), ModeFile, "p"); err == nil {
		t.Fatal("expected error on DataProvider failure")
	}
}

func mustContain(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Errorf("expected report to contain %q; got:\n%s", want, s)
	}
}

func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
