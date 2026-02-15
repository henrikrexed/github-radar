package cli

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hrexed/github-radar/internal/daemon"
	"github.com/hrexed/github-radar/internal/logging"
	"github.com/hrexed/github-radar/internal/state"
)

// ServeCmd handles the serve command.
type ServeCmd struct {
	cli *CLI
}

// NewServeCmd creates a new serve command handler.
func NewServeCmd(cli *CLI) *ServeCmd {
	return &ServeCmd{cli: cli}
}

// Run executes the serve command.
func (s *ServeCmd) Run(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var (
		interval  string
		httpAddr  string
		statePath string
	)

	fs.StringVar(&interval, "interval", "24h", "Scan interval (e.g., 6h, 24h)")
	fs.StringVar(&httpAddr, "http-addr", ":8080", "HTTP server address for health/status endpoints")
	fs.StringVar(&statePath, "state", "", "State file path (default: data/state.json)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Parse interval
	scanInterval, err := time.ParseDuration(interval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid interval %q: %v\n", interval, err)
		return 1
	}

	// Load config
	if err := s.cli.LoadConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		return 1
	}

	// Build daemon config
	daemonCfg := daemon.DaemonConfig{
		Interval:   scanInterval,
		HTTPAddr:   httpAddr,
		ConfigPath: s.cli.ConfigPath,
		DryRun:     s.cli.DryRun,
	}

	if statePath != "" {
		daemonCfg.StatePath = statePath
	} else {
		daemonCfg.StatePath = state.DefaultStatePath
	}

	// Create and run daemon
	d, err := daemon.New(s.cli.Config, daemonCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating daemon: %v\n", err)
		return 1
	}

	logging.Info("starting github-radar daemon",
		"config", s.cli.ConfigPath,
		"interval", interval,
		"http_addr", httpAddr,
		"dry_run", s.cli.DryRun)

	if err := d.Run(); err != nil {
		logging.Error("daemon error", "error", err)
		return 1
	}

	return 0
}
