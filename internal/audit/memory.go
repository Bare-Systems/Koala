package audit

import (
	"context"
	"sync"
)

type MemoryStore struct {
	mu     sync.Mutex
	nextID int64
	events []Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{nextID: 1, events: []Event{}}
}

func (s *MemoryStore) Record(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = s.nextID
	s.nextID++
	s.events = append(s.events, e)
	return nil
}

func (s *MemoryStore) List(_ context.Context, options ListOptions) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := options.Limit
	if limit <= 0 {
		limit = 100
	}
	filtered := make([]Event, 0)
	for _, e := range s.events {
		if options.Category != "" && e.Category != options.Category {
			continue
		}
		filtered = append(filtered, e)
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := make([]Event, len(filtered))
	copy(out, filtered)
	return out, nil
}

func (s *MemoryStore) Close() error {
	return nil
}
