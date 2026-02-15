package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// StatusCmd handles the status command.
type StatusCmd struct {
	cli *CLI
}

// NewStatusCmd creates a new status command handler.
func NewStatusCmd(cli *CLI) *StatusCmd {
	return &StatusCmd{cli: cli}
}

// StatusResponse matches the daemon's status response.
type StatusResponse struct {
	Status             string `json:"status"`
	LastScan           string `json:"last_scan,omitempty"`
	NextScan           string `json:"next_scan,omitempty"`
	ReposTracked       int    `json:"repos_tracked"`
	RateLimitRemaining int    `json:"rate_limit_remaining"`
	Uptime             string `json:"uptime"`
}

// Run executes the status command.
func (s *StatusCmd) Run(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	var (
		addr   string
		format string
	)

	fs.StringVar(&addr, "addr", "http://localhost:8080", "Daemon HTTP address")
	fs.StringVar(&format, "format", "text", "Output format: text, json")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Fetch status from daemon
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(addr + "/status")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to daemon: %v\n", err)
		fmt.Fprintf(os.Stderr, "Is the daemon running? Start it with: github-radar serve\n")
		return 1
	}
	defer resp.Body.Close()

	// Limit response body to 1MB to prevent memory exhaustion
	const maxResponseSize = 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading response: %v\n", err)
		return 1
	}

	var status StatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		return 1
	}

	if format == "json" {
		fmt.Println(string(body))
		return 0
	}

	// Text format
	fmt.Println("GitHub Radar Daemon Status")
	fmt.Println("==========================")
	fmt.Printf("Status:              %s\n", status.Status)
	fmt.Printf("Uptime:              %s\n", status.Uptime)
	fmt.Printf("Repos Tracked:       %d\n", status.ReposTracked)
	fmt.Printf("Rate Limit Remain:   %d\n", status.RateLimitRemaining)
	if status.LastScan != "" {
		fmt.Printf("Last Scan:           %s\n", status.LastScan)
	} else {
		fmt.Printf("Last Scan:           never\n")
	}
	if status.NextScan != "" {
		fmt.Printf("Next Scan:           %s\n", status.NextScan)
	}

	return 0
}
