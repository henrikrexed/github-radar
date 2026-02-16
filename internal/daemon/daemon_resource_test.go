package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	gh "github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
)

// newTestDaemonWithMock creates a Daemon wired to a mock GitHub server.
// It bypasses the normal New() constructor to avoid requiring real OTel setup.
func newTestDaemonWithMock(t *testing.T, repoCount int, mockServer *httptest.Server) *Daemon {
	t.Helper()

	client, err := gh.NewClient("test-token")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	client.SetBaseURL(mockServer.URL)

	tmpDir := t.TempDir()
	store := state.NewStore(filepath.Join(tmpDir, "state.json"))

	scanner := gh.NewScanner(client, store)
	scanner.SetLogger(func(level, msg string, args ...interface{}) {
		// silent during tests
	})

	repos := make([]config.TrackedRepo, repoCount)
	for i := 0; i < repoCount; i++ {
		repos[i] = config.TrackedRepo{
			Repo:       fmt.Sprintf("owner/repo-%d", i),
			Categories: []string{"test"},
		}
	}

	cfg := &config.Config{
		GitHub: config.GithubConfig{
			Token:     "test-token",
			RateLimit: 4000,
		},
		Repositories: repos,
	}

	ctx, cancel := context.WithCancel(context.Background())

	mux := http.NewServeMux()
	d := &Daemon{
		cfg:        cfg,
		daemonCfg:  DaemonConfig{Interval: 1 * time.Hour, HTTPAddr: ":0", DryRun: true},
		client:     client,
		scanner:    scanner,
		store:      store,
		status:     StatusIdle,
		startTime:  time.Now(),
		ctx:        ctx,
		cancel:     cancel,
		server:     &http.Server{Handler: mux},
		reloadChan: make(chan os.Signal, 1),
	}

	mux.HandleFunc("/health", d.handleHealth)
	mux.HandleFunc("/status", d.handleStatus)

	return d
}

func newMockGitHubServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")

		path := r.URL.Path
		if strings.Contains(path, "/releases") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		if strings.Contains(path, "/pulls") || strings.Contains(path, "/issues") ||
			strings.Contains(path, "/contributors") {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`{
			"owner": {"login": "owner"},
			"name": "repo",
			"full_name": "owner/repo",
			"stargazers_count": 100,
			"forks_count": 10,
			"open_issues_count": 5,
			"language": "Go",
			"topics": [],
			"description": ""
		}`))
	}))
}

func TestDaemon_MultipleScanCycles_NoMemoryLeak(t *testing.T) {
	mockServer := newMockGitHubServer()
	defer mockServer.Close()

	d := newTestDaemonWithMock(t, 10, mockServer)
	defer d.cancel()

	// Warm up with one scan
	d.runScan()

	runtime.GC()
	var memBaseline runtime.MemStats
	runtime.ReadMemStats(&memBaseline)

	// Run multiple scan cycles
	for cycle := 0; cycle < 10; cycle++ {
		d.runScan()
	}

	runtime.GC()
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	var memGrowthMB float64
	if memAfter.Alloc > memBaseline.Alloc {
		memGrowthMB = float64(memAfter.Alloc-memBaseline.Alloc) / 1024 / 1024
	}
	t.Logf("Memory growth after 10 scan cycles (10 repos each): %.2f MB", memGrowthMB)

	// 10 cycles of 10 repos should not accumulate significant memory
	if memGrowthMB > 20 {
		t.Errorf("Possible memory leak: %.2f MB growth after 10 scan cycles", memGrowthMB)
	}
}

func TestDaemon_ScanOverlapPrevention(t *testing.T) {
	mockServer := newMockGitHubServer()
	defer mockServer.Close()

	d := newTestDaemonWithMock(t, 5, mockServer)
	defer d.cancel()

	// Lock the scan mutex to simulate an in-progress scan
	d.scanMu.Lock()

	done := make(chan bool, 1)
	go func() {
		// This should skip because the mutex is held
		d.runScan()
		done <- true
	}()

	select {
	case <-done:
		// runScan returned immediately (TryLock failed)
		t.Log("Second scan correctly skipped while first is in progress")
	case <-time.After(2 * time.Second):
		t.Error("runScan blocked waiting for mutex - overlap prevention may not work")
	}

	d.scanMu.Unlock()
}

func TestDaemon_HealthEndpoint_DuringScan(t *testing.T) {
	mockServer := newMockGitHubServer()
	defer mockServer.Close()

	d := newTestDaemonWithMock(t, 5, mockServer)
	defer d.cancel()
	d.setReady(true)
	d.setStatus(StatusScanning)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	d.handleHealth(w, req)

	// Should still be healthy during scanning
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 during scanning, got %d", w.Code)
	}

	var resp map[string]bool
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp["healthy"] {
		t.Error("Expected healthy=true during scanning")
	}
}

func TestDaemon_StatusEndpoint_AfterScan(t *testing.T) {
	mockServer := newMockGitHubServer()
	defer mockServer.Close()

	d := newTestDaemonWithMock(t, 5, mockServer)
	defer d.cancel()

	// Run a scan to populate status
	d.runScan()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	d.handleStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status endpoint returned %d", w.Code)
	}

	var resp StatusResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Status != string(StatusIdle) {
		t.Errorf("Expected idle status after scan, got %s", resp.Status)
	}
	if resp.ReposTracked != 5 {
		t.Errorf("Expected 5 repos tracked, got %d", resp.ReposTracked)
	}
	if resp.LastScan == "" {
		t.Error("Expected last_scan to be set after scan")
	}

	t.Logf("Status after scan: %+v", resp)
}

func TestDaemon_GracefulStop(t *testing.T) {
	mockServer := newMockGitHubServer()
	defer mockServer.Close()

	d := newTestDaemonWithMock(t, 3, mockServer)

	// Start the daemon in background
	done := make(chan error, 1)
	go func() {
		done <- d.Run()
	}()

	// Give it time to start
	time.Sleep(200 * time.Millisecond)

	// Stop it
	d.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("Daemon stopped with: %v", err)
		}
		t.Log("Daemon stopped gracefully")
	case <-time.After(15 * time.Second):
		t.Fatal("Daemon did not stop within 15 seconds")
	}
}

func TestDaemon_MultipleCycles_GoroutineStability(t *testing.T) {
	mockServer := newMockGitHubServer()
	defer mockServer.Close()

	d := newTestDaemonWithMock(t, 5, mockServer)
	defer d.cancel()

	goroutinesBefore := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		d.runScan()
	}

	// Give time for any goroutines to settle
	time.Sleep(200 * time.Millisecond)

	goroutinesAfter := runtime.NumGoroutine()
	leaked := goroutinesAfter - goroutinesBefore

	t.Logf("Goroutines: before=%d, after=%d, diff=%d", goroutinesBefore, goroutinesAfter, leaked)

	if leaked > 3 {
		t.Errorf("Possible goroutine leak: %d new goroutines after 5 scan cycles", leaked)
	}
}
