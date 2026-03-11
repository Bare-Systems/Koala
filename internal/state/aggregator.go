package state

import (
	"sort"
	"sync"
	"time"

	"github.com/barelabs/koala/internal/zone"
)

type Detection struct {
	CameraID   string    `json:"camera_id"`
	ZoneID     string    `json:"zone_id"`
	Label      string    `json:"label"`
	Confidence float64   `json:"confidence"`
	ObservedAt time.Time `json:"observed_at"`
	BBox       zone.BBox `json:"bbox,omitempty"`
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
	// Stale is true when no detections have been ingested for this zone,
	// or when all detections in the window have expired.
	Stale bool `json:"stale"`
}

type Aggregator struct {
	mu            sync.RWMutex
	window        time.Duration
	minDetections int // 0 = disabled; >0 = min count required to declare entity present
	history       map[string][]Detection
	tracked       map[string]struct{}
}

// NewAggregator creates an aggregator with the given freshness window.
// minDetections sets the temporal smoothing threshold: an entity is only
// considered present if at least this many detections exist in the window.
// Pass 0 to disable smoothing (any single detection marks entity as present).
func NewAggregator(window time.Duration, minDetections ...int) *Aggregator {
	if window <= 0 {
		window = 90 * time.Second
	}
	n := 0
	if len(minDetections) > 0 {
		n = minDetections[0]
	}
	return &Aggregator{
		window:        window,
		minDetections: n,
		history:       map[string][]Detection{},
		tracked:       map[string]struct{}{"package": {}, "person": {}},
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

	// Count detections per label for temporal smoothing gate.
	countByLabel := map[string]int{}
	for _, d := range zoneDetections {
		countByLabel[d.Label]++
	}

	byLabel := map[string]EntityState{}
	for _, d := range zoneDetections {
		// Smoothing gate: skip label until it has enough observations.
		if a.minDetections > 0 && countByLabel[d.Label] < a.minDetections {
			continue
		}
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
		Stale:        observedAt.IsZero(),
	}
}
