// Package zone provides polygon-based zone filtering for detection results.
//
// Zone polygons are defined in normalized coordinates (0–1 in both X and Y,
// relative to the original frame dimensions). An axis-aligned bounding box
// (BBox) from a detection result is considered "in zone" when the fraction of
// its area that overlaps the zone polygon meets or exceeds a configurable
// minimum threshold.
package zone

import "math"

// Point is a 2-D coordinate in normalized (0–1) frame space.
type Point struct{ X, Y float64 }

// Polygon is an ordered list of vertices defining a closed region.
type Polygon []Point

// BBox is an axis-aligned bounding box in normalized frame coordinates.
// X, Y are the top-left corner; W, H are width and height.
type BBox struct{ X, Y, W, H float64 }

// Overlap returns the fraction of bbox area that is covered by the zone polygon.
// Returns 0 when there is no intersection or the polygon has fewer than 3 vertices.
// Returns 1 when the bbox is fully inside the polygon.
func Overlap(zone Polygon, bbox BBox) float64 {
	if len(zone) < 3 {
		return 0
	}
	bboxArea := bbox.W * bbox.H
	if bboxArea <= 0 {
		return 0
	}
	subject := bboxAsPolygon(bbox)
	clipped := sutherlandHodgman(subject, zone)
	if len(clipped) < 3 {
		return 0
	}
	return math.Min(polygonArea(clipped)/bboxArea, 1.0)
}

// InZone reports whether the bbox overlaps the zone polygon by at least minOverlap
// (fraction of bbox area). If zone has fewer than 3 vertices, all detections pass.
func InZone(zone Polygon, bbox BBox, minOverlap float64) bool {
	if len(zone) < 3 {
		return true // no polygon configured → no filtering
	}
	return Overlap(zone, bbox) >= minOverlap
}

// bboxAsPolygon converts a BBox to a counter-clockwise polygon (winding
// compatible with the Sutherland-Hodgman clip algorithm below).
func bboxAsPolygon(b BBox) Polygon {
	return Polygon{
		{b.X, b.Y},
		{b.X + b.W, b.Y},
		{b.X + b.W, b.Y + b.H},
		{b.X, b.Y + b.H},
	}
}

// sutherlandHodgman clips the subject polygon against each edge of the clip
// polygon in turn, returning the intersection polygon.
func sutherlandHodgman(subject, clip Polygon) Polygon {
	output := subject
	for i := range clip {
		if len(output) == 0 {
			return nil
		}
		input := output
		output = nil
		a := clip[i]
		b := clip[(i+1)%len(clip)]
		for j := range input {
			cur := input[j]
			prev := input[(j+len(input)-1)%len(input)]
			curIn := isInside(cur, a, b)
			prevIn := isInside(prev, a, b)
			if curIn {
				if !prevIn {
					output = append(output, edgeIntersect(prev, cur, a, b))
				}
				output = append(output, cur)
			} else if prevIn {
				output = append(output, edgeIntersect(prev, cur, a, b))
			}
		}
	}
	return output
}

// isInside reports whether point p is on the left (inner) side of directed edge a→b.
func isInside(p, a, b Point) bool {
	return (b.X-a.X)*(p.Y-a.Y) >= (b.Y-a.Y)*(p.X-a.X)
}

// edgeIntersect returns the intersection of lines (a→b) and (c→d).
func edgeIntersect(a, b, c, d Point) Point {
	a1 := b.Y - a.Y
	b1 := a.X - b.X
	c1 := a1*a.X + b1*a.Y
	a2 := d.Y - c.Y
	b2 := c.X - d.X
	c2 := a2*c.X + b2*c.Y
	det := a1*b2 - a2*b1
	if det == 0 {
		return a // parallel lines — return first point as degenerate fallback
	}
	return Point{
		X: (c1*b2 - c2*b1) / det,
		Y: (a1*c2 - a2*c1) / det,
	}
}

// polygonArea computes the area of a polygon using the shoelace formula.
func polygonArea(p Polygon) float64 {
	area := 0.0
	n := len(p)
	for i := range p {
		j := (i + 1) % n
		area += p[i].X * p[j].Y
		area -= p[j].X * p[i].Y
	}
	if area < 0 {
		area = -area
	}
	return area / 2
}
