// Package snapshot retains the camera frame that triggered a detection alert so
// operators can visually confirm what was detected. Snapshots are held in a
// bounded in-memory ring keyed by alert ID — the lifecycle mirrors the alert
// ring buffer in the state aggregator, so snapshots are lost on restart just
// like the alerts they belong to.
package snapshot

import "sync"

// DefaultMax matches the aggregator's alert ring capacity so a snapshot exists
// for roughly every retained alert.
const DefaultMax = 200

// Store is a bounded, concurrency-safe map of alert ID → JPEG bytes. The oldest
// entry is evicted once the store exceeds its capacity.
type Store struct {
	mu    sync.Mutex
	max   int
	order []string
	items map[string][]byte
}

// NewStore returns a Store retaining at most max snapshots. A max of 0 or less
// uses DefaultMax.
func NewStore(max int) *Store {
	if max <= 0 {
		max = DefaultMax
	}
	return &Store{
		max:   max,
		items: make(map[string][]byte, max),
	}
}

// Put stores the JPEG bytes under the given alert ID, evicting the oldest
// snapshot if the store is at capacity. Empty frames are ignored.
func (s *Store) Put(id string, jpeg []byte) {
	if id == "" || len(jpeg) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[id]; !exists {
		s.order = append(s.order, id)
	}
	s.items[id] = jpeg
	for len(s.order) > s.max {
		oldest := s.order[0]
		s.order = s.order[1:]
		delete(s.items, oldest)
	}
}

// Get returns the JPEG bytes stored for the given alert ID.
func (s *Store) Get(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	frame, ok := s.items[id]
	return frame, ok
}
