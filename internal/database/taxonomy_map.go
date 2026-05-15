package database

// Taxonomy v2 — 2-level classification. Single source of truth for:
//  • the 42-entry legacy → (category, subcategory) migration lookup
//  • the closed 14 domain × ~72 subcategory matrix consumed by the classifier
//    validator, the CLI, and the config loader
//
// Authoritative spec: ISI-712 architecture document (v2) §1.1 + §2.4.

// TaxonomyV2 is the closed set of allowed (category, subcategory) pairs.
// 14 top-level domain categories + 1 refusal sink ("other") per architecture §1.1.
// Each domain carries an "other" escape-hatch per §1.2 Case A.
var TaxonomyV2 = map[string][]string{
	"ai": {
		"agents", "coding-assistants", "llm-tooling", "mcp-ecosystem",
		"infrastructure", "rag", "vector-database", "computer-vision",
		"voice-and-audio", "mlops", "other",
	},
	"cloud-native": {
		"kubernetes", "observability", "service-mesh", "platform-engineering",
		"gitops", "networking", "infrastructure", "container-runtime",
		"wasm", "security", "other",
	},
	"web":            {"frameworks", "frontend-ui", "css-styling", "other"},
	"mobile-desktop": {"mobile", "desktop", "other"},
	"systems":        {"rust-ecosystem", "programming-languages", "embedded-iot", "other"},
	"security":       {"cybersecurity", "privacy-tools", "other"},
	"data":           {"databases", "data-engineering", "data-science", "other"},
	"productivity":   {"self-hosted", "cli-tools", "general", "low-code-automation", "other"},
	"devtools":       {"general", "testing", "awesome-lists", "other"},
	"creative":       {"game-development", "media-tools", "other"},
	"crypto":         {"blockchain-web3", "other"},
	"robotics":       {"robotics", "other"},
	"science":        {"bioinformatics", "computational-research", "datasets", "other"},
	"education":      {"tutorials", "learning-paths", "awesome-lists", "other"},
	// Refusal sink — see architecture §1.2 Case C. Valid only when every
	// classifier input (name/description/README/topics) is effectively empty.
	"other": {"other"},
}

// TaxonomyPair is a (category, subcategory) tuple returned by the legacy lookup.
type TaxonomyPair struct {
	Category    string
	Subcategory string
}

// LegacyCategoryMap maps each legacy flat `primary_category` value present in
// scanner.db to its (category, subcategory) pair under TaxonomyV2.
//
// Source: ISI-712 §2.4 (initial 42 entries from the 2026-04-24 ground-truth
// snapshot, 559 active repos). One additional entry — `css-and-styling` —
// was added in ISI-984 after the round-3 cardinality probe surfaced it as a
// classifier-emitted granular slug not yet in this lookup, leaking into the
// `category` field on the T3 exporter.
//
// The backfill SQL in MigrateTaxonomyV2 is derived from this table; this map
// is also the source of truth for the coverage unit test asserting every
// known legacy value is present and resolves to a TaxonomyV2 pair.
var LegacyCategoryMap = map[string]TaxonomyPair{
	"ai-agents":             {"ai", "agents"},
	"ai-coding-assistants":  {"ai", "coding-assistants"},
	"llm-tooling":           {"ai", "llm-tooling"},
	"mcp-ecosystem":         {"ai", "mcp-ecosystem"},
	"voice-and-audio-ai":    {"ai", "voice-and-audio"},
	"ai-infrastructure":     {"ai", "infrastructure"},
	"computer-vision":       {"ai", "computer-vision"},
	"rag":                   {"ai", "rag"},
	"vector-database":       {"ai", "vector-database"},
	"mlops":                 {"ai", "mlops"},
	"kubernetes":            {"cloud-native", "kubernetes"},
	"observability":         {"cloud-native", "observability"},
	"platform-engineering":  {"cloud-native", "platform-engineering"},
	"gitops":                {"cloud-native", "gitops"},
	"networking":            {"cloud-native", "networking"},
	"infrastructure":        {"cloud-native", "infrastructure"},
	"container-runtime":     {"cloud-native", "container-runtime"},
	"wasm":                  {"cloud-native", "wasm"},
	"cloud-native-security": {"cloud-native", "security"},
	"web-frameworks":        {"web", "frameworks"},
	"frontend-ui":           {"web", "frontend-ui"},
	"css-and-styling":       {"web", "css-styling"},
	"mobile-development":    {"mobile-desktop", "mobile"},
	"desktop-apps":          {"mobile-desktop", "desktop"},
	"rust-ecosystem":        {"systems", "rust-ecosystem"},
	"programming-languages": {"systems", "programming-languages"},
	"embedded-iot":          {"systems", "embedded-iot"},
	"cybersecurity":         {"security", "cybersecurity"},
	"privacy-tools":         {"security", "privacy-tools"},
	"databases":             {"data", "databases"},
	"data-engineering":      {"data", "data-engineering"},
	"data-science":          {"data", "data-science"},
	"self-hosted":           {"productivity", "self-hosted"},
	"cli-tools":             {"productivity", "cli-tools"},
	"productivity":          {"productivity", "general"},
	"low-code-automation":   {"productivity", "low-code-automation"},
	"developer-tools":       {"devtools", "general"},
	"testing":               {"devtools", "testing"},
	"game-development":      {"creative", "game-development"},
	"media-tools":           {"creative", "media-tools"},
	"blockchain-web3":       {"crypto", "blockchain-web3"},
	"robotics":              {"robotics", "robotics"},
	"other":                 {"other", "other"},
}

