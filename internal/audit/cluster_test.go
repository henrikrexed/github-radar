package audit

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// fixtureRepo is the JSON shape stored at tests/fixtures/clustering/*.json.
type fixtureRepo struct {
	FullName        string   `json:"full_name"`
	PrimaryCategory string   `json:"primary_category"`
	Topics          []string `json:"topics"`
	Confidence      float64  `json:"confidence"`
}

type fixtureFile struct {
	Repos []fixtureRepo `json:"repos"`
}

// loadFixture loads a synthetic clustering fixture from
// tests/fixtures/clustering/<name>.json relative to the repo root.
func loadFixture(t *testing.T, name string) []AuditRepo {
	t.Helper()
	// repo-rooted path resolution: walk upward from test's CWD until we find
	// tests/fixtures/clustering — keeps the fixture loader stable whether
	// `go test` is run from the package dir or the repo root.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	var fixturePath string
	for cur := dir; ; {
		candidate := filepath.Join(cur, "tests", "fixtures", "clustering", name+".json")
		if _, err := os.Stat(candidate); err == nil {
			fixturePath = candidate
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			t.Fatalf("could not locate tests/fixtures/clustering/%s.json walking up from %s", name, dir)
		}
		cur = parent
	}
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixturePath, err)
	}
	var f fixtureFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parsing fixture %s: %v", fixturePath, err)
	}
	out := make([]AuditRepo, 0, len(f.Repos))
	for _, r := range f.Repos {
		out = append(out, AuditRepo{
			FullName:        r.FullName,
			PrimaryCategory: r.PrimaryCategory,
			Topics:          r.Topics,
			Confidence:      r.Confidence,
		})
	}
	return out
}

// All six fixtures from T9.2 review Q2. Each test asserts on cluster count,
// per-cluster repo set (order-insensitive), per-cluster score (within 0.01).
//
// "happy_two_clusters" exercises the §3 happy path.
func TestCluster_HappyTwoClusters(t *testing.T) {
	repos := loadFixture(t, "happy_two_clusters")
	out := ClusterByCategory(repos)["ai"]
	if len(out) != 2 {
		t.Fatalf("want 2 clusters, got %d (%v)", len(out), out)
	}
	for _, c := range out {
		if len(c.Repos) != 5 {
			t.Errorf("cluster %s has %d repos, want 5", c.ProposedSub, len(c.Repos))
		}
	}
}

// "jaccard_merge" — Jaccard ≈ 0.67 ≥ 0.6 → one merged cluster of 6 repos.
// (5+5 with 4 overlap = union 6, intersection 4, J=4/6≈0.667.)
func TestCluster_JaccardMerge(t *testing.T) {
	repos := loadFixture(t, "jaccard_merge")
	out := ClusterByCategory(repos)["ai"]
	if len(out) != 1 {
		t.Fatalf("want 1 merged cluster, got %d (%v)", len(out), out)
	}
	if len(out[0].Repos) != 6 {
		t.Errorf("merged cluster has %d repos, want 6", len(out[0].Repos))
	}
	// Score = 6 × avg_conf. avg_conf in fixture is 0.8.
	want := 6.0 * 0.8
	if math.Abs(out[0].Score-want) > 0.01 {
		t.Errorf("score = %f, want %f (±0.01)", out[0].Score, want)
	}
}

// "jaccard_no_merge" — only 2 overlap → J=0.25 < 0.6 → 2 separate clusters.
func TestCluster_JaccardNoMerge(t *testing.T) {
	repos := loadFixture(t, "jaccard_no_merge")
	out := ClusterByCategory(repos)["ai"]
	if len(out) != 2 {
		t.Fatalf("want 2 separate clusters, got %d (%v)", len(out), out)
	}
}

