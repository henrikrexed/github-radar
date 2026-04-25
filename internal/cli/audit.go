package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hrexed/github-radar/internal/audit"
	"github.com/hrexed/github-radar/internal/database"
	"github.com/hrexed/github-radar/internal/logging"
)

// AuditCmd handles the `audit` command tree.
type AuditCmd struct {
	cli *CLI
}

// NewAuditCmd creates a new audit command handler.
func NewAuditCmd(cli *CLI) *AuditCmd { return &AuditCmd{cli: cli} }

// Run dispatches `audit <subcommand>`.
func (a *AuditCmd) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: github-radar audit <subcommand>\n  subcommands:\n    other-drift  Monthly `<cat>/other` drift audit (T9)\n")
		return 1
	}
	switch args[0] {
	case "other-drift":
		return a.runOtherDrift(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown audit subcommand: %s\n", args[0])
		return 1
	}
}

// runOtherDrift implements `github-radar audit other-drift [--dry-run] [--file]`.
//
// Behavior:
//   - --dry-run: render report to stdout, no Paperclip API calls, no file writes.
//   - --file:    persist report to ~/.local/share/github-radar/audits/<YYYY-MM>.md
//                AND auto-file via Paperclip API (when PAPERCLIP_API_KEY is set).
//   - default:   render report to stdout, no file writes, no Paperclip calls
//                (same as --dry-run, kept for backward-compatibility).
func (a *AuditCmd) runOtherDrift(args []string) int {
	fs := flag.NewFlagSet("audit other-drift", flag.ContinueOnError)
	var dryRun, doFile bool
	var dbPath, reportDir string
	fs.BoolVar(&dryRun, "dry-run", false, "Render report to stdout only (no Paperclip calls, no file writes)")
	fs.BoolVar(&doFile, "file", false, "Persist report and auto-file graduation proposals via Paperclip")
	fs.StringVar(&dbPath, "db", "", "Path to scanner.db (defaults to XDG_DATA_HOME/github-radar/scanner.db)")
	fs.StringVar(&reportDir, "report-dir", "", "Override report directory (defaults to XDG_DATA_HOME/github-radar/audits)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	if dryRun && doFile {
		fmt.Fprintln(os.Stderr, "Error: --dry-run and --file are mutually exclusive")
		return 1
	}

	if dbPath == "" {
		dbPath = database.DefaultDBPath
	}
	db, err := database.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		return 1
	}
	defer db.Close()

	opts := audit.Options{
		DB:        db.SQL(),
		DryRun:    !doFile,
		File:      doFile,
		ReportDir: reportDir,
		Now:       time.Now,
		Logger:    slogAdapter{},
	}

	if doFile {
		if reportDir == "" {
			opts.ReportDir = audit.DefaultReportDir()
		}
		if key := os.Getenv("PAPERCLIP_API_KEY"); key != "" {
			baseURL := envOr("PAPERCLIP_API_URL", "http://127.0.0.1:3100")
			companyID := os.Getenv("PAPERCLIP_COMPANY_ID")
			if companyID == "" {
				fmt.Fprintln(os.Stderr, "Error: --file requires PAPERCLIP_COMPANY_ID env var when PAPERCLIP_API_KEY is set")
				return 1
			}
			filer, err := audit.NewPaperclipFiler(audit.PaperclipConfig{
				BaseURL:    baseURL,
				APIKey:     key,
				CompanyID:  companyID,
				ProjectID:  os.Getenv("GITHUB_RADAR_PAPERCLIP_PROJECT_ID"),
				ParentID:   os.Getenv("GITHUB_RADAR_PAPERCLIP_AUDIT_PARENT_ID"),
				AssigneeID: os.Getenv("GITHUB_RADAR_PAPERCLIP_AUDIT_ASSIGNEE_ID"),
				Logger:     slogAdapter{},
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return 1
			}
			opts.Filer = filer
		} else {
			logging.Warn("PAPERCLIP_API_KEY not set; --file will persist the report but skip auto-file (degrade-to-watch posture)")
		}
	}

	ctx := context.Background()
	res, err := audit.Run(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running audit: %v\n", err)
		return 1
	}

	rendered := audit.Render(res)
	if doFile {
		path, err := audit.PersistReport(res, opts.ReportDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error persisting report: %v\n", err)
			return 1
		}
		logging.Info("audit report persisted", "path", path, "auto_filed", len(res.AutoFiled), "watch", len(res.WatchList), "auto_file_failures", res.AutoFileFailures)
	} else {
		// Render to stdout for dry-run / no-flag default.
		fmt.Print(rendered)
	}
	return 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// slogAdapter forwards audit's StructuredLogger calls to the package logger.
type slogAdapter struct{}

func (slogAdapter) Info(msg string, args ...any)  { logging.Info(msg, args...) }
func (slogAdapter) Warn(msg string, args ...any)  { logging.Warn(msg, args...) }
func (slogAdapter) Error(msg string, args ...any) { logging.Error(msg, args...) }
