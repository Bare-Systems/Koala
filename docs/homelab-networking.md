# Koala Homelab Networking

This document records the working Koala networking shape on the `blink` homelab
host (`192.168.86.53`) and the failure mode that forced it.

## Working Topology

- `blink` host IP: `192.168.86.53`
- DVR IP: `192.168.86.46`
- DVR ports verified open from the host:
  - `80`
  - `554`
  - `34567`
- DVR port verified closed during debugging:
  - `8899`
- BearClaw talks to Koala at `http://192.168.86.53:8082`
- Koala Live UI talks to Koala at `http://192.168.86.53:8082`
- Koala worker is published on `8092`

## Failure Mode

On March 20, 2026, the homelab hit a split-brain networking failure:

- The `blink` host could connect to the DVR on `80`, `554`, and `34567`.
- Docker bridge containers could not connect to the DVR at all.
- `bearclaw-web` and `koala-koala-orchestrator-1` both timed out to
  `192.168.86.46` while still being able to reach:
  - the router at `192.168.86.1`
  - the host itself at `192.168.86.53`

That meant the issue was not BearClaw code, not Koala code, and not the DVR
configuration. It was specifically Docker-bridge-to-LAN connectivity inside the
`blink` environment.

## Required Workaround

Run the camera-facing Koala orchestrator on host networking.

The orchestrator must share the host network namespace so it reaches the DVR the
same way the host does.

The worker can remain on bridge networking because it only needs to accept
requests from the orchestrator and does not need direct DVR access.

## Required Settings

### Koala orchestrator

- `network_mode: host`
- `listen_addr: ":8082"`
- `service.address: "http://192.168.86.53:8082"`

### Koala worker

- Keep bridge networking
- Publish `8092:8090`
- Point the orchestrator at the host-published worker URL:
  - `worker.url: "http://127.0.0.1:8092"`

### BearClaw

- `KOALA_URL=http://192.168.86.53:8082`
- `KOALA_TOKEN` must match Koala `mcp_token`

## Reference Files

- Compose reference:
  - `docs/examples/docker-compose.homelab.yml`
- Koala config reference:
  - `configs/koala.homelab.yaml`

## Validation Commands

Host can reach DVR:

```bash
for port in 80 554 34567; do
  nc -z -w 3 192.168.86.46 "$port" && echo "open:$port"
done
```

Koala API returns cameras:

```bash
curl -sS -X POST http://192.168.86.53:8082/mcp/tools/koala.list_cameras \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"input":{}}'
```

Koala snapshot endpoint returns JPEG:

```bash
curl -sS -o /tmp/cam1.jpg \
  -H "Authorization: Bearer <token>" \
  http://192.168.86.53:8082/admin/cameras/cam_1/snapshot
file /tmp/cam1.jpg
```

BearClaw container can reach Koala:

```bash
docker exec bearclaw-web curl -sS http://192.168.86.53:8082/healthz
```

## Operational Notes

- If the DVR is visible on its local monitor but Koala shows every camera as
  unavailable, test host-to-DVR connectivity before changing code.
- If the host can reach the DVR but containers cannot, prefer host networking
  for the orchestrator over trying to tune app logic.
- Keep the worker on bridge networking unless it also needs direct LAN camera
  access.
