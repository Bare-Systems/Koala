package state

import (
	"testing"
	"time"
)

func TestAggregatorZoneState(t *testing.T) {
	agg := NewAggregator(120 * time.Second)
	now := time.Now().UTC()
	agg.Ingest([]Detection{{
		CameraID:   "cam1",
		ZoneID:     "front_door",
		Label:      "package",
		Confidence: 0.94,
		ObservedAt: now,
	}})

	zone := agg.Zone("front_door")
	if zone.ZoneID != "front_door" {
		t.Fatalf("unexpected zone: %s", zone.ZoneID)
	}
	if len(zone.Entities) != 2 {
		t.Fatalf("expected two tracked entities, got %d", len(zone.Entities))
	}

	var packageSeen bool
	for _, entity := range zone.Entities {
		if entity.Label == "package" {
			packageSeen = entity.Present
		}
	}
	if !packageSeen {
		t.Fatalf("expected package to be present")
	}
}

func TestAggregatorWindowExpiry(t *testing.T) {
	agg := NewAggregator(10 * time.Millisecond)
	agg.Ingest([]Detection{{
		CameraID:   "cam1",
		ZoneID:     "front_door",
		Label:      "person",
		Confidence: 0.90,
		ObservedAt: time.Now().UTC().Add(-time.Second),
	}})
	zone := agg.Zone("front_door")
	for _, entity := range zone.Entities {
		if entity.Label == "person" && entity.Present {
			t.Fatalf("expected stale person detection to be dropped")
		}
	}
}

func TestAggregatorTemporalSmoothing(t *testing.T) {
	now := time.Now().UTC()
	// minDetections=3: single detection should not mark entity present.
	agg := NewAggregator(120*time.Second, 3)
	agg.Ingest([]Detection{{
		CameraID: "cam1", ZoneID: "front_door",
		Label: "package", Confidence: 0.94, ObservedAt: now,
	}})
	zone := agg.Zone("front_door")
	for _, e := range zone.Entities {
		if e.Label == "package" && e.Present {
			t.Fatalf("single detection should not satisfy minDetections=3")
		}
	}

	// Ingest 2 more (total 3) → entity should now be present.
	agg.Ingest([]Detection{
		{CameraID: "cam1", ZoneID: "front_door", Label: "package", Confidence: 0.92, ObservedAt: now.Add(time.Second)},
		{CameraID: "cam1", ZoneID: "front_door", Label: "package", Confidence: 0.90, ObservedAt: now.Add(2 * time.Second)},
	})
	zone = agg.Zone("front_door")
	var found bool
	for _, e := range zone.Entities {
		if e.Label == "package" {
			found = e.Present
		}
	}
	if !found {
		t.Fatalf("expected package present after 3 detections with minDetections=3")
	}
}

func TestAggregatorAlertOnRisingEdge(t *testing.T) {
	now := time.Now().UTC()
	agg := NewAggregator(120 * time.Second)

	// First appearance → one alert.
	agg.Ingest([]Detection{{
		CameraID: "cam1", ZoneID: "front_door",
		Label: "person", Confidence: 0.91, ObservedAt: now,
	}})
	alerts := agg.RecentAlerts(0)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert on first appearance, got %d", len(alerts))
	}
	if alerts[0].Label != "person" || alerts[0].CameraID != "cam1" || alerts[0].ZoneID != "front_door" {
		t.Fatalf("unexpected alert contents: %+v", alerts[0])
	}

	// Still present → no new alert (no continuous re-fire).
	agg.Ingest([]Detection{{
		CameraID: "cam1", ZoneID: "front_door",
		Label: "person", Confidence: 0.93, ObservedAt: now.Add(time.Second),
	}})
	if got := len(agg.RecentAlerts(0)); got != 1 {
		t.Fatalf("expected no new alert while still present, got %d total", got)
	}
}

func TestAggregatorAlertRefiresAfterAbsence(t *testing.T) {
	window := 20 * time.Millisecond
	agg := NewAggregator(window)

	// First appearance → alert #1.
	agg.Ingest([]Detection{{
		CameraID: "cam1", ZoneID: "front_door",
		Label: "package", Confidence: 0.9, ObservedAt: time.Now().UTC(),
	}})
	if got := len(agg.RecentAlerts(0)); got != 1 {
		t.Fatalf("expected 1 alert after first appearance, got %d", got)
	}

	// Wait past the window so the first detection expires, then ingest an
	// unrelated label to trigger pruning — package transitions to absent.
	time.Sleep(window * 3)
	agg.Ingest([]Detection{{
		CameraID: "cam1", ZoneID: "front_door",
		Label: "person", Confidence: 0.9, ObservedAt: time.Now().UTC(),
	}})

	// Package reappears with a fresh timestamp → a second alert fires.
	agg.Ingest([]Detection{{
		CameraID: "cam1", ZoneID: "front_door",
		Label: "package", Confidence: 0.95, ObservedAt: time.Now().UTC(),
	}})
	pkgAlerts := 0
	for _, a := range agg.RecentAlerts(0) {
		if a.Label == "package" {
			pkgAlerts++
		}
	}
	if pkgAlerts != 2 {
		t.Fatalf("expected package alert to re-fire after absence, got %d package alerts", pkgAlerts)
	}
}

func TestAggregatorSmoothingDisabledByDefault(t *testing.T) {
	// Without minDetections, a single detection marks entity present.
	now := time.Now().UTC()
	agg := NewAggregator(120 * time.Second)
	agg.Ingest([]Detection{{
		CameraID: "cam1", ZoneID: "front_door",
		Label: "package", Confidence: 0.90, ObservedAt: now,
	}})
	zone := agg.Zone("front_door")
	var found bool
	for _, e := range zone.Entities {
		if e.Label == "package" {
			found = e.Present
		}
	}
	if !found {
		t.Fatalf("expected package present without smoothing")
	}
}
