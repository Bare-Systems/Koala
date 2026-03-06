package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/service"
)

type fakeSnapshotter struct {
	mu      sync.Mutex
	payload []byte
	err     error
	calls   int
}

func (s *fakeSnapshotter) Capture(_ context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.payload, nil
}

func (s *fakeSnapshotter) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type fakeSubmitter struct {
	mu    sync.Mutex
	tasks []service.FrameTask
}

func (s *fakeSubmitter) Submit(task service.FrameTask) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	return true
}

func (s *fakeSubmitter) TaskCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.tasks)
}

func TestManagerSubmitsFramesAndMarksCameraAvailable(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", RTSPURL: "rtsp://example", ZoneID: "front_door", Status: camera.StatusUnknown}})
	snapshotter := &fakeSnapshotter{payload: []byte("jpeg")}
	submitter := &fakeSubmitter{}
	m := NewManager(registry, submitter, snapshotter, 10*time.Millisecond, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	time.Sleep(35 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)

	if snapshotter.CallCount() == 0 {
		t.Fatalf("expected snapshotter to be called")
	}
	if submitter.TaskCount() == 0 {
		t.Fatalf("expected frame tasks to be submitted")
	}
	cam, ok := registry.Get("cam1")
	if !ok {
		t.Fatalf("expected camera to exist")
	}
	if cam.Status != camera.StatusAvailable {
		t.Fatalf("expected camera status available, got %s", cam.Status)
	}
}

func TestManagerMarksCameraUnavailableOnCaptureFailure(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", RTSPURL: "rtsp://example", ZoneID: "front_door", Status: camera.StatusUnknown}})
	snapshotter := &fakeSnapshotter{err: errors.New("capture failed")}
	submitter := &fakeSubmitter{}
	m := NewManager(registry, submitter, snapshotter, 10*time.Millisecond, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	time.Sleep(25 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)

	cam, ok := registry.Get("cam1")
	if !ok {
		t.Fatalf("expected camera to exist")
	}
	if cam.Status != camera.StatusUnavailable {
		t.Fatalf("expected camera status unavailable, got %s", cam.Status)
	}
}
