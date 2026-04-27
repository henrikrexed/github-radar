// Package main generates testdata/load/3k_repos.jsonl deterministically.
//
// Run with `go run ./testdata/load/gen_fixture.go > testdata/load/3k_repos.jsonl`
// when the harness's fixture shape needs to change. The output is checked in
// so the loadtest harness has a stable, version-controlled corpus.
//
//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"
)

const (
	totalRepos    = 3000
	uniqueOwners  = 60  // -> avg 50 repos per owner
	newRepoFrac   = 0.2 // 20% inside 48h discovery window
	newRepoWindow = 48 * time.Hour
	maxAgeDays    = 365
)

type fixtureRow struct {
	FullName    string  `json:"full_name"`
	GrowthScore float64 `json:"growth_score"`
	FirstSeenAt string  `json:"first_seen_at"`
}

func main() {
	// Fixed seed so the fixture is byte-stable.
	r := rand.New(rand.NewSource(20260425))
	// Anchor "now" to a fixed instant so first_seen_at values are
	// reproducible across regenerations.
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)

	enc := json.NewEncoder(os.Stdout)
	for i := 0; i < totalRepos; i++ {
		owner := fmt.Sprintf("ghorg%d", i%uniqueOwners)
		name := fmt.Sprintf("repo%05d", i)
		// Growth score skewed log-style so a few hundred dominate.
		score := r.ExpFloat64() * 50.0

		var firstSeen time.Time
		if r.Float64() < newRepoFrac {
			// Inside 48h window — random offset between 0 and 48h ago.
			offset := time.Duration(r.Int63n(int64(newRepoWindow)))
			firstSeen = now.Add(-offset)
		} else {
			// Aged: between 49h and 365d ago.
			d := time.Duration(r.Int63n(int64(maxAgeDays-2)*24*int64(time.Hour))) + 49*time.Hour
			firstSeen = now.Add(-d)
		}

		_ = enc.Encode(fixtureRow{
			FullName:    owner + "/" + name,
			GrowthScore: score,
			FirstSeenAt: firstSeen.UTC().Format(time.RFC3339),
		})
	}
}
