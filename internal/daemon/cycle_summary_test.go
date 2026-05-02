package daemon

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/logging"
)

// TestLogCycleSummary asserts the ISI-775 cycle-summary INFO line carries
// every counter operators rely on to detect a silent classifier failure.
//
// The 26-hour ISI-714 incident went undetected because the daemon emitted
// no end-of-cycle health summary at all — the only signal was a board
// complaint. This test pins the log shape so a future refactor can't
// accidentally drop a field and recreate that gap.
func TestLogCycleSummary(t *testing.T) {
	// Real on-disk SQLite (modernc.org/sqlite is happy with a temp file). We
	// want the actual schema + status accounting, not a mock — the helpers
	// the daemon calls (CountByStatus, LastClassifiedAt) are SQL.
	dbPath := t.TempDir() + "/scanner.db"
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Swap the package logger AFTER opening the DB — the schema-migration
	// path emits its own INFO lines, and we want the buffer to contain only
	// the one cycle-summary line we're asserting on.
	prev := logging.Logger
	t.Cleanup(func() { logging.Logger = prev })

	var buf bytes.Buffer
	logging.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	seedStatusRow(t, db, "active1", "active", 0, "", "2026-04-25T12:00:00Z")
	seedStatusRow(t, db, "active2", "active", 0, "", "2026-04-24T00:00:00Z")
	seedStatusRow(t, db, "pending1", "pending", 0, "", "")
	seedStatusRow(t, db, "needsr1", "needs_review", 0, "", "")
	seedStatusRow(t, db, "needsre1", "needs_reclassify", 0, "", "")
	// excluded must not contribute — confirms the "active classifier pool only"
	// rule the cycle summary documents.
	seedStatusRow(t, db, "ex1", "active", 1, "", "2099-01-01T00:00:00Z")

	d := &Daemon{
		db:                    db,
		mu:                    sync.RWMutex{},
		classificationLastErr: "ollama: connection refused",
	}

	d.logCycleSummary()

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("parsing log line %q: %v", buf.String(), err)
	}

	if entry["msg"] != "cycle summary" {
		t.Errorf("msg = %v, want %q", entry["msg"], "cycle summary")
	}

	wantInts := map[string]float64{
		"repos_total":            5,
		"repos_active":           2,
		"repos_pending":          1,
		"repos_needs_reclassify": 1,
		"repos_needs_review":     1,
	}
	for k, want := range wantInts {
		got, ok := entry[k].(float64)
		if !ok {
			t.Errorf("%s missing or not numeric: %v", k, entry[k])
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}

	if got := entry["last_classified_at"]; got != "2026-04-25T12:00:00Z" {
		t.Errorf("last_classified_at = %v, want 2026-04-25T12:00:00Z", got)
	}
	if got := entry["classification_last_error"]; got != "ollama: connection refused" {
		t.Errorf("classification_last_error = %v, want %q", got, "ollama: connection refused")
	}
}

// TestLogCycleSummary_NoDB exercises the branch where the database isn't
// available (Daemon.db == nil). Operators still need a summary line — the
// counters fall back to -1 sentinels so a missing value isn't confused with
// a real zero.
func TestLogCycleSummary_NoDB(t *testing.T) {
	prev := logging.Logger
	t.Cleanup(func() { logging.Logger = prev })

	var buf bytes.Buffer
	logging.Logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	d := &Daemon{}
	d.logCycleSummary()

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("parsing log line %q: %v", buf.String(), err)
	}

	for _, k := range []string{
		"repos_total",
		"repos_active",
		"repos_pending",
		"repos_needs_reclassify",
		"repos_needs_review",
	} {
		got, ok := entry[k].(float64)
		if !ok {
			t.Errorf("%s missing or not numeric: %v", k, entry[k])
			continue
		}
		if got != -1 {
			t.Errorf("%s = %v, want -1 sentinel when DB unavailable", k, got)
		}
	}
}

func seedStatusRow(t *testing.T, db *database.DB, slug, status string, excluded int, forceCategory, classifiedAt string) {
	t.Helper()
	_, err := db.SQL().Exec(`
		INSERT INTO repos (full_name, owner, name, status, excluded, force_category, classified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"acme/"+slug, "acme", slug, status, excluded, forceCategory, classifiedAt,
	)
	if err != nil {
		t.Fatalf("seed %s: %v", slug, err)
	}
}
