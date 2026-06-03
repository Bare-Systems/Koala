package snapshot

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"

	"github.com/Bare-Systems/Koala/internal/zone"
)

// boxColor is the emerald used for detection outlines, matching the dashboard
// alerts theme.
var boxColor = color.RGBA{R: 16, G: 185, B: 129, A: 255}

// Annotate decodes the JPEG frame, draws an outline for each detection box
// (coordinates are normalized 0–1 relative to the frame), and re-encodes it.
// On any decode/encode failure it returns the original bytes unchanged — a
// plain snapshot is better than none.
func Annotate(frame []byte, boxes []zone.BBox) []byte {
	if len(boxes) == 0 {
		return frame
	}
	img, err := jpeg.Decode(bytes.NewReader(frame))
	if err != nil {
		return frame
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	w, h := bounds.Dx(), bounds.Dy()
	thickness := w / 240
	if thickness < 2 {
		thickness = 2
	}
	for _, b := range boxes {
		x0 := bounds.Min.X + int(b.X*float64(w))
		y0 := bounds.Min.Y + int(b.Y*float64(h))
		x1 := x0 + int(b.W*float64(w))
		y1 := y0 + int(b.H*float64(h))
		drawRect(rgba, x0, y0, x1, y1, thickness)
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, rgba, &jpeg.Options{Quality: 85}); err != nil {
		return frame
	}
	return out.Bytes()
}

// drawRect strokes an axis-aligned rectangle outline of the given thickness by
// filling its four edge bars, clamped to the image bounds.
func drawRect(img *image.RGBA, x0, y0, x1, y1, thickness int) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	fill := func(rx0, ry0, rx1, ry1 int) {
		b := img.Bounds()
		for y := ry0; y < ry1; y++ {
			if y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			for x := rx0; x < rx1; x++ {
				if x < b.Min.X || x >= b.Max.X {
					continue
				}
				img.SetRGBA(x, y, boxColor)
			}
		}
	}
	fill(x0, y0, x1, y0+thickness) // top
	fill(x0, y1-thickness, x1, y1) // bottom
	fill(x0, y0, x0+thickness, y1) // left
	fill(x1-thickness, y0, x1, y1) // right
}
