# Koala - AI-Agent-First Home Security Platform

## Project Vision

Build a local-first, privacy-preserving home security platform that gives AI agents deterministic access to current house state through MCP tools. Koala runs on embedded hardware (Jetson-class), ingests multiple camera streams (DVR-first), performs local vision inference, and answers state queries such as “is there a package at the front door?” with strict, testable JSON contracts.

## Product Principles

1. Local inference first: no cloud dependency for core functionality.
2. Agent reliability over conversational flair: strict MCP contracts, deterministic outputs.
3. Safety and privacy by default: metadata retention only in MVP.
4. Progressive hardening: correctness first, then performance, then resilience.
5. Every feature ships with tests and MCP contract checks.

## Current State (as of March 6, 2026)

Implemented scaffold:
- Go orchestrator with config loading, camera registry, RTSP probe, ingest queue, state aggregator, MCP HTTP tool routes, token auth, and degraded mode basics.
- Python inference worker with YOLO abstraction + deterministic fallback detector.
- Initial proto contract for future gRPC boundary.
- Docker Compose and container build setup.
- Unit/integration tests for core Go modules and Python detector/model parsing.
- Remote update foundation: signed/encrypted bundles, rotation-aware signature verification, rollout orchestration, polling mode, and SQLite-backed security/rollout history.
- Streaming foundation: persistent ffmpeg ingest workers with reconnect policy, stall watchdog, MCP-visible ingest incident snapshots.
- Discovery enhancements: ONVIF fallback probing and camera capability reporting.
- Replay harness added for `check_package_at_door` latency and precision gate validation.

Known gap:
- Runtime Go<->Python transport is currently HTTP, not gRPC yet.

## North-Star Outcomes

### MVP (v0.1)
- Front door package/person state is queryable via MCP with deterministic JSON schema.
- Multi-camera ingest path operational through DVR network streams (RTSP-first).
- Degraded responses explicitly surfaced when inference is unavailable.
- Metadata-only persistence enabled by default.
- Over-the-network software update delivery to embedded Koala devices (no USB/manual reflash workflow).

### Beta (v0.2)
- gRPC production transport, stronger observability, replay evaluation harness, better model tuning.
- Confidence and latency measurable by repeatable benchmarks.

### Production Candidate (v1.0)
- Robust stream handling, hardened security controls, operational tooling, upgrade/migration strategy, and acceptance metrics validated on target Jetson hardware.

---

# Master Delivery Roadmap (12+ Weeks)

## Phase 0 - Program Setup and Operating Model (Week 1)

### Goals
- Establish build/test/release rhythm that supports daily delivery.
- Lock architecture decisions and interface contracts.

### Deliverables
- [ ] `PLAN.md` (this file) accepted as source of truth.
- [ ] Branch + release conventions documented.
- [ ] Definition of Done (DoD) and acceptance gates published.
- [ ] Weekly milestone template and daily work protocol documented.

### Definition of Done
- [ ] Team can pick any queued task and knows required tests + artifacts.
- [ ] Every merged change maps to a roadmap item and acceptance criterion.

---

## Phase 1 - Foundation Hardening (Weeks 1-2)

### Goals
- Make current scaffold stable, reproducible, and maintainable.

### Workstreams

#### 1.1 Repo and Build Hygiene
- [ ] Add `Makefile` with: `fmt`, `lint`, `test`, `test-go`, `test-py`, `compose-up`, `compose-down`.
- [ ] Add CI workflow (GitHub Actions): Go tests, Python tests, contract tests.
- [ ] Add lint/type tools:
  - Go: `golangci-lint` or minimal `go vet` + staticcheck.
  - Python: `ruff`, `mypy`.
- [ ] Ensure `.gitignore` and local cache handling are clean.

#### 1.2 Config and Validation
- [ ] Expand config schema with versioning (`config_version`).
- [ ] Add validation for duplicate camera IDs, unknown zone refs, missing front door mapping.
- [ ] Add per-camera/per-zone thresholds and runtime limits.
- [ ] Add startup config sanity report endpoint/log output.

#### 1.3 MCP Contract Stabilization
- [ ] Freeze MVP schemas for 4 tools.
- [ ] Add schema fixtures for response validation.
- [ ] Add MCP error code taxonomy (`invalid_input`, `unauthorized`, `unavailable`, `internal_error`).

#### 1.4 Device Update Foundation (MVP-Critical)
- [ ] Define update artifact format (version manifest + signed package metadata).
- [ ] Define orchestrator update API contracts (check, download, stage, apply, rollback, status).
- [ ] Define device identity and enrollment model for local-network update targeting.
- [ ] Add explicit rollback and health-check policy for failed updates.

