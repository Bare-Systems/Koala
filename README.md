# Koala

Koala is a local-first AI home-state service for Bare Systems.

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
- The worker stays on bridge networking and is published on `8092`

That host-network orchestrator requirement is not optional on `blink`. During
the March 20, 2026 outage, the host could reach the DVR while Docker bridge
containers could not. If the host can reach cameras and containers cannot, do
not change camera code first. Preserve that network pattern as part of the
supported deployment contract described in `BLINK.md`.

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

For the `blink` homelab deployment shape and the Docker-to-LAN camera networking
workaround, see `BLINK.md`.

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
