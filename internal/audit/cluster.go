// Package audit implements the monthly `<cat>/other` drift audit per T9 plan
// (ISI-720). It scans the scanner.db for repos in `<cat>/other` buckets,
// clusters them by topic overlap, emits a markdown report, and (optionally)
// auto-files subcategory-graduation proposal issues via the Paperclip API.
package audit

import (
	"sort"
	"strings"
)

// AuditRepo is the projection of a repos row that the audit needs.
//
// Field name maps to plan §2 query columns. classification_confidence in the
// plan corresponds to the actual `category_confidence` column on disk; we
// keep the plan's external semantics but use the on-disk field name in code.
type AuditRepo struct {
	FullName        string
	PrimaryCategory string
	Topics          []string
	Description     string
	Confidence      float64
}

// Cluster is a candidate subcategory-graduation proposal.
type Cluster struct {
	Category      string   // primary_category bucket (e.g. "ai")
	ProposedSub   string   // proposed subcategory token (e.g. "quantum-computing")
	Repos         []string // sorted repo full_names
	MergedTokens  []string // every token that contributed to the cluster (sorted, deduped)
	AvgConfidence float64
	Score         float64 // RepoCount × AvgConfidence
}

// Plan §3 thresholds — pinned here as named constants so tests can pin behavior.
const (
	// MinClusterSize is the minimum repo count for a token to emit a candidate
	// cluster (plan §3 step 3: "any token with ≥5 repos behind it").
	MinClusterSize = 5
	// JaccardMergeThreshold is the Jaccard ≥ this on repo sets that triggers a
	// merge of two candidate clusters (plan §3 step 4). Boundary is inclusive.
	JaccardMergeThreshold = 0.6
	// ScoreAutoFileThreshold gates auto-file vs watch-list behavior (plan §3
	// step 5: "Clusters below score=3.0 are surfaced as 'watch' but not
	// auto-filed"). Boundary is inclusive — score≥3.0 auto-files.
	ScoreAutoFileThreshold = 3.0
	// MinAvgConfidence is the second auto-file gate (plan §6 wording: "≥5
	// repos with avg_confidence ≥ 0.6"). Plan §6 boundary is inclusive.
	MinAvgConfidence = 0.6
)

// ClusterByCategory groups repos by primary_category and runs the
// topic-overlap clustering algorithm per plan §3 inside each bucket.
//
// Repos with empty Topics are excluded from clustering (plan §3 fallback)
// but the caller is expected to count them in the aggregate-share denom/num —
// this function deliberately does not see them.
func ClusterByCategory(repos []AuditRepo) map[string][]Cluster {
	byCat := map[string][]AuditRepo{}
	for _, r := range repos {
		if len(r.Topics) == 0 {
			continue // empty-topics fallback (plan §3)
		}
		byCat[r.PrimaryCategory] = append(byCat[r.PrimaryCategory], r)
	}
	out := map[string][]Cluster{}
	for cat, list := range byCat {
		out[cat] = clusterBucket(cat, list)
	}
	return out
}

