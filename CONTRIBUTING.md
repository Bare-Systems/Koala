# Contributing to Koala

## Setup

Use the Go and Python toolchains required by the orchestrator and worker.

Representative validation commands:

```bash
go test ./...
python -m pytest worker/tests
```

## Expectations

- Keep contracts deterministic for agent consumption.
- Preserve privacy and degraded-mode behavior.
- Update `README.md` and `BLINK.md` when deployment or operator behavior changes.
- Update `CHANGELOG.md` for notable repo changes.

Active unfinished work belongs in the workspace root `ROADMAP.md`.
