package camera

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// cachedEntry is the persisted form of a single camera's last-known state.
type cachedEntry struct {
	Status     Status     `json:"status"`
	Capability Capability `json:"capability"`
	UpdatedAt  string     `json:"updated_at"`
}

// CapabilityCache persists per-camera probe results to a JSON file so the
// registry can be primed with last-known state on restart, avoiding the
// cold-start penalty of blocking on live probes before any frames are
// processed. Live probe results always win over cached data.
type CapabilityCache struct {
	mu      sync.Mutex
	path    string
	entries map[string]cachedEntry
}

// LoadCapabilityCache reads the cache from path.
// If the file does not exist, an empty cache is returned (no error).
// Corrupt or unreadable files are silently discarded — the orchestrator
// will fall back to live probing, which is always the source of truth.
func LoadCapabilityCache(path string) (*CapabilityCache, error) {
	c := &CapabilityCache{
		path:    path,
		entries: map[string]cachedEntry{},
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read capability cache: %w", err)
	}
	if jsonErr := json.Unmarshal(data, &c.entries); jsonErr != nil {
		// Corrupt file: start fresh rather than failing startup.
		c.entries = map[string]cachedEntry{}
	}
	return c, nil
}

// Warm primes the registry with last-known status and capability for cameras
// that are still at StatusUnknown (i.e. not yet probed this session).
// Live probe results always take precedence and must be applied after Warm.
func (c *CapabilityCache) Warm(reg *Registry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cam := range reg.List() {
		entry, ok := c.entries[cam.ID]
		if !ok {
			continue
		}
		// Only warm cameras that haven't been resolved by a live probe yet.
		if cam.Status != StatusUnknown {
			continue
		}
		reg.SetStatus(cam.ID, entry.Status)
		reg.SetCapability(cam.ID, entry.Capability)
	}
}

// Snapshot captures the current registry state and writes it to disk.
// The write is atomic (tmp file → rename) to prevent partial writes.
func (c *CapabilityCache) Snapshot(reg *Registry) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, cam := range reg.List() {
		c.entries[cam.ID] = cachedEntry{
			Status:     cam.Status,
			Capability: cam.Capability,
			UpdatedAt:  now,
		}
	}
	return c.saveLocked()
}

func (c *CapabilityCache) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return fmt.Errorf("mkdir for capability cache: %w", err)
	}
	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal capability cache: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write capability cache tmp: %w", err)
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("rename capability cache: %w", err)
	}
	return nil
}
