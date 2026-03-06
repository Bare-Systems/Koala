package audit

import "context"

type Event struct {
	ID        int64  `json:"id"`
	Category  string `json:"category"`
	EventType string `json:"event_type"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	RolloutID string `json:"rollout_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	KeyID     string `json:"key_id,omitempty"`
	CreatedAt string `json:"created_at"`
	Payload   any    `json:"payload,omitempty"`
}

type ListOptions struct {
	Category string
	Limit    int
}

type Store interface {
	Record(ctx context.Context, e Event) error
	List(ctx context.Context, options ListOptions) ([]Event, error)
	Close() error
}
