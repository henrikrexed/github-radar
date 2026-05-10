package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/discovery"
	"github.com/hrexed/github-radar/internal/github"
	"github.com/hrexed/github-radar/internal/state"
)

// TestMapDiscoveryGHArchiveConfig_Defaults — a zero-value
// DiscoveryGHArchiveConfig (no fields set) maps to a discovery
// GHArchiveSourceConfig that's also at its zero value. The discovery
// layer treats zero TopN / zero ActivityFloor as "use the package
// default" so the daemon does not need to copy DefaultGHArchive*
// constants here.
func TestMapDiscoveryGHArchiveConfig_Defaults(t *testing.T) {
	got := mapDiscoveryGHArchiveConfig(config.DiscoveryGHArchiveConfig{})

	want := discovery.GHArchiveSourceConfig{
		Enabled:       false,
		TopN:          0,
		ActivityFloor: 0,
		MinStarsGate:  0,
	}
	if got != want {
		t.Errorf("zero-value mapping = %+v, want %+v", got, want)
	}
}

// TestMapDiscoveryGHArchiveConfig_FullyPopulated — every relevant
// field on the user-facing config flows into the discovery shape.
// The collector-side knobs (window_hours, event_types) are *not*
// expected here — they go through mapDiscoveryGHArchiveCollectorConfig.
func TestMapDiscoveryGHArchiveConfig_FullyPopulated(t *testing.T) {
	in := config.DiscoveryGHArchiveConfig{
		Enabled:       true,
		WindowHours:   24,
		TopNPerHour:   500,
		ActivityFloor: 10,
		EventTypes:    []string{"WatchEvent"},
		MinStarsGate:  50,
		DailyCapWarn:  4000,
		DailyCapHard:  5000,
	}
	got := mapDiscoveryGHArchiveConfig(in)

	want := discovery.GHArchiveSourceConfig{
		Enabled:       true,
		TopN:          500,
		ActivityFloor: 10,
		MinStarsGate:  50,
	}
	if got != want {
		t.Errorf("mapping = %+v, want %+v", got, want)
	}
}

// TestMapDiscoveryGHArchiveCollectorConfig_Defaults — a zero-value
// user config produces a collector config with zero Window and nil
// EventTypes; both fall back to package defaults inside
// GHArchiveConfig.withDefaults().
func TestMapDiscoveryGHArchiveCollectorConfig_Defaults(t *testing.T) {
	got := mapDiscoveryGHArchiveCollectorConfig(config.DiscoveryGHArchiveConfig{})

	if got.Window != 0 {
		t.Errorf("Window = %v, want 0 (so withDefaults uses DefaultGHArchiveWindow)", got.Window)
	}
	if got.EventTypes != nil {
		t.Errorf("EventTypes = %+v, want nil (so withDefaults uses DefaultGHArchiveEventTypes)", got.EventTypes)
	}
}

// TestMapDiscoveryGHArchiveCollectorConfig_PopulatedKnobs — both
// Window and EventTypes propagate. WindowHours=24 → 24h. The
// EventTypes slice is copied so a later mutation of the user config
// does not race with the live collector.
func TestMapDiscoveryGHArchiveCollectorConfig_PopulatedKnobs(t *testing.T) {
	userTypes := []string{"WatchEvent", "ForkEvent"}
	in := config.DiscoveryGHArchiveConfig{
		Enabled:     true,
		WindowHours: 24,
		EventTypes:  userTypes,
	}
	got := mapDiscoveryGHArchiveCollectorConfig(in)

	if got.Window != 24*time.Hour {
		t.Errorf("Window = %v, want 24h", got.Window)
	}
	if len(got.EventTypes) != 2 || got.EventTypes[0] != "WatchEvent" || got.EventTypes[1] != "ForkEvent" {
		t.Errorf("EventTypes = %+v, want [WatchEvent ForkEvent]", got.EventTypes)
	}
	// Mutating the source slice must not affect the mapped slice.
	userTypes[0] = "MUTATED"
	if got.EventTypes[0] != "WatchEvent" {
		t.Errorf("EventTypes[0] = %q after source mutation, want WatchEvent (slice not copied)", got.EventTypes[0])
	}
}

