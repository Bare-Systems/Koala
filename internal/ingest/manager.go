package ingest

import (
	"context"
	"encoding/base64"
	"sync"
	"time"

	"github.com/barelabs/koala/internal/camera"
	"github.com/barelabs/koala/internal/service"
)

type Submitter interface {
	Submit(task service.FrameTask) bool
}

type Snapshotter interface {
	Capture(ctx context.Context, rtspURL string) ([]byte, error)
}

type Manager struct {
	registry       *camera.Registry
	submitter      Submitter
	snapshotter    Snapshotter
	sampleEvery    time.Duration
	captureTimeout time.Duration
}

func NewManager(registry *camera.Registry, submitter Submitter, snapshotter Snapshotter, sampleEvery time.Duration, captureTimeout time.Duration) *Manager {
	if sampleEvery <= 0 {
		sampleEvery = time.Second
	}
	if captureTimeout <= 0 {
		captureTimeout = 5 * time.Second
	}
	return &Manager{
		registry:       registry,
		submitter:      submitter,
		snapshotter:    snapshotter,
		sampleEvery:    sampleEvery,
		captureTimeout: captureTimeout,
	}
}

func (m *Manager) Start(ctx context.Context) {
	cameras := m.registry.List()
	var wg sync.WaitGroup
	for _, cam := range cameras {
		cameraInfo := cam
		if cameraInfo.RTSPURL == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.runCamera(ctx, cameraInfo)
		}()
	}
	go func() {
		<-ctx.Done()
		wg.Wait()
	}()
}

func (m *Manager) runCamera(ctx context.Context, cam camera.Camera) {
	ticker := time.NewTicker(m.sampleEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			captureCtx, cancel := context.WithTimeout(ctx, m.captureTimeout)
			frame, err := m.snapshotter.Capture(captureCtx, cam.RTSPURL)
			cancel()
			if err != nil {
				m.registry.SetStatus(cam.ID, camera.StatusUnavailable)
				continue
			}

			m.registry.SetStatus(cam.ID, camera.StatusAvailable)
			_ = m.submitter.Submit(service.FrameTask{
				CameraID: cam.ID,
				ZoneID:   cam.ZoneID,
				FrameB64: base64.StdEncoding.EncodeToString(frame),
				Captured: time.Now().UTC(),
			})
		}
	}
}
