# Koala

Koala is a local-first AI home-state service for Bare Systems.

## MVP Architecture

- Go orchestrator: camera discovery, RTSP ingest, zone state, REST APIs, and MCP on `6705`.
- Python worker: private HTTP inference service on `6704` (YOLO baseline with deterministic fallback).

## Runtime Flow

Koala does not passively intercept camera traffic. The orchestrator actively
connects to configured RTSP streams, samples frames with `ffmpeg`, forwards
those frames to the private worker, filters detections into zone state, and
then exposes AI-friendly APIs on `6705`.

```text
RTSP ingest -> frame sampling -> worker inference -> detection filtering -> temporal aggregation -> agent/API response
```

## Run

```bash
docker compose up --build
```

Or locally:

```bash
go run ./cmd/koala-orchestrator -config configs/koala.yaml
python -m koala_worker.server
```

## Edge Device Deployment (Jetson)

Code deploys are automated via blink:

```bash
blink deploy koala-worker
blink deploy koala-orchestrator
```

**One-time GPU setup — must be done manually on the Jetson before first deploy.**
Blink does not handle this because the install takes several minutes and is a system-level
operation that only needs to happen once per device.

```bash
# SSH into the Jetson, then:

# 1. System packages
sudo apt-get install -y python3-pip libopenblas-dev

# 2. cuSPARSELt — required for PyTorch 24.06+ builds
wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/arm64/cuda-keyring_1.1-1_all.deb
sudo dpkg -i cuda-keyring_1.1-1_all.deb
sudo apt-get update
sudo apt-get install -y libcusparselt0 libcusparselt-dev

# 3. numpy pinned version
pip3 install 'numpy==1.26.1'

# 4. PyTorch 2.5.0 — official NVIDIA wheel for JetPack 6.1 / R36.4 / Python 3.10
#    Source: https://docs.nvidia.com/deeplearning/frameworks/install-pytorch-jetson-platform/
pip3 install --no-cache \
  https://developer.download.nvidia.com/compute/redist/jp/v61/pytorch/torch-2.5.0a0+872d972e41.nv24.08.17622132-cp310-cp310-linux_aarch64.whl

# 5. Install ultralytics (YOLO) — do this BEFORE verifying torch
#    ultralytics will pull in a CPU-only torch from PyPI; we overwrite it next
pip3 install ultralytics

# 6. Re-pin the NVIDIA CUDA torch — ultralytics upgrades it to a CPU build from PyPI
pip3 install --no-cache --force-reinstall \
  https://developer.download.nvidia.com/compute/redist/jp/v61/pytorch/torch-2.5.0a0+872d972e41.nv24.08.17622132-cp310-cp310-linux_aarch64.whl

# 7. Verify CUDA is available (must print True)
python3 -c 'import torch; print(torch.__version__); print("cuda:", torch.cuda.is_available())'
```

After the one-time setup, restart the worker and confirm GPU inference is live:

```bash
blink restart koala-worker
blink test koala-worker --tags gpu   # passes when cuda=True
```

On first start after GPU setup, the worker downloads `yolov8n.pt` (~6 MB) into
`$KOALA_MODEL_DIR` (default: `~/baresystems/runtime/koala/worker/models/`).
Subsequent starts load the cached model. YOLO detects `person`, `package`, and
`animal` labels; without GPU setup the worker falls back to a deterministic stub.

See `BLINK.md` for the supported Jetson deployment contract and operator notes for the current managed deployment shape.

---

## Blink Homelab Contract

The stable `blink` deployment shape is:

- `blink` host: `192.168.86.53`
- DVR: `192.168.86.46`
- BearClaw and Koala Live call Koala at `http://192.168.86.53:6705`
- The camera-facing orchestrator runs on host networking
- The worker stays private on `127.0.0.1:6704`

That host-network orchestrator requirement is not optional on `blink`. During
the March 20, 2026 outage, the host could reach the DVR while Docker bridge
containers could not. If the host can reach cameras and containers cannot, do
not change camera code first. Preserve that network pattern as part of the
supported deployment contract described in `BLINK.md`.

## AI Agent Interface

All agent-facing interfaces on `6705` require `Authorization: Bearer <mcp_token>`.

- Legacy JSON tool routes:
  - `POST /mcp/tools/koala.get_system_health`
  - `POST /mcp/tools/koala.list_cameras`
  - `POST /mcp/tools/koala.get_zone_state`
  - `POST /mcp/tools/koala.check_package_at_door`
- MCP JSON-RPC endpoint:
  - `POST /mcp`

The `/mcp` endpoint is a stateless MCP HTTP entrypoint for BearClaw-class
agents. It supports `initialize`, `ping`, `tools/list`, and `tools/call` while
the legacy `/mcp/tools/...` routes remain available for direct HTTP clients.

When wiring Koala into BearClaw, use BearClaw's HTTP MCP transport in
`mcp_servers`:

```sh
bareclaw config set mcp_servers "koala=http_mcp http://192.168.86.53:6705/mcp <mcp_token>"
```

BearClaw will discover the `koala.*` MCP tools from `/mcp` and expose them in
its local tool catalog as `koala__get_system_health`, `koala__list_cameras`,
`koala__get_zone_state`, and `koala__check_package_at_door`.

Example direct tool route:

- `POST /mcp/tools/koala.get_zone_state` with `{ "input": { "zone_id": "front_door" } }`

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

For the `blink` homelab deployment shape and the Docker-to-LAN camera networking
workaround, see `BLINK.md`.

