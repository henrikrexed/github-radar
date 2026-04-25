package audit

import (
	"context"
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestDB sets up a minimal in-memory schema matching the on-disk forward
// schema (post-T3 migration) so audit queries run against the real columns
// without dragging in the rest of the database/migrate machinery.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("opening sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE repos (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			full_name TEXT NOT NULL UNIQUE,
			owner TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			topics TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			primary_category TEXT NOT NULL DEFAULT '',
			category_confidence REAL NOT NULL DEFAULT 0,
			primary_subcategory TEXT NOT NULL DEFAULT '',
			is_curated_list INTEGER NOT NULL DEFAULT 0,
			needs_review INTEGER NOT NULL DEFAULT 0,
			excluded INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	return db
}

type seedRepo struct {
	full          string
	cat           string
	subcat        string
	topics        string
	conf          float64
	curated       int
	status        string // status='active' to be in scope
}

func seed(t *testing.T, db *sql.DB, rows []seedRepo) {
	t.Helper()
	for _, r := range rows {
		st := r.status
		if st == "" {
			st = "active"
		}
		_, err := db.Exec(`INSERT INTO repos (full_name, owner, name, primary_category, primary_subcategory, topics, category_confidence, is_curated_list, status) VALUES (?, '', '', ?, ?, ?, ?, ?, ?)`,
			r.full, r.cat, r.subcat, r.topics, r.conf, r.curated, st)
		if err != nil {
			t.Fatalf("seeding %s: %v", r.full, err)
		}
	}
}

// stubFiler is the in-memory Filer used by integration tests. It records
// every File() call so tests can assert on POST-body content (U5) and on
// dedup behavior (I2). On dedup queries it returns whatever createdAt the
// caller has staged via SeedExisting — that's the Layer-1 Q3 pattern.
type stubFiler struct {
	mu          sync.Mutex
	posted      []Proposal
	failNext    int        // count of POST attempts that should return ErrCode
	errCode     int
	errReason   string
	existing    map[string]time.Time // titlePrefix → createdAt for dedup
}

func newStubFiler() *stubFiler {
	return &stubFiler{existing: map[string]time.Time{}}
}

func (s *stubFiler) FindRecentDuplicate(ctx context.Context, titlePrefix string, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ts, ok := s.existing[titlePrefix]; ok {
		if now.Sub(ts) <= 60*24*time.Hour {
			return "ISI-DUP", nil
		}
	}
	return "", nil
}

func (s *stubFiler) File(ctx context.Context, p Proposal) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext > 0 {
		s.failNext--
		return "", &FileError{StatusCode: s.errCode, Reason: s.errReason}
	}
	s.posted = append(s.posted, p)
	// Once posted, mark as existing so a subsequent run in the same test dedups.
	s.existing[proposalTitlePrefix(p.Category, p.ProposedSub)] = time.Now()
	return "ISI-NEW", nil
}

