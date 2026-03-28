# Koala Blink Contract

This file documents the real behavior of [`blink.toml`](/Users/joecaruso/Projects/BareSystems/Koala/blink.toml).

## Target

- `jetson`
- type: SSH
- host: `jetson`
- user: `joe`
- runtime dir: `/home/joe/baresystems/runtime/koala`

## Services

### `koala-worker`

- Build: Python wheel built locally in a container
- Deploy shape: wheel uploaded to the Jetson, extracted into a runtime site directory, then launched with `python3 -m koala_worker.server`
- Pipeline: `fetch_artifact`, `provision`, `stop`, `install`, `start`, `health_check`, `verify`
- One-time GPU setup is manual and intentionally outside the deploy pipeline

### `koala-orchestrator`

- Build: native orchestrator binary workflow defined in the same manifest
- Runtime role: camera management, ingest dispatch, admin APIs, and MCP surface

## Verification

The manifest includes health and diagnostic checks for:

- worker process and HTTP port
- optional GPU readiness
- zone-state and ingest diagnostics
- orchestrator logs
- DVR reachability and capture diagnostics

## Operator Notes

- Koala's Blink posture is Jetson-first.
- The manual GPU bootstrap is part of the supported deployment contract.
- Update this file whenever the worker packaging, orchestrator runtime, Jetson assumptions, or verification tags change.