Staging/apply storage:

- staged artifact: `<update.staging_dir>/<version>/artifact.bin`
- staged manifest: `<update.staging_dir>/<version>/manifest.json`
- active version marker: `<update.active_dir>/current_version`

## Detection snapshots

When an alert fires (an absent→present rising edge for a tracked entity in a
zone — see "AI Agent Interface"), Koala captures the exact camera frame that
triggered the detection, draws the detection bounding box(es) onto it, and
retains it so operators can visually confirm *what* was detected. The
BearClawWeb dashboard renders these as thumbnails on each "Recent Alerts" card.

### Data flow

```text
ingest manager        service.processTask           state.Aggregator         snapshot.Store        MCP server            BearClawWeb
(captures frame,  ->  (holds the exact          ->  (Ingest returns the  ->  (annotated JPEG   ->  GET /admin/alerts/ ->  Home::AlertsController
 submits FrameTask     triggering frame in           alerts that fired        keyed by alert ID,    {id}/snapshot          proxies; alert-snapshot
 with FrameB64)        task.FrameB64)                on this call)            bounded ring)         (bearer or ?token=)    Stimulus controller shows)
```

Key point: the frame is the **locally captured** one already present in
`FrameTask.FrameB64` at `internal/service/service.go`. It is **not** re-fetched
and is **never** forwarded to the inference worker, so detection snapshots are
independent of the `privacy.frame_buffer_enabled` gate (which only governs the
worker call). Annotation matches detections to the alert by `label` + `zone_id`
and draws every matching box.

### Code map

| Concern | Location |
| --- | --- |
| Config flag | `internal/config/config.go` → `PrivacyConfig.DetectionSnapshotsEnabled` (`privacy.detection_snapshots_enabled`) |
| Fired-alert signal | `internal/state/aggregator.go` → `Ingest` / `detectTransitionsLocked` return `[]Alert` |
| Capture + annotate | `internal/service/service.go` → `captureSnapshots`; `internal/snapshot/annotate.go` |
| Storage | `internal/snapshot/store.go` → `Store` (bounded in-memory ring) |
| Wiring | `cmd/koala-orchestrator/main.go` (creates the store when the flag is on) |
| Serve | `internal/mcp/server.go` → `alertSnapshot`, route `/admin/alerts/{id}/snapshot` |

### Storage model (current)

- **In-memory only.** `snapshot.Store` is a bounded map keyed by alert ID,
  capacity `snapshot.DefaultMax` (200). Oldest entries are evicted first.
- **Lifecycle mirrors the alert ring.** The aggregator keeps the newest 200
  alerts (`state.defaultMaxAlerts`); the snapshot store keeps the newest 200
  frames. They are inserted together, so a snapshot exists for roughly every
  retained alert. Both are **lost on restart** — this is deliberate and keeps
  the two structures consistent.
- **Missing snapshots are graceful.** Alerts older than the deploy, or evicted
  alerts, return `404` from the serve endpoint; the dashboard's `alert-snapshot`
  Stimulus controller hides the figure instead of showing a broken image.
- **Serving is cache-friendly.** An alert ID maps to a fixed frame, so the
  endpoint sends `Cache-Control: immutable`.

### Configuration

```yaml
privacy:
  detection_snapshots_enabled: true   # default false in code; on in the deployed config
```

Default is **off** in code (privacy-by-default stance). It is enabled in
`configs/koala.yaml` and in the Blink provision heredoc (`blink.toml`) so the
homelab deploy ships with it on. Disabling the flag stops capture entirely and
the serve endpoint returns `404` for every alert.

### Future refactors (intended seams)

The storage boundary is deliberately narrow so these are low-risk changes:

- **Change storage location (disk / object store / DB).** `snapshot.Store` is
  the only thing that holds bytes. Extract its `Put`/`Get` into a `Store`
  interface and provide an alternate implementation (e.g. write JPEGs under a
  configurable `privacy.snapshot_dir` with a retention sweeper, or push to S3).
  `service.captureSnapshots` and `mcp.alertSnapshot` already depend only on
  `Put`/`Get(id)`, so they need no changes. Add the path/retention knobs to
  `PrivacyConfig` and wire them in `main.go`.
- **Persist across restarts.** Requires persisting the alert ring too (today
  both are in-memory), or decoupling snapshot retention from alert retention and
  keying lookups purely by alert ID. Decide whether evicted-alert snapshots
  should remain reachable.
- **Store short video clips, not single frames.** This is the larger change: the
  ingest manager currently buffers only the *latest* frame per camera
  (`latestFrames` in `internal/ingest/manager.go`). A clip needs a rolling
  pre-/post-roll frame buffer per camera (a ring of recent frames + a few
  seconds of capture after the trigger), assembled into an MP4. The store would
  hold clip bytes/paths instead of a JPEG, and the serve endpoint would set
  `Content-Type: video/mp4`. The alert→media keying and the dashboard proxy stay
  the same; the new cost is buffering memory/CPU on the Jetson and encode time.
- **Retention tuning.** `snapshot.NewStore(max)` takes the cap; today `main.go`
  passes `0` (→ `DefaultMax`). Surface it as `privacy.snapshot_retention` if the
  fixed 200 becomes a constraint.

The BearClawWeb side is storage-agnostic: it proxies bytes through
`Home::AlertsController#snapshot` → `KoalaClient#alert_snapshot` and never sees
the Koala bearer token. Switching Koala to disk/clips requires no Rails changes
beyond possibly the `<img>` → `<video>` element if clips land.

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
