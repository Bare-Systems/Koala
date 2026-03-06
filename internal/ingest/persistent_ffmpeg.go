package ingest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type PersistentFFMpegSnapshotter struct {
	binary    string
	sampleFPS int

	mu      sync.Mutex
	workers map[string]*ffmpegWorker
	closed  bool
}

func NewPersistentFFMpegSnapshotter(sampleFPS int) *PersistentFFMpegSnapshotter {
	if sampleFPS <= 0 {
		sampleFPS = 1
	}
	return &PersistentFFMpegSnapshotter{
		binary:    "ffmpeg",
		sampleFPS: sampleFPS,
		workers:   map[string]*ffmpegWorker{},
	}
}

func (s *PersistentFFMpegSnapshotter) Capture(ctx context.Context, rtspURL string) ([]byte, error) {
	worker, err := s.ensureWorker(rtspURL)
	if err != nil {
		return nil, err
	}
	return worker.LatestFrame(ctx)
}

func (s *PersistentFFMpegSnapshotter) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	for _, w := range s.workers {
		w.Stop()
	}
	return nil
}

func (s *PersistentFFMpegSnapshotter) ensureWorker(rtspURL string) (*ffmpegWorker, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fmt.Errorf("snapshotter is closed")
	}
	if w, ok := s.workers[rtspURL]; ok {
		return w, nil
	}
	w := newFFMpegWorker(s.binary, rtspURL, s.sampleFPS)
	s.workers[rtspURL] = w
	w.Start()
	return w, nil
}

type ffmpegWorker struct {
	binary    string
	rtspURL   string
	sampleFPS int

	mu      sync.Mutex
	latest  []byte
	lastErr error
	notify  chan struct{}
	stop    chan struct{}
	stopped chan struct{}
	started bool
}

func newFFMpegWorker(binary string, rtspURL string, sampleFPS int) *ffmpegWorker {
	if binary == "" {
		binary = "ffmpeg"
	}
	if sampleFPS <= 0 {
		sampleFPS = 1
	}
	return &ffmpegWorker{
		binary:    binary,
		rtspURL:   rtspURL,
		sampleFPS: sampleFPS,
		notify:    make(chan struct{}, 1),
		stop:      make(chan struct{}),
		stopped:   make(chan struct{}),
	}
}

func (w *ffmpegWorker) Start() {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.mu.Unlock()
	go w.loop()
}

func (w *ffmpegWorker) Stop() {
	select {
	case <-w.stop:
		return
	default:
		close(w.stop)
	}
	<-w.stopped
}

func (w *ffmpegWorker) LatestFrame(ctx context.Context) ([]byte, error) {
	for {
		w.mu.Lock()
		if len(w.latest) > 0 {
			frame := append([]byte{}, w.latest...)
			w.mu.Unlock()
			return frame, nil
		}
		lastErr := w.lastErr
		notify := w.notify
		w.mu.Unlock()

		if lastErr != nil {
			// keep waiting for recovery unless context expires
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return nil, fmt.Errorf("no frame available: %w", lastErr)
			}
			return nil, ctx.Err()
		case <-notify:
		}
	}
}

func (w *ffmpegWorker) loop() {
	defer close(w.stopped)
	for {
		select {
		case <-w.stop:
			return
		default:
		}

		err := w.runOnce()
		if err != nil {
			w.setError(err)
		}
		select {
		case <-w.stop:
			return
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func (w *ffmpegWorker) runOnce() error {
	cmd := exec.Command(
		w.binary,
		"-hide_banner",
		"-loglevel", "error",
		"-rtsp_transport", "tcp",
		"-i", w.rtspURL,
		"-vf", fmt.Sprintf("fps=%d", w.sampleFPS),
		"-f", "image2pipe",
		"-vcodec", "mjpeg",
		"-",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	readErr := w.readFrames(stdout)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	if readErr != nil && readErr != io.EOF {
		return fmt.Errorf("read frames: %w", readErr)
	}
	if stderr.Len() > 0 {
		return fmt.Errorf("ffmpeg stderr: %s", stderr.String())
	}
	return nil
}

func (w *ffmpegWorker) readFrames(reader io.Reader) error {
	buf := make([]byte, 0, 2*1024*1024)
	tmp := make([]byte, 32*1024)
	for {
		select {
		case <-w.stop:
			return nil
		default:
		}
		n, err := reader.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			for {
				frame, rest, ok := extractJPEGFrame(buf)
				if !ok {
					buf = rest
					if len(buf) > 4*1024*1024 {
						buf = buf[len(buf)-2*1024*1024:]
					}
					break
				}
				buf = rest
				w.setFrame(frame)
			}
		}
		if err != nil {
			return err
		}
	}
}

func extractJPEGFrame(data []byte) ([]byte, []byte, bool) {
	start := bytes.Index(data, []byte{0xFF, 0xD8})
	if start < 0 {
		if len(data) > 2 {
			return nil, data[len(data)-2:], false
		}
		return nil, data, false
	}
	endRel := bytes.Index(data[start+2:], []byte{0xFF, 0xD9})
	if endRel < 0 {
		return nil, data[start:], false
	}
	end := start + 2 + endRel + 2
	frame := append([]byte{}, data[start:end]...)
	rest := data[end:]
	return frame, rest, true
}

func (w *ffmpegWorker) setFrame(frame []byte) {
	w.mu.Lock()
	w.latest = append([]byte{}, frame...)
	w.lastErr = nil
	w.mu.Unlock()
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

func (w *ffmpegWorker) setError(err error) {
	w.mu.Lock()
	w.lastErr = err
	w.mu.Unlock()
	select {
	case w.notify <- struct{}{}:
	default:
	}
}
