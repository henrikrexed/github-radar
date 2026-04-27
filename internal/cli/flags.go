// Package cli provides CLI command implementations for github-radar.
//
// flags.go centralizes flag-handling helpers shared across subcommands.
//
// The global-vs-local interaction matters for any flag (such as --dry-run)
// that exists both as a CLI-wide global and as a subcommand-local flag.
// CLI.extractGlobalFlags consumes the global occurrence before the
// subcommand FlagSet sees it, so a naive subcommand that only inspects
// its FlagSet-bound local flag will silently ignore the global form. This
// caused the unintended live drain documented in
// [ISI-774](/ISI/issues/ISI-774); the helper extraction lives in
// [ISI-778](/ISI/issues/ISI-778).
package cli

// effectiveDryRun returns whether dry-run semantics should apply for an
// admin subcommand, given the global CLI dry-run flag (set by
// extractGlobalFlags before the subcommand handler runs) and the
// subcommand-local dry-run flag (parsed by the subcommand's own FlagSet).
// Either source forces the dry-run path. New admin subcommands MUST call
// this helper instead of re-deriving the OR locally — see ISI-774 for the
// regression and the architecture doc for the canonical pattern.
func effectiveDryRun(c *CLI, localDryRun bool) bool {
	if c == nil {
		return localDryRun
	}
	return localDryRun || c.DryRun
}
