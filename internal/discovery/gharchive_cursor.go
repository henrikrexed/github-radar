package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// gharchive_cursor.go provides cursor-store bindings for the gharchive
// discovery collector defined in gharchive_source.go.
//
// The architecture decision in [ISI-950 plan](/ISI/issues/ISI-950#document-plan)
// is "cursor advances only after full archive aggregation + rollup
// write succeeds". The collector handles that contract; this file just
// supplies the persistence backends.

// GHArchiveCursorMetadataKey is the key under which the cursor JSON is
// stored in any key/value-style metadata backend. Matches the AC in
// [ISI-951](/ISI/issues/ISI-951): `gharchive_discovery_cursor`.
const GHArchiveCursorMetadataKey = "gharchive_discovery_cursor"

// MetadataKVStore is the minimal interface satisfied by both
// `*database.DB` and any test stub. We don't import the database
// package here so the discovery package stays free of SQL deps.
type MetadataKVStore interface {
	GetMetadata(key string) (string, error)
	SetMetadata(key, value string) error
}

// MetadataCursorStore binds GHArchiveCursorStore to any MetadataKVStore
// (the production binding is `*database.DB`). The cursor is encoded as
// a small JSON document under GHArchiveCursorMetadataKey.
type MetadataCursorStore struct {
	store MetadataKVStore
	key   string
}

// NewMetadataCursorStore returns a cursor store backed by the given
// metadata KV. Pass `*database.DB` here from the daemon wiring.
func NewMetadataCursorStore(store MetadataKVStore) *MetadataCursorStore {
	return &MetadataCursorStore{store: store, key: GHArchiveCursorMetadataKey}
}

// gharchiveCursorJSON is the wire format used in the metadata blob.
// Fields match the architect's spec in
// [ISI-951](/ISI/issues/ISI-951#document-plan).
type gharchiveCursorJSON struct {
	LastProcessedArchive string    `json:"last_processed_archive"`
	CompletedAt          time.Time `json:"completed_at"`
}

// GetCursor loads the persisted cursor. Returns a zero cursor (and no
// error) when the key is missing — first start case.
func (m *MetadataCursorStore) GetCursor(_ context.Context) (GHArchiveCursor, error) {
	raw, err := m.store.GetMetadata(m.key)
	if err != nil {
		return GHArchiveCursor{}, fmt.Errorf("read cursor: %w", err)
	}
	if raw == "" {
		return GHArchiveCursor{}, nil
	}
	var doc gharchiveCursorJSON
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return GHArchiveCursor{}, fmt.Errorf("decode cursor %q: %w", raw, err)
	}
	return GHArchiveCursor(doc), nil
}

// SetCursor persists the cursor. Atomic from the metadata layer's POV
// (single SQL upsert under SQLite WAL).
func (m *MetadataCursorStore) SetCursor(_ context.Context, c GHArchiveCursor) error {
	doc := gharchiveCursorJSON{
		LastProcessedArchive: c.LastProcessedArchive,
		CompletedAt:          c.CompletedAt.UTC(),
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encode cursor: %w", err)
	}
	if err := m.store.SetMetadata(m.key, string(raw)); err != nil {
		return fmt.Errorf("write cursor: %w", err)
	}
	return nil
}

// MemoryCursorStore is an in-memory GHArchiveCursorStore used by tests
// and ephemeral CLI runs. Safe for concurrent use.
type MemoryCursorStore struct {
	mu     sync.Mutex
	cursor GHArchiveCursor
}

// NewMemoryCursorStore constructs an empty in-memory store.
func NewMemoryCursorStore() *MemoryCursorStore {
	return &MemoryCursorStore{}
}

// GetCursor returns the in-memory cursor.
func (m *MemoryCursorStore) GetCursor(_ context.Context) (GHArchiveCursor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cursor, nil
}

// SetCursor overwrites the in-memory cursor.
func (m *MemoryCursorStore) SetCursor(_ context.Context, c GHArchiveCursor) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cursor = c
	return nil
}
