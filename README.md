# Koala

Koala is a local-first AI home-state service for Bare Labs.

## MVP Architecture

- Go orchestrator: camera discovery, ingest scheduling, zone state, MCP tools, auth.
- Python worker: package/person inference abstraction (YOLO baseline with deterministic fallback).
- Proto contract: `proto/koala_inference.proto` for versioned worker API definitions.

## Run

```bash
docker compose up --build
```

Or locally:

```bash
go run ./cmd/koala-orchestrator -config configs/koala.yaml
python -m koala_worker.server
```

## MCP tools

All tools require `Authorization: Bearer <mcp_token>`.

- `POST /mcp/tools/koala.get_system_health`
- `POST /mcp/tools/koala.list_cameras`
- `POST /mcp/tools/koala.get_zone_state` with `{ "input": { "zone_id": "front_door" } }`
- `POST /mcp/tools/koala.check_package_at_door` with `{ "input": { "camera_id": "cam_front_1" } }`

## Admin update APIs (MVP foundation)

All update APIs require `Authorization: Bearer <mcp_token>`.

- `GET /admin/updates/status`
- `GET /admin/updates/security`
- `GET /admin/updates/rollouts/list`
- `GET /admin/updates/history`
- `GET /admin/ingest/status`
- `POST /admin/updates/check`
- `POST /admin/updates/stage`
- `POST /admin/updates/apply`
- `POST /admin/updates/rollback`
- `POST /admin/updates/rollouts/start`
- `POST /admin/updates/rollouts/get`

Device agent endpoints (called by orchestrator update executor):

- `GET /agent/updates/health`
- `POST /agent/updates/stage`
- `POST /agent/updates/apply`
- `POST /agent/updates/rollback`

Manifest payload shape:

```json
{
  "input": {
    "manifest": {
      "key_id": "key-2026-03",
      "version": "0.2.1",
      "artifact_url": "http://updates.local/koala-0.2.1.bundle.json",
      "sha256": "<64-char-hex>",
      "signature": "<base64-ed25519-signature>",
      "min_orchestrator_version": "0.1.0-dev",
      "min_worker_version": "0.1.0-dev"
    },
    "device_ids": ["koala-local"]
  }
}
```

Signature payload is the newline-joined fields:

`key_id`, `version`, `artifact_url`, `sha256`, `created_at`, `min_orchestrator_version`, `min_worker_version`

When `update.enabled=true`, `update.encryption_key_base64` is required, plus one signing mode:
- Legacy mode: `update.public_key_base64` (deprecated)
- Rotation mode: `update.active_key_id`, `update.previous_keys`, and `update.public_keys`

Set `update.rotation_only_mode: true` to enforce rotation-only mode and reject legacy `public_key_base64`.

The agent now expects `artifact_url` to point to an encrypted/signed bundle JSON, not a raw binary artifact.
Unknown `key_id` signature attempts are tracked in agent health under `unknown_key_attempts` and emit security alert logs.
`GET /admin/updates/security` returns those counters plus recent unknown-key alert events.
`GET /admin/updates/history` returns persisted rollout/security events from SQLite (`update.audit_db_path`).

Rollout start input additionally supports:
- `mode`: `all`, `batch`, `canary`
- `batch_size`: required for `batch`; optional for `canary`
- `max_failures`: stop rollout if failures exceed this threshold
- `pause_between_batches_ms`: optional delay between batches
- `rollback_scope`: `failed` or `batch`

Pull mode configuration (client-initiated updates):

- `update.poll_enabled`: enable periodic manifest polling
- `update.poll_interval_seconds`: base polling interval
- `update.poll_jitter_seconds`: random jitter added per cycle
- `update.manifest_url`: manifest URL to poll

`GET /admin/updates/status` includes `data.poller` with latest poll result, last/next poll time, and failure counters.
Poll events are persisted in SQLite and available via `GET /admin/updates/history` with `category=\"poll\"`.

## Stream ingest

The orchestrator can continuously pull frames from RTSP cameras and feed inference automatically.

Runtime controls:
- `runtime.enable_stream_workers`
- `runtime.stream_sample_fps`
- `runtime.stream_capture_timeout_seconds`

Current implementation captures snapshots via `ffmpeg`, so `ffmpeg` must be installed on the device.
Ingest workers include watchdog + auto-reconnect behavior, and `/admin/ingest/status` now returns recent incident events (`stream_failure`, `stream_stalled`, `stream_recovered`) for MCP-visible operations monitoring.

Camera probing supports RTSP-first checks with ONVIF fallback probing (`camera.onvif_url`); ONVIF reachability is surfaced in `list_cameras` capability data.

Staging/apply storage:

- staged artifact: `<update.staging_dir>/<version>/artifact.bin`
- staged manifest: `<update.staging_dir>/<version>/manifest.json`
- active version marker: `<update.active_dir>/current_version`

## Secure packaging command

Generate encrypted/signed bundle + signed manifest:

```bash
go run ./cmd/koala-packager \
  --artifact /path/to/koala-update.tar.gz \
  --bundle-url http://updates.local/koala-update.bundle.json \
  --key-id key-2026-03 \
  --version 0.2.1 \
  --private-key-base64 "$KOALA_UPDATE_PRIVATE_KEY_B64" \
  --encryption-key-base64 "$KOALA_UPDATE_ENCRYPTION_KEY_B64" \
  --min-orchestrator-version 0.1.0-dev \
  --min-worker-version 0.1.0-dev \
  --bundle-out /tmp/koala-update.bundle.json \
  --manifest-out /tmp/koala-manifest.json
```

The generated bundle includes encrypted artifact bytes + bundle signature.
The generated manifest includes plaintext artifact `sha256`, `created_at`, and manifest signature.

## Test

```bash
go test ./...
cd worker && python -m pytest
```

Replay gate harness (latency + accuracy for `check_package_at_door`):

```bash
go test ./tests -run TestReplayHarnessPackageDoorLatencyAndAccuracyGates -v
```
