package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DedupWindow is the lookback window for the title-prefix dedup search
// (plan §6). Issues in the window suppress a fresh auto-file.
const DedupWindow = 60 * 24 * time.Hour

// MaxRetries is the per-request retry budget on the Paperclip API.
// Plan §6.1 specifies 1 retry (i.e. 2 total attempts) on 429, 5xx, or
// network/timeout failures. 4xx other than 429 are not retried.
const MaxRetries = 1

// RetryDelay is the backoff between attempts. Plan §6.1: 2s.
var RetryDelay = 2 * time.Second

// GraduationDraft is the input to FileGraduationProposal. Title is built
// from Category + ProposedSubcat per plan §6.
type GraduationDraft struct {
	Category       string
	ProposedSubcat string
	Cluster        Cluster
	AggregateShare float64 // % share, only used in the auto-generated body
	ParentIssueID  string  // rotating audit-parent-issue
}

// Title returns the canonical issue title used for both creation and
// the dedup grep (plan §6).
func (d GraduationDraft) Title() string {
	return fmt.Sprintf("Subcat graduation proposal: %s/%s", d.Category, d.ProposedSubcat)
}

// Body renders the issue description: repo list, token rationale, and
// a draft config-PR snippet. Pinned by U5 (golden-shape on the YAML).
func (d GraduationDraft) Body() string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Why\n\nMonthly `<cat>/other` drift audit identified a coherent cluster of %d repos in `%s/other` that share the topic(s) `%s`.\n\n",
		len(d.Cluster.Repos), d.Category, strings.Join(d.Cluster.Tokens, "`, `"))
	fmt.Fprintf(&b, "**Cluster size:** %d  \n", len(d.Cluster.Repos))
	fmt.Fprintf(&b, "**Avg classification confidence:** %.2f  \n", d.Cluster.AvgConfidence())
	fmt.Fprintf(&b, "**Score (repos × avg_confidence):** %.2f  \n", d.Cluster.Score())
	if d.AggregateShare > 0 {
		fmt.Fprintf(&b, "**`%s/other` aggregate share at audit time:** %.2f%%  \n", d.Category, d.AggregateShare)
	}
	b.WriteString("\n## Repos in cluster\n\n")
	for _, r := range d.Cluster.Repos {
		fmt.Fprintf(&b, "- `%s` (confidence %.2f)\n", r.FullName, r.Confidence)
	}
	b.WriteString("\n## Draft taxonomy config snippet\n\n")
	b.WriteString("```yaml\n")
	fmt.Fprintf(&b, "categories:\n  %s:\n    subcategories:\n      - %s\n", d.Category, d.ProposedSubcat)
	b.WriteString("```\n\n")
	b.WriteString("## Acceptance\n\n")
	b.WriteString("- Review repo list and proposed subcategory name.\n")
	b.WriteString("- Approve/reject — config PR follows on approval.\n")
	b.WriteString("\nFiled by `github-radar audit other-drift`. See [ISI-720](/ISI/issues/ISI-720) for the audit framework.\n")
	return b.String()
}

// Filer abstracts the Paperclip API surface used by the audit job. The
// audit orchestrator depends on this interface so tests can stub it.
type Filer interface {
	// AlreadyFiledRecently checks for issues with the given title prefix
	// created within the dedup window (plan §6).
	AlreadyFiledRecently(ctx context.Context, titlePrefix string) (bool, error)

	// File creates a graduation-proposal issue and returns the issue
	// identifier (e.g. "ISI-790") on success.
	File(ctx context.Context, draft GraduationDraft) (string, error)
}

// PaperclipFiler is the production HTTP-backed Filer. Construct via
// NewPaperclipFiler so the http.Client and Now are wired correctly.
//
// The API surface used:
//
//   - GET  /api/companies/{companyId}/issues?q={titlePrefix}  (dedup)
//   - POST /api/companies/{companyId}/issues                  (file)
//
// Auth is `Authorization: Bearer {APIKey}` on every request.
type PaperclipFiler struct {
	BaseURL        string
	CompanyID      string
	ProjectID      string
	APIKey         string
	AssigneeAgent  string // agent id to assign new graduation issues to
	HTTP           *http.Client
	Now            func() time.Time
	Logger         StructuredLogger // optional; nil-safe
}

// NewPaperclipFiler returns a Filer wired with sane defaults. APIKey
// pulled from env (PAPERCLIP_API_KEY) when blank.
func NewPaperclipFiler(baseURL, companyID, projectID, apiKey, assigneeAgent string) *PaperclipFiler {
	return &PaperclipFiler{
		BaseURL:       strings.TrimRight(baseURL, "/"),
		CompanyID:     companyID,
		ProjectID:     projectID,
		APIKey:        apiKey,
		AssigneeAgent: assigneeAgent,
		HTTP:          &http.Client{Timeout: 30 * time.Second},
		Now:           time.Now,
	}
}

// StructuredLogger is the minimal logger interface PaperclipFiler uses.
// Implementing this avoids a hard dep on internal/logging from tests.
type StructuredLogger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

func (p *PaperclipFiler) log() StructuredLogger {
	if p.Logger == nil {
		return noopLogger{}
	}
	return p.Logger
}

