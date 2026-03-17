package inference

import (
	"context"
	"time"

	"github.com/baresystems/koala/internal/zone"
)

type Detection struct {
	CameraID   string    `json:"camera_id"`
	ZoneID     string    `json:"zone_id"`
	Label      string    `json:"label"`
	Confidence float64   `json:"confidence"`
	Timestamp  time.Time `json:"timestamp"`
	BBox       zone.BBox `json:"bbox,omitempty"`
}

type FrameRequest struct {
	CameraID string    `json:"camera_id"`
	ZoneID   string    `json:"zone_id"`
	FrameB64 string    `json:"frame_b64,omitempty"`
	Captured time.Time `json:"captured_at"`
}

type FrameResponse struct {
	ModelVersion string      `json:"model_version"`
	Detections   []Detection `json:"detections"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type Client interface {
	AnalyzeFrame(ctx context.Context, req FrameRequest) (FrameResponse, error)
	WorkerHealth(ctx context.Context) (HealthResponse, error)
}
