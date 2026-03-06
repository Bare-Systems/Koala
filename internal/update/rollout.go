package update

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type RolloutMode string

const (
	RolloutModeAll    RolloutMode = "all"
	RolloutModeBatch  RolloutMode = "batch"
	RolloutModeCanary RolloutMode = "canary"
)

type RolloutStatus string

const (
	RolloutStatusRunning   RolloutStatus = "running"
	RolloutStatusCompleted RolloutStatus = "completed"
	RolloutStatusFailed    RolloutStatus = "failed"
	RolloutStatusStopped   RolloutStatus = "stopped"
)

type RolloutRequest struct {
	Manifest            Manifest      `json:"manifest"`
	DeviceIDs           []string      `json:"device_ids,omitempty"`
	Mode                RolloutMode   `json:"mode"`
	BatchSize           int           `json:"batch_size,omitempty"`
	MaxFailures         int           `json:"max_failures,omitempty"`
	PauseBetweenBatches time.Duration `json:"pause_between_batches_ms,omitempty"`
	RollbackScope       string        `json:"rollback_scope,omitempty"`
}

type RolloutBatch struct {
	Index      int      `json:"index"`
	DeviceIDs  []string `json:"device_ids"`
	StageError string   `json:"stage_error,omitempty"`
	ApplyError string   `json:"apply_error,omitempty"`
	Completed  bool     `json:"completed"`
}

type DeviceRolloutState struct {
	DeviceID       string      `json:"device_id"`
	State          DeviceState `json:"state"`
	CurrentVersion string      `json:"current_version"`
	TargetVersion  string      `json:"target_version,omitempty"`
	LastError      string      `json:"last_error,omitempty"`
	BatchIndex     int         `json:"batch_index"`
}

type Rollout struct {
	ID            string                        `json:"id"`
	Mode          RolloutMode                   `json:"mode"`
	Status        RolloutStatus                 `json:"status"`
	CreatedAt     time.Time                     `json:"created_at"`
	UpdatedAt     time.Time                     `json:"updated_at"`
	Manifest      Manifest                      `json:"manifest"`
	MaxFailures   int                           `json:"max_failures"`
	FailureCount  int                           `json:"failure_count"`
	RollbackScope string                        `json:"rollback_scope"`
	Batches       []RolloutBatch                `json:"batches"`
	Devices       map[string]DeviceRolloutState `json:"devices"`
	Events        []string                      `json:"events"`
}

func (m *Manager) StartRollout(req RolloutRequest) (Rollout, error) {
	if _, err := m.Check(req.Manifest, req.DeviceIDs); err != nil {
		return Rollout{}, err
	}
	mode := req.Mode
	if mode == "" {
		mode = RolloutModeAll
	}
	if mode != RolloutModeAll && mode != RolloutModeBatch && mode != RolloutModeCanary {
		return Rollout{}, fmt.Errorf("invalid rollout mode")
	}
	if req.MaxFailures < 0 {
		return Rollout{}, fmt.Errorf("max_failures must be >= 0")
	}
	if req.RollbackScope == "" {
		req.RollbackScope = "failed"
	}
	if req.RollbackScope != "failed" && req.RollbackScope != "batch" {
		return Rollout{}, fmt.Errorf("rollback_scope must be failed or batch")
	}

	deviceIDs := req.DeviceIDs
	if len(deviceIDs) == 0 {
		all := m.Status()
		deviceIDs = make([]string, 0, len(all))
		for _, d := range all {
			deviceIDs = append(deviceIDs, d.ID)
		}
		sort.Strings(deviceIDs)
	}

	batches, err := buildBatches(deviceIDs, mode, req.BatchSize)
	if err != nil {
		return Rollout{}, err
	}

	now := time.Now().UTC()
	rollout := Rollout{
		ID:            fmt.Sprintf("r-%d", now.UnixNano()),
		Mode:          mode,
		Status:        RolloutStatusRunning,
		CreatedAt:     now,
		UpdatedAt:     now,
		Manifest:      req.Manifest,
		MaxFailures:   req.MaxFailures,
		RollbackScope: req.RollbackScope,
		Batches:       make([]RolloutBatch, 0, len(batches)),
		Devices:       map[string]DeviceRolloutState{},
		Events:        []string{"rollout_started"},
	}
	for _, id := range deviceIDs {
		rollout.Devices[id] = DeviceRolloutState{DeviceID: id, BatchIndex: -1}
	}

	m.mu.Lock()
	m.rollouts[rollout.ID] = rollout
	m.mu.Unlock()

	for batchIndex, batchDeviceIDs := range batches {
		batch := RolloutBatch{Index: batchIndex, DeviceIDs: append([]string{}, batchDeviceIDs...)}
		rollout.Events = append(rollout.Events, fmt.Sprintf("batch_%d_stage_start", batchIndex))
		_, stageErr := m.Stage(req.Manifest, batchDeviceIDs)
		if stageErr != nil {
			batch.StageError = stageErr.Error()
			rollout.FailureCount++
			rollout.Events = append(rollout.Events, fmt.Sprintf("batch_%d_stage_failed", batchIndex))
			if req.RollbackScope == "batch" {
				_, _ = m.Rollback(batchDeviceIDs, "rollout_stage_failed")
			}
			rollout.Batches = append(rollout.Batches, batch)
			rollout = m.refreshRolloutDevices(rollout, batchDeviceIDs, batchIndex)
			if rollout.FailureCount > rollout.MaxFailures {
				rollout.Status = RolloutStatusStopped
				rollout.Events = append(rollout.Events, "failure_threshold_exceeded")
				break
			}
			continue
		}

		rollout.Events = append(rollout.Events, fmt.Sprintf("batch_%d_apply_start", batchIndex))
		_, applyErr := m.Apply(batchDeviceIDs)
		if applyErr != nil {
			batch.ApplyError = applyErr.Error()
			rollout.FailureCount++
			rollout.Events = append(rollout.Events, fmt.Sprintf("batch_%d_apply_failed", batchIndex))
			if req.RollbackScope == "batch" {
				_, _ = m.Rollback(batchDeviceIDs, "rollout_apply_failed")
			}
		} else {
			batch.Completed = true
			rollout.Events = append(rollout.Events, fmt.Sprintf("batch_%d_completed", batchIndex))
		}
		rollout.Batches = append(rollout.Batches, batch)
		rollout = m.refreshRolloutDevices(rollout, batchDeviceIDs, batchIndex)
		if rollout.FailureCount > rollout.MaxFailures {
			rollout.Status = RolloutStatusStopped
			rollout.Events = append(rollout.Events, "failure_threshold_exceeded")
			break
		}
		if req.PauseBetweenBatches > 0 && batchIndex < len(batches)-1 {
			time.Sleep(req.PauseBetweenBatches)
		}
	}

	if rollout.Status == RolloutStatusRunning {
		if rollout.FailureCount > 0 {
			rollout.Status = RolloutStatusFailed
		} else {
			rollout.Status = RolloutStatusCompleted
		}
	}
	rollout.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	m.rollouts[rollout.ID] = rollout
	m.mu.Unlock()
	return rollout, nil
}

