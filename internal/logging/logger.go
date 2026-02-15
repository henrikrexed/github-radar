// Package logging provides structured logging for github-radar using slog.
package logging

import (
	"io"
	"log/slog"
	"os"
)

// Logger is the package-level logger instance.
var Logger *slog.Logger

func init() {
	// Default to INFO level, JSON format, stdout
	Logger = New(os.Stdout, false)
}

// New creates a new JSON logger with the specified verbosity.
// If verbose is true, DEBUG level is enabled; otherwise INFO level.
func New(w io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewJSONHandler(w, opts)
	return slog.New(handler)
}

// Init initializes the package-level logger with the specified settings.
func Init(verbose bool) {
	Logger = New(os.Stdout, verbose)
	slog.SetDefault(Logger)
}

// Debug logs at DEBUG level.
func Debug(msg string, args ...any) {
	Logger.Debug(msg, args...)
}

// Info logs at INFO level.
func Info(msg string, args ...any) {
	Logger.Info(msg, args...)
}

// Warn logs at WARN level.
func Warn(msg string, args ...any) {
	Logger.Warn(msg, args...)
}

// Error logs at ERROR level.
func Error(msg string, args ...any) {
	Logger.Error(msg, args...)
}

// With returns a logger with the given attributes.
func With(args ...any) *slog.Logger {
	return Logger.With(args...)
}

// Standard attribute keys (snake_case per architecture spec)
const (
	// Repository attributes
	AttrRepoOwner = "repo_owner"
	AttrRepoName  = "repo_name"
	AttrRepoFull  = "repo_full_name"

	// Scan attributes
	AttrScanID       = "scan_id"
	AttrReposTotal   = "repos_total"
	AttrReposScanned = "repos_scanned"

	// Timing attributes
	AttrDurationMS = "duration_ms"

	// Metrics attributes
	AttrStars        = "stars"
	AttrForks        = "forks"
	AttrGrowthScore  = "growth_score"
	AttrStarVelocity = "star_velocity"

	// Error attributes
	AttrError = "error"

	// Config attributes
	AttrConfigPath = "config_path"
)

// Repo returns common repository attributes for logging.
func Repo(owner, name string) []any {
	return []any{
		AttrRepoOwner, owner,
		AttrRepoName, name,
	}
}

// RepoFull returns the full repository name attribute.
func RepoFull(owner, name string) []any {
	return []any{
		AttrRepoFull, owner + "/" + name,
	}
}

// Scan returns common scan attributes for logging.
func Scan(scanID string, reposTotal int) []any {
	return []any{
		AttrScanID, scanID,
		AttrReposTotal, reposTotal,
	}
}

// Duration returns the duration attribute in milliseconds.
func Duration(ms int64) []any {
	return []any{
		AttrDurationMS, ms,
	}
}

// Err returns the error attribute.
func Err(err error) []any {
	if err == nil {
		return nil
	}
	return []any{
		AttrError, err.Error(),
	}
}