### Tests
- [ ] Go unit tests for config validation edge cases.
- [ ] MCP contract tests validating strict field presence/types.
- [ ] Negative auth tests and malformed input tests.

### Exit Criteria
- [ ] `make test` is deterministic locally and in CI.
- [ ] Contract changes require explicit version bump policy.

---

## Phase 2 - Camera Ingest and DVR Compatibility (Weeks 2-4)

### Goals
- Support real-world DVR-centric deployments and robust stream status handling.

### Workstreams

#### 2.1 DVR Discovery Layer
- [ ] RTSP probe strategy for common DVR channel URL patterns.
- [ ] Optional ONVIF discovery probe (secondary path).
- [ ] Channel status model: `unknown`, `available`, `unavailable`, `degraded`.
- [ ] Startup discovery report and persistent last-known capability cache.

#### 2.2 Stream Ingest Engine
- [ ] Replace placeholder frame ingestion with real RTSP frame capture pipeline.
- [ ] Bounded queues per camera with global backpressure policy.
- [ ] Frame sampling policy (N fps target per camera, configurable).
- [ ] Clock handling and timestamp normalization.

#### 2.3 Failure Handling
- [ ] Reconnect strategy with jittered backoff.
- [ ] Stream stall detection and status transitions.
- [ ] Event annotations for camera outages.

### Tests
- [ ] Ingest unit tests for queue overflow/backpressure policy.
- [ ] Integration tests with RTSP fixture streams (including unavailable channel).
- [ ] Contract tests for `koala.list_cameras` status transitions.

### Exit Criteria
- [ ] Multi-camera ingest runs continuously for 24h soak without crash.
- [ ] Camera status changes are reflected in MCP responses within 5s.

---

## Phase 3 - Inference Service and gRPC Migration (Weeks 4-6)

### Goals
- Move to stable gRPC boundary and harden inference semantics.

### Workstreams

#### 3.1 Proto and Versioning
- [ ] Finalize `koala_inference.proto` messages and service methods.
- [ ] Add semantic contract version field and compatibility policy.
- [ ] Generate Go + Python stubs and check in deterministic generation workflow.

#### 3.2 Python Worker Runtime
- [ ] Implement gRPC server with `AnalyzeFrame`, `AnalyzeBatch`, `WorkerHealth`.
- [ ] Preserve deterministic fallback path for local and CI tests.
- [ ] Add model adapter abstraction for YOLO/TensorRT backends.
- [ ] Add request deadlines, max payload checks, and structured errors.

#### 3.3 Orchestrator gRPC Client
- [ ] Replace HTTP client with gRPC client and retry/timeout policy.
- [ ] Add worker circuit-breaker behavior.
- [ ] Keep degraded-mode contract unchanged for MCP clients.

### Tests
- [ ] gRPC contract integration tests (Go client <-> Python server).
- [ ] Compatibility tests for contract version mismatch behavior.
- [ ] Worker health degradation/resume scenario tests.

### Exit Criteria
- [ ] HTTP inference path removed or feature-flagged for legacy fallback.
- [ ] gRPC path stable under sustained ingest load.

---

## Phase 3.5 - Device Fleet Updates and Remote Rollout (Weeks 5-7, MVP-Critical)

### Goals
- Push Koala updates to Jetson devices over the local network without physically reconnecting devices.

### Workstreams

#### 3.5.1 Update Control Plane
- [ ] Add orchestrator admin endpoints for fleet update management.
- [ ] Add package repository source configuration (local NAS, LAN HTTP host, or signed bundle directory).
- [ ] Add per-device state model: `idle`, `downloading`, `staged`, `applying`, `restarting`, `healthy`, `failed`, `rolled_back`.

#### 3.5.2 Device Update Agent
- [ ] Implement lightweight on-device update agent/service.
- [ ] Add staged deploy flow: download -> integrity verify -> preflight check -> apply -> restart.
- [ ] Add health watchdog with automatic rollback on failed boot/health checks.

#### 3.5.3 Update Safety and Security
- [ ] Require signed manifests/packages and checksum verification.
- [ ] Add version compatibility checks (orchestrator, worker, proto contract).
- [ ] Add rollout policies: all-at-once, canary subset, and batched rollout.

### Tests
- [ ] Integration test for successful remote update on at least one Jetson test node.
- [ ] Failure-path tests: corrupt package, interrupted download, failed health check, rollback success.
- [ ] Contract tests for update status API and fleet progress reporting.

### Exit Criteria
- [ ] Operator can remotely update device software from the network without physical access.
- [ ] Failed updates auto-rollback and preserve last known healthy version.

