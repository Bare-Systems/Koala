package snapshot

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"testing"

	"github.com/Bare-Systems/Koala/internal/zone"
)

func TestStorePutGet(t *testing.T) {
	s := NewStore(3)
	s.Put("1", []byte("a"))
	got, ok := s.Get("1")
	if !ok || string(got) != "a" {
		t.Fatalf("expected to retrieve stored snapshot, got ok=%v val=%q", ok, got)
	}
	if _, ok := s.Get("missing"); ok {
		t.Fatal("expected miss for unknown id")
	}
}

func TestStoreEvictsOldest(t *testing.T) {
	s := NewStore(2)
	for i := 1; i <= 3; i++ {
		s.Put(strconv.Itoa(i), []byte{byte(i)})
	}
	if _, ok := s.Get("1"); ok {
		t.Fatal("expected oldest snapshot (1) to be evicted")
	}
	for _, id := range []string{"2", "3"} {
		if _, ok := s.Get(id); !ok {
			t.Fatalf("expected snapshot %s to be retained", id)
		}
	}
}

func TestStoreIgnoresEmpty(t *testing.T) {
	s := NewStore(2)
	s.Put("", []byte("x"))
	s.Put("1", nil)
	if _, ok := s.Get(""); ok {
		t.Fatal("empty id should not be stored")
	}
	if _, ok := s.Get("1"); ok {
		t.Fatal("empty frame should not be stored")
	}
}

func TestAnnotateReturnsValidJPEG(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			src.SetRGBA(x, y, color.RGBA{R: 50, G: 50, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatalf("encode source: %v", err)
	}

	out := Annotate(buf.Bytes(), []zone.BBox{{X: 0.25, Y: 0.25, W: 0.5, H: 0.5}})
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("annotated output is not a valid JPEG: %v", err)
	}
}

func TestAnnotateNoBoxesReturnsInput(t *testing.T) {
	in := []byte("not-a-jpeg")
	if out := Annotate(in, nil); !bytes.Equal(out, in) {
		t.Fatal("expected unchanged input when no boxes provided")
	}
}

func TestAnnotateBadFrameReturnsInput(t *testing.T) {
	in := []byte("not-a-jpeg")
	out := Annotate(in, []zone.BBox{{X: 0, Y: 0, W: 0.5, H: 0.5}})
	if !bytes.Equal(out, in) {
		t.Fatal("expected original bytes returned on decode failure")
	}
}
