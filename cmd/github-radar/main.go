// Package main is the entry point for github-radar CLI.
package main

import (
	"fmt"
	"os"

	"github.com/hrexed/github-radar/internal/cli"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	fmt.Printf("github-radar %s - GitHub Trend Scanner\n", Version)

	c := cli.New()
	exitCode := c.Run(os.Args[1:])

	if c.Config != nil && c.Verbose {
		fmt.Printf("Config loaded: otel.endpoint=%s\n", c.Config.Otel.Endpoint)
	}

	os.Exit(exitCode)
}
