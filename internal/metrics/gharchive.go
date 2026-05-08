package metrics

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hrexed/github-radar/internal/logging"
)

type ghArchiveEvent struct {
	Type string `json:"type"`
	Repo struct {
		Name string `json:"name"`
	} `json:"repo"`
	Payload json.RawMessage `json:"payload"`
	Action  string          `json:"action"`
	Actor   struct {
		Login string `json:"login"`
	} `json:"actor"`
	CreatedAt string `json:"created_at"`
}

type releasePayload struct {
	Release struct {
		PublishedAt string `json:"published_at"`
		TagName     string `json:"tag_name"`
	} `json:"release"`
	Action string `json:"action"`
}

type pullRequestPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Merged   bool   `json:"merged"`
		MergedAt string `json:"merged_at"`
		State    string `json:"state"`
	} `json:"pull_request"`
}

type issuesPayload struct {
	Action string `json:"action"`
	Number int    `json:"number"`
}

type repoAccumulator struct {
	owner          string
	name           string
	starEvents     int
	forkEvents     int
	mergedPRs      map[int]bool
	openedIssues   map[int]bool
	uniqueStarrers map[string]bool
	uniqueForkers  map[string]bool
	releases       []time.Time
}

func newRepoAccumulator(owner, name string) *repoAccumulator {
	return &repoAccumulator{
		owner:          owner,
		name:           name,
		mergedPRs:      make(map[int]bool),
		openedIssues:   make(map[int]bool),
		uniqueStarrers: make(map[string]bool),
		uniqueForkers:  make(map[string]bool),
	}
}

type HourlyArchiveCollector struct {
	httpClient *http.Client
	baseURL    string
	exporter   *Exporter
}

func NewHourlyArchiveCollector(baseURL string, timeout time.Duration, exporter *Exporter) *HourlyArchiveCollector {
	if baseURL == "" {
		baseURL = "https://data.gharchive.org"
	}
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &HourlyArchiveCollector{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		exporter:   exporter,
	}
}

func (h *HourlyArchiveCollector) Collect(ctx context.Context, repos []RepoRef, window time.Duration) ([]CollectedMetrics, error) {
	if len(repos) == 0 {
		return nil, nil
	}

	accumulators := make(map[string]*repoAccumulator, len(repos))
	for _, r := range repos {
		key := r.Owner + "/" + r.Name
		accumulators[key] = newRepoAccumulator(r.Owner, r.Name)
	}

	now := time.Now().UTC()
	hours := int(window.Hours())
	if hours < 1 {
		hours = 1
	}

	totalBytes := int64(0)
	for offset := 0; offset < hours; offset++ {
		hourTime := now.Add(-time.Duration(offset) * time.Hour)
		url := fmt.Sprintf("%s/%s.json.gz", h.baseURL, hourTime.Format("2006-01-02-15"))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request for %s: %w", url, err)
		}

		resp, err := h.httpClient.Do(req)
		if err != nil {
			logging.Warn("gharchive: failed to fetch hour", "url", url, "error", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			logging.Warn("gharchive: non-200 status", "url", url, "status", resp.StatusCode)
			continue
		}

		bytesRead, err := h.processArchive(ctx, resp.Body, accumulators)
		resp.Body.Close()
		totalBytes += bytesRead

		if err != nil {
			logging.Warn("gharchive: error processing hour", "url", url, "error", err)
		}
	}

	if h.exporter != nil {
		h.exporter.RecordGHArchiveBytesDownloaded(ctx, totalBytes)
	}

	results := make([]CollectedMetrics, 0, len(repos))
	daysElapsed := window.Hours() / 24.0
	if daysElapsed < 1.0/24.0 {
		daysElapsed = 1.0 / 24.0
	}

	for _, r := range repos {
		key := r.Owner + "/" + r.Name
		acc := accumulators[key]
		cm := CollectedMetrics{
			Owner:         r.Owner,
			Name:          r.Name,
			Stars:         acc.starEvents,
			Forks:         acc.forkEvents,
			StarVelocity:  float64(acc.starEvents) / daysElapsed,
			ForkVelocity:  float64(acc.forkEvents) / daysElapsed,
			PRVelocity:    float64(len(acc.mergedPRs)) / daysElapsed,
			IssueVelocity: float64(len(acc.openedIssues)) / daysElapsed,
			ReleaseDates:  acc.releases,
			CollectedAt:   now,
		}
		results = append(results, cm)
	}

	return results, nil
}

type countingReader struct {
	reader io.Reader
	n      int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.reader.Read(p)
	cr.n += int64(n)
	return n, err
}

func (h *HourlyArchiveCollector) processArchive(ctx context.Context, body io.Reader, accs map[string]*repoAccumulator) (int64, error) {
	cr := &countingReader{reader: body}

	start := time.Now()
	gz, err := gzip.NewReader(cr)
	if err != nil {
		return cr.n, fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	var kept, discarded int64
	dec := json.NewDecoder(gz)

	for {
		var evt ghArchiveEvent
		if err := dec.Decode(&evt); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		acc, ok := accs[evt.Repo.Name]
		if !ok {
			discarded++
			continue
		}

		kept++

		switch evt.Type {
		case "WatchEvent":
			acc.starEvents++
			acc.uniqueStarrers[evt.Actor.Login] = true

		case "ForkEvent":
			acc.forkEvents++
			acc.uniqueForkers[evt.Actor.Login] = true

		case "PullRequestEvent":
			var pl pullRequestPayload
			if json.Unmarshal(evt.Payload, &pl) == nil {
				if pl.Action == "closed" && pl.PullRequest.Merged {
					acc.mergedPRs[pl.Number] = true
				}
			}

		case "IssuesEvent":
			var pl issuesPayload
			if json.Unmarshal(evt.Payload, &pl) == nil {
				if pl.Action == "opened" {
					acc.openedIssues[pl.Number] = true
				}
			}

		case "ReleaseEvent":
			var pl releasePayload
			if json.Unmarshal(evt.Payload, &pl) == nil {
				if pl.Action == "published" && pl.Release.PublishedAt != "" {
					if t, err := time.Parse(time.RFC3339, pl.Release.PublishedAt); err == nil {
						acc.releases = append(acc.releases, t)
					}
				}
			}
		}
	}

	if h.exporter != nil {
		decodeDur := time.Since(start)
		h.exporter.RecordGHArchiveDecodeDuration(ctx, decodeDur)
		h.exporter.RecordGHArchiveEventsFiltered(ctx, true, kept)
		h.exporter.RecordGHArchiveEventsFiltered(ctx, false, discarded)
	}

	return cr.n, nil
}
