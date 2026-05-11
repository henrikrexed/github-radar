package database

import (
	"sort"
	"testing"
)

// The canonical legacy flat values present in scanner.db. Initial 42 entries
// from the 2026-04-24 migration-design ground-truth snapshot (ISI-712 §2.4);
// `css-and-styling` was appended in ISI-984 after round-3 cardinality probes
// surfaced it as a classifier-emitted granular slug not yet in the lookup,
// leaking into the metric `category` field on the T3 exporter.
//
// This list is duplicated here on purpose: LegacyCategoryMap is the production
// source of truth; this slice is a test-time pin that fails loudly if a value
// is ever dropped.
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
	"cloud-native-security", "css-and-styling",
}

const legacyFlatValuesExpectedCount = 43

func TestLegacyCategoryMap_CoversAllLegacyValues(t *testing.T) {
	if len(legacyFlatValuesSpec) != legacyFlatValuesExpectedCount {
		t.Fatalf("spec list length = %d, want %d — test constant drifted",
			len(legacyFlatValuesSpec), legacyFlatValuesExpectedCount)
	}
	if len(LegacyCategoryMap) != legacyFlatValuesExpectedCount {
		t.Fatalf("LegacyCategoryMap has %d entries, want %d",
			len(LegacyCategoryMap), legacyFlatValuesExpectedCount)
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

// TestRepoRecord_ResolveTaxonomy_ISI984LegacyCategoryRollup covers the
// regression where rows that had been *partially* re-classified — primary
// category still on a legacy flat slug but PrimarySubcategory already
// populated with a v3 subcategory — slipped through the original
// `subcategory == ""` collapse guard and leaked the legacy slug into the
// metric `category` field on the T3 exporter.
//
// Smoking-gun rows are the verdict-comment samples on ISI-984, captured
// from `dynatrace-dev` (oat05854) at 2026-05-11T08:18Z.
func TestRepoRecord_ResolveTaxonomy_ISI984LegacyCategoryRollup(t *testing.T) {
	cases := []struct {
		name                         string
		r                            RepoRecord
		wantCat, wantSub, wantLegacy string
	}{
		{
			name: "(llm-tooling, llm-tooling, llm-tooling) → (ai, llm-tooling, llm-tooling)",
			r: RepoRecord{
				PrimaryCategory:       "llm-tooling",
				PrimarySubcategory:    "llm-tooling",
				PrimaryCategoryLegacy: "llm-tooling",
			},
			wantCat: "ai", wantSub: "llm-tooling", wantLegacy: "llm-tooling",
		},
		{
			name: "(mcp-ecosystem, mcp-ecosystem, mcp-ecosystem) → (ai, mcp-ecosystem, mcp-ecosystem)",
			r: RepoRecord{
				PrimaryCategory:       "mcp-ecosystem",
				PrimarySubcategory:    "mcp-ecosystem",
				PrimaryCategoryLegacy: "mcp-ecosystem",
			},
			wantCat: "ai", wantSub: "mcp-ecosystem", wantLegacy: "mcp-ecosystem",
		},
		{
			name: "(ai-agents, agents, ai-agents) → (ai, agents, ai-agents)",
			r: RepoRecord{
				PrimaryCategory:       "ai-agents",
				PrimarySubcategory:    "agents",
				PrimaryCategoryLegacy: "ai-agents",
			},
			wantCat: "ai", wantSub: "agents", wantLegacy: "ai-agents",
		},
		{
			name: "(ai-agents, infrastructure, ai-infrastructure) → (ai, infrastructure, ai-infrastructure)",
			r: RepoRecord{
				PrimaryCategory:       "ai-agents",
				PrimarySubcategory:    "infrastructure",
				PrimaryCategoryLegacy: "ai-infrastructure",
			},
			wantCat: "ai", wantSub: "infrastructure", wantLegacy: "ai-infrastructure",
		},
		{
			name: "(ai-agents, llm-tooling, llm-tooling) → (ai, llm-tooling, llm-tooling)",
			r: RepoRecord{
				PrimaryCategory:       "ai-agents",
				PrimarySubcategory:    "llm-tooling",
				PrimaryCategoryLegacy: "llm-tooling",
			},
			wantCat: "ai", wantSub: "llm-tooling", wantLegacy: "llm-tooling",
		},
		{
			// Defense-in-depth: if a row carries a legacy `category` slug AND a
			// `subcategory` value that is NOT valid under the rolled-up top-level,
			// fall back to the legacy pair's subcategory so the (cat, sub) tuple
			// stays inside TaxonomyV2 — otherwise an invalid pair leaks downstream.
			name: "(llm-tooling, datasets, '') → (ai, llm-tooling, llm-tooling) — invalid sub falls back",
			r: RepoRecord{
				PrimaryCategory:       "llm-tooling",
				PrimarySubcategory:    "datasets", // datasets is under `science`, not `ai`
				PrimaryCategoryLegacy: "",
			},
			wantCat: "ai", wantSub: "llm-tooling", wantLegacy: "llm-tooling",
		},
		{
			// `css-and-styling` was added to LegacyCategoryMap in ISI-984.
			name: "(css-and-styling, '', '') → (web, css-styling, css-and-styling) — new ISI-984 entry",
			r: RepoRecord{
				PrimaryCategory:       "css-and-styling",
				PrimarySubcategory:    "",
				PrimaryCategoryLegacy: "",
			},
			wantCat: "web", wantSub: "css-styling", wantLegacy: "css-and-styling",
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

// TestRepoRecord_ResolveTaxonomy_EveryLegacySlugRollsUp sweeps every key in
// LegacyCategoryMap, feeds it into ResolveTaxonomy with a representative
// "partially-reclassified" row shape (PrimarySubcategory non-empty and valid
// under the rolled-up top-level), and asserts the emitted `category` is the
// v3 top-level — never the legacy flat slug itself. This is the structural
// guarantee ISI-984 round-3 needed: no legacy slug ever leaks into the
// exporter `category` field once it's in the lookup. Lifting `cardinality(category)`
// to ≤ len(TaxonomyV2) is a function of this property + classifier output.
func TestRepoRecord_ResolveTaxonomy_EveryLegacySlugRollsUp(t *testing.T) {
	for legacy, pair := range LegacyCategoryMap {
		legacy, pair := legacy, pair
		t.Run("legacy="+legacy, func(t *testing.T) {
			// Pick a representative subcategory that is valid under the
			// rolled-up top-level. pair.Subcategory is by construction valid
			// under pair.Category (TestLegacyCategoryMap_AllPairsAreInTaxonomyV2).
			r := RepoRecord{
				PrimaryCategory:       legacy,
				PrimarySubcategory:    pair.Subcategory,
				PrimaryCategoryLegacy: legacy,
			}
			cat, sub, gotLegacy := r.ResolveTaxonomy()
			if cat == legacy && legacy != pair.Category {
				t.Fatalf("legacy slug %q leaked into category field after rollup", legacy)
			}
			if cat != pair.Category {
				t.Errorf("category = %q, want %q (v3 top-level for legacy %q)",
					cat, pair.Category, legacy)
			}
			if sub != pair.Subcategory {
				t.Errorf("subcategory = %q, want %q (v3 sub for legacy %q)",
					sub, pair.Subcategory, legacy)
			}
			if gotLegacy != legacy {
				t.Errorf("legacy = %q, want %q (original flat slug preserved)",
					gotLegacy, legacy)
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
