package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// PaperclipFiler retry semantics (plan §6.1): one retry on 5xx/429/transport.
// On persistent 5xx, return *FileError so the orchestrator can degrade to
// watch-list.
func TestPaperclipFiler_Retries5xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // dedup search
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		c := atomic.AddInt32(&calls, 1)
		if c <= 2 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte("Service Unavailable"))
			return
		}
		_, _ = w.Write([]byte(`{"identifier":"ISI-NEW","id":"abc"}`))
	}))
	defer srv.Close()

	f, err := NewPaperclipFiler(PaperclipConfig{
		BaseURL: srv.URL, APIKey: "k", CompanyID: "c",
		RetryDelay: 1 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.File(context.Background(), Proposal{Category: "ai", ProposedSub: "x", Repos: []string{"a/b"}})
	if err == nil {
		t.Fatal("want error after both attempts return 503")
	}
	var fe *FileError
	if !asFileErr(err, &fe) {
		t.Fatalf("err = %v, want *FileError", err)
	}
	if fe.StatusCode != 503 {
		t.Errorf("StatusCode = %d, want 503", fe.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("POST attempts = %d, want 2 (1 initial + 1 retry)", got)
	}
}

// 4xx (non-429) → no retry.
func TestPaperclipFiler_NoRetryOn4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(400)
		_, _ = w.Write([]byte("Bad Request"))
	}))
	defer srv.Close()

	f, _ := NewPaperclipFiler(PaperclipConfig{
		BaseURL: srv.URL, APIKey: "k", CompanyID: "c",
		RetryDelay: 1 * time.Millisecond,
	})
	_, err := f.File(context.Background(), Proposal{Category: "ai", ProposedSub: "x"})
	if err == nil {
		t.Fatal("want error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("POST attempts = %d, want 1 (no retry on 4xx)", got)
	}
}

// 429 → retry once.
func TestPaperclipFiler_RetryOn429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		_, _ = w.Write([]byte(`{"identifier":"ISI-OK"}`))
	}))
	defer srv.Close()

	f, _ := NewPaperclipFiler(PaperclipConfig{
		BaseURL: srv.URL, APIKey: "k", CompanyID: "c",
		RetryDelay: 1 * time.Millisecond,
	})
	id, err := f.File(context.Background(), Proposal{Category: "ai", ProposedSub: "x"})
	if err != nil {
		t.Fatalf("want success after retry, got %v", err)
	}
	if id != "ISI-OK" {
		t.Errorf("identifier = %q, want ISI-OK", id)
	}
}

// FindRecentDuplicate uses the search response, not wall-clock — Q3 Layer 1.
func TestPaperclipFiler_DedupSearchPattern(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		issues    []map[string]string
		wantHit   bool
	}{
		{
			name:    "no_prior",
			issues:  nil,
			wantHit: false,
		},
		{
			name: "within_60d",
			issues: []map[string]string{{
				"id": "abc", "identifier": "ISI-1",
				"title":     "Subcat graduation proposal: ai/quantum",
				"createdAt": now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
				"status":    "todo",
			}},
			wantHit: true,
		},
		{
			name: "outside_60d",
			issues: []map[string]string{{
				"id": "abc", "identifier": "ISI-2",
				"title":     "Subcat graduation proposal: ai/quantum",
				"createdAt": now.Add(-65 * 24 * time.Hour).Format(time.RFC3339),
				"status":    "todo",
			}},
			wantHit: false,
		},
		{
			name: "title_prefix_mismatch",
			issues: []map[string]string{{
				"id": "abc", "identifier": "ISI-3",
				"title":     "Subcat graduation proposal: ai/SOMETHING-ELSE",
				"createdAt": now.Format(time.RFC3339),
				"status":    "todo",
			}},
			wantHit: false,
		},
		{
			name: "cancelled_within_window_does_not_dedupe",
			issues: []map[string]string{{
				"id": "abc", "identifier": "ISI-4",
				"title":     "Subcat graduation proposal: ai/quantum",
				"createdAt": now.Add(-10 * 24 * time.Hour).Format(time.RFC3339),
				"status":    "cancelled",
			}},
			wantHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"issues": tc.issues})
			}))
			defer srv.Close()
			f, _ := NewPaperclipFiler(PaperclipConfig{
				BaseURL: srv.URL, APIKey: "k", CompanyID: "c",
				Now: func() time.Time { return now },
			})
			id, err := f.FindRecentDuplicate(context.Background(), "Subcat graduation proposal: ai/quantum", now)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			gotHit := id != ""
			if gotHit != tc.wantHit {
				t.Errorf("hit = %v (id=%q), want %v", gotHit, id, tc.wantHit)
			}
		})
	}
}

// Smoke: filer issues a proper POST body — content-type, auth header, JSON shape.
func TestPaperclipFiler_PostShape(t *testing.T) {
	var capturedAuth string
	var capturedCT string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"issues":[]}`))
			return
		}
		capturedAuth = r.Header.Get("Authorization")
		capturedCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_, _ = w.Write([]byte(`{"identifier":"ISI-99"}`))
	}))
	defer srv.Close()

	f, _ := NewPaperclipFiler(PaperclipConfig{
		BaseURL: srv.URL, APIKey: "secret", CompanyID: "c",
		ProjectID: "proj-1", ParentID: "ISI-720", AssigneeID: "agent-1",
	})
	_, err := f.File(context.Background(), Proposal{
		Category: "ai", ProposedSub: "quantum-computing",
		Repos: []string{"ai/r1", "ai/r2"}, Tokens: []string{"quantum-computing"},
		AvgConf: 0.85, Score: 4.25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(capturedAuth, "Bearer ") {
		t.Errorf("Authorization header = %q, want Bearer ...", capturedAuth)
	}
	if !strings.HasPrefix(capturedCT, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", capturedCT)
	}
	for _, k := range []string{"title", "description", "status", "priority", "projectId", "parentId", "assigneeAgentId"} {
		if _, ok := capturedBody[k]; !ok {
			t.Errorf("POST body missing key %q (have: %v)", k, capturedBody)
		}
	}
	if title, _ := capturedBody["title"].(string); !strings.HasPrefix(title, "Subcat graduation proposal: ai/quantum-computing") {
		t.Errorf("title = %q, want prefix 'Subcat graduation proposal: ai/quantum-computing'", title)
	}
}