---

## Phase 4 - Vision Quality and Zone Semantics (Weeks 6-8)

### Goals
- Improve package/person detection quality and state accuracy for front door queries.

### Workstreams

#### 4.1 Detection Pipeline
- [ ] Standardize label mapping (`person`, `package`) across model outputs.
- [ ] Confidence calibration strategy (global + camera override).
- [ ] Temporal smoothing policy to reduce flicker false positives.

#### 4.2 Zone Logic
- [ ] Polygon-based zone definitions for front door region.
- [ ] In-zone filtering for detections by bbox overlap threshold.
- [ ] Sliding-window state aggregator with explicit stale-state semantics.

#### 4.3 Replay Dataset Harness
- [ ] Add labeled replay sets:
  - package present
  - person only
  - no object
  - occlusion
  - low light/night
- [ ] Build evaluation runner generating precision/recall and confusion stats.

### Tests
- [ ] Unit tests for zone intersection math and smoothing behavior.
- [ ] Replay tests asserting expected MCP tool outputs.
- [ ] Regression suite for confidence threshold changes.

### Exit Criteria
- [ ] >90% precision target met on agreed front-door replay set.
- [ ] No schema drift in MCP outputs under varied scenarios.

---

## Phase 5 - MCP Reliability and Agent Validation (Weeks 8-9)

### Goals
- Ensure agents can reliably call Koala tools and receive actionable, deterministic responses.

### Workstreams

#### 5.1 Tooling Contracts
- [ ] Publish tool specs with examples and failure semantics.
- [ ] Enforce strict JSON output contracts (no accidental free-form response paths).
- [ ] Add freshness semantics and stale-data explanation consistency.

#### 5.2 Agent Prompt/Tool Harness
- [ ] Build harness that simulates prompt-driven tool selection.
- [ ] Validate tool-call arguments and response parsing for common prompts.
- [ ] Add deterministic transcripts for CI replay.

#### 5.3 Error Handling UX
- [ ] Standard explanations for degraded and unavailable states.
- [ ] Guidance fields for next-best action (e.g., “camera unavailable, last seen at ...”).

### Tests
- [ ] MCP schema conformance tests across all tools.
- [ ] Prompt-to-tool routing tests for top user intents.
- [ ] Degraded-mode conversational continuity tests.

### Exit Criteria
- [ ] Agent harness passes deterministic contract checks in CI.
- [ ] Top intent set succeeds without manual interpretation hacks.

---

## Phase 6 - Security and Privacy Hardening (Weeks 9-10)

### Goals
- Harden local deployment and access surface before broader rollout.

### Workstreams

#### 6.1 Access Control
- [ ] Token auth rotation mechanism and startup secret validation.
- [ ] Optional local network allowlist support.
- [ ] Request audit logs with request IDs.

#### 6.2 Privacy Controls
- [ ] Enforce metadata-only persistence policy by default.
- [ ] Optional short-lived frame buffering toggled off by default.
- [ ] Data retention scheduler for metadata TTL.

#### 6.3 Threat Mitigation
- [ ] Input size limits and rate limiting for MCP endpoints.
- [ ] Safe failure behavior on malformed/burst traffic.

### Tests
- [ ] Auth negative tests and token rotation tests.
- [ ] Privacy tests proving no frame payload persistence in default mode.
- [ ] Basic endpoint abuse/rate-limit tests.

### Exit Criteria
- [ ] Security checklist complete for LAN deployment.
- [ ] Privacy defaults verified by tests.

---

## Phase 7 - Observability and Operations (Weeks 10-11)

### Goals
- Make Koala diagnosable and operable on embedded hardware.

### Workstreams

#### 7.1 Telemetry
- [ ] Structured logs (service, camera_id, zone_id, request_id).
- [ ] Metrics endpoint for key signals:
  - ingest queue depth
  - frame throughput
  - inference latency (p50/p95)
  - tool latency and error rate
  - camera availability
- [ ] Health/readiness endpoints per service.

#### 7.2 Operational Controls
- [ ] Graceful shutdown and startup dependency ordering.
- [ ] Runtime config snapshot endpoint.
- [ ] Troubleshooting runbook draft.

### Tests
- [ ] Health/readiness tests under dependency failures.
- [ ] Telemetry smoke tests (metrics keys present, logs parseable).

### Exit Criteria
- [ ] Operator can diagnose top 10 expected failures using docs + telemetry.

---

## Phase 8 - Jetson Performance Optimization (Weeks 11-12)

### Goals
- Reach MVP latency targets and stable runtime on target hardware.

### Workstreams