type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

// searchIssue is the subset of the Paperclip search-response shape we use.
type searchIssue struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"` // RFC3339
}

// AlreadyFiledRecently implements Filer. It calls the search endpoint
// and returns true if any returned issue matches the title prefix and
// was created within DedupWindow.
func (p *PaperclipFiler) AlreadyFiledRecently(ctx context.Context, titlePrefix string) (bool, error) {
	endpoint := fmt.Sprintf("%s/api/companies/%s/issues?q=%s",
		p.BaseURL, url.PathEscape(p.CompanyID), url.QueryEscape(titlePrefix))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("build search request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, body, err := p.doWithRetry(req, nil)
	if err != nil {
		return false, fmt.Errorf("dedup search: %w", err)
	}
	if resp.StatusCode >= 400 {
		return false, fmt.Errorf("dedup search: %d %s", resp.StatusCode, truncate(body, 256))
	}

	// Search may return either a bare array or a {"issues": [...]} envelope.
	var issues []searchIssue
	if jerr := json.Unmarshal(body, &issues); jerr != nil {
		var env struct {
			Issues []searchIssue `json:"issues"`
		}
		if eerr := json.Unmarshal(body, &env); eerr != nil {
			return false, fmt.Errorf("decode search response: %w (envelope: %v)", jerr, eerr)
		}
		issues = env.Issues
	}

	cutoff := p.Now().Add(-DedupWindow)
	for _, iss := range issues {
		if !strings.HasPrefix(iss.Title, titlePrefix) {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, iss.CreatedAt)
		if perr != nil {
			// If we can't parse, be conservative: skip this candidate.
			// A bad timestamp shouldn't suppress a legitimate file action.
			continue
		}
		if ts.After(cutoff) {
			return true, nil
		}
	}
	return false, nil
}

type createIssueRequest struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	Status           string `json:"status"`
	Priority         string `json:"priority"`
	ParentID         string `json:"parentId,omitempty"`
	ProjectID        string `json:"projectId,omitempty"`
	AssigneeAgentID  string `json:"assigneeAgentId,omitempty"`
}

type createIssueResponse struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
}

// File implements Filer.
func (p *PaperclipFiler) File(ctx context.Context, draft GraduationDraft) (string, error) {
	body := createIssueRequest{
		Title:           draft.Title(),
		Description:     draft.Body(),
		Status:          "todo",
		Priority:        "medium",
		ParentID:        draft.ParentIssueID,
		ProjectID:       p.ProjectID,
		AssigneeAgentID: p.AssigneeAgent,
	}
	enc, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("encode create payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/api/companies/%s/issues", p.BaseURL, url.PathEscape(p.CompanyID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(enc))
	if err != nil {
		return "", fmt.Errorf("build create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, respBody, err := p.doWithRetry(req, enc)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", &APIError{Status: resp.StatusCode, Body: truncate(respBody, 256)}
	}

	var out createIssueResponse
	if jerr := json.Unmarshal(respBody, &out); jerr != nil {
		return "", fmt.Errorf("decode create response: %w", jerr)
	}
	id := out.Identifier
	if id == "" {
		id = out.ID
	}
	return id, nil
}

// APIError is the error type returned for 4xx/5xx responses (post-retry).
// The orchestrator inspects Status to decide whether to degrade-to-watch.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("paperclip api: %d %s", e.Status, e.Body)
}

// doWithRetry sends the request, retrying once on 429/5xx/network errors
// per plan §6.1. The retryBody is needed because http.Request.Body is a
// one-shot Reader — pass nil for GET requests.
func (p *PaperclipFiler) doWithRetry(req *http.Request, retryBody []byte) (*http.Response, []byte, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		// Reset the request body for retry attempts.
		if attempt > 0 && retryBody != nil {
			req.Body = io.NopCloser(bytes.NewReader(retryBody))
		}

		resp, err := p.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if attempt < MaxRetries {
				p.log().Warn("paperclip http error, retrying", "attempt", attempt, "err", err.Error())
				select {
				case <-time.After(RetryDelay):
				case <-req.Context().Done():
					return nil, nil, req.Context().Err()
				}
				continue
			}
			return nil, nil, fmt.Errorf("paperclip http: %w", err)
		}

		body, rerr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			if attempt < MaxRetries {
				select {
				case <-time.After(RetryDelay):
				case <-req.Context().Done():
					return nil, nil, req.Context().Err()
				}
				continue
			}
			return nil, nil, fmt.Errorf("read response body: %w", rerr)
		}

		// Plan §6.1 retry policy: 429 + 5xx are retriable; other 4xx are not.
		retriable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if retriable && attempt < MaxRetries {
			p.log().Warn("paperclip retriable status, retrying", "attempt", attempt, "status", resp.StatusCode)
			select {
			case <-time.After(RetryDelay):
			case <-req.Context().Done():
				return nil, nil, req.Context().Err()
			}
			continue
		}
		return resp, body, nil
	}
	if lastErr == nil {
		lastErr = errors.New("retry budget exhausted")
	}
	return nil, nil, lastErr
}

func truncate(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
