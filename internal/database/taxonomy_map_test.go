package database

import (
	"sort"
	"testing"
)

// The canonical 42 legacy flat values present in scanner.db as of the 2026-04-24
// migration-design ground-truth snapshot (ISI-712 §2.4). This list is duplicated
// here on purpose: LegacyCategoryMap is the production source of truth; this
// slice is a test-time pin that fails loudly if a value is ever dropped.
var legacyFlatValuesSpec = []string{
	"ai-agents", "ai-coding-assistants", "llm-tooling", "mcp-ecosystem",
	"voice-and-audio-ai", "ai-infrastructure", "desktop-apps", "cybersecurity",
	"databases", "computer-vision", "self-hosted", "developer-tools",
	"frontend-ui", "productivity", "robotics", "rag", "other",
	"observability", "mobile-development", "media-tools", "embedded-iot",
	"platform-engineering", "cli-tools", "networking", "infrastructure",
	"data-science", "vector-database", "rust-ecosystem", "programming-languages",
	"data-engineering", "privacy-tools", "low-code-automation", "kubernetes",
	"game-development", "blockchain-web3", "wasm", "testing",
	"container-runtime", "web-frameworks", "mlops", "gitops",
	"cloud-native-security",
}

func TestLegacyCategoryMap_CoversAll42Values(t *testing.T) {
	if len(legacyFlatValuesSpec) != 42 {
		t.Fatalf("spec list length = %d, want 42 — test constant drifted", len(legacyFlatValuesSpec))
	}
	if len(LegacyCategoryMap) != 42 {
		t.Fatalf("LegacyCategoryMap has %d entries, want 42", len(LegacyCategoryMap))
	}
	for _, legacy := range legacyFlatValuesSpec {
		if _, ok := LegacyCategoryMap[legacy]; !ok {
			t.Errorf("legacy flat value %q is missing from LegacyCategoryMap", legacy)
		}
	}
}

func TestLegacyCategoryMap_AllPairsAreInTaxonomyV2(t *testing.T) {
	for legacy, pair := range LegacyCategoryMap {
		if !IsAllowedPair(pair.Category, pair.Subcategory) {
			t.Errorf("legacy %q → (%s, %s) is not an allowed pair in TaxonomyV2",
				legacy, pair.Category, pair.Subcategory)
		}
	}
}

func TestLookupLegacyCategory_UnknownFallsBackToRefusalSink(t *testing.T) {
	p := LookupLegacyCategory("this-value-does-not-exist")
	if p.Category != "other" || p.Subcategory != "other" {
		t.Errorf("unknown legacy fallback = (%s, %s), want (other, other)",
			p.Category, p.Subcategory)
	}
}

func TestTaxonomyV2_TopLevelCardinality(t *testing.T) {
	// Architecture §1.1 success target: 15 top-level (14 domains + 1 refusal sink).
	if got := len(TaxonomyV2); got != 15 {
		t.Errorf("TaxonomyV2 top-level count = %d, want 15 (14 domains + 1 refusal sink)", got)
	}
	if _, ok := TaxonomyV2["other"]; !ok {
		t.Errorf("TaxonomyV2 missing refusal-sink category 'other'")
	}
}

func TestTaxonomyV2_PairCardinality(t *testing.T) {
	// Architecture §1.1 success target: ≤75 pairs. The spec reports 73.
	pairs := 0
	for _, subs := range TaxonomyV2 {
		pairs += len(subs)
	}
	if pairs > 75 {
		t.Errorf("TaxonomyV2 pair count = %d, want ≤75", pairs)
	}
	if pairs < 60 {
		t.Errorf("TaxonomyV2 pair count = %d, seems suspiciously small", pairs)
	}
}

func TestTaxonomyV2_EveryDomainHasOtherEscapeHatch(t *testing.T) {
	for cat, subs := range TaxonomyV2 {
		sort.Strings(subs)
		found := false
		for _, s := range subs {
			if s == "other" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("category %q missing 'other' subcategory escape hatch", cat)
		}
	}
}

// TestRepoRecord_ResolveTaxonomy guards the (category, subcategory, legacy)
// triple emitted on every github.radar.* metric series (ISI-786). The cases
// encode the four ways a row can land in the DB during the v3 backfill /
// post-drain reclassify window.
func TestRepoRecord_ResolveTaxonomy(t *testing.T) {
	cases := []struct {
		name                         string
		r                            RepoRecord
		wantCat, wantSub, wantLegacy string
	}{
		{
			name: "v3 row with primary subcategory + legacy",
			r: RepoRecord{
				PrimaryCategory:       "cloud-native",
				PrimarySubcategory:    "kubernetes",
				PrimaryCategoryLegacy: "kubernetes",
			},
			wantCat: "cloud-native", wantSub: "kubernetes", wantLegacy: "kubernetes",
		},
		{
			name: "force_category overrides primary; force_subcategory carries through",
			r: RepoRecord{
				PrimaryCategory:       "ai",
				PrimarySubcategory:    "agents",
				PrimaryCategoryLegacy: "ai-agents",
				ForceCategory:         "cloud-native",
				ForceSubcategory:      "observability",
			},
			wantCat: "cloud-native", wantSub: "observability", wantLegacy: "ai-agents",
		},
		{
			name: "pre-v3 row: primary_category still holds legacy flat slug — collapses to v3 (cat, sub) and lifts legacy",
			r: RepoRecord{
				PrimaryCategory:       "ai-agents",
				PrimarySubcategory:    "",
				PrimaryCategoryLegacy: "",
			},
			wantCat: "ai", wantSub: "agents", wantLegacy: "ai-agents",
		},
		{
			name: "newly-classified row with no legacy and no subcategory — emit empties (stable shape)",
			r: RepoRecord{
				PrimaryCategory: "",
			},
			wantCat: "", wantSub: "", wantLegacy: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cat, sub, legacy := tc.r.ResolveTaxonomy()
			if cat != tc.wantCat || sub != tc.wantSub || legacy != tc.wantLegacy {
				t.Errorf("ResolveTaxonomy() = (%q, %q, %q), want (%q, %q, %q)",
					cat, sub, legacy, tc.wantCat, tc.wantSub, tc.wantLegacy)
			}
		})
	}
}

func TestIsAllowedPair(t *testing.T) {
	tests := []struct {
		cat, sub string
		want     bool
	}{
		{"ai", "agents", true},
		{"ai", "kubernetes", false}, // wrong domain
		{"cloud-native", "kubernetes", true},
		{"other", "other", true},   // refusal sink
		{"other", "agents", false}, // sink rejects domain subs
		{"nonexistent", "anything", false},
		{"web", "frameworks", true},
		{"productivity", "general", true},
		{"devtools", "general", true},
	}
	for _, tc := range tests {
		got := IsAllowedPair(tc.cat, tc.sub)
		if got != tc.want {
			t.Errorf("IsAllowedPair(%q, %q) = %v, want %v", tc.cat, tc.sub, got, tc.want)
		}
	}
}
