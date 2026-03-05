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

## Test

```bash
go test ./...
cd worker && python -m pytest
```
