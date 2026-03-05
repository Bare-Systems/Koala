package camera

import "sync"

type Status string

const (
	StatusUnknown     Status = "unknown"
	StatusAvailable   Status = "available"
	StatusUnavailable Status = "unavailable"
)

type Camera struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RTSPURL   string `json:"rtsp_url,omitempty"`
	ZoneID    string `json:"zone_id"`
	FrontDoor bool   `json:"front_door"`
	Status    Status `json:"status"`
}

type Registry struct {
	mu      sync.RWMutex
	cameras map[string]Camera
}

func NewRegistry(cameras []Camera) *Registry {
	result := make(map[string]Camera, len(cameras))
	for _, c := range cameras {
		result[c.ID] = c
	}
	return &Registry{cameras: result}
}

func (r *Registry) SetStatus(cameraID string, status Status) {
	r.mu.Lock()
	defer r.mu.Unlock()

	camera, ok := r.cameras[cameraID]
	if !ok {
		return
	}
	camera.Status = status
	r.cameras[cameraID] = camera
}

func (r *Registry) List() []Camera {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Camera, 0, len(r.cameras))
	for _, camera := range r.cameras {
		out = append(out, camera)
	}
	return out
}

func (r *Registry) FrontDoorCameraID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, camera := range r.cameras {
		if camera.FrontDoor {
			return camera.ID
		}
	}
	return ""
}

func (r *Registry) Get(cameraID string) (Camera, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	camera, ok := r.cameras[cameraID]
	return camera, ok
}