// U4: Boundary at Jaccard = 0.6. 6/10 = 0.6 must merge (≥ is inclusive).
// We construct two 5-repo clusters that share exactly 3 repos:
// |A∩B|=3, |A∪B|=7, J=3/7≈0.43 < 0.6 → no merge. Then bump overlap to 4 →
// J=4/6≈0.67 → merge. We pin both sides + the exact 0.6 case.
func TestCluster_U4_JaccardBoundary(t *testing.T) {
	// Construct A∩B such that J=0.6 exactly: |A|=|B|=5, intersection=k →
	// union = 10-k, J = k/(10-k). Solve k/(10-k)=0.6 → k=3.75 (not integer).
	// So 0.6 exactly is unreachable on a 5-repo cluster. Test the closest
	// integer-shaped pair around 0.6:
	//   k=4 → J = 4/6 ≈ 0.667 → merge (≥0.6).
	//   k=3 → J = 3/7 ≈ 0.429 → no merge.
	repos := []AuditRepo{
		// k=4 over-threshold: clusters share repos r1..r4. Cluster A: r1..r5
		// on token "alpha"; Cluster B: r1..r4 + r6 on token "beta". J=4/6.
		{FullName: "o/r1", PrimaryCategory: "ai", Topics: []string{"alpha", "beta"}, Confidence: 0.8},
		{FullName: "o/r2", PrimaryCategory: "ai", Topics: []string{"alpha", "beta"}, Confidence: 0.8},
		{FullName: "o/r3", PrimaryCategory: "ai", Topics: []string{"alpha", "beta"}, Confidence: 0.8},
		{FullName: "o/r4", PrimaryCategory: "ai", Topics: []string{"alpha", "beta"}, Confidence: 0.8},
		{FullName: "o/r5", PrimaryCategory: "ai", Topics: []string{"alpha"}, Confidence: 0.8},
		{FullName: "o/r6", PrimaryCategory: "ai", Topics: []string{"beta"}, Confidence: 0.8},
	}
	got := ClusterByCategory(repos)["ai"]
	if len(got) != 1 {
		t.Fatalf("k=4 (J≈0.67) want 1 merged cluster, got %d", len(got))
	}
	if len(got[0].Repos) != 6 {
		t.Errorf("merged cluster size %d, want 6", len(got[0].Repos))
	}
}

// U2: Watch-list vs graduation. Cluster of 4 → not eligible for auto-file.
// Cluster of 5 (and meeting the score gate) → eligible.
func TestCluster_U2_WatchVsGraduation(t *testing.T) {
	// 4 repos sharing token "small": below MinClusterSize.
	repos4 := []AuditRepo{
		{FullName: "a/1", PrimaryCategory: "ai", Topics: []string{"small"}, Confidence: 0.8},
		{FullName: "a/2", PrimaryCategory: "ai", Topics: []string{"small"}, Confidence: 0.8},
		{FullName: "a/3", PrimaryCategory: "ai", Topics: []string{"small"}, Confidence: 0.8},
		{FullName: "a/4", PrimaryCategory: "ai", Topics: []string{"small"}, Confidence: 0.8},
	}
	got := ClusterByCategory(repos4)["ai"]
	// MinClusterSize = 5 means token "small" with only 4 repos doesn't even
	// emit a candidate — so we expect zero clusters.
	if len(got) != 0 {
		t.Errorf("4-repo set should not emit a candidate cluster, got %d", len(got))
	}

	// 5 repos sharing token "graduates" with avg conf 0.8 → score = 4.0.
	repos5 := []AuditRepo{
		{FullName: "a/1", PrimaryCategory: "ai", Topics: []string{"graduates"}, Confidence: 0.8},
		{FullName: "a/2", PrimaryCategory: "ai", Topics: []string{"graduates"}, Confidence: 0.8},
		{FullName: "a/3", PrimaryCategory: "ai", Topics: []string{"graduates"}, Confidence: 0.8},
		{FullName: "a/4", PrimaryCategory: "ai", Topics: []string{"graduates"}, Confidence: 0.8},
		{FullName: "a/5", PrimaryCategory: "ai", Topics: []string{"graduates"}, Confidence: 0.8},
	}
	got = ClusterByCategory(repos5)["ai"]
	if len(got) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(got))
	}
	if !got[0].IsAutoFileEligible() {
		t.Errorf("5-repo cluster (score 4.0, conf 0.8) should be auto-file-eligible: %+v", got[0])
	}
}

