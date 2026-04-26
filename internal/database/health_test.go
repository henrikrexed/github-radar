package database

import (
	"testing"
)

// healthSeed defines a single repo row used by the classification-health
// helper tests. It only captures columns the helpers (CountByStatus,
// PendingCountsByDimension, LastClassifiedAt) actually read; everything else
// inherits the schema defaults. Mirrors the drain_test.go fixture pattern.
type healthSeed struct {
	fullName        string
	status          string
	excluded        int
	forceCategory   string
	primaryCategory string
	classifiedAt    string
}

func seedHealthRow(t *testing.T, db *DB, s healthSeed) {
	t.Helper()
	owner, name := splitHealthFullName(s.fullName)
	_, err := db.SQL().Exec(`
		INSERT INTO repos (
			full_name, owner, name, status, excluded,
			force_category, primary_category, classified_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.fullName, owner, name, s.status, s.excluded,
		s.forceCategory, s.primaryCategory, s.classifiedAt,
	)
	if err != nil {
		t.Fatalf("seed %s: %v", s.fullName, err)
	}
}

func splitHealthFullName(fullName string) (string, string) {
	for i := 0; i < len(fullName); i++ {
		if fullName[i] == '/' {
			return fullName[:i], fullName[i+1:]
		}
	}
	return fullName, ""
}

// TestCountByStatus exercises the cycle-summary counter feeding ISI-775's
// log line and the OTel pending gauge. The contract is: non-excluded only,
// keyed by raw status string, missing buckets absent from the map.
func TestCountByStatus(t *testing.T) {
	db := mustOpen(t)

	got, err := db.CountByStatus()
	if err != nil {
		t.Fatalf("CountByStatus on empty DB: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty DB returned %d statuses, want 0", len(got))
	}

	seedHealthRow(t, db, healthSeed{fullName: "a/active1", status: "active"})
	seedHealthRow(t, db, healthSeed{fullName: "a/active2", status: "active"})
	seedHealthRow(t, db, healthSeed{fullName: "a/pending1", status: "pending"})
	seedHealthRow(t, db, healthSeed{fullName: "a/needsr1", status: "needs_review"})
	seedHealthRow(t, db, healthSeed{fullName: "a/needsre1", status: "needs_reclassify"})
	// Excluded rows must NOT contribute to any bucket: the cycle-summary line
	// is meant to surface drift in the active classifier population, not a
	// count operators have already chosen to ignore.
	seedHealthRow(t, db, healthSeed{fullName: "a/excluded1", status: "active", excluded: 1})

	got, err = db.CountByStatus()
	if err != nil {
		t.Fatalf("CountByStatus: %v", err)
	}
	if got["active"] != 2 {
		t.Errorf("active = %d, want 2", got["active"])
	}
	if got["pending"] != 1 {
		t.Errorf("pending = %d, want 1", got["pending"])
	}
	if got["needs_review"] != 1 {
		t.Errorf("needs_review = %d, want 1", got["needs_review"])
	}
	if got["needs_reclassify"] != 1 {
		t.Errorf("needs_reclassify = %d, want 1", got["needs_reclassify"])
	}
	if _, ok := got["bogus"]; ok {
		t.Error("missing bucket should be absent, not zero")
	}
}

// TestPendingCountsByDimension covers the (excluded, force_category_set)
// split that backs the radar.repos.pending gauge. Operators rely on the
// (false,false) bucket as the alert signal — that's the genuinely-stuck pile.
func TestPendingCountsByDimension(t *testing.T) {
	db := mustOpen(t)

	// (excluded=false, force_set=false) — the bucket that should alert at >5/24h.
	seedHealthRow(t, db, healthSeed{fullName: "a/p1", status: "pending"})
	seedHealthRow(t, db, healthSeed{fullName: "a/p2", status: "pending"})
	// (excluded=false, force_set=true) — config-managed, transient new-discovery.
	seedHealthRow(t, db, healthSeed{fullName: "b/p1", status: "pending", forceCategory: "observability"})
	// (excluded=true, force_set=false) — excluded but pending; should not alert.
	seedHealthRow(t, db, healthSeed{fullName: "c/p1", status: "pending", excluded: 1})
	// Active row must be ignored entirely.
	seedHealthRow(t, db, healthSeed{fullName: "d/active", status: "active"})

	buckets, err := db.PendingCountsByDimension()
	if err != nil {
		t.Fatalf("PendingCountsByDimension: %v", err)
	}
	// All four tuples must always be present so the downstream gauge has a
	// stable shape. Missing dimensions silently dropping out of dashboards
	// is part of the failure mode this metric exists to prevent.
	if len(buckets) != 4 {
		t.Fatalf("got %d buckets, want 4 (stable shape)", len(buckets))
	}

	want := map[[2]bool]int{
		{false, false}: 2,
		{false, true}:  1,
		{true, false}:  1,
		{true, true}:   0,
	}
	for _, b := range buckets {
		key := [2]bool{b.Excluded, b.ForceCategorySet}
		if w, ok := want[key]; !ok {
			t.Errorf("unexpected bucket %+v", b)
		} else if b.Count != w {
			t.Errorf("bucket excluded=%v force_set=%v count = %d, want %d",
				b.Excluded, b.ForceCategorySet, b.Count, w)
		}
	}
}

// TestPendingCountsByDimension_Empty: an empty DB must still emit all four
// buckets with Count=0 — otherwise the gauge would silently disappear from
// dashboards on a fresh deploy.
func TestPendingCountsByDimension_Empty(t *testing.T) {
	db := mustOpen(t)

	buckets, err := db.PendingCountsByDimension()
	if err != nil {
		t.Fatalf("PendingCountsByDimension: %v", err)
	}
	if len(buckets) != 4 {
		t.Fatalf("empty DB returned %d buckets, want 4", len(buckets))
	}
	for _, b := range buckets {
		if b.Count != 0 {
			t.Errorf("bucket excluded=%v force_set=%v has count %d, want 0",
				b.Excluded, b.ForceCategorySet, b.Count)
		}
	}
}

// TestLastClassifiedAt picks the most recent timestamp across non-excluded
// repos. Empty DB returns "" so callers can distinguish "never classified"
// from a real value without sentinel-string parsing.
func TestLastClassifiedAt(t *testing.T) {
	db := mustOpen(t)

	got, err := db.LastClassifiedAt()
	if err != nil {
		t.Fatalf("LastClassifiedAt on empty DB: %v", err)
	}
	if got != "" {
		t.Errorf("empty DB returned %q, want empty string", got)
	}

	seedHealthRow(t, db, healthSeed{fullName: "a/r1", status: "active", classifiedAt: "2026-04-20T00:00:00Z"})
	seedHealthRow(t, db, healthSeed{fullName: "a/r2", status: "active", classifiedAt: "2026-04-25T12:00:00Z"})
	// An excluded repo with a more recent timestamp must NOT win — operators
	// asked us to track classifier drift in the active pool only.
	seedHealthRow(t, db, healthSeed{fullName: "a/r3", status: "active", classifiedAt: "2099-01-01T00:00:00Z", excluded: 1})

	got, err = db.LastClassifiedAt()
	if err != nil {
		t.Fatalf("LastClassifiedAt: %v", err)
	}
	if got != "2026-04-25T12:00:00Z" {
		t.Errorf("LastClassifiedAt = %q, want 2026-04-25T12:00:00Z", got)
	}
}
