package state

import (
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Bare-Systems/Koala/internal/zone"
)

const defaultMaxAlerts = 200

// Alert records a single absent→present transition for a tracked entity in a
// zone. Alerts are the user-facing signal that "something happened" — they fire
// only on the rising edge, not continuously while an entity remains present.
type Alert struct {
	ID         string    `json:"id"`
	ZoneID     string    `json:"zone_id"`
	CameraID   string    `json:"camera_id"`
	Label      string    `json:"label"`
	Confidence float64   `json:"confidence"`
	ObservedAt time.Time `json:"observed_at"`
}

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

	// present tracks the last-known presence of each tracked label per zone so
	// that alerts fire only on the absent→present rising edge.
	present   map[string]map[string]bool
	alerts    []Alert
	maxAlerts int
	alertSeq  int64
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
		present:       map[string]map[string]bool{},
		maxAlerts:     defaultMaxAlerts,
	}
}

// Ingest records detections and returns the alerts that fired on this call
// (absent→present rising edges). The return value lets callers capture the
// triggering frame for each new alert; callers that don't need it may ignore it.
func (a *Aggregator) Ingest(detections []Detection) []Alert {
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

	return a.detectTransitionsLocked(now)
}

// detectTransitionsLocked compares current presence to the last-known presence
// for each zone and emits an Alert on every absent→present rising edge. It
// returns the alerts that fired on this call. The caller must hold a.mu.
func (a *Aggregator) detectTransitionsLocked(now time.Time) []Alert {
	var fired []Alert
	for zoneID, zoneDetections := range a.history {
		reps := a.presentReps(zoneDetections)
		zonePresent := a.present[zoneID]
		if zonePresent == nil {
			zonePresent = map[string]bool{}
			a.present[zoneID] = zonePresent
		}
		for label := range a.tracked {
			rep, isPresent := reps[label]
			was := zonePresent[label]
			if isPresent && !was {
				a.alertSeq++
				alert := Alert{
					ID:         strconv.FormatInt(a.alertSeq, 10),
					ZoneID:     zoneID,
					CameraID:   rep.CameraID,
					Label:      label,
					Confidence: rep.Confidence,
					ObservedAt: rep.ObservedAt,
				}
				a.alerts = append(a.alerts, alert)
				if len(a.alerts) > a.maxAlerts {
					a.alerts = a.alerts[len(a.alerts)-a.maxAlerts:]
				}
				fired = append(fired, alert)
			}
			zonePresent[label] = isPresent
		}
	}
	return fired
}

// presentReps returns the representative detection (latest, highest-confidence)
// for each tracked label currently considered present in the given window-pruned
// detections, applying the same smoothing gate used by Zone().
func (a *Aggregator) presentReps(zoneDetections []Detection) map[string]Detection {
	countByLabel := map[string]int{}
	for _, d := range zoneDetections {
		countByLabel[d.Label]++
	}
	reps := map[string]Detection{}
	for _, d := range zoneDetections {
		if _, ok := a.tracked[d.Label]; !ok {
			continue
		}
		if a.minDetections > 0 && countByLabel[d.Label] < a.minDetections {
			continue
		}
		cur, ok := reps[d.Label]
		if !ok || d.ObservedAt.After(cur.ObservedAt) || d.Confidence > cur.Confidence {
			reps[d.Label] = d
		}
	}
	return reps
}

// RecentAlerts returns up to limit most-recent alerts, newest first. A limit of
// 0 or less returns all retained alerts.
func (a *Aggregator) RecentAlerts(limit int) []Alert {
	a.mu.RLock()
	defer a.mu.RUnlock()

	n := len(a.alerts)
	if limit > 0 && limit < n {
		n = limit
	}
	out := make([]Alert, 0, n)
	for i := len(a.alerts) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, a.alerts[i])
	}
	return out
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
