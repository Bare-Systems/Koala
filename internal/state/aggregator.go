package state

import (
	"sort"
	"sync"
	"time"
)

type Detection struct {
	CameraID   string    `json:"camera_id"`
	ZoneID     string    `json:"zone_id"`
	Label      string    `json:"label"`
	Confidence float64   `json:"confidence"`
	ObservedAt time.Time `json:"observed_at"`
}

type EntityState struct {
	Label      string    `json:"label"`
	Present    bool      `json:"present"`
	Confidence float64   `json:"confidence"`
	ObservedAt time.Time `json:"observed_at"`
}

type ZoneState struct {
	ZoneID       string        `json:"zone_id"`
	ObservedAt   time.Time     `json:"observed_at"`
	Entities     []EntityState `json:"entities"`
	FreshnessSec int64         `json:"freshness_seconds"`
}

type Aggregator struct {
	mu      sync.RWMutex
	window  time.Duration
	history map[string][]Detection
	tracked map[string]struct{}
}

func NewAggregator(window time.Duration) *Aggregator {
	if window <= 0 {
		window = 90 * time.Second
	}
	return &Aggregator{
		window:  window,
		history: map[string][]Detection{},
		tracked: map[string]struct{}{"package": {}, "person": {}},
	}
}

func (a *Aggregator) Ingest(detections []Detection) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now().UTC()
	for _, d := range detections {
		if _, ok := a.tracked[d.Label]; !ok {
			continue
		}
		a.history[d.ZoneID] = append(a.history[d.ZoneID], d)
	}
	for zoneID, zoneDetections := range a.history {
		filtered := zoneDetections[:0]
		for _, d := range zoneDetections {
			if now.Sub(d.ObservedAt) <= a.window {
				filtered = append(filtered, d)
			}
		}
		a.history[zoneID] = filtered
	}
}

func (a *Aggregator) Zone(zoneID string) ZoneState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	now := time.Now().UTC()
	zoneDetections := a.history[zoneID]

	byLabel := map[string]EntityState{}
	for _, d := range zoneDetections {
		current, exists := byLabel[d.Label]
		if !exists || d.ObservedAt.After(current.ObservedAt) || d.Confidence > current.Confidence {
			byLabel[d.Label] = EntityState{
				Label:      d.Label,
				Present:    true,
				Confidence: d.Confidence,
				ObservedAt: d.ObservedAt,
			}
		}
	}

	entities := make([]EntityState, 0, len(a.tracked))
	for label := range a.tracked {
		entry, ok := byLabel[label]
		if !ok {
			entry = EntityState{Label: label, Present: false}
		}
		entities = append(entities, entry)
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].Label < entities[j].Label })

	observedAt := time.Time{}
	for _, e := range entities {
		if e.ObservedAt.After(observedAt) {
			observedAt = e.ObservedAt
		}
	}
	freshness := int64(0)
	if !observedAt.IsZero() {
		freshness = int64(now.Sub(observedAt).Seconds())
	}

	return ZoneState{
		ZoneID:       zoneID,
		ObservedAt:   observedAt,
		Entities:     entities,
		FreshnessSec: freshness,
	}
}
