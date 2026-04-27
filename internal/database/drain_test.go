package database

import (
	"os"
	"testing"
)

// drainSeed defines a single repo row used by the drain test fixture. It
// captures only the columns the drain predicate cares about — the other
// repos columns inherit the schema defaults.
type drainSeed struct {
	fullName       string
	status         string
	primaryCat     string
	primarySubcat  string
	needsReview    int
	excluded       int
	confidence     float64
	classifiedAt   string
	categoryLegacy string
}

// seedDrainRow inserts a repo row using direct SQL so we can populate the
// taxonomy v3 columns (primary_subcategory, needs_review) that the public
// UpsertRepo helper does not yet expose. The drain predicate reads these
// columns directly, so test seeding must as well.
func seedDrainRow(t *testing.T, db *DB, s drainSeed) {
	t.Helper()
	owner, name := splitFullName(s.fullName)
	_, err := db.SQL().Exec(`
		INSERT INTO repos (
			full_name, owner, name, status, excluded,
			primary_category, primary_subcategory, primary_category_legacy,
			category_confidence, classified_at, needs_review
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		s.fullName, owner, name, s.status, s.excluded,
		s.primaryCat, s.primarySubcat, s.categoryLegacy,
		s.confidence, s.classifiedAt, s.needsReview,
	)
	if err != nil {
		t.Fatalf("seed %s: %v", s.fullName, err)
	}
}

func splitFullName(fullName string) (string, string) {
	for i := 0; i < len(fullName); i++ {
		if fullName[i] == '/' {
			return fullName[:i], fullName[i+1:]
		}
	}
	return fullName, ""
}

// statusOf returns the status column for a single repo — used to assert
// drain mutations precisely.
func statusOf(t *testing.T, db *DB, fullName string) string {
	t.Helper()
	var s string
	if err := db.SQL().QueryRow(
		`SELECT status FROM repos WHERE full_name = ?`, fullName,
	).Scan(&s); err != nil {
		t.Fatalf("statusOf %s: %v", fullName, err)
	}
	return s
}

// TestDrain_DeterministicCases covers the four canonical states from the
// ISI-773 plan on a synthetic 4-row DB: a drainable repo, a repo in the
// (other,*) refusal sink, an already-active repo (no-op), and an excluded
// repo (must be ignored).
func TestDrain_DeterministicCases(t *testing.T) {
	db := mustOpen(t)

	seedDrainRow(t, db, drainSeed{
		fullName:       "acme/drainable",
		status:         "needs_reclassify",
		primaryCat:     "ai",
		primarySubcat:  "agents",
		categoryLegacy: "ai-agents",
		confidence:     0.95,
		classifiedAt:   "2026-04-20T00:00:00Z",
	})
	seedDrainRow(t, db, drainSeed{
		fullName:       "acme/refusal-sink",
		status:         "needs_reclassify",
		primaryCat:     "other",
		primarySubcat:  "other",
		categoryLegacy: "other",
		needsReview:    1,
		confidence:     0.0,
	})
	seedDrainRow(t, db, drainSeed{
		fullName:       "acme/already-active",
		status:         "active",
		primaryCat:     "systems",
		primarySubcat:  "ebpf",
		categoryLegacy: "ebpf-system-tracing",
		confidence:     0.95,
	})
	seedDrainRow(t, db, drainSeed{
		fullName:       "acme/excluded",
		status:         "needs_reclassify",
		primaryCat:     "ai",
		primarySubcat:  "agents",
		categoryLegacy: "ai-agents",
		excluded:       1,
		confidence:     0.95,
	})

	report, err := db.DrainNeedsReclassify(false, 0)
	if err != nil {
		t.Fatalf("DrainNeedsReclassify: %v", err)
	}

	// Examined excludes excluded=1 rows.
	if report.Examined != 2 {
		t.Errorf("Examined = %d, want 2 (drainable + refusal-sink)", report.Examined)
	}
	if report.Drained != 1 {
		t.Errorf("Drained = %d, want 1 (only acme/drainable)", report.Drained)
	}
	if report.HeldInSink != 1 {
		t.Errorf("HeldInSink = %d, want 1 (only acme/refusal-sink)", report.HeldInSink)
	}
	if report.HeldOther != 0 {
		t.Errorf("HeldOther = %d, want 0", report.HeldOther)
	}
	if got := report.ByCategory["ai"]; got != 1 {
		t.Errorf("ByCategory[ai] = %d, want 1", got)
	}
	if report.BackupPath == "" {
		t.Error("BackupPath empty on real run; expected pre-drain backup")
	} else if _, err := os.Stat(report.BackupPath); err != nil {
		t.Errorf("backup file not present at %s: %v", report.BackupPath, err)
	}

	// Row-level assertions: drainable flipped, refusal-sink + already-active
	// + excluded untouched.
	if got := statusOf(t, db, "acme/drainable"); got != "active" {
		t.Errorf("drainable status = %q, want active", got)
	}
	if got := statusOf(t, db, "acme/refusal-sink"); got != "needs_reclassify" {
		t.Errorf("refusal-sink status = %q, want needs_reclassify", got)
	}
	if got := statusOf(t, db, "acme/already-active"); got != "active" {
		t.Errorf("already-active status = %q, want active (untouched)", got)
	}
	if got := statusOf(t, db, "acme/excluded"); got != "needs_reclassify" {
		t.Errorf("excluded status = %q, want needs_reclassify (untouched)", got)
	}
}

// TestDrain_DryRun verifies the dry-run pass produces the same numeric
// preview as a real run but does not mutate any rows or write a backup.
func TestDrain_DryRun(t *testing.T) {
	db := mustOpen(t)

	seedDrainRow(t, db, drainSeed{
		fullName:      "acme/drainable",
		status:        "needs_reclassify",
		primaryCat:    "ai",
		primarySubcat: "agents",
		confidence:    0.95,
	})
	seedDrainRow(t, db, drainSeed{
		fullName:      "acme/refusal-sink",
		status:        "needs_reclassify",
		primaryCat:    "other",
		primarySubcat: "other",
		needsReview:   1,
	})

	report, err := db.DrainNeedsReclassify(true, 0)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !report.DryRun {
		t.Error("DryRun flag not set on report")
	}
	if report.Drained != 1 {
		t.Errorf("Drained preview = %d, want 1", report.Drained)
	}
	if report.HeldInSink != 1 {
		t.Errorf("HeldInSink preview = %d, want 1", report.HeldInSink)
	}
	if report.BackupPath != "" {
		t.Errorf("BackupPath = %q on dry-run; want empty", report.BackupPath)
	}

	// No row should have been mutated by the dry-run pass.
	if got := statusOf(t, db, "acme/drainable"); got != "needs_reclassify" {
		t.Errorf("drainable status post dry-run = %q, want needs_reclassify (no mutation)", got)
	}
}

// TestDrain_Idempotent verifies that running the drain twice in succession
// produces 0 drained rows on the second pass — the predicate is
// self-extinguishing.
func TestDrain_Idempotent(t *testing.T) {
	db := mustOpen(t)

	seedDrainRow(t, db, drainSeed{
		fullName:      "acme/drainable",
		status:        "needs_reclassify",
		primaryCat:    "ai",
		primarySubcat: "agents",
		confidence:    0.95,
	})

	first, err := db.DrainNeedsReclassify(false, 0)
	if err != nil {
		t.Fatalf("first drain: %v", err)
	}
	if first.Drained != 1 {
		t.Fatalf("first drain Drained = %d, want 1", first.Drained)
	}

	second, err := db.DrainNeedsReclassify(false, 0)
	if err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if second.Drained != 0 {
		t.Errorf("second drain Drained = %d, want 0 (idempotent)", second.Drained)
	}
	if second.Examined != 0 {
		t.Errorf("second drain Examined = %d, want 0", second.Examined)
	}
}

// TestDrain_Limit verifies --limit caps the UPDATE rowcount but the
// preview ByCategory aggregation reflects what would be drained in this
// (capped) pass — important so staged drains report honest numbers.
func TestDrain_Limit(t *testing.T) {
	db := mustOpen(t)
	for i := 0; i < 5; i++ {
		seedDrainRow(t, db, drainSeed{
			fullName:      fmtName("acme/repo-", i),
			status:        "needs_reclassify",
			primaryCat:    "ai",
			primarySubcat: "agents",
			confidence:    0.95,
		})
	}

	report, err := db.DrainNeedsReclassify(false, 2)
	if err != nil {
		t.Fatalf("limited drain: %v", err)
	}
	if report.Drained != 2 {
		t.Errorf("Drained = %d, want 2 (capped by --limit=2)", report.Drained)
	}

	// Three rows should still be sitting in needs_reclassify.
	remaining, err := db.NeedsReclassifyCount()
	if err != nil {
		t.Fatalf("NeedsReclassifyCount: %v", err)
	}
	if remaining != 3 {
		t.Errorf("remaining needs_reclassify = %d, want 3", remaining)
	}
}

// TestDrain_NeedsReviewBlocks ensures a row with needs_review=1 stays in
// needs_reclassify regardless of category — protects the human-triage
// queue from getting silently flushed.
func TestDrain_NeedsReviewBlocks(t *testing.T) {
	db := mustOpen(t)
	seedDrainRow(t, db, drainSeed{
		fullName:      "acme/needs-review",
		status:        "needs_reclassify",
		primaryCat:    "ai",
		primarySubcat: "agents",
		needsReview:   1,
		confidence:    0.55,
	})

	report, err := db.DrainNeedsReclassify(false, 0)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if report.Drained != 0 {
		t.Errorf("Drained = %d, want 0 (needs_review=1 blocks drain)", report.Drained)
	}
	if got := statusOf(t, db, "acme/needs-review"); got != "needs_reclassify" {
		t.Errorf("needs-review status = %q, want needs_reclassify (blocked)", got)
	}
}

// TestNeedsReclassifyCount sanity-checks the gauge accessor used by the
// daemon shutdown log line and the admin CLI report.
func TestNeedsReclassifyCount(t *testing.T) {
	db := mustOpen(t)

	got, err := db.NeedsReclassifyCount()
	if err != nil {
		t.Fatalf("count on empty DB: %v", err)
	}
	if got != 0 {
		t.Errorf("empty DB count = %d, want 0", got)
	}

	seedDrainRow(t, db, drainSeed{
		fullName:      "acme/r1",
		status:        "needs_reclassify",
		primaryCat:    "ai",
		primarySubcat: "agents",
	})
	seedDrainRow(t, db, drainSeed{
		fullName:      "acme/r2",
		status:        "active",
		primaryCat:    "ai",
		primarySubcat: "agents",
	})
	seedDrainRow(t, db, drainSeed{
		fullName:      "acme/r3",
		status:        "needs_reclassify",
		primaryCat:    "ai",
		primarySubcat: "agents",
		excluded:      1,
	})

	got, err = db.NeedsReclassifyCount()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 1 {
		t.Errorf("count = %d, want 1 (1 needs_reclassify excluded=0)", got)
	}
}

func fmtName(prefix string, i int) string {
	return prefix + string(rune('0'+i))
}
