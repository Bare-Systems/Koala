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
