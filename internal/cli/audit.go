package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hrexed/github-radar/internal/audit"
	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/logging"
)

// AuditCmd handles the `github-radar audit ...` family of subcommands.
// Currently only `audit other-drift` is implemented (T9, ISI-720).
type AuditCmd struct {
	cli *CLI
}

// NewAuditCmd creates a new audit command handler.
func NewAuditCmd(cli *CLI) *AuditCmd {
	return &AuditCmd{cli: cli}
}

// Run dispatches to subcommands.
func (c *AuditCmd) Run(args []string) int {
	if len(args) == 0 {
		c.printHelp()
		return 1
	}
	switch args[0] {
	case "other-drift":
		return c.runOtherDrift(args[1:])
	case "help", "--help", "-h":
		c.printHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown audit subcommand: %s\n", args[0])
		c.printHelp()
		return 1
	}
}

func (c *AuditCmd) printHelp() {
	fmt.Fprintln(os.Stderr, "Usage: github-radar audit other-drift [--dry-run] [--file]")
	fmt.Fprintln(os.Stderr, "  --dry-run   Render report to stdout, do not auto-file or persist")
	fmt.Fprintln(os.Stderr, "  --file      Persist report and auto-file qualifying clusters via Paperclip API")
}

// runOtherDrift implements the `other-drift` subcommand. Dry-run is the
// default: it prints the report and skips Paperclip POSTs. --file flips
// to the persist + auto-file mode for systemd-timer use.
func (c *AuditCmd) runOtherDrift(args []string) int {
	fs := flag.NewFlagSet("audit other-drift", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "render report to stdout without filing")
	doFile := fs.Bool("file", false, "persist report + auto-file qualifying clusters via Paperclip API")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if !*dryRun && !*doFile {
		// Default to dry-run when neither flag is given. Operators must
		// explicitly opt in to --file. Matches systemd-unit invocation
		// (`audit other-drift --file`) without surprising humans on a CLI.
		*dryRun = true
	}
	if *dryRun && *doFile {
		fmt.Fprintln(os.Stderr, "Error: --dry-run and --file are mutually exclusive")
		return 1
	}

	if err := c.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}
	cfg := c.cli.Config

	db, err := database.Open("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		return 1
	}
	defer db.Close()

	gh, err := github.NewClient(cfg.GitHub.Token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating GitHub client: %v\n", err)
		return 1
	}

	mode := audit.ModeDryRun
	if *doFile {
		mode = audit.ModeFile
	}

	dataProvider := &audit.DBDataProvider{
		DB:                db,
		Topics:            githubTopicsFetcher{client: gh},
		IgnoreFetchErrors: true, // empty topics -> skip from clustering, count in denom
	}

	var filer audit.Filer
	if *doFile {
		paperclipURL := os.Getenv("PAPERCLIP_API_URL")
		paperclipKey := os.Getenv("PAPERCLIP_API_KEY")
		companyID := os.Getenv("PAPERCLIP_COMPANY_ID")
		projectID := os.Getenv("GITHUB_RADAR_PROJECT_ID")
		assignee := os.Getenv("GITHUB_RADAR_AUDIT_ASSIGNEE_AGENT")
		if paperclipURL == "" || paperclipKey == "" || companyID == "" {
			fmt.Fprintln(os.Stderr, "Error: --file requires PAPERCLIP_API_URL, PAPERCLIP_API_KEY, PAPERCLIP_COMPANY_ID env vars")
			return 1
		}
		filer = audit.NewPaperclipFiler(paperclipURL, companyID, projectID, paperclipKey, assignee)
	}

	reportsDir := os.Getenv("GITHUB_RADAR_AUDIT_DIR")
	if reportsDir == "" {
		home, herr := os.UserHomeDir()
		if herr != nil {
			fmt.Fprintf(os.Stderr, "Error resolving home: %v\n", herr)
			return 1
		}
		reportsDir = filepath.Join(home, ".local", "share", "github-radar", "audits")
	}

	auditor := audit.NewAuditor(dataProvider, filer, reportsDir)
	auditor.Logger = auditLogger{}

	parentIssueID := os.Getenv("GITHUB_RADAR_AUDIT_PARENT_ISSUE_ID")
	logging.Info("audit starting",
		"mode", modeName(mode),
		"reports_dir", reportsDir,
		"parent_issue", parentIssueID,
	)

	out, err := auditor.Run(context.Background(), mode, parentIssueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running audit: %v\n", err)
		return 1
	}

	// Always print the report to stdout — operators tail journalctl on
	// the systemd unit and want the report in the log.
	fmt.Println(out.Report.Markdown)

	logging.Info("audit complete",
		"mode", modeName(mode),
		"corpus_size", "see report",
		"aggregate_share_pct", out.AggregateSharePct,
		"escalated", out.Escalated,
		"filed_count", len(out.Filed),
		"watch_count", len(out.WatchOnly),
	)
	if mode == audit.ModeFile && reportsDir != "" {
		fmt.Fprintf(os.Stderr, "report persisted to %s/%s\n", reportsDir, out.Report.Filename)
	}

	return 0
}

// modeName is a tiny helper for log fields.
func modeName(m audit.Mode) string {
	switch m {
	case audit.ModeFile:
		return "file"
	default:
		return "dry-run"
	}
}

// githubTopicsFetcher adapts internal/github.Client to the audit.TopicsFetcher
// interface. It calls GetRepository and pulls Topics off the returned metrics.
type githubTopicsFetcher struct {
	client *github.Client
}

func (g githubTopicsFetcher) FetchTopics(ctx context.Context, fullName string) ([]string, error) {
	owner, repo := splitFullName(fullName)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid full_name %q", fullName)
	}
	metrics, err := g.client.GetRepository(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	return metrics.Topics, nil
}

// auditLogger adapts internal/logging to the StructuredLogger interface
// the audit package uses (defined there to avoid an import cycle).
type auditLogger struct{}

func (auditLogger) Info(msg string, kv ...any)  { logging.Info(msg, kv...) }
func (auditLogger) Warn(msg string, kv ...any)  { logging.Warn(msg, kv...) }
func (auditLogger) Error(msg string, kv ...any) { logging.Error(msg, kv...) }

// splitFullName splits "owner/repo" → ("owner", "repo"). Returns empty
// strings when the input is malformed.
func splitFullName(full string) (string, string) {
	idx := strings.Index(full, "/")
	if idx <= 0 || idx == len(full)-1 {
		return "", ""
	}
	return full[:idx], full[idx+1:]
}
