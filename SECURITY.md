# Security Policy

Koala handles home-state data, camera connectivity, update workflows, and bearer-protected admin and MCP surfaces.

## Reporting

Report vulnerabilities privately with:

- affected endpoint or subsystem
- whether the issue affects privacy, remote updates, or camera reachability
- reproduction steps
- expected versus actual enforcement

## Baseline Expectations

- Local-first and privacy-preserving behavior stays the default.
- Update and rollout actions must be auditable.
- Camera-network assumptions must stay explicit in `README.md` and `BLINK.md`.
- Secrets and tokens must not be committed.