func clusterBucket(category string, repos []AuditRepo) []Cluster {
	// Step 1+2: token → repos inverted index. Use a map[token] → set-of-repos
	// keyed by FullName so dedup of repeated topics is automatic.
	repoIdx := map[string]int{}
	for i, r := range repos {
		repoIdx[r.FullName] = i
	}
	tokenRepos := map[string]map[string]struct{}{}
	for _, r := range repos {
		for _, t := range normalizeTokens(r.Topics) {
			if _, ok := tokenRepos[t]; !ok {
				tokenRepos[t] = map[string]struct{}{}
			}
			tokenRepos[t][r.FullName] = struct{}{}
		}
	}

	// Step 3: emit candidate clusters for any token with ≥ MinClusterSize repos.
	type candidate struct {
		token string
		repos map[string]struct{}
	}
	var candidates []candidate
	for tok, set := range tokenRepos {
		if len(set) >= MinClusterSize {
			candidates = append(candidates, candidate{token: tok, repos: set})
		}
	}
	// Stable order before merge so the algorithm is deterministic across runs.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].token < candidates[j].token })

	// Step 4: merge overlapping clusters with Jaccard ≥ threshold. Iterate
	// pair-wise; on merge, restart so transitive merges are picked up.
	merged := make([]struct {
		tokens map[string]struct{}
		repos  map[string]struct{}
	}, len(candidates))
	for i, c := range candidates {
		merged[i].tokens = map[string]struct{}{c.token: {}}
		merged[i].repos = cloneSet(c.repos)
	}
	for {
		didMerge := false
		for i := 0; i < len(merged); i++ {
			for j := i + 1; j < len(merged); j++ {
				if jaccard(merged[i].repos, merged[j].repos) >= JaccardMergeThreshold {
					for r := range merged[j].repos {
						merged[i].repos[r] = struct{}{}
					}
					for t := range merged[j].tokens {
						merged[i].tokens[t] = struct{}{}
					}
					merged = append(merged[:j], merged[j+1:]...)
					didMerge = true
					break
				}
			}
			if didMerge {
				break
			}
		}
		if !didMerge {
			break
		}
	}

	// Step 5: score = repo_count × avg_confidence. Build typed Clusters.
	clusters := make([]Cluster, 0, len(merged))
	for _, m := range merged {
		repoNames := setToSorted(m.repos)
		var sumConf float64
		for _, name := range repoNames {
			if idx, ok := repoIdx[name]; ok {
				sumConf += repos[idx].Confidence
			}
		}
		avg := sumConf / float64(len(repoNames))
		// Pick the proposed-subcategory token: the lex-smallest token in the
		// merged set that actually appears in every repo of the cluster, or
		// fall back to the lex-smallest merged token otherwise. This gives a
		// stable, explainable name without ML.
		tokens := setToSorted(m.tokens)
		clusters = append(clusters, Cluster{
			Category:      category,
			ProposedSub:   pickProposedSub(repos, repoNames, tokens),
			Repos:         repoNames,
			MergedTokens:  tokens,
			AvgConfidence: avg,
			Score:         float64(len(repoNames)) * avg,
		})
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Score != clusters[j].Score {
			return clusters[i].Score > clusters[j].Score
		}
		return clusters[i].ProposedSub < clusters[j].ProposedSub
	})
	return clusters
}

// pickProposedSub picks the most-shared token across the cluster as the
// proposed subcategory name. Prefers tokens shared by every repo; falls back
// to highest-coverage; ties broken lexicographically for determinism.
func pickProposedSub(allRepos []AuditRepo, clusterRepoNames []string, mergedTokens []string) string {
	if len(mergedTokens) == 0 {
		return ""
	}
	repoIdx := map[string]int{}
	for i, r := range allRepos {
		repoIdx[r.FullName] = i
	}
	type tokScore struct {
		tok   string
		count int
	}
	var scored []tokScore
	for _, t := range mergedTokens {
		c := 0
		for _, name := range clusterRepoNames {
			r := allRepos[repoIdx[name]]
			for _, rt := range normalizeTokens(r.Topics) {
				if rt == t {
					c++
					break
				}
			}
		}
		scored = append(scored, tokScore{tok: t, count: c})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].count != scored[j].count {
			return scored[i].count > scored[j].count
		}
		return scored[i].tok < scored[j].tok
	})
	return scored[0].tok
}

// IsAutoFileEligible returns true when the cluster meets BOTH gates from
// plan §6 (count ≥ MinClusterSize, avg_confidence ≥ MinAvgConfidence) AND
// the score gate from plan §3 step 5 (score ≥ ScoreAutoFileThreshold).
// All three boundaries are inclusive.
func (c Cluster) IsAutoFileEligible() bool {
	if len(c.Repos) < MinClusterSize {
		return false
	}
	if c.AvgConfidence < MinAvgConfidence {
		return false
	}
	if c.Score < ScoreAutoFileThreshold {
		return false
	}
	return true
}

func normalizeTokens(topics []string) []string {
	out := make([]string, 0, len(topics))
	seen := map[string]struct{}{}
	for _, t := range topics {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersect := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersect++
		}
	}
	union := len(a) + len(b) - intersect
	if union == 0 {
		return 0
	}
	return float64(intersect) / float64(union)
}

func cloneSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for k := range in {
		out[k] = struct{}{}
	}
	return out
}

func setToSorted(s map[string]struct{}) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
