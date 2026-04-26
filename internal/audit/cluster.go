// Package audit implements the monthly `<cat>/other` drift audit (T9).
//
// The audit clusters repos parked in `<cat>/other` buckets by topic
// overlap, scores each cluster, and (in --file mode) auto-files
// graduation-proposal issues for clusters that meet the size and
// confidence thresholds.
//
// See ISI-720 (T9 design plan) §3–§7 for the algorithm contract and
// ISI-752 (T9.2 test review) for the test plan this package satisfies.
package audit

import (
	"sort"
	"strings"
)

// Score gates from T9 plan §3 + §6.
const (
	// MinClusterSize is the minimum repo count for a cluster to be
	// considered a graduation candidate (plan §3 step 3 + §6).
	MinClusterSize = 5

	// JaccardMergeThreshold controls cluster merging on repo-set overlap
	// (plan §3 step 4).
	JaccardMergeThreshold = 0.6

	// MinClusterScore is the cutoff for auto-filing (plan §3 step 5).
	// Below this, the cluster is surfaced as "watch" but not auto-filed.
	MinClusterScore = 3.0

	// MinAvgConfidence is the auto-file gate from plan §6: clusters
	// must clear this in addition to MinClusterSize and MinClusterScore.
	MinAvgConfidence = 0.6
)

// Repo is the per-repo input to clustering. The `Topics` slice is what
// drives clustering; empty `Topics` means the repo is excluded from
// clustering (plan §3 fallback) but still counts in aggregate share
// (handled by the orchestrator).
type Repo struct {
	FullName        string
	PrimaryCategory string
	Topics          []string
	Confidence      float64
}

// Cluster is a group of repos that share enough topic overlap to be a
// candidate subcategory. It is keyed by a stable, deterministic set of
// merged tokens that drove the merge — used by the orchestrator to
// build the proposed subcategory name.
type Cluster struct {
	// Tokens that produced this cluster (sorted, deduped). These drive
	// the proposed subcategory slug and the Watch-list rationale text.
	Tokens []string

	// Repos in this cluster (sorted by FullName for deterministic output).
	Repos []Repo
}

// Score is repo_count × avg_confidence (plan §3 step 5).
func (c Cluster) Score() float64 {
	if len(c.Repos) == 0 {
		return 0
	}
	return float64(len(c.Repos)) * c.AvgConfidence()
}

// AvgConfidence is the mean classification_confidence over the cluster.
// Empty clusters return 0 (defensive — never happens in practice).
func (c Cluster) AvgConfidence() float64 {
	if len(c.Repos) == 0 {
		return 0
	}
	var sum float64
	for _, r := range c.Repos {
		sum += r.Confidence
	}
	return sum / float64(len(c.Repos))
}

// QualifiesForAutoFile returns true if the cluster meets all three
// auto-file gates: MinClusterSize, MinAvgConfidence, MinClusterScore.
// (Plan §3 step 5 + §6.)
func (c Cluster) QualifiesForAutoFile() bool {
	return len(c.Repos) >= MinClusterSize &&
		c.AvgConfidence() >= MinAvgConfidence &&
		c.Score() >= MinClusterScore
}

