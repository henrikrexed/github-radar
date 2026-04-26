package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hrexed/github-radar/internal/database"
)

// TestEffectiveDryRun covers the full truth table for the global-OR-local
// helper (see flags.go and ISI-774 for the footgun this defends against).
// The asymmetric cases — one source set, the other not — are the
// regression cases for the original bug where the global --dry-run was
// silently dropped because only the subcommand-local flag was inspected.
func TestEffectiveDryRun(t *testing.T) {
	cases := []struct {
		name   string
		global bool
		local  bool
		want   bool
	}{
		{"both off", false, false, false},
		{"global only", true, false, true},
		{"local only", false, true, true},
		{"both on", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &CLI{DryRun: tc.global}
			if got := effectiveDryRun(c, tc.local); got != tc.want {
				t.Errorf("effectiveDryRun(global=%v, local=%v) = %v, want %v",
					tc.global, tc.local, got, tc.want)
			}
		})
	}
}

// TestEffectiveDryRun_NilCLI is a defensive check: the helper should not
// panic if called before a CLI is fully wired (e.g. by a future unit test
// that constructs a subcommand directly without the dispatch wrapper).
func TestEffectiveDryRun_NilCLI(t *testing.T) {
	if got := effectiveDryRun(nil, true); !got {
		t.Errorf("effectiveDryRun(nil, true) = false, want true")
	}
	if got := effectiveDryRun(nil, false); got {
		t.Errorf("effectiveDryRun(nil, false) = true, want false")
	}
}

// withTempDefaultDB redirects database.DefaultDBPath to a fresh per-test
// SQLite file so the admin handler's database.Open("") call resolves to
// an isolated fixture instead of the operator's real scanner DB. The
// previous value is restored on test cleanup. Open() is called inside
// the handler, not at init time, so overriding the package var is safe.
func withTempDefaultDB(t *testing.T) string {
	t.Helper()
	orig := database.DefaultDBPath
	dbPath := filepath.Join(t.TempDir(), "scanner.db")
	database.DefaultDBPath = dbPath
	t.Cleanup(func() { database.DefaultDBPath = orig })
	return dbPath
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// what was written. The admin handler's printDrainReport writes to stdout,
// so this is how the dispatch tests assert which print branch was taken.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String()
}

// runAdminViaCLI exercises the full dispatch path the production binary
// uses: CLI.runCommand -> extractGlobalFlags -> AdminCmd.Run -> action.
// Going through runCommand — rather than calling AdminCmd.Run directly —
// is what makes the global-flag-after-subcommand case meaningful, because
// extractGlobalFlags is the function that consumes --dry-run before the
// subcommand FlagSet sees it. That is the exact dispatch shape that
// produced the ISI-774 footgun.
func runAdminViaCLI(t *testing.T, args []string) (int, string) {
	t.Helper()
	c := New()
	var rc int
	out := captureStdout(t, func() {
		rc = c.runCommand("admin", args)
	})
	return rc, out
}

// seedDrainable inserts n rows in `needs_reclassify` with a deterministic
// (category, subcategory) pair that the drain predicate will release.
// Using raw SQL mirrors the seed pattern from drain_test.go because the
// public UpsertRepo helper does not expose primary_subcategory or
// needs_review, and the drain predicate reads those columns.
func seedDrainable(t *testing.T, dbPath string, n int) {
	t.Helper()
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	defer db.Close()
	for i := 0; i < n; i++ {
		full := fakeFullName(i)
		if _, err := db.SQL().Exec(`
			INSERT INTO repos (
				full_name, owner, name, status, excluded,
				primary_category, primary_subcategory, primary_category_legacy,
				category_confidence, classified_at, needs_review
			) VALUES (?, ?, ?, 'needs_reclassify', 0,
				'ai', 'agents', 'ai-agents',
				0.95, '2026-04-20T00:00:00Z', 0)
		`, full, "acme", "drainable-"+itoa(i)); err != nil {
			t.Fatalf("seed row %d: %v", i, err)
		}
	}
}

// itoa is a tiny no-import helper for constructing seed names. strconv
// would also work; this keeps the test file self-contained for the small
// number of integers we ever stringify here.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func fakeFullName(i int) string { return "acme/drainable-" + itoa(i) }

// TestAdminDrain_GlobalDryRun: `admin --dry-run drain-needs-reclassify`.
// The global flag sits between the subcommand name and the action, so it
// is consumed by extractGlobalFlags. The dry-run path must still be taken
// — this is the regression case for the ISI-774 footgun.
func TestAdminDrain_GlobalDryRun(t *testing.T) {
	withTempDefaultDB(t)

	rc, out := runAdminViaCLI(t, []string{"--dry-run", "drain-needs-reclassify"})
	if rc != 0 {
		t.Fatalf("exit code = %d, want 0; out=%q", rc, out)
	}
	if !strings.Contains(out, "Drain plan (dry-run") {
		t.Errorf("expected dry-run preview header in output, got:\n%s", out)
	}
	// Real-run prefix must NOT appear.
	if strings.Contains(out, "Drained ") {
		t.Errorf("output should not contain real-run 'Drained ...' line on dry-run, got:\n%s", out)
	}
}

