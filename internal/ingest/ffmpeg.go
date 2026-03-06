package ingest

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

type FFMpegSnapshotter struct {
	Binary string
}

func NewFFMpegSnapshotter() *FFMpegSnapshotter {
	return &FFMpegSnapshotter{Binary: "ffmpeg"}
}

func (s *FFMpegSnapshotter) Capture(ctx context.Context, rtspURL string) ([]byte, error) {
	binary := s.Binary
	if binary == "" {
		binary = "ffmpeg"
	}
	cmd := exec.CommandContext(
		ctx,
		binary,
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", rtspURL,
		"-frames:v", "1",
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg capture failed after %s: %w (%s)", time.Since(start).Round(time.Millisecond), err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("ffmpeg returned empty frame")
	}
	return stdout.Bytes(), nil
}