#### 8.1 Model Runtime Optimization
- [ ] Evaluate YOLO variants for Jetson memory/latency tradeoff.
- [ ] Add TensorRT export/inference path.
- [ ] Batch vs single-frame policy benchmarking.

#### 8.2 Pipeline Tuning
- [ ] Tune frame sampling rate per camera.
- [ ] Tune queue sizes and worker concurrency.
- [ ] Pinpoint bottlenecks via profiling (CPU/GPU/memory).

#### 8.3 Burn-In and Soak
- [ ] 24h and 72h stability runs with replay + live streams.
- [ ] Memory and thermal stability checks.

### Tests and Benchmarks
- [ ] Benchmark suite with reproducible datasets and configs.
- [ ] P95 latency report for `koala.check_package_at_door`.

### Exit Criteria
- [ ] P95 tool response <2s on target Jetson profile.
- [ ] No fatal crash across 72h soak.

---

## Phase 9 - Beta Readiness and v0.1 Release (Week 13+)

### Goals
- Prepare for controlled beta usage.

### Deliverables
- [ ] Release notes and versioned migration notes.
- [ ] Deployment guide for DVR + Jetson setup.
- [ ] Known limitations and support boundaries.
- [ ] Acceptance report (quality, latency, reliability, privacy).

### Exit Criteria
- [ ] Stakeholder sign-off on MVP acceptance gates.
- [ ] Backlog triaged into post-v0.1 roadmap.

---

# Cross-Cutting Engineering Tracks

## A. Testing Strategy (Always-On)

### Test layers
- Unit tests (Go + Python).
- Integration tests (service boundaries).
- Contract tests (MCP and gRPC schemas).
- Replay E2E tests.
- Soak/performance tests.

### Policy
- No feature is complete without:
  - [ ] at least one automated test,
  - [ ] contract/schema verification when interfaces change,
  - [ ] docs updates when behavior changes.

## B. Interface Governance

### MCP stability rules
- Additive changes only in minor versions.
- Breaking schema changes require versioned tool naming or explicit migration plan.

### Inference contract rules
- Proto changes require compatibility note and test updates.
- Backward compatibility target: support N and N-1 worker/orchestrator pairings during development.

## C. Documentation Discipline
- `README.md`: quickstart and high-level architecture.
- `PLAN.md`: roadmap and acceptance gates.
- `CHANGELOG.md`: feature history.
- `docs/runbooks/*.md`: operational procedures.

## D. Reliability and Failure Taxonomy
- Ingest failures: camera unavailable, stream timeout, decode failures.
- Inference failures: worker unavailable, model error, timeout.
- Contract failures: invalid input, auth failure, schema mismatch.
- Each class must have deterministic MCP response mapping.

---

# Detailed Backlog Seed (First 30 Working Days)

## Week 1
- [ ] Add CI + Makefile + lint/type check stack.
- [ ] Freeze MCP schemas and add contract fixtures.
- [ ] Expand config validation + tests.

## Week 2
- [ ] Implement robust RTSP discovery patterns.
- [ ] Add channel status transitions and tests.
- [ ] Improve ingest queue instrumentation.

## Week 3
- [ ] Integrate real frame capture from RTSP.
- [ ] Add reconnect/backoff and stall handling.
- [ ] Add replay fixture runner baseline.

## Week 4
- [ ] Finalize proto and generate language stubs.
- [ ] Implement Python gRPC server path.
- [ ] Implement Go gRPC client path.

## Week 5
- [ ] Remove/flag HTTP inference transport.
- [ ] Add gRPC compatibility tests and CI gate.
- [ ] Add worker circuit-breaker behavior.

## Week 6
- [ ] Implement zone polygon filtering.
- [ ] Implement temporal smoothing in state engine.
- [ ] Add low-light and occlusion replay cases.

## Week 7
- [ ] Build first remote update agent + orchestrator update API path.
- [ ] Add signed manifest + checksum verification.
- [ ] Add rollback on failed health check.

## Week 8
- [ ] Add evaluation metrics output (precision/recall).
- [ ] Tune thresholds per camera.
- [ ] Publish first quality report.

## Week 9
- [ ] Build prompt-to-tool agent harness.
- [ ] Add degraded mode conversational validation.
- [ ] Harden error explanation consistency.

## Week 10
- [ ] Add token rotation + rate limiting.
- [ ] Add metadata retention TTL jobs.
- [ ] Add privacy verification tests.

## Week 11
- [ ] Implement metrics endpoint.
- [ ] Add runbook for common incidents.
- [ ] Add readiness/liveness probes.