// U1: a repo with topics=[] is excluded from clustering but counted in BOTH
// the numerator and denominator of the aggregate-share calc.
func TestAudit_U1_EmptyTopicsCountedInDenominator(t *testing.T) {
	db := newTestDB(t)
	// 100 active+non-curated repos in scope. 5 of them are 'other' with
	// empty topics. None form a cluster. Aggregate share = 5/100 = 5% > 3%.
	rows := []seedRepo{}
	for i := 0; i < 95; i++ {
		rows = append(rows, seedRepo{full: "x/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9})
	}
	for i := 0; i < 5; i++ {
		rows = append(rows, seedRepo{full: "x/empty-" + itoa(i), cat: "ai", subcat: "other", topics: "", conf: 0.8})
	}
	seed(t, db, rows)
	res, err := Run(context.Background(), Options{DB: db, DryRun: true, Now: fixedClock("2026-05-01")})
	if err != nil {
		t.Fatal(err)
	}
	if res.CorpusSize != 100 {
		t.Errorf("CorpusSize = %d, want 100 (all active+non-curated counted in denom)", res.CorpusSize)
	}
	if res.OtherCount != 5 {
		t.Errorf("OtherCount = %d, want 5 (empty-topic 'other' rows still counted in num)", res.OtherCount)
	}
	if !res.EscalateCritical {
		t.Errorf("aggregate share %.3f should escalate (>3%%)", res.AggregateShare)
	}
	// And empty-topic repos must produce NO clusters and NO auto-files.
	if len(res.AutoFiled) != 0 {
		t.Errorf("empty-topic repos should not cluster: AutoFiled=%v", res.AutoFiled)
	}
}

// U5: auto-file POST body content — repo list, token rationale, draft
// config-PR snippet (regex/golden-file assertion).
func TestAudit_U5_PostBodyContract(t *testing.T) {
	db := newTestDB(t)
	rows := []seedRepo{}
	for i := 0; i < 5; i++ {
		rows = append(rows, seedRepo{
			full: "ai/quantum-" + itoa(i), cat: "ai", subcat: "other",
			topics: "quantum-computing,variational-algorithms", conf: 0.85,
		})
	}
	// Pad the corpus to keep aggregate share below 3% so escalation doesn't
	// confound the assertion (5 of 100 active+non-curated = 5% would escalate;
	// we want this test to focus on the POST body shape only).
	for i := 0; i < 200; i++ {
		rows = append(rows, seedRepo{full: "x/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9})
	}
	seed(t, db, rows)

	stub := newStubFiler()
	res, err := Run(context.Background(), Options{DB: db, Filer: stub, Now: fixedClock("2026-05-01")})
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.posted) != 1 {
		t.Fatalf("want 1 POST, got %d (autofiled=%v)", len(stub.posted), res.AutoFiled)
	}
	body := buildProposalBody(stub.posted[0])
	for _, want := range []string{
		"## Auto-filed by github-radar audit",
		"**Category:** `ai`",
		"**Proposed subcategory:** `quantum-computing`",
		"### Repos",
		"### Token rationale",
		"### Draft config PR snippet",
		"```yaml",
		"categories:",
		"  ai:",
		"    subcategories:",
		"      quantum-computing:",
		"        topics:",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("POST body missing %q.\n--- body:\n%s", want, body)
		}
	}
	// Repo list must enumerate every clustered repo full_name.
	for i := 0; i < 5; i++ {
		fn := "ai/quantum-" + itoa(i)
		if !strings.Contains(body, fn) {
			t.Errorf("POST body missing repo %q", fn)
		}
	}
	// Plan §6 review U5: regex/golden-file. We pin the YAML snippet structure.
	yamlBlock := regexp.MustCompile("(?s)```yaml\\s*\\ncategories:\\s*\\n\\s+ai:\\s*\\n\\s+subcategories:\\s*\\n\\s+quantum-computing:\\s*\\n\\s+topics:\\s*\\n.*?```")
	if !yamlBlock.MatchString(body) {
		t.Errorf("POST body YAML snippet does not match expected shape:\n%s", body)
	}
}

// I1: golden-file test on rendered audits/<YYYY-MM>.md (headings, table cols,
// link format). We assert presence-and-shape rather than exact bytes so the
// test is robust to numeric formatting differences.
func TestAudit_I1_GoldenReportShape(t *testing.T) {
	db := newTestDB(t)
	// 100 active+non-curated, 4 'other' (4%) → escalation.
	rows := []seedRepo{}
	for i := 0; i < 96; i++ {
		rows = append(rows, seedRepo{full: "x/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9})
	}
	for i := 0; i < 4; i++ {
		rows = append(rows, seedRepo{full: "ai/other-" + itoa(i), cat: "ai", subcat: "other", topics: "novel-token", conf: 0.7})
	}
	seed(t, db, rows)
	res, err := Run(context.Background(), Options{DB: db, DryRun: true, Now: fixedClock("2026-05-01")})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path, err := PersistReport(res, dir)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if filepath.Base(path) != "2026-05.md" {
		t.Errorf("report basename = %s, want 2026-05.md", filepath.Base(path))
	}
	out := Render(res)
	for _, want := range []string{
		"# `<cat>/other` drift audit — 2026-05-01",
		"**Corpus size:** 100 repos",
		"**`<cat>/other` aggregate share:** 4.0% (4 repos) [FAIL >3% (escalate priority=critical)]",
		"## Per-category breakdown",
		"| category | other count | % of category | clusters ≥5 |",
		"## Graduation proposals auto-filed",
		"## Watch list",
		"@BigBoss",
		"priority=critical",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n--- report:\n%s", want, out)
		}
	}
}

// I2: end-to-end dedup. Run audit twice, second run posts zero new issues.
func TestAudit_I2_Dedup(t *testing.T) {
	db := newTestDB(t)
	rows := []seedRepo{}
	for i := 0; i < 5; i++ {
		rows = append(rows, seedRepo{full: "ai/q-" + itoa(i), cat: "ai", subcat: "other", topics: "quantum-computing", conf: 0.85})
	}
	for i := 0; i < 200; i++ {
		rows = append(rows, seedRepo{full: "x/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9})
	}
	seed(t, db, rows)

	stub := newStubFiler()
	first, err := Run(context.Background(), Options{DB: db, Filer: stub, Now: fixedClock("2026-05-01")})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.AutoFiled) != 1 {
		t.Fatalf("first run: want 1 autofile, got %d", len(first.AutoFiled))
	}
	if len(stub.posted) != 1 {
		t.Fatalf("stub posted = %d, want 1", len(stub.posted))
	}
	// Second run within dedup window: stub has the existing entry, must skip.
	second, err := Run(context.Background(), Options{DB: db, Filer: stub, Now: fixedClock("2026-05-15")})
	if err != nil {
		t.Fatal(err)
	}
	if len(stub.posted) != 1 {
		t.Errorf("dedup failure: stub posted = %d, want 1 (no new POST)", len(stub.posted))
	}
	if len(second.AutoFiled) != 0 {
		t.Errorf("second run AutoFiled = %d, want 0 (dedup'd)", len(second.AutoFiled))
	}
}

// I3: end-to-end escalation. Aggregate share at 4% → priority=critical
// marker emitted. Also pins plan §7 strict-`>` boundary: 3% does NOT escalate.
func TestAudit_I3_Escalation(t *testing.T) {
	cases := []struct {
		name           string
		corpus         int
		other          int
		wantEscalate   bool
	}{
		{"escalate_above_4pct", 100, 4, true},
		{"escalate_boundary_strict_3pct", 100, 3, false},
		{"escalate_below_2pct", 100, 2, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			rows := []seedRepo{}
			for i := 0; i < tc.corpus-tc.other; i++ {
				rows = append(rows, seedRepo{full: "x/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9})
			}
			for i := 0; i < tc.other; i++ {
				rows = append(rows, seedRepo{full: "ai/o-" + itoa(i), cat: "ai", subcat: "other", topics: "tok", conf: 0.7})
			}
			seed(t, db, rows)
			res, err := Run(context.Background(), Options{DB: db, DryRun: true, Now: fixedClock("2026-05-01")})
			if err != nil {
				t.Fatal(err)
			}
			if res.EscalateCritical != tc.wantEscalate {
				t.Errorf("EscalateCritical = %v, want %v (share=%.4f)", res.EscalateCritical, tc.wantEscalate, res.AggregateShare)
			}
			rendered := Render(res)
			marker := "priority=critical"
			has := strings.Contains(rendered, "> **priority=critical**")
			if has != tc.wantEscalate {
				t.Errorf("report contains %q = %v, want %v", marker, has, tc.wantEscalate)
			}
		})
	}
}

// I4: denominator scope — curated and inactive repos must be excluded from
// BOTH numerator AND denominator.
func TestAudit_I4_DenominatorScope(t *testing.T) {
	db := newTestDB(t)
	rows := []seedRepo{}
	// 100 active, non-curated, in-scope.
	for i := 0; i < 96; i++ {
		rows = append(rows, seedRepo{full: "x/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9})
	}
	for i := 0; i < 4; i++ {
		rows = append(rows, seedRepo{full: "ai/active-other-" + itoa(i), cat: "ai", subcat: "other", topics: "tok", conf: 0.7})
	}
	// 50 curated repos — must NOT count. 4 of them are 'other'.
	for i := 0; i < 46; i++ {
		rows = append(rows, seedRepo{full: "c/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9, curated: 1})
	}
	for i := 0; i < 4; i++ {
		rows = append(rows, seedRepo{full: "c/other-" + itoa(i), cat: "ai", subcat: "other", topics: "tok", conf: 0.7, curated: 1})
	}
	// 30 inactive (status='archived') — must NOT count. 5 of them 'other'.
	for i := 0; i < 25; i++ {
		rows = append(rows, seedRepo{full: "arc/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9, status: "archived"})
	}
	for i := 0; i < 5; i++ {
		rows = append(rows, seedRepo{full: "arc/other-" + itoa(i), cat: "ai", subcat: "other", topics: "tok", conf: 0.7, status: "archived"})
	}
	seed(t, db, rows)

	res, err := Run(context.Background(), Options{DB: db, DryRun: true, Now: fixedClock("2026-05-01")})
	if err != nil {
		t.Fatal(err)
	}
	if res.CorpusSize != 100 {
		t.Errorf("CorpusSize = %d, want 100 (curated+inactive excluded)", res.CorpusSize)
	}
	if res.OtherCount != 4 {
		t.Errorf("OtherCount = %d, want 4 (curated+inactive excluded from numerator)", res.OtherCount)
	}
	// 4 of 100 = 4.0% → escalates per plan §7. If curated/inactive 'other'
	// rows had wrongly leaked in we'd see OtherCount=13 above; this is a
	// belt-and-suspenders sanity check on the share calc.
	if !res.EscalateCritical {
		t.Errorf("4%% (4 of 100) should escalate per plan §7; got escalate=false (share=%.4f)", res.AggregateShare)
	}
}

// API-failure → degrade-to-watch (plan §6.1). Cluster qualifying for
// auto-file → API stub returns 503 twice (initial + retry both fail) → entry
// appears in Watch list, no exception, exit code 0.
func TestAudit_APIFailure_DegradeToWatch(t *testing.T) {
	db := newTestDB(t)
	rows := []seedRepo{}
	for i := 0; i < 5; i++ {
		rows = append(rows, seedRepo{full: "ai/q-" + itoa(i), cat: "ai", subcat: "other", topics: "quantum-computing", conf: 0.85})
	}
	for i := 0; i < 200; i++ {
		rows = append(rows, seedRepo{full: "x/" + itoa(i), cat: "ai", subcat: "computer-vision", conf: 0.9})
	}
	seed(t, db, rows)

	stub := newStubFiler()
	stub.failNext = 99 // every attempt fails
	stub.errCode = 503
	stub.errReason = "Service Unavailable"
	res, err := Run(context.Background(), Options{DB: db, Filer: stub, Now: fixedClock("2026-05-01")})
	if err != nil {
		t.Fatalf("audit run should not error on API failure (plan §6.1), got: %v", err)
	}
	if res.AutoFileFailures != 1 {
		t.Errorf("AutoFileFailures = %d, want 1", res.AutoFileFailures)
	}
	if len(res.WatchList) == 0 {
		t.Fatalf("expected a watch entry for failed auto-file")
	}
	w := res.WatchList[0]
	if !strings.Contains(w.Reason, "503") || !strings.Contains(w.Reason, "auto-file FAILED") || !strings.Contains(w.Reason, "file manually") {
		t.Errorf("watch reason = %q, want contains '503' + 'auto-file FAILED' + 'file manually'", w.Reason)
	}
}

// Helpers.

func fixedClock(yyyymmdd string) Clock {
	t, err := time.Parse("2006-01-02", yyyymmdd)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func itoa(i int) string {
	// tiny inline to avoid pulling strconv in test signatures
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}
