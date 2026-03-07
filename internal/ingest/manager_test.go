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
	errors  []error
	calls   int
}

func (s *fakeSnapshotter) Capture(_ context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if len(s.errors) > 0 {
		err := s.errors[0]
		s.errors = s.errors[1:]
		if err != nil {
			return nil, err
		}
		return s.payload, nil
	}
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
	status := m.Status()
	if len(status.Incidents) == 0 {
		t.Fatalf("expected incidents to be recorded")
	}
	if status.Incidents[0].Type != "stream_failure" {
		t.Fatalf("expected stream_failure incident, got %s", status.Incidents[0].Type)
	}
}

func TestManagerRecordsRecoveryIncident(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", RTSPURL: "rtsp://example", ZoneID: "front_door", Status: camera.StatusUnknown}})
	snapshotter := &fakeSnapshotter{
		payload: []byte("jpeg"),
		errors:  []error{errors.New("capture failed"), nil, nil},
	}
	submitter := &fakeSubmitter{}
	m := NewManager(registry, submitter, snapshotter, 10*time.Millisecond, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)
	time.Sleep(45 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)

	status := m.Status()
	foundFailure := false
	foundRecovery := false
	for _, incident := range status.Incidents {
		if incident.Type == "stream_failure" {
			foundFailure = true
		}
		if incident.Type == "stream_recovered" {
			foundRecovery = true
		}
	}
	if !foundFailure || !foundRecovery {
		t.Fatalf("expected both failure and recovery incidents, got %+v", status.Incidents)
	}
}

func TestManagerWatchdogMarksStalled(t *testing.T) {
	registry := camera.NewRegistry([]camera.Camera{{ID: "cam1", RTSPURL: "rtsp://example", ZoneID: "front_door", Status: camera.StatusUnknown}})
	m := NewManager(registry, &fakeSubmitter{}, &fakeSnapshotter{payload: []byte("jpeg")}, time.Second, time.Second)
	m.stallTimeout = 20 * time.Millisecond
	m.increment("cam1", func(s *CameraStats) {
		s.LastCaptureAt = time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
		s.LastStatus = camera.StatusAvailable
	})
	m.checkStalls()

	status := m.Status()
	camStats := status.Cameras["cam1"]
	if camStats.LastStatus != camera.StatusDegraded {
		t.Fatalf("expected degraded camera status, got %s", camStats.LastStatus)
	}
	if len(status.Incidents) == 0 || status.Incidents[len(status.Incidents)-1].Type != "stream_stalled" {
		t.Fatalf("expected stream_stalled incident")
	}
}
