# Linux Worker Process Containment

**Date:** 2026-08-04

**Status:** Proposed — implementation awaits maintainer direction on #2523

**Delivery slice:** PR A

**Tracks:** [Untrivial-ai/agent-orchestrator#2523](https://github.com/Untrivial-ai/agent-orchestrator/issues/2523)
**Evidence baseline:** upstream `main` at `5f3e6bcd5a47bb7312f80cfc3966464a8f948cda`

This document is an implementation plan, not current architecture. The
containment backend described below is not implemented on the evidence
baseline.

The refreshed baseline adds best-effort Docker container reaping on terminal
state. It does not add systemd/cgroup process containment, scope-empty proof,
or a durable cleanup finalizer, so the core PR A topology remains unchanged.

## Goal

When the operator explicitly enables the Linux systemd containment backend,
make worker teardown own every process launched for a tmux runtime, including
descendants that call `setsid`, and make synchronous
`Create`/`Restart`/`Destroy` paths fail closed whenever AO cannot prove that
the worker's operating-system containment boundary is empty. The unconfigured
compatibility backend retains its current weaker SID semantics in this slice.

## Contribution gate

`CONTRIBUTING.md` requires non-trivial work to be discussed and accepted before
implementation. A design question has been posted on #2523, but no maintainer
thumbs-up or assignment has been recorded yet.

Do not begin implementation or open the upstream code pull request until a
maintainer confirms the proposed scope and ownership on #2523 or Discord. When
that happens, create the implementation branch from a freshly fetched upstream
`main`; do not use this fork's documentation branch as a code baseline.

## Problem and current evidence

On the evidence baseline, `tmux.Runtime.Destroy`:

1. records each tmux pane PID;
2. destroys the tmux session;
3. runs `pkill -s <pane-pid>` and polls with `pgrep -s <pane-pid>`; and
4. returns no structured release result from the SID reaper.

tmux initially launches a pane with `pane PID == SID`, but a descendant that
calls `setsid` receives a new SID. It is no longer selected by the original
`pkill -s` call and can survive after the tmux session disappears. Process-tree
walks and pidfds can safely address discovered PIDs, but they do not provide a
stable ownership boundary after reparenting or an already-completed `setsid`.

The current unit tests cover the SID reaper's TERM/wait/KILL sequence. The
current tmux integration tests cover ordinary create, destroy, terminal input,
and restart, but do not create a `setsid` descendant. GitHub-hosted CI also does
not guarantee a usable user systemd manager, so repository CI alone cannot
prove this fix against a real scope.

Upstream's new Docker reaper is an independent, label-based cleanup leg. It is
invoked by `MarkTerminated` after the session manager's synchronous runtime and
workspace path, logs failures, and deliberately does not make termination
fail. It cannot prove that host processes exited and must not satisfy or mask
the process-containment postcondition below.

## Scoped-backend outcome contract

The following guarantees apply when `AO_PROCESS_CONTAINMENT=systemd` is
selected. They do not describe the unconfigured SID compatibility backend.

- A successful scoped `Create` proves that the pane command belongs to the
  deterministic scope assigned to the runtime handle.
- `Destroy` returns `nil` only after the tmux session is absent and the assigned
  scope is proven empty or fully unloaded.
- A release that is populated, unreadable, or otherwise unprovable returns a
  typed retryable error. It must not be converted into success because tmux is
  absent.
- `Restart` never overlaps old and new generations. It releases and verifies
  the old scope before starting the replacement command.
- A caller must not remove a workspace, delete the last durable ownership
  record, or start a replacement generation after a release-pending error.
- If `Create` has produced a tmux session or scope and its rollback cannot prove
  release, it returns the deterministic non-empty runtime handle together with
  the release-pending error. The caller durably records that handle before
  returning and preserves the workspace; it must not treat `handle + error` as
  equivalent to “nothing was created.”
- Explicitly requested systemd containment fails closed when its binaries,
  user manager, cgroup hierarchy, or required probes are unavailable.
- The existing non-systemd behavior remains compatible unless the operator
  explicitly enables the systemd backend.

## Architectural decisions

### 1. Opt-in Linux backend

Add an explicit daemon configuration value with an initial environment surface:

```text
AO_PROCESS_CONTAINMENT=systemd
```

Unset means the current platform behavior. `systemd` is accepted only on Linux
and requires a usable user manager. Unknown values and an explicitly requested
but unavailable backend are startup/configuration errors; they do not silently
fall back to SID matching.

Do not make systemd the universal default in this slice. Default selection,
Desktop settings, and broader rollout require representative platform evidence
after the opt-in implementation has landed.

### 2. Private tmux containment module

Keep the implementation inside the tmux adapter behind a small private
interface used by `Create`, `Restart`, and `Destroy`. A representative shape is:

```go
type processContainment interface {
	Prepare(ctx context.Context, handle string) error
	WrapLaunch(handle, shell, launch string) (string, error)
	Verify(ctx context.Context, handle string, panePID int) error
	Release(ctx context.Context, handle string, grace time.Duration) error
}
```

The exact method names may follow nearby code, but the module must own scope
naming, launch wrapping, membership verification, TERM/KILL delivery, and empty
proof. Callers must not duplicate systemctl or cgroup parsing.

Use a deterministic scope name derived from the already-sanitized tmux runtime
handle, for example `ao-session-<handle>.scope`. Determinism allows `Destroy`
to find the scope after tmux has disappeared without changing the persisted
runtime handle or adding a database migration.

### 3. Put the pane command inside the scope

Wrap the command executed inside the pane, not the `tmux new-session` client.
The persistent tmux server creates pane processes from its own cgroup, so
wrapping only the client does not contain the worker.

The scoped command is conceptually:

```bash
exec systemd-run --user --scope --collect \
  --unit=ao-session-<handle>.scope \
  -- <shell> -c '<existing launch command>'
```

Preserve the existing launch command's environment, working-directory guard,
agent supervisor, and keep-alive interactive shell. Before `Create` returns,
verify both tmux liveness and pane membership in the expected scope.

In scoped mode, enable and verify tmux's window-level `remain-on-exit` option.
Without it, releasing the old scope during `Restart` can remove the last pane
and therefore the tmux session before `respawn-pane` has a target.

### 4. Release by scope, then prove emptiness

`Destroy` must attempt containment cleanup even when tmux reports that the
session is already absent:

```text
tmux kill-session
→ scope TERM
→ bounded poll through the grace interval
→ scope KILL when still populated
→ final empty/unloaded proof
```

For cgroup v2, read `cgroup.events` and require `populated 0`. For cgroup v1,
resolve the systemd hierarchy and recursively require empty `tasks` or
`cgroup.procs` files. Missing or malformed evidence is unknown, not empty.

The scope release implementation must use exact unit names and must not search
or signal processes globally by command name, environment value, repository
path, or user ID.

Expose a stable sentinel such as `ports.ErrRuntimeReleasePending` for callers
that must preserve state. Keep detailed unit, handle, and probe causes in the
wrapped error for diagnostics.

### 5. Restart through a retained dead pane

The scoped restart sequence is:

1. verify `remain-on-exit` is enabled;
2. release the stable old scope and prove it empty;
3. wait for `--collect` to unload the old unit;
4. run the existing `respawn-pane -k` path with the newly wrapped command;
5. verify the new scope membership, tmux liveness, and terminal usability.

If step 2 or 3 fails, leave the dead pane available for diagnosis and retry.
Do not respawn. A generation-specific unit is not required in this slice and
would require additional persistent identity and rediscovery rules.

### 6. Preserve state at runtime callers

Audit every synchronous `Runtime.Destroy` call. Apply one rule consistently:

```text
Destroy == nil   → runtime release is proven; workspace cleanup may continue
Destroy != nil   → preserve the workspace and ownership facts; do not replace
```

At minimum, cover:

- spawn and relaunch rollback paths that currently discard cleanup errors;
- shutdown save-and-teardown paths before `ForceDestroy`;
- project `Cleanup`, which currently treats runtime teardown as best effort;
- boot reconciliation when tmux is gone but a deterministic scope may remain;
- tmux `Create` failure rollback; and
- shell-terminal teardown, where `IsAlive=false` for tmux must not override a
  release-pending containment error and delete the terminal record.

`Runtime.Create` already returns `(RuntimeHandle, error)`. Preserve that
signature, but define its partial-create contract: after a created runtime
cannot be proven released, return its deterministic handle with
`ErrRuntimeReleasePending`. The seeded session's existing durable metadata must
record the handle before the session manager returns the spawn failure. If
rollback proves both tmux and scope released, returning a zero handle with the
original create error remains valid. No caller may delete the workspace while
a non-empty failed-create handle remains unreleased.

Do not broaden `Runtime.IsAlive` to mean both tmux liveness and containment
release. These are different facts. `Destroy` owns the strong release
postcondition in this slice.

### 7. Keep container and process release facts separate

Do not fold the label-based Docker reaper into the systemd scope abstraction.
PR A owns the synchronous host-process invariant required before workspace
removal. The existing container reaper remains best effort and may run when
terminal intent is recorded, but its success is not evidence that the scope is
empty and its failure must not weaken `ErrRuntimeReleasePending`.

Add an ordering test for the explicit Kill path: a release-pending process
scope preserves the workspace and prevents terminal-state side effects from
being treated as completed runtime release. A later lifecycle redesign may
durably sequence both cleanup legs, but that belongs with PR B and requires
maintainer direction.

## Expected file surface

The final implementation should stay close to this boundary; exact test file
splits may follow repository conventions.

| Area | Expected files | Responsibility |
| --- | --- | --- |
| Configuration | `backend/internal/config/config.go`, tests | parse and validate containment selection |
| Runtime selection | `backend/internal/adapters/runtime/runtimeselect/`, daemon wiring | pass resolved selection to tmux |
| Containment | new files under `backend/internal/adapters/runtime/tmux/` | systemd scope and unsupported-platform behavior |
| tmux lifecycle | `tmux.go`, `commands.go` | wire prepare/wrap/verify/release and `remain-on-exit` |
| Error contract | `backend/internal/ports/outbound.go` | release-pending sentinel without changing method signatures |
| Fail-closed callers | session manager, lifecycle persistence, and shell-terminal service | preserve runtime handle, workspace, and durable records on incomplete release |
| Container interaction | lifecycle manager tests | preserve the existing best-effort reaper while proving it cannot satisfy process release |
| Tests | adjacent package tests and tmux integration test | state machine, parsers, errors, real process behavior |

No frontend, HTTP DTO, OpenAPI, generated TypeScript, SQLite migration, or
derived display-status change belongs in PR A.

## Task graph

### A1. Configuration and containment contract

- Add and validate the opt-in configuration.
- Define deterministic unit naming and the release-pending error contract.
- Add injected command/filesystem seams so unit tests require neither a real
  daemon nor a real systemd manager.
- Verify invalid values and explicit-unavailable systemd fail before tmux
  creation.

### A2. Linux systemd implementation

- Implement launch wrapping and membership verification.
- Implement exact-unit TERM, bounded polling, KILL, and final proof.
- Support cgroup v2 `cgroup.events` and the cgroup v1 systemd hierarchy.
- Add fixture tests for populated, empty, nested, malformed, unreadable, and
  unloaded states.

### A3. Create, Restart, and Destroy integration

- Wire the actual pane command through containment.
- Enable `remain-on-exit` before reporting scoped Create ready.
- Make Restart release-first and refuse overlap.
- Make Destroy idempotent only when both tmux and containment are released.
- Keep the current SID reaper as the unconfigured compatibility path.

### A4. Fail-closed caller audit

- Stop workspace deletion and generation replacement on Destroy errors.
- Preserve shell-terminal rows on release-pending errors.
- Join or surface rollback cleanup errors instead of silently discarding them.
- Persist a non-empty handle returned with a partial-Create cleanup error before
  returning the spawn failure.
- Add focused caller tests proving that an unconfirmed runtime keeps its
  workspace and record, including daemon-restart readback of the failed-Create
  handle.

### A5. Representative Linux canary

Add an explicitly enabled integration test that starts inside the scope:

- one ordinary background process;
- one descendant that demonstrably changes SID with `setsid`; and
- one descendant that ignores TERM.

Start an unrelated process outside the scope as a negative control. After
Destroy, require all owned PIDs gone, the negative control alive, tmux gone,
the scope empty or unloaded, the same handle reusable, and terminal input
working after Create and Restart.

When the explicit canary flag is set, missing tmux, user systemd, or cgroup
permissions is a failure, not a skip. Default hermetic unit tests may skip the
real-host canary.

## CI/CD acceptance

Run focused feedback first:

```bash
cd backend
go test ./internal/adapters/runtime/tmux
go test ./internal/session_manager ./internal/service/shellterm ./internal/daemon
```

Then mirror the backend CI gate from the repository root:

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

The upstream PR also exercises the native CLI E2E matrix on Ubuntu, macOS, and
Windows, the Docker fresh-install check, API drift, and gitleaks through the
existing path-filtered workflows. Those checks do not substitute for the real
systemd canary because their environments do not guarantee tmux plus a running
user manager.

Record the canary environment, exact command, PID/SID/cgroup evidence, and
final empty proof in the PR's Testing section.

## Compatibility and rollback

- Unset configuration preserves the current runtime backend.
- Darwin remains on the current fast unsupported-matcher fallback.
- Windows ConPTY is untouched.
- Removing the configuration value and reverting the contained runtime commits
  restores prior behavior without a data migration.
- Do not advertise systemd containment as current architecture until the code,
  fake tests, CI, and real canary have all passed.

## Delivery

- Use one focused upstream PR from current upstream `main` with a conventional
  `fix:` commit.
- Use `Refs #2523`, not `Fixes #2523`; durable daemon-restart cleanup remains PR
  B and #2523 also discusses resource ceilings outside this slice.
- Fill the repository PR template's What, Why, How, Testing, and intentional
  omissions sections.
- Maintainers control review and merge. Do not request automatic merge on the
  upstream repository.

PR A is complete only when the synchronous systemd-scoped, known-handle
lifecycle satisfies the outcome contract and the real Linux canary proves that
a changed-SID descendant is reaped without killing the negative control.
