# Koala Handoff (March 6, 2026)

## Current Stop Point
Koala is at a good pause point with all tests passing after the latest streaming/discovery/replay work.

- Go tests: `go test ./...` (pass)
- Python worker tests: `python3 -m unittest discover -s tests -q` (pass)

## What Was Completed Recently

### 1) Stream health watchdog + auto-reconnect + MCP-visible incidents
- Added jittered exponential reconnect loop for persistent ffmpeg workers.
- Added ingest watchdog stall detection and status transitions.
- Added incident tracking (`stream_failure`, `stream_stalled`, `stream_recovered`) in ingest status.
- `/admin/ingest/status` now includes incident snapshots for operations visibility.

Files:
- `internal/ingest/persistent_ffmpeg.go`
- `internal/ingest/manager.go`
- `internal/ingest/manager_test.go`
- `internal/mcp/server_ingest_test.go`

### 2) ONVIF fallback + camera capability probing
- Extended probe flow to RTSP-first and ONVIF reachability fallback.
- Added degraded status support for fallback/partial availability.
- Added camera capability metadata (`rtsp_reachable`, `onvif_reachable`, `selected_source`, probe time, last error).
- Added optional RTSP URL discovery from ONVIF host when RTSP URL missing.

Files:
- `internal/camera/discovery.go`
- `internal/camera/discovery_test.go`
- `internal/camera/registry.go`
- `cmd/koala-orchestrator/main.go`
- `internal/config/config.go`
- `configs/koala.example.yaml`
- `configs/koala.yaml`

### 3) End-to-end replay harness with latency/accuracy gates
- Added fixture-driven replay harness for `koala.check_package_at_door`.
- Added gate assertions:
  - P95 latency <= 2s
  - Precision >= 0.90
- Added replay fixture cases for package/person/no-object/occlusion/night.

Files:
- `tests/replay_e2e_test.go`
- `tests/fixtures/replay/front_door_cases.json`

### 4) Documentation updates
- Updated `README.md` with stream incident behavior, ONVIF fallback notes, and replay gate test command.

### 5) Homelab camera networking note (March 20, 2026)
- On `blink`, the host could reach the DVR at `192.168.86.46`, but Docker bridge
  containers could not.
- The operational workaround was to run the camera-facing orchestrator on host
  networking and route worker traffic through the worker's published host port.
- Added:
  - `docs/examples/docker-compose.homelab.yml`
  - `configs/koala.homelab.yaml`
  - `docs/homelab-networking.md`

## Existing Major Platform Features Already Landed (Earlier Iterations)
- Secure update flow with signed manifest and encrypted/signed bundle support.
- Key rotation support (`active_key_id`, `previous_keys`, keyring).
- Rotation-only mode enforcement and unknown-key signature telemetry.
- Remote rollout orchestration (`all`, `batch`, `canary`) with policy controls.
- Persistent rollout/security history via SQLite (`internal/audit` + admin history endpoints).
- Poll-mode (pull updates) for client-initiated update checks.

## Important Operational Endpoints
- MCP tools:
  - `/mcp/tools/koala.get_system_health`
  - `/mcp/tools/koala.list_cameras`
  - `/mcp/tools/koala.get_zone_state`
  - `/mcp/tools/koala.check_package_at_door`
- Admin:
  - `/admin/ingest/status`
  - `/admin/updates/status`
  - `/admin/updates/security`
  - `/admin/updates/history`
  - rollout endpoints under `/admin/updates/rollouts/*`

## Suggested Next Work (Priority Order)
1. Implement full ONVIF SOAP capability/profile parsing (`GetCapabilities`, `GetProfiles`, `GetStreamUri`) instead of TCP reachability fallback.
2. Add persistent ingest incident storage (SQLite) and retention policy so stream events survive restarts.
3. Replace HTTP inference transport with gRPC (align with proto contract and roadmap Phase 3), with compatibility tests.
4. Add real video replay assets + golden labels for stronger precision/recall metrics (beyond synthetic frame tags).
5. Add CI (`make test`, lint/type checks) and include replay harness in CI gate.

## Notes For Fresh Session
- Branch currently: `main`.
- Working tree currently contains uncommitted changes for the latest feature set (stream watchdog/ONVIF/replay).
- If resuming implementation, start by reviewing:
  - `README.md`
  - `docs/HANDOFF.md`
  - `PLAN.md` (roadmap source of truth)
