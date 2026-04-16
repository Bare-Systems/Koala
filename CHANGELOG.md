# Changelog

All notable changes to Koala are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Changed

- Koala is now HTTP-only: worker inference stays private on `6704` and the orchestrator exposes the supported REST + MCP surfaces on `6705`
- Added a stateless MCP JSON-RPC endpoint at `/mcp` for BearClaw-class agent integrations while preserving existing `/mcp/tools/...` routes
- MCP `tools/call` now validates arguments against each tool's published `inputSchema` before dispatch and returns typed `invalid_input` errors instead of handler-dependent failures
- Ignored the repository-root `blink.toml` and `BLINK.md` and stopped tracking them so homelab-specific Blink targets and operator notes stay local-only.

### Removed

- Removed the legacy worker transport path and normalized the runtime contract to `6704` and `6705`

---

## [0.1.0-dev] — 2026-03-14

### Added

#### Core Orchestrator
- Go orchestrator binary (`cmd/koala-orchestrator`) with graceful shutdown (SIGINT/SIGTERM)
- Configurable HTTP listen address, MCP bearer-token auth, and IP allow-list
- YAML config system (`internal/config`) with strict validation, defaults, and version guard

#### Camera & Discovery
- Camera registry with RTSP/ONVIF probe-at-boot; per-camera status (`available`, `degraded`, `unavailable`, `unknown`)
- ONVIF TCP reachability probe with RTSP URL discovery fallback
- Capability cache (`--capability-cache-path`) for warm restarts — stores last-known probe results across reboots
- Startup discovery report with per-camera source and error details

#### Stream Ingest
- Persistent FFmpeg snapshotter with configurable FPS (`runtime.stream_sample_fps`)
- Ingest manager with health watchdog, auto-reconnect on failure, and per-camera incident tracking
- `/admin/ingest/status` endpoint reporting live stream health

#### Inference Pipeline
- Worker pool with backpressure queue (`runtime.queue_size`); drops frames when full
- Privacy mode: frame bytes stripped before forwarding to inference when `privacy.frame_buffer_enabled: false` (default)
- Configurable metadata retention window (`privacy.metadata_retention_seconds`)
- Degraded-mode flag set on consecutive inference errors

#### Inference Transport
- HTTP client (`internal/inference`) for the private Python worker API
- Python worker (`worker/`) serving YOLOv8 detections over HTTP with a deterministic fallback detector for CI

#### Zone & Detection Filtering
- Sutherland-Hodgman polygon clipping for BBox/zone overlap (`internal/zone`)
- `ZoneFilter` with per-zone, per-camera, and global confidence thresholds (priority: camera > zone > global)
- `min_bbox_overlap` per zone rejects detections that don't sufficiently overlap the defined polygon
- `runtime.min_detections` temporal smoothing: state transitions require N consecutive detections

#### State & Aggregation
- State aggregator with configurable freshness window and optional minimum-detections gate
- Zone-state tracking for arbitrary labels (person, package, vehicle, …)
- Front-door package-presence shortcut with dedicated MCP tool

#### MCP Tool Server
- `koala.list_cameras` — camera inventory with status
- `koala.get_zone_state` — entity presence and confidence per zone
- `koala.check_package_at_door` — single-call package-at-front-door answer
- `koala.get_system_health` — inference worker health, degraded flag, queue depth
- Rate limiter middleware (configurable; disabled in tests)

#### OTA Update System
- Signed + AES-256-GCM encrypted update bundles (`internal/update`)
- Ed25519 signature verification with key-rotation support (keyring + `previous_keys` grace window)
- Unknown-key security alert hook with audit log recording
- Stage → apply → rollback lifecycle via MCP tools (`/mcp/tools/koala.update_stage`, `.update_apply`, `.update_rollback`)
- Post-apply watchdog goroutine: monitors worker health for 2 min after apply; auto-rolls back on failure
- Update poller (`update.Poller`) with jitter for fleet-safe manifest fetching
- SQLite audit log (`internal/audit`) recording all update and security events

#### Fleet Management
- Device registration/deregistration (`/admin/fleet/register`, `/admin/fleet/deregister`)
- Fleet device listing (`/admin/fleet/devices`)
- Remote rollout orchestration (all / batch / canary) via `/admin/update/rollout`

#### Observability
- `/health` liveness endpoint
- `/metrics` Prometheus-style counters (frames ingested, detections, errors)
- `/admin/config` sanitised config snapshot (tokens/keys redacted)
- Consolidated startup report log block with version, transport, privacy, runtime params, camera/zone counts

#### CI / Developer Experience
- GitHub Actions workflow (`.github/workflows/ci.yml`): Go vet → golangci-lint → build → race-detector tests
- Python CI job: ruff → mypy → pytest
- Branch concurrency group cancels stale in-progress runs
- Makefile with 14 targets: `fmt`, `lint`, `test`, `test-go`, `test-py`, `test-contracts`, `test-agent`, `test-replay`, `build`, `compose-up/down/logs`, `proto`, `eval`, `bench`
- Docker Compose stack: orchestrator + Python worker

#### Testing
- Replay e2e harness (`tests/replay_e2e_test.go`) with confusion-matrix accuracy gates:
  - Package precision ≥ 0.90, recall ≥ 0.90
  - P95 end-to-end latency ≤ 2 s
- Zone polygon filtering e2e test (in-zone / out-of-zone / edge-majority-in cases)
- MCP contract tests validating JSON schema of all tool responses
- Agent harness simulating LLM prompt → tool selection paths
- Unit tests for zone filter (confidence tiers, polygon overlap), state aggregator, camera registry, update crypto, fleet endpoints

### Changed
- `state.NewAggregator` now accepts optional `minDetections int` variadic — wired from `runtime.min_detections`
- Config `ZoneConfig.ConfidenceThreshold` and `CameraConfig.ConfidenceThreshold` now wired into `ZoneFilter` at startup (previously parsed but unused)

### Security
- Frame bytes never leave the device when `privacy.frame_buffer_enabled: false`
- MCP token required on all non-health routes; IP allow-list available
- Update signing keys verified with Ed25519; encrypted with AES-256-GCM
- Audit log immutably records all update lifecycle and unknown-key events

---

[Unreleased]: https://github.com/Bare-Systems/Koala/compare/v0.1.0-dev...HEAD
[0.1.0-dev]: https://github.com/Bare-Systems/Koala/releases/tag/v0.1.0-dev
