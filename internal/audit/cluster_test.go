package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fixturesDir is resolved relative to this test file (internal/audit) so
// `go test ./internal/audit/...` from the repo root works the same as
// from the package dir.
const fixturesDir = "../../tests/fixtures/clustering"

type fixtureRepo struct {
	FullName        string   `json:"full_name"`
	PrimaryCategory string   `json:"primary_category"`
	Topics          []string `json:"topics"`
	Confidence      float64  `json:"confidence"`
}

type fixtureExpected struct {
	Tokens                 []string `json:"tokens"`
	RepoCount              int      `json:"repo_count"`
	QualifiesForAutoFile   bool     `json:"qualifies_for_auto_file"`
}

type clusteringFixture struct {
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Input            []fixtureRepo     `json:"input"`
	ExpectedClusters []fixtureExpected `json:"expected_clusters"`
}

func loadFixture(t *testing.T, name string) clusteringFixture {
	t.Helper()
	p := filepath.Join(fixturesDir, name+".json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read fixture %s: %v", p, err)
	}
	var f clusteringFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", p, err)
	}
	if f.Name == "" {
		t.Fatalf("fixture %s missing name", p)
	}
	return f
}

func toRepos(fr []fixtureRepo) []Repo {
	out := make([]Repo, 0, len(fr))
	for _, r := range fr {
		out = append(out, Repo{
			FullName:        r.FullName,
			PrimaryCategory: r.PrimaryCategory,
			Topics:          r.Topics,
			Confidence:      r.Confidence,
		})
	}
	return out
}

// TestClusterRepos_Fixtures runs ClusterRepos against every JSON fixture
// in tests/fixtures/clustering and asserts shape + qualification flags.
//
// This covers the fixture matrix from T9.2 review (Q2): happy_two_clusters,
// jaccard_merge, jaccard_no_merge, score_below_threshold,
// score_at_threshold, empty_topics_fallback.
func TestClusterRepos_Fixtures(t *testing.T) {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("read fixtures dir: %v", err)
	}

	saw := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fixtureName := strings.TrimSuffix(e.Name(), ".json")
		t.Run(fixtureName, func(t *testing.T) {
			f := loadFixture(t, fixtureName)
			got := ClusterRepos(toRepos(f.Input))

			if len(got) != len(f.ExpectedClusters) {
				t.Fatalf("cluster count: got %d, want %d. got=%+v", len(got), len(f.ExpectedClusters), summary(got))
			}

			// Match by sorted Tokens because fixture order matches the
			// deterministic output order from ClusterRepos.
			for i, want := range f.ExpectedClusters {
				gotTokens := append([]string{}, got[i].Tokens...)
				sort.Strings(gotTokens)
				wantTokens := append([]string{}, want.Tokens...)
				sort.Strings(wantTokens)
				if !equalStringSlice(gotTokens, wantTokens) {
					t.Errorf("cluster[%d] tokens: got %v, want %v", i, gotTokens, wantTokens)
				}
				if len(got[i].Repos) != want.RepoCount {
					t.Errorf("cluster[%d] repo_count: got %d, want %d", i, len(got[i].Repos), want.RepoCount)
				}
				if q := got[i].QualifiesForAutoFile(); q != want.QualifiesForAutoFile {
					t.Errorf("cluster[%d] qualifies_for_auto_file: got %v, want %v (size=%d, score=%.3f, avg_conf=%.3f)",
						i, q, want.QualifiesForAutoFile, len(got[i].Repos), got[i].Score(), got[i].AvgConfidence())
				}
			}
		})
		saw++
	}

	// Spec from T9.2 review Q2 requires exactly 6 clustering fixtures.
	const expected = 6
	if saw != expected {
		t.Errorf("expected exactly %d clustering fixtures in %s, found %d", expected, fixturesDir, saw)
	}
}

// TestClusterRepos_U1_EmptyTopicsExcludedFromClustering pins the §3
// fallback: repos with empty topics never participate in clustering.
// The orchestrator handles their inclusion in aggregate share.
func TestClusterRepos_U1_EmptyTopicsExcludedFromClustering(t *testing.T) {
	repos := []Repo{
		{FullName: "a/1", Topics: []string{"x"}, Confidence: 0.8},
		{FullName: "a/2", Topics: []string{"x"}, Confidence: 0.8},
		{FullName: "a/3", Topics: []string{"x"}, Confidence: 0.8},
		{FullName: "a/4", Topics: []string{"x"}, Confidence: 0.8},
		{FullName: "a/5", Topics: []string{"x"}, Confidence: 0.8},
		// Two repos with empty topics MUST NOT lift any cluster size.
		{FullName: "a/6", Topics: nil, Confidence: 0.9},
		{FullName: "a/7", Topics: []string{}, Confidence: 0.9},
	}
	clusters := ClusterRepos(repos)
	if len(clusters) != 1 {
		t.Fatalf("expected exactly 1 cluster, got %d", len(clusters))
	}
	if got := len(clusters[0].Repos); got != 5 {
		t.Errorf("cluster[0] repo_count: got %d, want 5 (empty-topics repos must be excluded)", got)
	}
	for _, r := range clusters[0].Repos {
		if r.FullName == "a/6" || r.FullName == "a/7" {
			t.Errorf("empty-topics repo %s leaked into cluster", r.FullName)
		}
	}
}

