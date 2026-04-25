package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// PaperclipConfig configures the Paperclip API client.
type PaperclipConfig struct {
	BaseURL    string
	APIKey     string
	CompanyID  string
	ProjectID  string
	ParentID   string
	AssigneeID string
	HTTPClient *http.Client
	RetryDelay time.Duration  // defaults to 2s — plan §6.1 retry policy
	DedupWin   time.Duration  // defaults to 60d
	Now        Clock          // defaults to time.Now
	Logger     StructuredLogger
}

// PaperclipFiler is the production Filer that talks to the Paperclip REST
// API. It honors plan §6.1 retry semantics: 1 retry with 2s delay on 429,
// 5xx, or transport error; on any other 4xx, fail without retry.
type PaperclipFiler struct {
	cfg PaperclipConfig
}

// NewPaperclipFiler constructs the production Filer. APIKey, BaseURL, and
// CompanyID are required; the rest get sensible defaults.
func NewPaperclipFiler(cfg PaperclipConfig) (*PaperclipFiler, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("paperclip: APIKey is required (set PAPERCLIP_API_KEY)")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("paperclip: BaseURL is required")
	}
	if cfg.CompanyID == "" {
		return nil, fmt.Errorf("paperclip: CompanyID is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 2 * time.Second
	}
	if cfg.DedupWin == 0 {
		cfg.DedupWin = 60 * 24 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = nopLogger{}
	}
	return &PaperclipFiler{cfg: cfg}, nil
}

// FindRecentDuplicate searches Paperclip for any open issue whose title
// starts with `titlePrefix` and was created inside the dedup window. The
// dedup decision is a pure function of the search response — no clock
// mocking is required (plan §6.1 review Q3).
func (f *PaperclipFiler) FindRecentDuplicate(ctx context.Context, titlePrefix string, now time.Time) (string, error) {
	q := url.Values{}
	q.Set("q", titlePrefix)
	endpoint := fmt.Sprintf("%s/api/companies/%s/issues?%s", strings.TrimRight(f.cfg.BaseURL, "/"), f.cfg.CompanyID, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+f.cfg.APIKey)
	resp, err := f.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("paperclip search transport: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("paperclip search %d: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Issues []struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			CreatedAt  string `json:"createdAt"`
			Status     string `json:"status"`
		} `json:"issues"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&payload); err != nil {
		return "", fmt.Errorf("decoding search response: %w", err)
	}
	cutoff := now.Add(-f.cfg.DedupWin)
	for _, iss := range payload.Issues {
		if !strings.HasPrefix(iss.Title, titlePrefix) {
			continue
		}
		if iss.Status == "cancelled" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, iss.CreatedAt)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, iss.CreatedAt)
			if err != nil {
				continue
			}
		}
		if ts.After(cutoff) {
			id := iss.Identifier
			if id == "" {
				id = iss.ID
			}
			return id, nil
		}
	}
	return "", nil
}

// File posts a graduation-proposal issue with one retry on 429/5xx/transport
// per plan §6.1.
func (f *PaperclipFiler) File(ctx context.Context, p Proposal) (string, error) {
	body := buildProposalBody(p)
	payload := map[string]any{
		"title":       proposalTitlePrefix(p.Category, p.ProposedSub),
		"description": body,
		"status":      "todo",
		"priority":    "medium",
		"projectId":   f.cfg.ProjectID,
	}
	if f.cfg.ParentID != "" {
		payload["parentId"] = f.cfg.ParentID
	}
	if f.cfg.AssigneeID != "" {
		payload["assigneeAgentId"] = f.cfg.AssigneeID
	}
	raw, _ := json.Marshal(payload)
	endpoint := fmt.Sprintf("%s/api/companies/%s/issues", strings.TrimRight(f.cfg.BaseURL, "/"), f.cfg.CompanyID)

	id, err := f.postOnce(ctx, endpoint, raw)
	if err == nil {
		return id, nil
	}
	if shouldRetry(err) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(f.cfg.RetryDelay):
		}
		id, err = f.postOnce(ctx, endpoint, raw)
		if err == nil {
			return id, nil
		}
	}
	return "", err
}

func (f *PaperclipFiler) postOnce(ctx context.Context, endpoint string, raw []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+f.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", &FileError{StatusCode: 0, Reason: "transport: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var out struct {
			Identifier string `json:"identifier"`
			ID         string `json:"id"`
		}
		_ = json.Unmarshal(body, &out)
		if out.Identifier != "" {
			return out.Identifier, nil
		}
		return out.ID, nil
	}
	reason := http.StatusText(resp.StatusCode)
	if len(body) > 0 {
		reason = string(body)
	}
	return "", &FileError{StatusCode: resp.StatusCode, Reason: truncReason(reason)}
}

func shouldRetry(err error) bool {
	var fe *FileError
	if !asFileErr(err, &fe) {
		// non-FileError = transport not classified — retry once.
		return true
	}
	if fe.StatusCode == 0 {
		return true // transport
	}
	if fe.StatusCode == 429 {
		return true
	}
	if fe.StatusCode >= 500 && fe.StatusCode < 600 {
		return true
	}
	return false
}

// buildProposalBody renders the markdown body for an auto-filed proposal.
// Plan §6 + plan §6 review item U5 require: repo list, token rationale,
// draft config-PR snippet. Tests pin this contract via golden-file regex.
func buildProposalBody(p Proposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Auto-filed by github-radar audit\n\n")
	fmt.Fprintf(&b, "**Category:** `%s`\n", p.Category)
	fmt.Fprintf(&b, "**Proposed subcategory:** `%s`\n", p.ProposedSub)
	fmt.Fprintf(&b, "**Cluster size:** %d repos · **avg confidence:** %.2f · **score:** %.2f\n\n", len(p.Repos), p.AvgConf, p.Score)

	b.WriteString("### Token rationale\n")
	b.WriteString("Cluster was emitted because the following topic tokens overlapped across the repos (Jaccard ≥ 0.6 merges, plan §3 step 4):\n\n")
	for _, t := range p.Tokens {
		fmt.Fprintf(&b, "- `%s`\n", t)
	}
	b.WriteString("\n### Repos\n")
	for _, r := range p.Repos {
		fmt.Fprintf(&b, "- `%s`\n", r)
	}
	b.WriteString("\n### Draft config PR snippet\n")
	b.WriteString("Add the following to `configs/taxonomy.yaml` (or the equivalent classification config) once approved:\n\n")
	b.WriteString("```yaml\n")
	fmt.Fprintf(&b, "categories:\n")
	fmt.Fprintf(&b, "  %s:\n", p.Category)
	fmt.Fprintf(&b, "    subcategories:\n")
	fmt.Fprintf(&b, "      %s:\n", p.ProposedSub)
	fmt.Fprintf(&b, "        topics:\n")
	for _, t := range p.Tokens {
		fmt.Fprintf(&b, "          - %s\n", t)
	}
	b.WriteString("```\n\n")
	b.WriteString("Auto-filed by `github-radar audit other-drift`. See [T9 plan §6](/ISI/issues/ISI-720#document-plan).\n")
	return b.String()
}
