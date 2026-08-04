# agent-orchestrator rewrite docs

The agent-orchestrator is being rebuilt as a long-running Go backend daemon
(`backend/`) plus an Electron + TypeScript frontend (`frontend/`). The backend
supervises coding-agent sessions and exposes daemon control, project/session
state, terminal streaming, and CDC/event infrastructure.

Start with [architecture.md](architecture.md) for the current backend model and
[cli/README.md](cli/README.md) for the CLI surface.

## Reference docs

| Doc                                                    | What it covers                                                                                                        |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- |
| [architecture.md](architecture.md)                     | Current backend model, package layout, status derivation, persistence/CDC, and load-bearing rules.                    |
| [backend-code-structure.md](backend-code-structure.md) | Package ownership rules for the Go backend: domain, services, ports, adapters, storage, HTTP, CLI, and daemon wiring. |
| [cli/README.md](cli/README.md)                         | CLI commands and daemon control surface.                                                                              |
| [development.md](development.md)                       | Prerequisites, build steps, running tests, and troubleshooting for local development.                                 |
| [STATUS.md](STATUS.md)                                 | What is shipped on `main` today and what is still in flight.                                                          |
| [stack.md](stack.md)                                   | Accepted library/runtime choices, pending stack decisions, and dependencies explicitly avoided for V1.                |
| [telemetry.md](telemetry.md)                           | Telemetry collection, privacy safeguards, configuration, and PostHog dashboard guidance.                              |
| [posthog-cost-controls.md](posthog-cost-controls.md)   | PostHog event-name migration, ingestion drop rules, and dashboard queries for reducing telemetry spend.              |
| [adr/0002-upstream-first-orchestration-convergence.md](adr/0002-upstream-first-orchestration-convergence.md) | Why the fork uses upstream AO lifecycle and keeps Linear as a thin intake adapter. |
| [adr/0003-stable-upstream-baseline-sync.md](adr/0003-stable-upstream-baseline-sync.md) | Why the fork synchronizes from pinned stable releases and preserves a minimal downstream delta. |
| [adr/0004-explicit-upstream-main-snapshots.md](adr/0004-explicit-upstream-main-snapshots.md) | Why an explicitly authorized development baseline may pin and merge an upstream `main` snapshot. |
| [plans/2026-08-03-v0.11.2-baseline-sync.md](plans/2026-08-03-v0.11.2-baseline-sync.md) | Completed historical plan for synchronizing the fork to upstream v0.11.2. |
| [plans/2026-08-04-upstream-main-sync.md](plans/2026-08-04-upstream-main-sync.md) | Completed plan for synchronizing the fork to upstream `main` at `5f3e6bcd`. |
| [plans/2026-08-04-worker-process-containment.md](plans/2026-08-04-worker-process-containment.md) | Submitted upstream PR #3550 for the minimal opt-in systemd scope slice that makes tmux Restart/Destroy `setsid`-safe; awaiting maintainer review. |
| [plans/2026-08-04-durable-runtime-cleanup.md](plans/2026-08-04-durable-runtime-cleanup.md) | Deferred PR B plan for generation-fenced cleanup facts, daemon-restart rediscovery, and bounded retry after PR A. |
| [v0.11.2-sync-verification.md](v0.11.2-sync-verification.md) | Canonical-gate and isolated-canary evidence for the v0.11.2 fork synchronization. |
| [upstream-main-sync-verification.md](upstream-main-sync-verification.md) | Merge-specific, canonical-gate, and isolated CLI evidence for the pinned upstream `main` synchronization. |
| [worker-process-containment-verification.md](worker-process-containment-verification.md) | Exact-head fork CI and host-canary evidence for upstream PR #3550; not upstream acceptance or production deployment. |
| [downstream-patches.md](downstream-patches.md)       | Retained fork patches, their evidence, and the condition for deleting each one.                                      |
| [upstream-migration-map.md](upstream-migration-map.md) | Immutable mapping from colliding upstream migration versions to shipped fork versions. |

## Mental model

Persist durable facts, derive display status:

- session table: `activity_state`, `is_terminated`, identity, metadata
- PR tables: PR/CI/review facts
- derived read model: `service.Session` computes display status from session + PR facts