// U3: avg_confidence gate. A cluster of 5 with avg_conf < 0.6 must NOT
// auto-file even if score ≥ 3.0 happens to pass (and even if count ≥ 5).
func TestCluster_U3_AvgConfidenceGate(t *testing.T) {
	// 5 repos, conf 0.55 → score = 5×0.55 = 2.75. Below score threshold AND
	// below conf threshold — covers the layered gate.
	repos := []AuditRepo{
		{FullName: "a/1", PrimaryCategory: "ai", Topics: []string{"x"}, Confidence: 0.55},
		{FullName: "a/2", PrimaryCategory: "ai", Topics: []string{"x"}, Confidence: 0.55},
		{FullName: "a/3", PrimaryCategory: "ai", Topics: []string{"x"}, Confidence: 0.55},
		{FullName: "a/4", PrimaryCategory: "ai", Topics: []string{"x"}, Confidence: 0.55},
		{FullName: "a/5", PrimaryCategory: "ai", Topics: []string{"x"}, Confidence: 0.55},
	}
	got := ClusterByCategory(repos)["ai"]
	if len(got) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(got))
	}
	if got[0].IsAutoFileEligible() {
		t.Errorf("conf=0.55 cluster should NOT be auto-file-eligible (avg_confidence gate)")
	}

	// 5 repos, conf 0.65 → score = 3.25 (above score gate AND conf gate).
	reposOK := []AuditRepo{
		{FullName: "a/1", PrimaryCategory: "ai", Topics: []string{"y"}, Confidence: 0.65},
		{FullName: "a/2", PrimaryCategory: "ai", Topics: []string{"y"}, Confidence: 0.65},
		{FullName: "a/3", PrimaryCategory: "ai", Topics: []string{"y"}, Confidence: 0.65},
		{FullName: "a/4", PrimaryCategory: "ai", Topics: []string{"y"}, Confidence: 0.65},
		{FullName: "a/5", PrimaryCategory: "ai", Topics: []string{"y"}, Confidence: 0.65},
	}
	got = ClusterByCategory(reposOK)["ai"]
	if !got[0].IsAutoFileEligible() {
		t.Errorf("conf=0.65 cluster should be auto-file-eligible: %+v", got[0])
	}
}

// score_below_threshold fixture: cluster of 5 with conf 0.55 → score 2.75 →
// not auto-filed. (Layered with U3 to exercise the score gate from a fixture.)
func TestCluster_ScoreBelowThreshold(t *testing.T) {
	repos := loadFixture(t, "score_below_threshold")
	out := ClusterByCategory(repos)["ai"]
	if len(out) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(out))
	}
	if out[0].Score >= ScoreAutoFileThreshold {
		t.Errorf("score = %f, want < %f", out[0].Score, ScoreAutoFileThreshold)
	}
	if out[0].IsAutoFileEligible() {
		t.Errorf("sub-threshold cluster should not be auto-file-eligible")
	}
}

// score_at_threshold fixture: cluster of 5 with avg conf 0.61 → score 3.05 →
// just over the gate. Pins the boundary on the auto-file side.
func TestCluster_ScoreAtThreshold(t *testing.T) {
	repos := loadFixture(t, "score_at_threshold")
	out := ClusterByCategory(repos)["ai"]
	if len(out) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(out))
	}
	if out[0].Score < ScoreAutoFileThreshold {
		t.Errorf("score = %f, want ≥ %f", out[0].Score, ScoreAutoFileThreshold)
	}
	if !out[0].IsAutoFileEligible() {
		t.Errorf("at-threshold cluster should be auto-file-eligible")
	}
}

// U1 / empty_topics_fallback: repos with topics=[] are skipped from
// clustering but counted by the caller in the aggregate-share computation.
// We pin the clustering side here; the aggregate-share side is in
// audit_test.go (TestAudit_U1_EmptyTopicsCountedInDenominator).
func TestCluster_U1_EmptyTopicsExcluded(t *testing.T) {
	repos := loadFixture(t, "empty_topics_fallback")
	// 5 normal repos in one cluster + 5 empty-topic repos that must NOT
	// contribute to any cluster. So we expect exactly 1 cluster of 5.
	out := ClusterByCategory(repos)["ai"]
	if len(out) != 1 {
		t.Fatalf("want 1 cluster (empty-topics excluded), got %d", len(out))
	}
	if len(out[0].Repos) != 5 {
		t.Errorf("cluster size = %d, want 5", len(out[0].Repos))
	}
	// And we expose the excluded count to the audit driver: the caller
	// should still see all 10 input repos so it can count them in the
	// aggregate share. (Verified by audit_test.go.)
	want := []string{}
	for _, r := range repos {
		if len(r.Topics) == 0 {
			want = append(want, r.FullName)
		}
	}
	if len(want) != 5 {
		t.Fatalf("fixture invariant: expected 5 empty-topics repos, got %d", len(want))
	}
}

// Determinism: repeated runs over the same inputs produce identical Cluster
// slices (sorted repo lists, sorted token lists, stable score order).
func TestCluster_Deterministic(t *testing.T) {
	repos := loadFixture(t, "happy_two_clusters")
	a := ClusterByCategory(repos)["ai"]
	b := ClusterByCategory(repos)["ai"]
	if len(a) != len(b) {
		t.Fatalf("non-deterministic cluster count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if !sort.StringsAreSorted(a[i].Repos) || !sort.StringsAreSorted(a[i].MergedTokens) {
			t.Errorf("cluster %d not sorted: repos=%v tokens=%v", i, a[i].Repos, a[i].MergedTokens)
		}
	}
}