// Cluster runs the clustering algorithm from plan §3 over the given
// `<cat>/other` repos and returns clusters in deterministic order
// (highest score first, then alphabetical token).
//
// Repos with empty Topics are skipped (per the §3 fallback). They must
// still be counted in the aggregate share by the caller.
func ClusterRepos(repos []Repo) []Cluster {
	// Step 1+2: tokenize and build token → repos inverted index.
	// Repos with empty topics are excluded here (§3 fallback).
	index := make(map[string][]Repo)
	for _, r := range repos {
		if len(r.Topics) == 0 {
			continue
		}
		seen := make(map[string]struct{}, len(r.Topics))
		for _, t := range r.Topics {
			tok := strings.ToLower(strings.TrimSpace(t))
			if tok == "" {
				continue
			}
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			index[tok] = append(index[tok], r)
		}
	}

	// Step 3: emit candidate clusters for tokens with ≥ MinClusterSize repos.
	candidates := make([]Cluster, 0, len(index))
	for tok, rs := range index {
		if len(rs) < MinClusterSize {
			continue
		}
		candidates = append(candidates, Cluster{
			Tokens: []string{tok},
			Repos:  cloneAndSortRepos(rs),
		})
	}

	// Sort candidates deterministically before merging so merge order
	// is stable across runs (largest first, ties broken by token).
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].Repos) != len(candidates[j].Repos) {
			return len(candidates[i].Repos) > len(candidates[j].Repos)
		}
		return candidates[i].Tokens[0] < candidates[j].Tokens[0]
	})

	// Step 4: merge clusters whose repo sets overlap by Jaccard ≥ threshold.
	merged := mergeByJaccard(candidates, JaccardMergeThreshold)

	// Step 5: deterministic output order — highest score first, then by
	// joined token list. The "watch vs auto-file" gate is applied by the
	// caller, not here, so both watch and graduation candidates are
	// returned together.
	sort.SliceStable(merged, func(i, j int) bool {
		si, sj := merged[i].Score(), merged[j].Score()
		if si != sj {
			return si > sj
		}
		return strings.Join(merged[i].Tokens, ",") < strings.Join(merged[j].Tokens, ",")
	})

	return merged
}

// mergeByJaccard greedily merges clusters whose repo sets satisfy
// Jaccard(a, b) >= threshold. Merge is stable: the largest cluster
// absorbs smaller overlapping ones, keeping a sorted, deduped union of
// tokens.
//
// At threshold=0.6 with the typical cluster sizes (5–20), greedy merge
// is sufficient. Test U4 pins boundary behavior at exactly 0.6.
func mergeByJaccard(in []Cluster, threshold float64) []Cluster {
	if len(in) == 0 {
		return nil
	}

	clusters := make([]Cluster, 0, len(in))
	for _, c := range in {
		clusters = append(clusters, c)
	}

	// Iterate until no merges happen in a pass.
	for {
		merged := false
		for i := 0; i < len(clusters); i++ {
			for j := i + 1; j < len(clusters); j++ {
				if jaccardRepoSet(clusters[i].Repos, clusters[j].Repos) >= threshold {
					clusters[i] = mergeClusters(clusters[i], clusters[j])
					// Drop j by swapping with last and shrinking.
					clusters[j] = clusters[len(clusters)-1]
					clusters = clusters[:len(clusters)-1]
					merged = true
					// Restart j over the same i since clusters[j] is new.
					j = i
				}
			}
		}
		if !merged {
			break
		}
	}

	return clusters
}

// jaccardRepoSet returns |a ∩ b| / |a ∪ b| over the FullName key.
// Both inputs are assumed to have unique FullNames within themselves
// (true for clusters built from the inverted index).
func jaccardRepoSet(a, b []Repo) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(a))
	for _, r := range a {
		set[r.FullName] = struct{}{}
	}
	intersection := 0
	for _, r := range b {
		if _, ok := set[r.FullName]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// mergeClusters returns a new cluster whose Tokens is the sorted-deduped
// union of inputs, and whose Repos is the de-duplicated union (by FullName).
func mergeClusters(a, b Cluster) Cluster {
	tokSet := make(map[string]struct{}, len(a.Tokens)+len(b.Tokens))
	for _, t := range a.Tokens {
		tokSet[t] = struct{}{}
	}
	for _, t := range b.Tokens {
		tokSet[t] = struct{}{}
	}
	tokens := make([]string, 0, len(tokSet))
	for t := range tokSet {
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)

	repoSet := make(map[string]Repo, len(a.Repos)+len(b.Repos))
	for _, r := range a.Repos {
		repoSet[r.FullName] = r
	}
	for _, r := range b.Repos {
		repoSet[r.FullName] = r
	}
	repos := make([]Repo, 0, len(repoSet))
	for _, r := range repoSet {
		repos = append(repos, r)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].FullName < repos[j].FullName })

	return Cluster{Tokens: tokens, Repos: repos}
}

func cloneAndSortRepos(in []Repo) []Repo {
	out := make([]Repo, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].FullName < out[j].FullName })
	return out
}