// TestAdminDrain_LocalDryRun: `admin drain-needs-reclassify --dry-run`.
// The local flag is parsed by the subcommand's own FlagSet, so this case
// passes even on the pre-fix code; we keep the test to lock in the
// behavior so a future refactor of the FlagSet wiring cannot regress it.
func TestAdminDrain_LocalDryRun(t *testing.T) {
	withTempDefaultDB(t)

	rc, out := runAdminViaCLI(t, []string{"drain-needs-reclassify", "--dry-run"})
	if rc != 0 {
		t.Fatalf("exit code = %d, want 0; out=%q", rc, out)
	}
	if !strings.Contains(out, "Drain plan (dry-run") {
		t.Errorf("expected dry-run preview header in output, got:\n%s", out)
	}
}

// TestAdminDrain_NoFlag: `admin drain-needs-reclassify` (no flag) must
// NOT take the dry-run path. If a future change accidentally inverts the
// global-OR-local helper, this test catches it before the binary ships
// — exactly the gap that caused the unintended live drain in ISI-774.
func TestAdminDrain_NoFlag(t *testing.T) {
	withTempDefaultDB(t)

	rc, out := runAdminViaCLI(t, []string{"drain-needs-reclassify"})
	if rc != 0 {
		t.Fatalf("exit code = %d, want 0; out=%q", rc, out)
	}
	if strings.Contains(out, "(dry-run") {
		t.Errorf("output should not contain dry-run marker, got:\n%s", out)
	}
	// Real-run signature: the handler prints "Drained N repos ... -> active."
	if !strings.Contains(out, "Drained ") {
		t.Errorf("expected real-run 'Drained' line in output, got:\n%s", out)
	}
}

// TestAdminDrain_LimitForwarded: `--limit N` must reach
// DrainNeedsReclassify. We seed five drainable rows and request a limit
// of two; the report should show exactly two drained, proving the flag
// value flowed through the dispatch path into the database call.
func TestAdminDrain_LimitForwarded(t *testing.T) {
	dbPath := withTempDefaultDB(t)
	seedDrainable(t, dbPath, 5)

	rc, out := runAdminViaCLI(t, []string{"drain-needs-reclassify", "--limit", "2"})
	if rc != 0 {
		t.Fatalf("exit code = %d, want 0; out=%q", rc, out)
	}
	if !strings.Contains(out, "Drained 2 repos from needs_reclassify -> active.") {
		t.Errorf("expected 'Drained 2 repos' (limit forwarded), got:\n%s", out)
	}
}

// TestAdminDrain_LimitDefaultZero: omitting --limit drains everything
// available. With five seeded rows and no cap, the report should show
// five drained — confirming the default-zero (no-cap) semantics survive
// the dispatch path unchanged.
func TestAdminDrain_LimitDefaultZero(t *testing.T) {
	dbPath := withTempDefaultDB(t)
	seedDrainable(t, dbPath, 5)

	rc, out := runAdminViaCLI(t, []string{"drain-needs-reclassify"})
	if rc != 0 {
		t.Fatalf("exit code = %d, want 0; out=%q", rc, out)
	}
	if !strings.Contains(out, "Drained 5 repos from needs_reclassify -> active.") {
		t.Errorf("expected 'Drained 5 repos' (no cap), got:\n%s", out)
	}
}

// TestAdminCmd_NoArgs: `admin` with no action exits non-zero and prints
// usage. Locks in the dispatch shape so adding a new admin action does
// not regress the help path.
func TestAdminCmd_NoArgs(t *testing.T) {
	cli := New()
	cmd := NewAdminCmd(cli)

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	rc := cmd.Run(nil)
	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if rc != 1 {
		t.Errorf("exit code = %d, want 1", rc)
	}
	if !strings.Contains(buf.String(), "drain-needs-reclassify") {
		t.Errorf("usage should mention drain-needs-reclassify, got:\n%s", buf.String())
	}
}

// TestAdminCmd_UnknownAction: an unknown admin action exits non-zero
// with a clear error message naming the offending action.
func TestAdminCmd_UnknownAction(t *testing.T) {
	cli := New()
	cmd := NewAdminCmd(cli)

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	rc := cmd.Run([]string{"bogus-action"})
	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if rc != 1 {
		t.Errorf("exit code = %d, want 1", rc)
	}
	if !strings.Contains(buf.String(), "Unknown admin action") {
		t.Errorf("expected 'Unknown admin action' in stderr, got:\n%s", buf.String())
	}
}
