// Package cli provides CLI command implementations for github-radar.
//
// admin.go implements the `admin` subcommand tree for low-frequency
// operator interventions on the scanner database (drains, repairs,
// audits). See [ISI-773](/ISI/issues/ISI-773) for the rationale on the
// `drain-needs-reclassify` action and the deterministic-mapping decision.
package cli

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/hrexed/github-radar/internal/database"
)

// AdminCmd handles the admin subcommand tree.
type AdminCmd struct {
	cli *CLI
}

// NewAdminCmd creates a new admin command handler.
func NewAdminCmd(cli *CLI) *AdminCmd {
	return &AdminCmd{cli: cli}
}

// Run dispatches to an admin sub-action.
func (a *AdminCmd) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: github-radar admin <action> [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Actions:\n")
		fmt.Fprintf(os.Stderr, "  drain-needs-reclassify    Drain repos stuck in needs_reclassify\n")
		return 1
	}

	switch args[0] {
	case "drain-needs-reclassify":
		return a.runDrainNeedsReclassify(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown admin action: %s\n", args[0])
		return 1
	}
}

// runDrainNeedsReclassify implements `admin drain-needs-reclassify`.
//
// Flags:
//
//	--dry-run         Preview the drain without mutating rows or writing
//	                  the pre-drain backup. Honored both as a global flag
//	                  (parsed by extractGlobalFlags before this handler is
//	                  reached) and as a subcommand-local flag.
//	--limit N         Cap the number of rows drained in one pass (0 = no
//	                  limit). Useful for staged drains in production.
func (a *AdminCmd) runDrainNeedsReclassify(args []string) int {
	fs := flag.NewFlagSet("admin drain-needs-reclassify", flag.ContinueOnError)
	localDryRun := fs.Bool("dry-run", false, "Preview only; do not mutate rows or write backup")
	limit := fs.Int("limit", 0, "Cap drained rows per pass (0 = no limit)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// extractGlobalFlags consumes --dry-run before the subcommand sees it,
	// so we OR the global flag (CLI.DryRun) with the local flag here. Either
	// source forces the dry-run path.
	dryRun := *localDryRun || a.cli.DryRun

	db, err := database.Open("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		return 1
	}
	defer db.Close()

	// Pre-drain count for the report.
	preCount, err := db.NeedsReclassifyCount()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error counting pre-drain needs_reclassify: %v\n", err)
		return 1
	}

	report, err := db.DrainNeedsReclassify(dryRun, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during drain: %v\n", err)
		return 1
	}

	a.printDrainReport(report, preCount, db)
	return 0
}

// printDrainReport renders the human-readable summary of a drain pass.
// The format intentionally matches the preview shown in the [ISI-773
// plan](/ISI/issues/ISI-773#document-plan) so PM verification is a direct
// numeric comparison.
func (a *AdminCmd) printDrainReport(r database.DrainReport, preCount int, db *database.DB) {
	if r.DryRun {
		fmt.Printf("Drain plan (dry-run, no changes):\n")
		fmt.Printf("  Examined %d repos in needs_reclassify\n", r.Examined)
		fmt.Printf("  Would drain %d repos -> active\n", r.Drained)
		fmt.Printf("  Would hold %d repos in (other,*) refusal sink (needs_review=1)\n", r.HeldInSink)
		if r.HeldOther > 0 {
			fmt.Printf("  WARNING: %d repos with unexpected residual state — investigate\n", r.HeldOther)
		}
		printByCategory(r.ByCategory, "  Top categories that would drain:")
		return
	}

	if r.BackupPath != "" {
		fmt.Printf("Wrote pre-drain backup: %s\n", r.BackupPath)
	}
	fmt.Printf("Drained %d repos from needs_reclassify -> active.\n", r.Drained)
	fmt.Printf("Held %d repos in (other,*) refusal sink (needs_review=1).\n", r.HeldInSink)
	if r.HeldOther > 0 {
		fmt.Printf("WARNING: %d repos with unexpected residual state — investigate\n", r.HeldOther)
	}
	printByCategory(r.ByCategory, "Top categories drained:")

	// Final report: post-drain count for symmetric pre/post evidence.
	postCount, err := db.NeedsReclassifyCount()
	if err == nil {
		fmt.Printf("Pre-drain  needs_reclassify count: %d\n", preCount)
		fmt.Printf("Post-drain needs_reclassify count: %d\n", postCount)
	}
}

// printByCategory renders the ByCategory map sorted by descending count
// then by category name for ties; helps the operator eyeball-check that
// the drained corpus shape matches the expected v3 taxonomy distribution
// (ai, systems, cloud-native, ...).
func printByCategory(byCat map[string]int, header string) {
	if len(byCat) == 0 {
		return
	}
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(byCat))
	for k, v := range byCat {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	fmt.Println(header)
	for _, p := range pairs {
		fmt.Printf("    %s=%d\n", p.k, p.v)
	}
}