func (m *Manager) GetRollout(id string) (Rollout, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.rollouts[id]
	return r, ok
}

func (m *Manager) ListRollouts() []Rollout {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Rollout, 0, len(m.rollouts))
	for _, r := range m.rollouts {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Manager) refreshRolloutDevices(rollout Rollout, ids []string, batchIndex int) Rollout {
	status := m.Status()
	byID := map[string]Device{}
	for _, d := range status {
		byID[d.ID] = d
	}
	for _, id := range ids {
		d, ok := byID[id]
		if !ok {
			continue
		}
		rollout.Devices[id] = DeviceRolloutState{
			DeviceID:       id,
			State:          d.State,
			CurrentVersion: d.CurrentVersion,
			TargetVersion:  d.TargetVersion,
			LastError:      d.LastError,
			BatchIndex:     batchIndex,
		}
	}
	rollout.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	m.rollouts[rollout.ID] = rollout
	m.mu.Unlock()
	return rollout
}

func buildBatches(deviceIDs []string, mode RolloutMode, batchSize int) ([][]string, error) {
	if len(deviceIDs) == 0 {
		return nil, fmt.Errorf("no devices selected for rollout")
	}
	sorted := append([]string{}, deviceIDs...)
	sort.Strings(sorted)
	if mode == RolloutModeAll {
		return [][]string{sorted}, nil
	}
	if mode == RolloutModeCanary {
		if len(sorted) == 1 {
			return [][]string{sorted}, nil
		}
		if batchSize <= 0 {
			batchSize = len(sorted) - 1
		}
		out := [][]string{{sorted[0]}}
		rest := sorted[1:]
		for len(rest) > 0 {
			n := batchSize
			if n > len(rest) {
				n = len(rest)
			}
			out = append(out, append([]string{}, rest[:n]...))
			rest = rest[n:]
		}
		return out, nil
	}
	if mode == RolloutModeBatch {
		if batchSize <= 0 {
			return nil, fmt.Errorf("batch_size must be > 0 for batch rollout")
		}
		out := make([][]string, 0)
		remaining := sorted
		for len(remaining) > 0 {
			n := batchSize
			if n > len(remaining) {
				n = len(remaining)
			}
			out = append(out, append([]string{}, remaining[:n]...))
			remaining = remaining[n:]
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported rollout mode: %s", mode)
}

func parseRolloutMode(mode string) RolloutMode {
	return RolloutMode(strings.ToLower(strings.TrimSpace(mode)))
}
