package database

import "testing"

// TestAuditOtherDriftCandidates_PostV3Schema verifies the audit query
// scopes correctly against the post-T3 v3 taxonomy schema:
//   - primary_subcategory = 'other'
//   - is_curated_list = 0
//   - status = 'active'
//
// Curated rows, inactive rows, and non-other subcategory rows must be
// excluded. This pins the contract that ISI-751 audit relies on (I4).
func TestAuditOtherDriftCandidates_PostV3Schema(t *testing.T) {
	db := mustOpen(t)

	repos := []*RepoRecord{
		// IN: <cat>/other, non-curated, active
		{FullName: "a/match1", Owner: "a", Name: "match1", Status: "active", PrimaryCategory: "ai", CategoryConfidence: 0.82},
		{FullName: "a/match2", Owner: "a", Name: "match2", Status: "active", PrimaryCategory: "devtools", CategoryConfidence: 0.71},
		// OUT: subcategory != 'other'
		{FullName: "a/skip-subcat", Owner: "a", Name: "skip-subcat", Status: "active", PrimaryCategory: "ai", CategoryConfidence: 0.9},
		// OUT: curated list
		{FullName: "a/skip-curated", Owner: "a", Name: "skip-curated", Status: "active", PrimaryCategory: "ai", CategoryConfidence: 0.95},
		// OUT: inactive (status != active)
		{FullName: "a/skip-inactive", Owner: "a", Name: "skip-inactive", Status: "pending", PrimaryCategory: "ai", CategoryConfidence: 0.5},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo %s: %v", r.FullName, err)
		}
	}

	// Set the v3 columns directly (RepoRecord doesn't expose them yet).
	subcats := map[string]string{
		"a/match1":        "other",
		"a/match2":        "other",
		"a/skip-subcat":   "agents", // != other → excluded
		"a/skip-curated":  "other",  // is_curated_list=1 below
		"a/skip-inactive": "other",
	}
	for full, sub := range subcats {
		if _, err := db.db.Exec(`UPDATE repos SET primary_subcategory = ? WHERE full_name = ?`, sub, full); err != nil {
			t.Fatalf("set subcategory %s: %v", full, err)
		}
	}
	if _, err := db.db.Exec(`UPDATE repos SET is_curated_list = 1 WHERE full_name = 'a/skip-curated'`); err != nil {
		t.Fatalf("set is_curated_list: %v", err)
	}

	got, err := db.AuditOtherDriftCandidates()
	if err != nil {
		t.Fatalf("AuditOtherDriftCandidates: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2 (only a/match1, a/match2). got=%+v", len(got), got)
	}
	if got[0].FullName != "a/match1" || got[1].FullName != "a/match2" {
		t.Errorf("ordering: got %v, want [a/match1 a/match2]", []string{got[0].FullName, got[1].FullName})
	}
	if got[0].PrimaryCategory != "ai" || got[1].PrimaryCategory != "devtools" {
		t.Errorf("primary_category fields: got %v", []string{got[0].PrimaryCategory, got[1].PrimaryCategory})
	}
	if got[0].Confidence < 0.81 || got[0].Confidence > 0.83 {
		t.Errorf("confidence carried through: got %f, want ~0.82", got[0].Confidence)
	}
}

// TestAuditActiveNonCuratedCount excludes curated and inactive rows from
// the denominator (plan §7).
func TestAuditActiveNonCuratedCount(t *testing.T) {
	db := mustOpen(t)
	repos := []*RepoRecord{
		{FullName: "a/n1", Owner: "a", Name: "n1", Status: "active", PrimaryCategory: "ai"},
		{FullName: "a/n2", Owner: "a", Name: "n2", Status: "active", PrimaryCategory: "ai"},
		{FullName: "a/curated", Owner: "a", Name: "curated", Status: "active", PrimaryCategory: "ai"},
		{FullName: "a/inactive", Owner: "a", Name: "inactive", Status: "pending", PrimaryCategory: "ai"},
	}
	for _, r := range repos {
		if err := db.UpsertRepo(r); err != nil {
			t.Fatalf("UpsertRepo %s: %v", r.FullName, err)
		}
	}
	if _, err := db.db.Exec(`UPDATE repos SET is_curated_list = 1 WHERE full_name = 'a/curated'`); err != nil {
		t.Fatalf("set is_curated_list: %v", err)
	}

	n, err := db.AuditActiveNonCuratedCount()
	if err != nil {
		t.Fatalf("AuditActiveNonCuratedCount: %v", err)
	}
	if n != 2 {
		t.Errorf("count: got %d, want 2 (curated + inactive excluded)", n)
	}
}