// LookupLegacyCategory resolves a legacy flat `primary_category` value to its
// new (category, subcategory) pair. Unknown values return the refusal sink
// (other, other) so orphan rows cannot escape the backfill — the migration's
// post-condition check still surfaces them via needs_review=1 + refusal reason
// "backfill_legacy_other".
func LookupLegacyCategory(legacy string) TaxonomyPair {
	if p, ok := LegacyCategoryMap[legacy]; ok {
		return p
	}
	return TaxonomyPair{Category: "other", Subcategory: "other"}
}

// ResolveTaxonomy returns the (category, subcategory, legacy) triple to emit
// for a repo, honoring force_* overrides and gracefully handling rows that
// still hold pre-v3 flat values in primary_category. ISI-786, ISI-984.
//
// Resolution rules:
//
//   - force_category set → category = ForceCategory, subcategory =
//     ForceSubcategory, legacy = PrimaryCategoryLegacy. Note: the legacy-slug
//     rollup below still applies — if ForceCategory is a legacy flat slug
//     (e.g. "ai-agents"), it gets rolled up to the v3 top-level ("ai") so
//     the admin pin stays within the closed-set cardinality guarantee.
//   - otherwise → category = PrimaryCategory, subcategory =
//     PrimarySubcategory, legacy = PrimaryCategoryLegacy.
//   - if `category` matches a legacy flat slug from LegacyCategoryMap,
//     always roll it up to the v3 top-level (regardless of subcategory).
//     The original flat slug is preserved in `legacy` when that field was
//     empty. The existing subcategory wins when it is a valid pair under
//     the rolled-up top-level (TaxonomyV2); otherwise we fall back to the
//     legacy pair's subcategory so the (cat, sub) tuple stays closed-set.
//
// The earlier ISI-786 implementation gated the rollup on `subcategory == ""`,
// which only caught pre-v3 rows that had never been re-classified. Rows
// that had been *partially* re-classified — `PrimaryCategory` still on the
// legacy flat slug but `PrimarySubcategory` already populated with a v3
// subcategory — slipped through and leaked the legacy slug into the metric
// `category` field on the T3 exporter (ISI-984 round-3 verdict).
//
// The triple is always emitted unconditionally on the metric — even when
// any leg is empty — so the dashboard sees a stable attribute shape across
// rows. Gating on non-empty would silently drop the dimension on rows
// that haven't been re-classified yet (the regression observed in ISI-786).
func (r *RepoRecord) ResolveTaxonomy() (category, subcategory, legacy string) {
	if r.ForceCategory != "" {
		category = r.ForceCategory
		subcategory = r.ForceSubcategory
		legacy = r.PrimaryCategoryLegacy
	} else {
		category = r.PrimaryCategory
		subcategory = r.PrimarySubcategory
		legacy = r.PrimaryCategoryLegacy
	}
	if pair, ok := LegacyCategoryMap[category]; ok {
		if legacy == "" {
			legacy = category
		}
		if subcategory == "" || !IsAllowedPair(pair.Category, subcategory) {
			subcategory = pair.Subcategory
		}
		category = pair.Category
	}
	return category, subcategory, legacy
}

// IsAllowedPair returns true if (category, subcategory) is in TaxonomyV2 and
// subcategory is not the special refusal sink when category != "other".
// Graceful-refusal validation is handled separately by the classifier (§1.2 C).
func IsAllowedPair(category, subcategory string) bool {
	subs, ok := TaxonomyV2[category]
	if !ok {
		return false
	}
	for _, s := range subs {
		if s == subcategory {
			return true
		}
	}
	return false
}