## Week 12
- [ ] Add TensorRT path experimentation.
- [ ] Run benchmark matrix and tune sampling/queues.
- [ ] Capture p95 latency profiles.

## Week 13
- [ ] 72h soak runs.
- [ ] Release candidate packaging.
- [ ] Complete v0.1 acceptance report.

---

# Acceptance Gates (Must Pass for MVP v0.1)

## Functional
- [ ] `koala.check_package_at_door` returns deterministic JSON with expected fields.
- [ ] `koala.get_zone_state` returns current/last-known entities with freshness.
- [ ] `koala.list_cameras` accurately reports channel statuses.
- [ ] `koala.get_system_health` reflects dependency states.
- [ ] Fleet update API can remotely stage/apply updates to target device(s) and report status end-to-end.

## Quality
- [ ] Package/person precision >90% on approved front-door replay dataset.
- [ ] No critical schema contract test failures.

## Performance
- [ ] P95 response time for `koala.check_package_at_door` <2s on target Jetson.

## Reliability
- [ ] Degraded mode behaves deterministically during inference outages.
- [ ] No crash during 24h continuous ingest and query run.
- [ ] Failed update path automatically rolls back to last known healthy version.

## Privacy/Security
- [ ] Metadata-only persistence default verified.
- [ ] Token auth required and tested for all MCP tools.
- [ ] Update artifacts are signed and integrity-checked before apply.

---

# Post-MVP Expansion Roadmap (v0.2+)

## R1 - Proactive Intelligence and Alerting
- [ ] Event stream and alert engine (`package_arrived`, `loitering`, `camera_tamper`, `after_hours_presence`).
- [ ] Notification channels: push, SMS, Slack, and Home Assistant events.
- [ ] Policy scheduler for quiet hours, escalation tiers, and acknowledgement rules.

## R2 - Advanced Agent Capabilities
- [ ] New MCP tools for timeline search, cross-time comparisons, and evidence summaries.
- [ ] Prompt-to-tool reliability suite for natural-language time and state questions.
- [ ] Structured evidence references (camera, timestamp, confidence, related events).

## R3 - Identity and Context Intelligence
- [ ] Familiar person and vehicle recognition with user-managed allowlists.
- [ ] Delivery signal enrichment (carrier logo/text heuristics where feasible).
- [ ] Context-aware suppression policies for known expected activity.

## R4 - Multi-Camera Reasoning
- [ ] Multi-camera entity handoff/tracking across zones.
- [ ] Persistent object lifecycle tracking (arrived/still_present/removed).
- [ ] Cross-camera consistency checks to reduce false positives.

## R5 - Automation and Action Framework
- [ ] Integrations with smart home systems (lights, locks, sirens) behind policy gates.
- [ ] Confirmation-required flows for high-risk actions initiated by agent requests.
- [ ] Rule builder for event-driven automations tied to Koala state signals.

## R6 - Security and Compliance Hardening
- [ ] mTLS option and fine-grained per-tool authorization scopes.
- [ ] Signed and tamper-evident audit logs with export support.
- [ ] Optional encrypted at-rest metadata storage profiles.

## R7 - Fleet and Lifecycle Management
- [ ] Multi-site fleet dashboard for device health, versioning, and rollout control.
- [ ] Canary and phased rollouts with automated stop conditions.
- [ ] Drift detection and automated evaluation pipelines for model/version updates.

## R8 - Forensics and Evidence Workflows
- [ ] Optional encrypted short-term clip retention policies (opt-in only).
- [ ] Searchable incident timeline and report export.
- [ ] Evidence packaging with provenance metadata for insurance/legal workflows.

---

# Daily Execution Protocol

For each day’s work item:
1. Pick one roadmap checkbox and create a scoped task branch.
2. Write/update task note in `changes/<task>.md` with acceptance criteria.
3. Implement the smallest vertical slice.
4. Add/adjust tests first or alongside code.
5. Run local checks (`make test` equivalent).
6. Update docs/changelog for behavior/interface changes.
7. Merge only when all acceptance checks for the item pass.

## Commit Style
- One behavior change per commit.
- Include test evidence in commit message body.

---

# Open Decisions to Revisit (Tracked, Not Blocking)

- ONVIF scope level for MVP vs post-MVP.
- Exact object model variants per Jetson SKU.
- Whether to expose event subscriptions in MCP before v1.
- Optional encrypted at-rest metadata store for post-MVP.

---

# Definition of Done (Global)

A roadmap item is complete only when:
- [ ] Implementation merged.
- [ ] Automated tests added/updated and passing.
- [ ] MCP/gRPC contracts validated if relevant.
- [ ] Documentation updated.
- [ ] Observability/error behavior reviewed.
