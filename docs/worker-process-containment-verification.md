# Worker process containment verification

**Date:** 2026-08-04

**Scope:** contributor/fork evidence for upstream [PR #3550](https://github.com/Untrivial-ai/agent-orchestrator/pull/3550)

The submitted head is `bd7baa54e829c3426cdeefe345b8252d1c8ed746`, based on upstream `5f3e6bcd5a47bb7312f80cfc3966464a8f948cda`.

## Fork workflow evidence

Temporary fork PR [#20](https://github.com/FuqingZh/agent-orchestrator/pull/20) ran the exact head through the repository workflows. The following completed successfully:

- [Go workflow](https://github.com/FuqingZh/agent-orchestrator/actions/runs/30884181033): build-test, lint, and API-drift.
- [CLI E2E workflow](https://github.com/FuqingZh/agent-orchestrator/actions/runs/30884181008): Ubuntu, macOS, Windows, and container jobs.
- [gitleaks workflow](https://github.com/FuqingZh/agent-orchestrator/actions/runs/30884181029): secret scan.

The temporary PR was closed after verification and was not merged into the fork main branch. Its checks prove the fork workflow result for this exact head; they do not prove upstream required checks, maintainer acceptance, or production deployment.

## Host-canary boundary

The PR body records the opt-in Linux systemd containment canary as passing: a `setsid` child was scoped and reaped, an outside negative-control process survived, and Restart preserved the handle and terminal input. This remains contributor evidence until maintainers accept the design and the implementation is deployed and re-read from the production host.

PR B durable cleanup remains deferred behind PR A acceptance and maintainer coordination with [#2931](https://github.com/Untrivial-ai/agent-orchestrator/pull/2931).