// newTestDiscoverer builds a Discoverer with stubbed GitHub client +
// state.Store, suitable for asserting that wireDiscoveryGHArchive
// flipped the gharchive source on/off via the public
// DiscoverFromGHArchive contract.
func newTestDiscoverer(t *testing.T, src discovery.GHArchiveSourceConfig) *discovery.Discoverer {
	t.Helper()
	client, err := github.NewClient("test-token")
	if err != nil {
		t.Fatalf("github.NewClient: %v", err)
	}
	store := state.NewStore("")
	d := discovery.NewDiscoverer(client, store, discovery.Config{
		Sources: discovery.SourcesConfig{GHArchive: src},
	})
	d.SetSearchThrottle(0)
	return d
}

// TestWireDiscoveryGHArchive_DisabledIsNoOp — when Enabled=false the
// helper does nothing and the Discoverer's gharchive source remains
// nil (DiscoverFromGHArchive returns (nil, nil)).
func TestWireDiscoveryGHArchive_DisabledIsNoOp(t *testing.T) {
	d := newTestDiscoverer(t, discovery.GHArchiveSourceConfig{Enabled: false})

	if err := wireDiscoveryGHArchive(d, config.DiscoveryGHArchiveConfig{Enabled: false}, discovery.NewMemoryCursorStore()); err != nil {
		t.Fatalf("wireDiscoveryGHArchive(disabled) returned err = %v, want nil", err)
	}

	res, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive err = %v, want nil", err)
	}
	if res != nil {
		t.Errorf("DiscoverFromGHArchive result = %+v, want nil (disabled gate)", res)
	}
}

// TestWireDiscoveryGHArchive_EnabledRegistersSource — when Enabled=true
// the helper constructs a GHArchiveSource and calls
// SetGHArchiveSource(src). The Discoverer's gharchive source is no
// longer nil, which we assert via the public DiscoverFromGHArchive
// contract: a wired-but-empty source returns a non-nil *Result with
// TotalFound=0 (no candidates yet, no REST hydration required).
func TestWireDiscoveryGHArchive_EnabledRegistersSource(t *testing.T) {
	cfg := config.DiscoveryGHArchiveConfig{
		Enabled:       true,
		WindowHours:   24,
		TopNPerHour:   500,
		ActivityFloor: 10,
		EventTypes:    []string{"WatchEvent"},
	}
	// Sources.GHArchive on the Discoverer must be populated separately
	// (NewDiscoverer copies the config block at construction time).
	d := newTestDiscoverer(t, mapDiscoveryGHArchiveConfig(cfg))

	if err := wireDiscoveryGHArchive(d, cfg, discovery.NewMemoryCursorStore()); err != nil {
		t.Fatalf("wireDiscoveryGHArchive(enabled) returned err = %v, want nil", err)
	}

	res, err := d.DiscoverFromGHArchive(context.Background())
	if err != nil {
		t.Fatalf("DiscoverFromGHArchive err = %v, want nil", err)
	}
	if res == nil {
		t.Fatal("DiscoverFromGHArchive result = nil, want non-nil (collector should be wired)")
	}
	if res.Topic != "gharchive" {
		t.Errorf("result.Topic = %q, want %q", res.Topic, "gharchive")
	}
	if res.TotalFound != 0 {
		t.Errorf("result.TotalFound = %d, want 0 (collector is empty, no archives processed yet)", res.TotalFound)
	}
}

// TestWireDiscoveryGHArchive_NilDiscovererErrors — defensive guard.
// The daemon's discoverer is nil when discovery is disabled at the
// top level; the wiring helper must surface a clear error rather than
// segfault.
func TestWireDiscoveryGHArchive_NilDiscovererErrors(t *testing.T) {
	err := wireDiscoveryGHArchive(nil, config.DiscoveryGHArchiveConfig{Enabled: true}, discovery.NewMemoryCursorStore())
	if err == nil {
		t.Fatal("wireDiscoveryGHArchive(nil disc) err = nil, want non-nil")
	}
}

// TestWireDiscoveryGHArchive_NilCursorStoreErrors — when Enabled=true
// but the cursor store is nil (e.g., metadata DB failed to open) the
// helper must error out instead of letting NewGHArchiveSource panic.
// The daemon caller logs a warning and skips wiring when the DB is
// unavailable; this guard catches a programmer mistake further in.
func TestWireDiscoveryGHArchive_NilCursorStoreErrors(t *testing.T) {
	d := newTestDiscoverer(t, discovery.GHArchiveSourceConfig{Enabled: true})

	err := wireDiscoveryGHArchive(d, config.DiscoveryGHArchiveConfig{Enabled: true}, nil)
	if err == nil {
		t.Fatal("wireDiscoveryGHArchive(nil cursorStore) err = nil, want non-nil")
	}
}
