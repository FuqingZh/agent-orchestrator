# Minimal `setsid`-safe tmux containment

**Date:** 2026-08-04

**Status:** Implemented on the fork branch and submitted upstream as [PR #3550](https://github.com/Untrivial-ai/agent-orchestrator/pull/3550); awaiting maintainer review

The submitted head is `bd7baa54e829c3426cdeefe345b8252d1c8ed746` against upstream base `5f3e6bcd5a47bb7312f80cfc3966464a8f948cda`. The exact head passed the fork validation PR [#20](https://github.com/FuqingZh/agent-orchestrator/pull/20): Go build/lint/API-drift, Ubuntu/macOS/Windows CLI E2E, Docker fresh-install, and gitleaks. The PR body records the opt-in Linux host canary as passing; these are contributor/fork evidence, not upstream required checks, maintainer acceptance, or production deployment.

**Delivery slice:** upstream PR A

**Tracks:** [Untrivial-ai/agent-orchestrator#2523](https://github.com/Untrivial-ai/agent-orchestrator/issues/2523)

**Evidence baseline:** upstream `main` at `5f3e6bcd5a47bb7312f80cfc3966464a8f948cda`

This is an implementation plan, not current architecture. The evidence
baseline still reaps tmux descendants by the pane's POSIX session ID and does
not contain descendants that call `setsid`.

## Repository contract audit

The plan was checked against current upstream `CONTRIBUTING.md`, `AGENTS.md`,
`docs/development.md`, the pull-request template, and the path-filtered GitHub
workflows.

The implementation must:

- branch from a freshly fetched upstream `main` and keep one issue in one PR;
- remain surgical, use the existing tmux runtime boundary, and avoid one-off
  forwarding abstractions;
- use a conventional `fix:` commit and fill the PR template's What, Why, How,
  Testing, and intentional-omissions sections;
- use Go 1.25.7 or newer, Node.js 20.19.0 or newer, and npm 10 or newer for the
  documented local gate; and
- pass the backend Go, API-drift, native CLI E2E, Docker fresh-install, and
  gitleaks workflows triggered by a `backend/**` change.

No repository development or test rule requires broadening this fix into
durable cleanup, Docker cleanup, database, API, or frontend behavior.

## Outcome and claim boundary

When `AO_PROCESS_CONTAINMENT=systemd` is explicitly selected on Linux, every
process launched by a tmux worker belongs to one deterministic systemd user
scope. `Restart` and `Destroy` release that scope, so a descendant cannot
escape cleanup by changing its POSIX session with `setsid`.

The PR is complete when it proves the normal successful
`Create -> Restart -> Destroy` lifecycle. It does not claim crash-restart or
durable retry closure; those remain PR B.

Unset configuration preserves the current tmux SID-reaper behavior. An
explicitly requested but unavailable systemd backend fails before creating the
tmux session rather than silently falling back.

## Non-goals

PR A does not change:

- Docker or other container reaping;
- cleanup facts, generations, SQLite, daemon-restart reconciliation, or retry;
- workspace disposition, Linear, GitHub, Dashboard, HTTP, OpenAPI, or frontend
  behavior;
- macOS's existing tmux compatibility path or Windows ConPTY behavior; or
- resource ceilings and shared-filesystem search policy.

No Docker interaction test belongs in this PR. If implementation evidence
forces a lifecycle call-order change outside the tmux adapter, stop and
re-scope rather than silently absorbing it.

## Minimal design

### 1. Explicit opt-in

Add one validated configuration value:

```text
AO_PROCESS_CONTAINMENT=systemd
```

Unset selects the current backend. `systemd` is Linux-only and requires
`systemd-run --user` plus a usable user manager. Unknown values and explicit
unavailability are configuration errors.

Do not add a Desktop setting, project-level setting, API field, or automatic
backend selection in this PR.

### 2. Put the pane command in one exact scope

Wrap the command executed inside the tmux pane, not the transient
`tmux new-session` client. The persistent tmux server otherwise launches the
pane outside the desired boundary.

Use an exact unit name derived from the already-sanitized runtime handle, for
example:

```text
ao-session-<handle>.scope
```

The launch is conceptually:

```bash
exec systemd-run --user --scope --collect \
  --unit=ao-session-<handle>.scope \
  --property=KillMode=control-group \
  --property=TimeoutStopSec=<reap-grace> \
  --property=SendSIGKILL=yes \
  -- <existing shell and launch command>
```

Preserve the existing environment filtering, workspace `cd` guard, supervisor,
interactive keep-alive shell, and terminal input behavior. Do not build a
second launch-command pipeline.

Keep the implementation private to the tmux adapter. A small private helper is
allowed when it owns unit naming, command wrapping, status interpretation, and
release; do not add a new public port unless a real caller needs it.

### 3. Let systemd own TERM, grace, KILL, and empty proof

Do not add bespoke cgroup v1/v2 filesystem walkers in PR A. Configure the
scope's control-group kill policy and stop timeout, call `systemctl --user stop`
for the exact unit, and then read back the unit state.

Release succeeds only when the unit is authoritatively `inactive/dead` or
unloaded (`not-found`). `active`, `activating`, `deactivating`, malformed,
unreadable, timed-out, or otherwise unknown state is an error. A missing unit
is idempotent success.

The real Linux canary is the acceptance oracle for whether systemd unit state
is sufficient on supported hosts. Add direct cgroup probing only if that
evidence disproves the simpler contract.

### 4. Integrate only `Create`, `Restart`, and `Destroy`

- `Create` wraps the actual pane command and verifies that the expected scope
  became active before reporting success.
- In scoped mode, enable tmux `remain-on-exit` so stopping the old scope does
  not remove the pane required by `respawn-pane`.
- `Restart` releases the old scope first. It must not respawn when release is
  unsuccessful, and it preserves the existing runtime handle on success.
- `Destroy` attempts exact-scope release even when the tmux session is already
  absent, then returns success only after tmux is absent and the scope is
  released.
- The unconfigured path continues to use the existing SID reaper without
  behavioral changes.

Use the existing `Destroy` error channel. Current normal Kill and Restart paths
already stop on runtime-destroy errors, so PR A does not add a public
`ErrRuntimeReleasePending`, audit unrelated teardown call sites, or implement
durable retry.

## Expected code surface

| Area | Expected files | Responsibility |
| --- | --- | --- |
| Configuration | `backend/internal/config/config.go` and tests | parse and validate the opt-in value |
| Runtime wiring | existing runtime selection/daemon wiring | pass the resolved backend to tmux |
| systemd scope | focused Linux file plus non-Linux build stub under `backend/internal/adapters/runtime/tmux/` | exact unit wrap, status, release |
| tmux lifecycle | `tmux.go`, command helpers, adjacent tests | integrate Create, Restart, Destroy, and `remain-on-exit` |
| Acceptance | tmux integration test | prove changed-SID cleanup and restart behavior on a real user manager |

Expected production changes stay in the backend configuration/wiring and tmux
adapter. If implementation requires lifecycle manager, storage, API, generated
types, frontend, or container changes, pause and reassess the PR boundary.

## Test plan

### Layer 1: hermetic unit tests

Use injected command results; do not require a real daemon, tmux server, or
systemd manager.

Cover:

- unset, valid `systemd`, unknown, non-Linux, and unavailable-backend config;
- deterministic and escaped unit naming;
- launch wrapping with exact systemd properties and safe argument handling;
- active-scope verification;
- release success for `inactive/dead` and `not-found`;
- release failure for command failure, timeout, `active`, `activating`,
  `deactivating`, malformed, and unreadable state;
- Destroy releasing the scope when tmux exists and when tmux is already gone;
- Restart releasing before respawn and refusing respawn after release failure;
- handle preservation and terminal command behavior after successful restart;
  and
- unchanged commands and SID-reaper behavior when containment is unset.

Run the narrow feedback loop first:

```bash
cd backend
go test ./internal/config ./internal/adapters/runtime/runtimeselect
go test ./internal/adapters/runtime/tmux \
  -run 'Systemd|Containment|Destroy|Restart' -count=1
```

### Layer 2: explicit real-Linux canary

Add one opt-in integration test that uses an isolated tmux server and an exact
transient user scope. Default CI/unit runs may skip it; when the canary flag is
explicitly set, missing tmux, user systemd, or permissions is a failure.

```bash
AO_TEST_SYSTEMD_CONTAINMENT=1 \
go test ./internal/adapters/runtime/tmux \
  -run TestRuntimeIntegrationSystemdContainment -count=1 -v
```

The canary must:

1. launch a worker whose child calls `setsid` and ignores TERM;
2. prove before teardown that the child's SID differs from the pane SID and
   that the child belongs to the expected scope;
3. launch an unrelated process outside the scope as a negative control;
4. call `Destroy` and prove the escaped process identity is gone, tmux is gone,
   the scope is inactive/dead or unloaded, and the negative control is alive;
5. repeat through `Restart`, proving the old process identity is gone before
   the new command starts, the runtime handle is unchanged, and terminal input
   still works; and
6. clean the negative control, tmux server, and transient unit on every exit.

Record PID, SID, `/proc/<pid>/stat` start time, expected unit, unit state, and
negative-control evidence. PID alone is not sufficient because it can be
reused.

This repository's GitHub-hosted runners do not guarantee a usable user systemd
manager, so the explicit canary is host acceptance evidence rather than a new
required default-CI job.

### Layer 3: repository gate

Use bounded concurrency on constrained hosts, but run the same logical gate as
the repository:

```bash
cd backend
test -z "$(gofmt -l .)"
go build ./...
go vet ./...
go test -race ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 \
  run --path-mode=abs
cd ..
npm ci
npm run api
git diff --exit-code -- \
  backend/internal/httpd/apispec/openapi.yaml \
  frontend/src/api/schema.ts
```

Also run the tagged CLI suite locally when practical:

```bash
cd backend
go test -tags e2e -v ./internal/cli/...
```

The pull request must pass the exact-head Go workflow, native CLI E2E on
Ubuntu/macOS/Windows, Docker fresh-install, API drift, and gitleaks. Full
frontend Vitest, Playwright, mobile, release, and packaging gates are outside
the local scope because the PR does not touch those surfaces.

## Delivery

- Start the implementation from freshly fetched upstream `main`, not the fork
  documentation branch.
- Keep one focused `fix:` commit or a small reviewable series with one logical
  contract.
- Link #2523 with `Refs #2523`; do not claim durable cleanup or resource-limit
  closure.
- In the PR body, explicitly omit Docker cleanup, durable retry, database/API,
  frontend, and automatic/default systemd rollout.
- Maintainers own review and merge; do not request automatic merge on the
  upstream repository.

## Acceptance

PR A is complete only when all of the following are true:

1. a descendant with `SID != pane SID` and ignored TERM is removed by scoped
   Destroy;
2. an unrelated process outside the exact scope survives;
3. Restart cannot overlap old and new scope occupants and preserves terminal
   usability;
4. unknown or incomplete scope release returns an error rather than success;
5. the unset backend retains current behavior on Linux, macOS, and Windows;
   and
6. the focused tests, real-host canary, complete backend gate, and exact-head
   GitHub checks all pass.
