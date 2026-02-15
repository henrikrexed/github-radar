// Package cli provides CLI command implementations for github-radar.
package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/logging"
)

// CLI represents the command-line interface.
type CLI struct {
	ConfigPath string
	Verbose    bool
	DryRun     bool
	Config     *config.Config
	args       []string // remaining args after flag parsing
}

// New creates a new CLI instance with default values.
func New() *CLI {
	return &CLI{}
}

// ParseFlags parses command-line flags.
func (c *CLI) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("github-radar", flag.ContinueOnError)
	fs.StringVar(&c.ConfigPath, "config", "", "Path to configuration file")
	fs.BoolVar(&c.Verbose, "verbose", false, "Enable verbose output")
	fs.BoolVar(&c.DryRun, "dry-run", false, "Simulate without exporting metrics")

	if err := fs.Parse(args); err != nil {
		return err
	}

	c.args = fs.Args()
	return nil
}

// LoadConfig loads the configuration from the resolved path.
func (c *CLI) LoadConfig() error {
	cfg, err := config.LoadFromPath(c.ConfigPath)
	if err != nil {
		return err
	}
	c.Config = cfg
	return nil
}

// Run executes the CLI application.
func (c *CLI) Run(args []string) int {
	if err := c.ParseFlags(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Initialize logger based on verbose flag
	logging.Init(c.Verbose)

	logging.Debug("cli initialized",
		"verbose", c.Verbose,
		"dry_run", c.DryRun,
		logging.AttrConfigPath, c.ConfigPath,
	)

	// Handle subcommands
	if len(c.args) > 0 {
		return c.runCommand(c.args[0], c.args[1:])
	}

	// No subcommand - load config if available
	if err := c.LoadConfig(); err != nil {
		if c.Verbose {
			logging.Warn("config not loaded", logging.Err(err)...)
		}
	}

	return 0
}

// runCommand executes a subcommand.
func (c *CLI) runCommand(cmd string, args []string) int {
	switch cmd {
	case "config":
		return c.runConfigCommand(args)
	case "add":
		repoCmd := NewRepoCmd(c)
		return repoCmd.Add(args)
	case "remove":
		repoCmd := NewRepoCmd(c)
		return repoCmd.Remove(args)
	case "list":
		repoCmd := NewRepoCmd(c)
		return repoCmd.List(args)
	case "exclude":
		excludeCmd := NewExcludeCmd(c)
		return excludeCmd.Run(args)
	case "help":
		c.printHelp()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		c.printHelp()
		return 1
	}
}

// runConfigCommand handles the config subcommand.
func (c *CLI) runConfigCommand(args []string) int {
	configCmd := NewConfigCmd(c)

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: github-radar config <validate|show>\n")
		return 1
	}

	switch args[0] {
	case "validate":
		return configCmd.Validate()
	case "show":
		return configCmd.Show()
	default:
		fmt.Fprintf(os.Stderr, "Unknown config command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "Usage: github-radar config <validate|show>\n")
		return 1
	}
}

// printHelp prints usage information.
func (c *CLI) printHelp() {
	fmt.Println(`Usage: github-radar [flags] <command> [args]

Commands:
  add <repo>         Add a repository to tracking
                     Options: --category <name>
  remove <repo>      Remove a repository from tracking
                     Options: --keep-state
  list               List all tracked repositories
                     Options: --category <name>, --format <table|json|csv>
  exclude <action>   Manage exclusion list
                     Actions: add <pattern>, remove <pattern>, list
  config validate    Validate configuration file
  config show        Display current configuration
  help               Show this help message

Flags:
  --config <path>    Path to configuration file
  --verbose          Enable verbose output
  --dry-run          Simulate without exporting metrics`)
}
