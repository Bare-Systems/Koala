package zone

import (
	"math"
	"testing"
)

func TestOverlapFullyInside(t *testing.T) {
	// Zone covers the whole frame; any bbox should have overlap ≈ 1.
	zone := Polygon{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	bbox := BBox{X: 0.3, Y: 0.3, W: 0.2, H: 0.2}
	got := Overlap(zone, bbox)
	if math.Abs(got-1.0) > 0.01 {
		t.Fatalf("expected overlap ≈ 1, got %f", got)
	}
}

func TestOverlapFullyOutside(t *testing.T) {
	// Zone is the bottom half; bbox is entirely in the top half.
	zone := Polygon{{0, 0.5}, {1, 0.5}, {1, 1}, {0, 1}}
	bbox := BBox{X: 0.2, Y: 0.0, W: 0.2, H: 0.2}
	got := Overlap(zone, bbox)
	if got > 0.01 {
		t.Fatalf("expected overlap ≈ 0 (bbox above zone), got %f", got)
	}
}

func TestOverlapPartial(t *testing.T) {
	// Zone covers right half of frame.
	zone := Polygon{{0.5, 0}, {1, 0}, {1, 1}, {0.5, 1}}
	// Bbox straddles x=0.5 with equal halves on each side.
	bbox := BBox{X: 0.25, Y: 0.25, W: 0.5, H: 0.5}
	got := Overlap(zone, bbox)
	// Half of bbox should be in zone ≈ 0.5.
	if math.Abs(got-0.5) > 0.05 {
		t.Fatalf("expected overlap ≈ 0.5, got %f", got)
	}
}

func TestInZoneNoPolygon(t *testing.T) {
	// Empty polygon → always in-zone.
	if !InZone(nil, BBox{0, 0, 0.5, 0.5}, 0.5) {
		t.Fatal("empty polygon should pass all detections")
	}
}

func TestInZoneThresholdMet(t *testing.T) {
	zone := Polygon{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	bbox := BBox{X: 0.1, Y: 0.1, W: 0.3, H: 0.3}
	if !InZone(zone, bbox, 0.5) {
		t.Fatal("bbox fully inside zone should be in-zone")
	}
}

func TestInZoneThresholdNotMet(t *testing.T) {
	// Zone is bottom-right quarter; bbox is in top-left quarter — no overlap.
	zone := Polygon{{0.5, 0.5}, {1, 0.5}, {1, 1}, {0.5, 1}}
	bbox := BBox{X: 0, Y: 0, W: 0.4, H: 0.4}
	if InZone(zone, bbox, 0.5) {
		t.Fatal("bbox outside zone should not be in-zone")
	}
}

func TestOverlapDegenerateZeroSizeBBox(t *testing.T) {
	zone := Polygon{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	bbox := BBox{X: 0.5, Y: 0.5, W: 0, H: 0}
	got := Overlap(zone, bbox)
	if got != 0 {
		t.Fatalf("zero-size bbox should return 0 overlap, got %f", got)
	}
}