// TestClusterRepos_U4_JaccardBoundary pins behavior at the Jaccard merge
// threshold (0.6). Both candidate clusters must have size ≥ MinClusterSize
// to even reach the merge step (per plan §3 step 3), so the realistic
// merge/no-merge integer cases at size 5 are J=4/6=0.667 (merge) and
// J=3/7=0.428 (no merge). The exact-0.6 boundary is exercised on the
// underlying jaccardRepoSet primitive where any sizes are allowed.
func TestClusterRepos_U4_JaccardBoundary(t *testing.T) {
	t.Run("below_threshold_no_merge", func(t *testing.T) {
		// Token x on {a1..a5}, token y on {a3, a4, a5, b1, b2}: |∩|=3, |∪|=7, J≈0.428.
		repos := []Repo{
			{FullName: "a1", Topics: []string{"x"}, Confidence: 0.8},
			{FullName: "a2", Topics: []string{"x"}, Confidence: 0.8},
			{FullName: "a3", Topics: []string{"x", "y"}, Confidence: 0.8},
			{FullName: "a4", Topics: []string{"x", "y"}, Confidence: 0.8},
			{FullName: "a5", Topics: []string{"x", "y"}, Confidence: 0.8},
			{FullName: "b1", Topics: []string{"y"}, Confidence: 0.8},
			{FullName: "b2", Topics: []string{"y"}, Confidence: 0.8},
		}
		got := ClusterRepos(repos)
		if len(got) != 2 {
			t.Errorf("J=0.428 < 0.6: expected 2 clusters (no merge), got %d. clusters=%v", len(got), summary(got))
		}
	})

	t.Run("above_threshold_merge", func(t *testing.T) {
		// Token x on {a1..a5}, token y on {a2..a5, b1}: |∩|=4, |∪|=6, J≈0.667 ≥ 0.6.
		repos := []Repo{
			{FullName: "a1", Topics: []string{"x"}, Confidence: 0.8},
			{FullName: "a2", Topics: []string{"x", "y"}, Confidence: 0.8},
			{FullName: "a3", Topics: []string{"x", "y"}, Confidence: 0.8},
			{FullName: "a4", Topics: []string{"x", "y"}, Confidence: 0.8},
			{FullName: "a5", Topics: []string{"x", "y"}, Confidence: 0.8},
			{FullName: "b1", Topics: []string{"y"}, Confidence: 0.8},
		}
		got := ClusterRepos(repos)
		if len(got) != 1 {
			t.Errorf("J=0.667 ≥ 0.6: expected 1 merged cluster, got %d. clusters=%v", len(got), summary(got))
		}
	})

	t.Run("primitive_at_exact_0.6_merges", func(t *testing.T) {
		// |A|=4, |B|=4, |A∩B|=3 → J = 3/(4+4-3) = 3/5 = 0.6 exactly.
		// Sizes are below MinClusterSize so this exercises the primitive
		// directly — the >= operator in mergeByJaccard treats 0.6 as a merge.
		A := []Repo{{FullName: "x1"}, {FullName: "x2"}, {FullName: "x3"}, {FullName: "x4"}}
		B := []Repo{{FullName: "x1"}, {FullName: "x2"}, {FullName: "x3"}, {FullName: "y1"}}
		j := jaccardRepoSet(A, B)
		if j != 0.6 {
			t.Fatalf("jaccardRepoSet boundary: got %v, want exactly 0.6", j)
		}
		if j < JaccardMergeThreshold {
			t.Errorf("J=0.6 must be >= JaccardMergeThreshold (0.6); >= is the merge operator")
		}
	})
}

// TestQualifiesForAutoFile_AvgConfidenceGate covers the U3 axis: cluster
// of 10 repos with avg confidence 0.5 — score = 5.0 ≥ 3.0 (passes), size
// = 10 ≥ 5 (passes), but avg confidence < 0.6 (fails) → MUST NOT auto-file.
func TestQualifiesForAutoFile_AvgConfidenceGate(t *testing.T) {
	repos := make([]Repo, 10)
	for i := range repos {
		repos[i] = Repo{FullName: "r" + string(rune('0'+i)), Confidence: 0.5}
	}
	c := Cluster{Tokens: []string{"x"}, Repos: repos}
	if c.Score() < MinClusterScore {
		t.Fatalf("test setup: expected score ≥ %.1f, got %.3f", MinClusterScore, c.Score())
	}
	if c.QualifiesForAutoFile() {
		t.Errorf("cluster with avg_confidence=0.5 must NOT qualify for auto-file (gate is %.2f)", MinAvgConfidence)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func summary(cs []Cluster) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = strings.Join(c.Tokens, ",") + "(" + itoa(len(c.Repos)) + ")"
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
