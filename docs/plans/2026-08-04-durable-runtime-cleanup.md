# Durable Runtime Cleanup and Reconciliation

**Date:** 2026-08-04

**Status:** Deferred dependency — PR A is submitted upstream but not maintainer-accepted

PR B remains closed to implementation until [PR #3550](https://github.com/Untrivial-ai/agent-orchestrator/pull/3550) is accepted and maintainers resolve ownership and coordination with [#2931](https://github.com/Untrivial-ai/agent-orchestrator/pull/2931). The fork CI and host-canary evidence for PR A do not satisfy that contribution gate.

**Delivery slice:** PR B

**Depends on:** accepted PR A containment semantics and a maintainer-approved durable-finalizer direction

**Related work:** [#2523](https://github.com/Untrivial-ai/agent-orchestrator/issues/2523), [#2931](https://github.com/Untrivial-ai/agent-orchestrator/pull/2931)
**Evidence baseline:** upstream `main` at `5f3e6bcd5a47bb7312f80cfc3966464a8f948cda`

This document is an implementation plan, not current architecture. Upstream
`main` contains cleanup persistence substrate but does not yet run the durable
finalizer described below.

The refreshed baseline adds a best-effort, label-based Docker container reaper
at `MarkTerminated`. It remains non-durable, logs rather than returns failure,
and does not change the missing process-containment or finalizer behavior.

## Goal

Make an incomplete runtime or workspace release remain durable, observable,
generation-safe, and automatically retryable across daemon crashes and
restarts. Mark `RuntimeReleasedAt` only after the operating-system containment
boundary from PR A is authoritatively empty.

## Contribution gate

Do not implement or open PR B until all of the following are true:

1. maintainers have accepted the containment direction and ownership for PR A;
2. maintainers have decided whether PR B extends, replaces, or coordinates
   with #2931; and
3. the accepted issue/PR topology gives this contribution a focused owner.

#2931 is currently open with requested changes. This plan must not assume that
its branch will merge, copy its implementation blindly, or compete with an
active contributor. Unless maintainers request a stack, create PR B from the
latest `main` after PR A merges.

## Current substrate and missing behavior

The evidence baseline already has:

- `domain.SessionCleanupRecord` with runtime release, workspace disposition,
  attempt, schedule, and failure fields;
- migration `0030_session_cleanup_facts.sql`;
- SQLite queries and store methods to upsert, read, and find retry candidates;
  and
- CDC triggers for cleanup-fact changes, intended for read-model invalidation
  rather than as a finalizer self-wake loop.

Do not redesign or duplicate those structures merely because they are not yet
wired into session lifecycle.

The missing behavior is orchestration:

- session spawn does not advance a cleanup generation fence;
- termination paths do not consistently create/update cleanup facts;
- no finalizer owns runtime release followed by workspace release;
- daemon boot and termination/restore CDC do not wake durable cleanup work;
- no bounded backoff loop retries a populated or temporarily unreadable scope;
- several synchronous paths still ignore `Runtime.Destroy` errors; and
- the current read model must not infer release merely from tmux disappearance;
  and
- Docker container reaping is a separate best-effort terminal side effect, not
  a durable cleanup fact or proof of host-process release.

## Dependencies produced by PR A

PR B consumes the following accepted contracts from PR A:

- a deterministic containment identity recoverable from persisted session and
  runtime identity;
- `Destroy == nil` means tmux is absent and containment is proven empty;
- `Destroy != nil` preserves the distinction between proven release and an
  unknown or incomplete release; PR B may add internal retry classification
  when it owns durable retry;
- no generation replacement while the preceding scope is populated; and
- caller behavior that already preserves workspaces synchronously on release
  failure.

If the accepted PR A implementation changes any of those interfaces, update
this plan before implementation rather than reconstructing the original design
inside the finalizer.

## Required outcome contract

- Every cleanup attempt is fenced by the authoritative session cleanup
  generation.
- Old-generation workers cannot mark a newer generation released or delete its
  workspace.
- `RuntimeReleasedAt` remains unset while containment is populated, unreadable,
  or not authoritatively attributable.
- Workspace removal starts only after runtime release is proven.
- Dirty-workspace preservation remains distinct from transient or exhausted
  runtime/workspace failures.
- Incomplete release remains stored as pending with bounded retry metadata.
- Retry exhaustion becomes an explicit terminal failure that requires manual
  retry; it does not loop forever or silently become success.
- Daemon boot rediscovers due cleanup work and a daemon crash between any two
  steps does not lose the obligation.
- Display status remains derived from durable facts. Do not persist a parallel
  UI status column.

## Architectural decisions

### 1. Reuse cleanup facts as the durable state machine

Extend the session manager's store boundary to use the existing cleanup-facts
methods. Do not add a new manifest, sidecar database, or service-owned JSON
file.

On a newly prepared launch, advance `sessions.cleanup_generation`. Cleanup
facts record the generation they describe. Every finalizer attempt re-reads the
session and facts after acquiring its session lock and exits harmlessly when
its generation is stale.

If current schema and queries can represent the accepted behavior, add no
migration. If implementation evidence exposes a genuinely missing durable
field, add a new migration; never edit migration 0030 or hand-edit generated
`backend/internal/storage/sqlite/gen` files.

### 2. One idempotent finalizer owns teardown ordering

Introduce one session-scoped finalizer whose main path is linear:

```text
re-read session + generation
→ destroy/verify runtime containment
→ record RuntimeReleasedAt
→ close scoped shell terminals
→ release workspace or preserve dirty state
→ record terminal workspace disposition
```

Each successful step is durable before the next destructive step. Re-running
the same generation is safe. A crash after runtime release but before workspace
release resumes at the workspace step rather than recreating or re-killing the
runtime.

The accepted design must explicitly decide where Docker container cleanup
belongs. Until then, retain the current adapter contract but do not let its
best-effort result advance `RuntimeReleasedAt`, workspace disposition, or scope
release. If maintainers want container cleanup made durable, add a distinct
fact/step rather than overloading the operating-system process-release fact.

All lifecycle entry points that can terminate, restore, replace, clean, or
reconcile a session must serialize through the same per-session lock. Holding a
bulk list result is not enough: after acquiring the lock, re-read the
authoritative session and generation before acting.

### 3. Bounded retry and explicit terminal outcomes

Use named retry constants with tests for:

- initial delay;
- bounded exponential backoff;
- maximum attempts; and
- terminal transition after exhaustion.

A containment-release error that PR B classifies as transient records attempt
time, failure code, and `NextAttemptAt`. It remains retryable. A dirty workspace
records `preserved_dirty` and pauses automatic retry until explicit user
action. A non-dirty cleanup that reaches the retry cap records `failed` and
also requires explicit retry.

Do not apply retry to authentication, permission, invalid configuration, stale
generation, or cancellation failures as if they were transient.

### 4. Boot, event, and periodic wakeups

The finalizer runner should wake from:

- daemon boot for already-due cleanup facts;
- relevant session termination or restoration CDC events;
- an explicit enqueue after a user-triggered retry reset; and
- a bounded periodic timer that covers missed events and future
  `NextAttemptAt` values.

It must deduplicate concurrent wakeups per session. During daemon shutdown,
stop accepting new finalizer work, cancel/join the runner, and only then close
the store. Do not leave goroutines writing after storage teardown.

Cleanup-facts CDC continues to invalidate the read model, but must not enqueue
the finalizer merely because the finalizer wrote attempt or disposition facts;
that would create a self-wake loop. Due retries are owned by boot and the timer.

### 5. Deterministic containment rediscovery

When the persisted runtime handle is present, call the strong PR A release
path even if tmux `IsAlive` is false. tmux disappearance is not release proof.

For partial-spawn records with no persisted runtime handle, use only an
accepted deterministic reconstruction from the session identity. If ownership
cannot be authoritatively reconstructed, fail closed and keep cleanup pending;
do not search globally or delete the workspace.

### 6. Preserve mixed workspace outcomes

For workspace projects, collect per-repository teardown facts before deriving
the session-level disposition. A dirty repository must be preserved, but it
must not mask an independent teardown error in another repository. The
session-level result remains pending or failed while any non-dirty teardown
error still requires work.

Integrate the current `ShellTerminalCloser.BeginSessionTeardown` boundary before
workspace removal. Older finalizer proposals that predate this gate are not a
drop-in implementation.

## Expected file surface

Exact packages may be adjusted to the accepted #2931 direction, but the
responsibilities should remain separated.

| Area | Expected files | Responsibility |
| --- | --- | --- |
| Finalization | new focused file(s) in `backend/internal/session_manager/` | generation-fenced idempotent teardown |
| Session locking | session manager or existing lifecycle lock owner | serialize cleanup, restore, replace, and kill |
| Lifecycle facts | `backend/internal/lifecycle/manager.go` and tests | advance generation and record terminal intent |
| Wake/reconcile | daemon/session-manager wiring; dedicated `internal/observe/reconciler` only if maintainers retain the #2931 topology | boot, session lifecycle CDC, explicit retry, timer, shutdown join |
| Persistence | existing cleanup store/queries; migration only if proven necessary | durable facts and due-candidate reads |
| Read model | existing service/controller mapping only if needed | derive, never duplicate, cleanup state |
| Tests | manager, lifecycle, store, reconciler, daemon/integration | crash boundaries, retries, generation races |

No frontend work belongs in PR B unless maintainers separately accept a UI
contract for pending cleanup. A CLI/doctor or Dashboard exposure should be a
later focused slice after the durable facts are proven.

## Task graph

### B0. Refresh ownership and accepted design

- Read the merged PR A contract and current upstream `main`.
- Resolve #2931 ownership and extract only maintainer-accepted semantics.
- Update this plan if package boundaries or persistence contracts have changed.
- Start no code until the contribution gate is satisfied.

### B1. Generation and store integration

- Add cleanup-fact methods to the owning session-manager store interface.
- Advance cleanup generation exactly once for each accepted new launch.
- Seed or update cleanup facts for terminal intent without marking runtime
  released.
- Add tests for fresh launch, restore, stale generation, and idempotent repeat.

### B2. Idempotent finalizer

- Implement runtime-first, workspace-second ordering.
- Record `RuntimeReleasedAt` only after PR A returns proven release.
- Integrate scoped shell-terminal closure before workspace removal.
- Preserve dirty workspaces and mixed multi-repository failures accurately.
- Re-read generation under the per-session lock before every destructive pass.

### B3. Durable retry runner

- Implement due-candidate selection, per-session deduplication, bounded
  backoff, retry cap, and explicit manual-retry reset.
- Wake from boot, relevant session lifecycle CDC, explicit retry, and the
  periodic deadline without self-waking from cleanup-fact writes.
- Join the runner before storage closes.
- Make clock, timer, and retry policy injectable for deterministic tests.

### B4. Lifecycle convergence

- Route Kill, Cleanup, retirement/replacement, save-and-teardown, restore, and
  boot reconciliation through the same finalization contract.
- Remove or replace best-effort `Destroy` calls that can otherwise delete
  workspaces or durable records after incomplete runtime release.
- Prevent restore or replacement while the preceding generation remains
  populated or cleanup-pending.

### B5. Crash and race acceptance

Cover at least these boundaries:

- daemon stops after terminal intent but before runtime release;
- daemon stops after runtime release but before workspace release;
- scope remains populated through one or more retries and later becomes empty;
- stale old-generation finalizer races a restored generation;
- cleanup scan races an explicit restore or replacement;
- workspace project contains both dirty state and an independent teardown
  error; and
- shutdown begins while a finalizer attempt is running.

## CI/CD acceptance

Run focused tests first:

```bash
cd backend
go test ./internal/domain ./internal/storage/sqlite/store
go test ./internal/session_manager ./internal/lifecycle ./internal/daemon
go test ./internal/integration
```

If the maintainer-approved implementation adds the dedicated reconciler
package proposed by #2931, also run:

```bash
cd backend
go test ./internal/observe/reconciler
```

If schema or queries change, run from the repository root:

```bash
npm run sqlc
git diff --check -- backend/internal/storage/sqlite
git diff -- backend/internal/storage/sqlite/queries \
  backend/internal/storage/sqlite/migrations \
  backend/internal/storage/sqlite/gen
```

Inspect and commit the intended query, migration, and generated changes
together. After the commit, run `npm run sqlc` again and require
`git diff --exit-code` to remain clean. Never edit generated files by hand.

Then mirror the complete backend CI gate:

```bash
cd backend
test -z "$(gofmt -l .)"
go build ./...
go vet ./...
go test -race ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --path-mode=abs
cd ..
npm ci
npm run api
git diff --exit-code -- frontend/src/api/schema.ts
```

The upstream workflows additionally run native CLI E2E on Ubuntu, macOS, and
Windows, Docker fresh-install, API drift, and gitleaks. If PR B adds a public
DTO or generated frontend type despite the default non-goal, regenerate the API
artifacts and run the frontend typecheck/test workflow required by those paths.

## Representative restart canary

Use isolated AO data, ports, tmux server identity, and user-service units. The
canary must:

1. start a worker whose scope remains populated;
2. request termination and observe pending cleanup facts;
3. stop the daemon before release completes;
4. restart the daemon and observe boot rediscovery;
5. allow the scope to empty;
6. observe retry success and `RuntimeReleasedAt` written afterward; and
7. verify workspace removal or dirty preservation according to its actual
   state.

Read back the database facts, daemon logs, scope emptiness, tmux state, and
workspace state. A passing unit suite without this restart evidence is not
enough to claim durable closure.

## Compatibility and rollback

- Reuse existing cleanup facts and CDC instead of introducing a parallel state
  store.
- Add a new migration only for a proven missing field; never modify shipped
  migrations.
- Keep legacy rows with no cleanup facts readable and migrate behavior through
  idempotent initialization, not destructive backfill assumptions.
- On rollback, pending facts remain conservative data; an older binary may
  ignore them but must not be paired with a schema-incompatible database
  without the repository's normal migration/rollback review.

## Delivery

- PR B is independent from PR A after PR A merges unless maintainers explicitly
  request a stack.
- Use one focused issue accepted by maintainers and explain coordination with
  #2931 in the PR body.
- Use conventional commits and the repository PR template.
- Call out intentional omissions: UI, doctor output, resource ceilings,
  durable container-cleanup facts, and non-systemd containment backends. The
  existing best-effort Docker reaper remains compatible and separately owned.
- Maintainers control review and merge; do not request automatic merge on the
  upstream repository.

PR B is complete only when cleanup survives daemon restart, stale generations
cannot act, scope-populated state remains pending and retryable, and
`RuntimeReleasedAt` is written only after authoritative empty proof.
