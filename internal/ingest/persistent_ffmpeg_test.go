package ingest

import "testing"

func TestExtractJPEGFrame(t *testing.T) {
	jpegA := []byte{0xFF, 0xD8, 0x01, 0x02, 0xFF, 0xD9}
	jpegB := []byte{0xFF, 0xD8, 0x03, 0x04, 0xFF, 0xD9}
	stream := append([]byte{0x00, 0x00}, append(jpegA, jpegB...)...)

	frame, rest, ok := extractJPEGFrame(stream)
	if !ok {
		t.Fatalf("expected first frame")
	}
	if len(frame) != len(jpegA) {
		t.Fatalf("unexpected first frame length")
	}
	frame2, rest2, ok := extractJPEGFrame(rest)
	if !ok {
		t.Fatalf("expected second frame")
	}
	if len(frame2) != len(jpegB) {
		t.Fatalf("unexpected second frame length")
	}
	if len(rest2) != 0 {
		t.Fatalf("expected empty rest")
	}
}
