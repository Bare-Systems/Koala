# Koala Deployment Guide

This guide covers deploying Koala on a Jetson Orin Nano (primary target) or any
Linux host with Docker Compose (development/staging).

---

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Docker Compose (quick start)](#docker-compose-quick-start)
3. [Jetson Native Deployment](#jetson-native-deployment)
4. [Configuration Reference](#configuration-reference)
5. [Camera Setup](#camera-setup)
6. [Zone Configuration](#zone-configuration)
7. [Privacy Settings](#privacy-settings)
8. [OTA Updates](#ota-updates)
9. [Connecting an AI Agent](#connecting-an-ai-agent)
10. [Health & Observability](#health--observability)
11. [Troubleshooting](#troubleshooting)

---

## Prerequisites

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| OS | Ubuntu 20.04 | Ubuntu 22.04 / JetPack 6 |
| CPU | 4-core ARM64 | Jetson Orin Nano (6-core) |
| RAM | 4 GB | 8 GB |
| Storage | 16 GB | 32 GB (NVMe) |
| Docker | 24+ | latest |
| Go | 1.22+ | latest (build only) |
| Python | 3.11 | 3.11 |
| FFmpeg | 4.4+ | 6.x |

Network access to cameras (RTSP/ONVIF) is required from the host running the
orchestrator.

---

## Docker Compose (quick start)

This is the fastest way to get Koala running locally or on a staging host.

### 1 — Clone and configure

```bash
git clone https://github.com/barelabs/koala.git
cd koala
cp configs/koala.example.yaml configs/koala.yaml
```

Edit `configs/koala.yaml` — at minimum set:

```yaml
mcp_token: "your-strong-secret-here"   # required; used for all API calls
service:
  device_id: "koala-prod-01"
  address: "http://<this-host-ip>:8080"

cameras:
  - id: "cam_front_1"
    name: "Front Door"
    rtsp_url: "rtsp://192.168.1.100:554/stream1"
    zone_id: "front_door"
    front_door: true
```

### 2 — Start

```bash
make compose-up
```

Or directly:

```bash
docker compose up --build -d
```

### 3 — Verify

```bash
curl http://localhost:8080/health
# → {"status":"ok"}

curl -s -X POST http://localhost:8080/mcp/tools/koala.get_system_health \
  -H "Authorization: Bearer your-strong-secret-here" \
  -H "Content-Type: application/json" \
  -d '{"input":{}}' | jq .
```

---

## Jetson Native Deployment

Running natively (no Docker) gives full GPU access for TensorRT inference and
lower overhead for the Python worker.

### 1 — Install system dependencies

```bash
sudo apt-get update
sudo apt-get install -y ffmpeg python3.11 python3.11-venv python3-pip build-essential
```

### 2 — Install Go

```bash
wget https://go.dev/dl/go1.22.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.22.linux-arm64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

### 3 — Build the orchestrator

```bash
cd /opt/koala
make build
# outputs: bin/koala-orchestrator  bin/koala-packager
```

### 4 — Install the Python worker

```bash
cd /opt/koala/worker
python3.11 -m venv .venv
source .venv/bin/activate
pip install -e ".[gpu]"   # installs ultralytics + CUDA extras
```

For CPU-only / CI, omit `[gpu]`:

```bash
pip install -e "."
```

### 5 — Create systemd units

**`/etc/systemd/system/koala-worker.service`**

```ini
[Unit]
Description=Koala Inference Worker
After=network.target

[Service]
User=koala
WorkingDirectory=/opt/koala/worker
ExecStart=/opt/koala/worker/.venv/bin/python -m koala_worker
Restart=on-failure
RestartSec=5s
Environment=KOALA_WORKER_PORT=8090

[Install]
WantedBy=multi-user.target
```

**`/etc/systemd/system/koala-orchestrator.service`**

```ini
[Unit]
Description=Koala Orchestrator
After=network.target koala-worker.service
Requires=koala-worker.service

[Service]
User=koala
WorkingDirectory=/opt/koala
ExecStart=/opt/koala/bin/koala-orchestrator -config /etc/koala/koala.yaml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now koala-worker koala-orchestrator
sudo journalctl -fu koala-orchestrator
```

### 6 — Startup report

On successful boot you should see a consolidated log block:

```
startup: ── Koala Orchestrator ──────────────────────────────────────
startup: version=0.1.0-dev device_id=koala-jetson-01
startup: listen=:8080 service_addr=http://192.168.1.50:8080
startup: inference transport=http
startup: privacy=metadata-only
startup: runtime queue=64 freshness=90s min_detections=0 confidence=0.00
startup: cameras=1 front_door=1 zones=1
startup: stream_workers=enabled fps=1 update=disabled ip_allowlist=disabled
startup: ─────────────────────────────────────────────────────────────
```

---

## Configuration Reference

The config file is YAML with version guard (`config_version: "1"`).

### Top-level keys

| Key | Type | Required | Description |
|-----|------|----------|-------------|
| `listen_addr` | string | no | HTTP listen address (default `:8080`) |
| `mcp_token` | string | **yes** | Bearer token for all API calls |
| `allowed_ips` | []string | no | IP allowlist (CIDR or exact); empty = allow all |

### `service`

```yaml
service:
  device_id: "koala-jetson-01"      # unique device identifier for fleet ops
  version: "0.1.0-dev"              # reported in /health and update checks
  address: "http://192.168.1.50:8080"  # externally reachable address (fleet use)
```

### `worker`

```yaml
worker:
  url: "http://127.0.0.1:8090"    # HTTP inference worker (default)

# or for gRPC:
worker:
  grpc_addr: "127.0.0.1:9090"
```

`protocol` is auto-detected from which field is set.

### `runtime`

```yaml
runtime:
  queue_size: 64                  # frame backpressure queue depth
  freshness_window_seconds: 90    # detections older than this are stale
  min_detections: 0               # require N consecutive detections before state=true (0=disabled)
  confidence_threshold: 0.0       # global min confidence (0=disabled); overridden per-zone/camera
  enable_stream_workers: true     # start FFmpeg capture workers at boot
  stream_sample_fps: 1            # frames per second to sample from each stream
  stream_capture_timeout_seconds: 5
  capability_cache_path: "/var/lib/koala/capability-cache.json"
```

### `privacy`

```yaml
privacy:
  frame_buffer_enabled: false     # default: strip frame bytes — metadata-only mode
  metadata_retention_seconds: 90  # overrides runtime.freshness_window_seconds if set
```

> **Default is private.** Frame bytes are never forwarded to the inference
> worker unless `frame_buffer_enabled: true` is explicitly set.

### `cameras`

```yaml
cameras:
  - id: "cam_front_1"           # must be unique
    name: "Front Door Camera"
    rtsp_url: "rtsp://dvr.local:554/ch1"
    onvif_url: "http://dvr.local/onvif/device_service"  # optional; used for probe
    zone_id: "front_door"       # must match a zone id
    front_door: true            # exactly one camera must have this
    probe_at_boot: true         # probe RTSP/ONVIF reachability on startup
    max_fps: 5                  # per-camera FPS cap (0=unlimited)
    confidence_threshold: 0.85  # per-camera threshold (overrides zone + global)
```

### `zones`

```yaml
zones:
  - id: "front_door"
    name: "Front Door"
    confidence_threshold: 0.75  # zone-level threshold (overrides global)
    min_bbox_overlap: 0.30      # fraction of detection BBox that must be inside polygon
    polygon:                    # normalized [0,1] coordinates
      - [0.0, 0.0]
      - [1.0, 0.0]
      - [1.0, 1.0]
      - [0.0, 1.0]
```

---

## Camera Setup

### Finding your RTSP URL

Most IP cameras and DVRs expose RTSP. Common patterns:

| Brand | URL pattern |
|-------|-------------|
| Hikvision | `rtsp://user:pass@ip:554/Streaming/Channels/101` |
| Dahua | `rtsp://user:pass@ip:554/cam/realmonitor?channel=1&subtype=0` |
| Generic DVR | `rtsp://user:pass@ip:554/ch1` |
| Reolink | `rtsp://user:pass@ip:554/h264Preview_01_main` |

Test with:

```bash
ffmpeg -i "rtsp://user:pass@192.168.1.100:554/stream1" -frames:v 1 test.jpg
```

### ONVIF discovery

If you don't know the RTSP URL, configure `onvif_url` and Koala will probe it at
boot to discover the stream URI automatically. The discovered URL is cached in
`runtime.capability_cache_path` for fast restarts.

---

## Zone Configuration

Zones define the spatial region of interest within a camera's field of view.
Coordinates are normalised to `[0, 1]` where `(0,0)` is top-left.

Example — bottom-right quadrant:

```yaml
zones:
  - id: "front_door"
    polygon:
      - [0.5, 0.5]
      - [1.0, 0.5]
      - [1.0, 1.0]
      - [0.5, 1.0]
    min_bbox_overlap: 0.30
```

A detection is accepted only if at least 30 % of its bounding box area falls
inside the polygon (Sutherland-Hodgman clipping).

To disable spatial filtering, omit `polygon` or leave it empty — all detections
in that zone will pass.

---

## Privacy Settings

Koala defaults to **metadata-only mode**: frame pixels are stripped before being
forwarded to the inference worker. Only timestamps, camera IDs, and bounding-box
coordinates leave the device.

To enable full-frame forwarding (e.g. for custom model tuning):

```yaml
privacy:
  frame_buffer_enabled: true
```

The startup report and the `privacy=metadata-only` log confirm the active mode.

---

## OTA Updates

> Update signing requires key generation outside this guide. See
> `internal/update/keygen.go` for Ed25519 key generation and
> `cmd/koala-packager` for bundle creation.

### Enabling updates

```yaml
update:
  enabled: true
  rotation_only_mode: true
  active_key_id: "key-2026-03"
  public_keys:
    key-2026-03: "<base64-ed25519-public-key>"
  encryption_key_base64: "<base64-aes256-key>"
  audit_db_path: "/var/lib/koala/audit/events.db"
  staging_dir: "/var/lib/koala/staging"
  active_dir: "/var/lib/koala/active"
```

### Manual update via MCP

```bash
# 1. Stage the bundle
curl -X POST http://localhost:8080/mcp/tools/koala.update_stage \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"input":{"url":"http://updates.example.com/bundle.tar.gz.enc"}}'

# 2. Apply (post-apply watchdog monitors health for 2 min; auto-rolls back on failure)
curl -X POST http://localhost:8080/mcp/tools/koala.update_apply \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"input":{}}'

# 3. Check audit log
curl http://localhost:8080/admin/audit/events \
  -H "Authorization: Bearer $TOKEN"
```

### Key rotation

Add the expiring key to `previous_keys` (grace-window acceptance) and set the
new key as `active_key_id`:

```yaml
update:
  active_key_id: "key-2026-06"
  public_keys:
    key-2026-06: "<new-public-key>"
  previous_keys:
    key-2026-03: "<old-public-key>"
```

---

## Connecting an AI Agent

Koala exposes an MCP (Model Context Protocol) HTTP server. Point any MCP-compatible
agent (Claude, etc.) at the orchestrator address with your token.

### Available tools

| Tool | Description |
|------|-------------|
| `koala.list_cameras` | Camera inventory with live status |
| `koala.get_zone_state` | Entity presence per zone (person, package, vehicle, …) |
| `koala.check_package_at_door` | Quick answer: is there a package at the front door? |
| `koala.get_system_health` | Worker health, degraded flag, queue depth |

### Example (curl)

```bash
curl -X POST http://localhost:8080/mcp/tools/koala.check_package_at_door \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"input":{}}'
```

```json
{
  "data": {
    "package_present": true,
    "confidence": 0.95,
    "camera_id": "cam_front_1",
    "last_seen": "2026-03-14T10:22:01Z"
  }
}
```

---

## Health & Observability

| Endpoint | Auth | Description |
|----------|------|-------------|
| `GET /health` | none | Liveness probe — returns `{"status":"ok"}` |
| `GET /metrics` | token | Prometheus-style counters |
| `GET /admin/config` | token | Sanitised config snapshot (no secrets) |
| `GET /admin/ingest/status` | token | Per-camera stream health |
| `GET /admin/audit/events` | token | Audit log (update and security events) |

### Log levels

All logs use Go's standard `log` package with structured `key=value` fields.
The startup report block, camera discovery, and stream health warnings are the
primary signals to watch.

---

## Troubleshooting

### Camera shows `status=degraded`

- Verify RTSP URL is reachable: `ffmpeg -i "rtsp://..." -frames:v 1 /dev/null`
- Check `onvif_url` resolves on the network
- Review `startup: camera id=... error=` log lines

### Worker health degraded

- Confirm the Python worker is running: `systemctl status koala-worker`
- Check worker logs: `journalctl -fu koala-worker`
- The orchestrator retries automatically; degraded state clears after a successful inference call

### Package never detected

- Confirm `privacy.frame_buffer_enabled: true` if the inference worker needs
  actual pixel data (required unless the worker supports metadata-only mode)
- Verify `confidence_threshold` is not set too high
- Use `koala.get_zone_state` to check raw entity presence before `check_package_at_door`

### Stream drops frames silently

- `runtime.queue_size` may be too small; increase to 128 or 256
- Lower `runtime.stream_sample_fps` to reduce load
- Check for FFmpeg stderr output in worker logs

### Tests

```bash
make test                # all tests
make test-replay         # precision/recall gate
make test-contracts      # MCP schema validation
```
