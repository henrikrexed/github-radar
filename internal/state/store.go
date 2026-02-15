// Package state provides JSON state persistence for github-radar.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultStatePath is the default path for the state file.
const DefaultStatePath = "data/state.json"

// CurrentVersion is the current state file format version.
const CurrentVersion = 1

// State represents the persisted state of the scanner.
type State struct {
	Version   int                  `json:"version"`
	LastScan  time.Time            `json:"last_scan"`
	Repos     map[string]RepoState `json:"repos"`
	Discovery DiscoveryState       `json:"discovery"`
}

// RepoState contains persisted metrics for a single repository.
type RepoState struct {
	Owner            string    `json:"owner"`
	Name             string    `json:"name"`
	Stars            int       `json:"stars"`
	StarsPrev        int       `json:"stars_prev"`
	Forks            int       `json:"forks"`
	Contributors     int       `json:"contributors"`
	LastCollected    time.Time `json:"last_collected"`
	StarVelocity     float64   `json:"star_velocity"`
	StarAcceleration float64   `json:"star_acceleration"`
	GrowthScore      float64   `json:"growth_score"`
	ETag             string    `json:"etag"`
	LastModified     string    `json:"last_modified"`
}

// DiscoveryState contains persisted state for topic discovery.
type DiscoveryState struct {
	LastScan   time.Time         `json:"last_scan"`
	KnownRepos map[string]bool   `json:"known_repos"`
	TopicScans map[string]time.Time `json:"topic_scans"`
}

// Store manages state persistence.
type Store struct {
	mu       sync.RWMutex
	path     string
	state    *State
	modified bool
}

// NewStore creates a new state store with the given file path.
func NewStore(path string) *Store {
	if path == "" {
		path = DefaultStatePath
	}
	return &Store{
		path: path,
		state: &State{
			Version:   CurrentVersion,
			Repos:     make(map[string]RepoState),
			Discovery: DiscoveryState{
				KnownRepos: make(map[string]bool),
				TopicScans: make(map[string]time.Time),
			},
		},
	}
}

// Load loads state from the file. Returns nil error if file doesn't exist.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize empty state
			s.state = &State{
				Version:   CurrentVersion,
				Repos:     make(map[string]RepoState),
				Discovery: DiscoveryState{
					KnownRepos: make(map[string]bool),
					TopicScans: make(map[string]time.Time),
				},
			}
			return nil
		}
		return fmt.Errorf("reading state file: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("parsing state file: %w", err)
	}

	// Ensure maps are initialized
	if state.Repos == nil {
		state.Repos = make(map[string]RepoState)
	}
	if state.Discovery.KnownRepos == nil {
		state.Discovery.KnownRepos = make(map[string]bool)
	}
	if state.Discovery.TopicScans == nil {
		state.Discovery.TopicScans = make(map[string]time.Time)
	}

	s.state = &state
	s.modified = false
	return nil
}

// Save persists state to the file using atomic write.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	// Marshal state
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	// Write to temp file with unique name (PID + timestamp) to avoid collisions
	tmpPath := fmt.Sprintf("%s.tmp.%d.%d", s.path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, s.path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("renaming state file: %w", err)
	}

	s.modified = false
	return nil
}

// Path returns the state file path.
func (s *Store) Path() string {
	return s.path
}

// GetRepoState returns the state for a repository.
// Returns nil if not found.
func (s *Store) GetRepoState(fullName string) *RepoState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if state, ok := s.state.Repos[fullName]; ok {
		// Return a copy
		copy := state
		return &copy
	}
	return nil
}

// SetRepoState updates the state for a repository.
func (s *Store) SetRepoState(fullName string, state RepoState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Repos[fullName] = state
	s.modified = true
}

// DeleteRepoState removes a repository from state.
func (s *Store) DeleteRepoState(fullName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.state.Repos, fullName)
	s.modified = true
}

// AllRepoStates returns a copy of all repository states.
func (s *Store) AllRepoStates() map[string]RepoState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]RepoState, len(s.state.Repos))
	for k, v := range s.state.Repos {
		result[k] = v
	}
	return result
}

// RepoCount returns the number of tracked repositories.
func (s *Store) RepoCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.state.Repos)
}

// GetLastScan returns the timestamp of the last scan.
func (s *Store) GetLastScan() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state.LastScan
}

// SetLastScan updates the last scan timestamp.
func (s *Store) SetLastScan(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.LastScan = t
	s.modified = true
}

// IsModified returns true if the state has unsaved changes.
func (s *Store) IsModified() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.modified
}

// MarkKnownRepo marks a repository as known for discovery.
func (s *Store) MarkKnownRepo(fullName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Discovery.KnownRepos[fullName] = true
	s.modified = true
}

// IsKnownRepo checks if a repository is already known.
func (s *Store) IsKnownRepo(fullName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state.Discovery.KnownRepos[fullName]
}

// SetTopicScan records when a topic was last scanned.
func (s *Store) SetTopicScan(topic string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Discovery.TopicScans[topic] = t
	s.modified = true
}

// GetTopicScan returns when a topic was last scanned.
func (s *Store) GetTopicScan(topic string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state.Discovery.TopicScans[topic]
}

// GetDiscoveryLastScan returns the last discovery scan time.
func (s *Store) GetDiscoveryLastScan() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.state.Discovery.LastScan
}

// SetDiscoveryLastScan updates the last discovery scan time.
func (s *Store) SetDiscoveryLastScan(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Discovery.LastScan = t
	s.modified = true
}
