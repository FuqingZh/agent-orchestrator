# Plan: Tear Down Reviewer Runtimes With Worker Sessions

## Status

Implementation, local verification, and fork candidate publication are
complete. Upstream issue and pull request publication are the next phase and
remain out of scope for this task.

## Problem

The review engine creates one stable auxiliary runtime handle per worker,
`review-<worker-id>`, and intentionally reuses it across review passes. User
cancellation interrupts the reviewer while keeping its terminal available.
Worker termination, however, only destroys the worker runtime recorded in the
session row. It does not close the auxiliary reviewer runtime.

As a result, a worker can be terminated and removed from the active session
view while its reviewer terminal and processes remain alive. The runtime is no
longer useful, still references the worker worktree, and is not represented as
pending cleanup.

The defect is present on upstream `main` at `4d004fe6`: the review launcher can
destroy its internal runtime but does not expose permanent teardown, and the
session manager has no reviewer lifecycle dependency.

## Scope

This change will:

- permanently destroy the reviewer runtime when its worker enters a resource
  teardown path;
- serialize reviewer teardown with review triggering for the same worker;
- mark running review passes cancelled and clear only the live reviewer handle;
- retain historical review and review-run records;
- make teardown failure stop worktree release so cleanup remains retryable;
- cover explicit kill, daemon save/teardown, boot reconciliation of legacy
  residue, and cleanup of already-terminated sessions.

This change will not:

- automatically enable `terminate_on_pr_merge`;
- change user-requested review cancellation, which continues to preserve the
  terminal for inspection;
- add a process-containment backend or systemd scope;
- change HTTP, CLI, OpenAPI, frontend, or SQLite schemas;
- publish an upstream issue or pull request before fork verification succeeds.

## Design

### Reviewer teardown gate

Add a consumer-owned `ReviewerLifecycle` interface to `session_manager`. Its
teardown method returns a release function. The review engine implements the
interface by acquiring its existing per-worker trigger lock, destroying the
stable reviewer handle, cancelling any running review rows, clearing the live
handle from the review record, and returning the lock release function.

Holding the trigger lock across the worker teardown closes the race in which a
new review could start after the reviewer was destroyed but before the worker
was marked terminated. Once the manager releases the gate, a waiting trigger
re-reads the now-terminated worker and refuses to spawn.

### Session-manager ordering

For paths that release a worker runtime or worktree:

1. acquire and complete reviewer teardown;
2. keep the reviewer trigger gate held;
3. destroy the worker runtime and release or preserve its worktree according to
   the existing safety policy;
4. record terminal state where the path owns that transition;
5. release the reviewer trigger gate.

If reviewer teardown cannot be confirmed, the path returns or records a
retryable cleanup failure before removing the worktree. Existing dirty-worktree
preservation remains unchanged.

### Ownership and wiring

- `backend/internal/review` owns reviewer run state and permanent reviewer
  runtime teardown.
- `backend/internal/session_manager` owns teardown ordering and consumes only
  the narrow lifecycle interface.
- `backend/internal/daemon` constructs the review engine and injects it into
  the session manager.

No layer duplicates the `review-<worker-id>` naming convention outside the
review package.

## Verification

### Red reproduction

Add a focused regression test demonstrating that terminating a worker with a
live reviewer currently leaves the auxiliary runtime untouched. Record the
failure against the exact upstream baseline before adding the production call.

Recorded on 2026-08-04 against upstream baseline `4d004fe6`, after adding only
the consumer interface and test seam, with no production teardown call:

```text
GOCACHE=<task-temp>/go-build go test ./internal/session_manager \
  -run '^TestKill_TearsDownRuntimeAndWorkspace$' -count=1
--- FAIL: TestKill_TearsDownRuntimeAndWorkspace
reviewer teardown began=[] ended=[], want [mer-1] for both
```

This is the expected red state: `Kill` completes worker runtime and workspace
teardown without invoking reviewer cleanup.

### Focused green checks

- launcher permanent-destroy behavior is idempotent;
- review-engine teardown destroys the handle, cancels only running passes,
  clears the live handle, and retains history;
- a concurrent trigger cannot recreate the reviewer during worker teardown;
- `Kill` performs reviewer teardown before worker runtime and worktree release;
- reviewer teardown failure leaves the worker active and its worktree intact;
- boot reconciliation and cleanup remove reviewer residue for terminal legacy
  sessions;
- ordinary user cancellation still keeps the reviewer terminal alive.

### Repository gate

Run from the repository root:

```bash
cd backend && go test ./internal/review ./internal/session_manager ./internal/daemon
cd backend && go build ./...
cd backend && go test -race ./...
cd backend && go vet ./...
npm run lint
git diff --check
```

No API or storage generation command should be needed. If the implementation
changes an API or schema, stop and reassess the scope.

### Runtime canaries

1. Run an isolated Linux container canary using the candidate build and a
   disposable data directory.
2. Run a host canary against a disposable tmux/runtime namespace, never the
   active AO daemon or existing sessions.
3. In both canaries, create a worker plus reviewer runtime, terminate the
   worker, and verify the worker handle, reviewer handle, and reviewer process
   tree are gone while persisted review history remains.

### Verification result

The focused review, session-manager, and daemon tests pass. `go build ./...`,
`go vet ./...`, and golangci-lint 2.12.2 also pass; the linter reports zero
issues. A 104-package run excluding the tmux adapter passed every package
except `internal/service/session`. Its failing
`TestWorkspaceFilesIncludeWorkspaceProjectChildRepoDiffs` assertion reproduces
unchanged on the detached upstream baseline and is unrelated to reviewer
teardown.

The repository's tmux integration test remains unstable on this host. The
candidate and detached upstream baseline both fail
`TestRuntimeIntegrationSupervisedExitKeepsInteractiveShell`, with failures
before this change's review lifecycle is involved. A separate ordinary tmux
runtime integration smoke passes.

The race-enabled full suite could not be completed locally: the available Go
toolchain is newer than the repository-declared Go 1.25.7 toolchain and fails
to link the race runtime. Fetching an exact-toolchain container also timed out.
The upstream pull request must therefore retain its normal exact-toolchain CI
gate; this local limitation is not reported as a passing race check.

Disposable host and resource-limited container canaries both pass. Each
created a real tmux-backed reviewer running a long-lived child process, invoked
the review engine's teardown gate, and verified that the reviewer runtime and
child process disappeared, the live handle was cleared, the running pass was
cancelled, and completed history remained. The canary does not claim
containment of independently detached descendants; that is a separate runtime
hardening boundary.

The fork branch push was read back successfully and matched the local head.
An ordinary feature-branch push does not trigger this repository's pull-request
workflows, so GitHub reported no check runs for the candidate; no remote CI pass
is claimed. Exact-toolchain CI remains required on the eventual upstream pull
request.

## Delivery Boundary

Push the verified candidate branch to the fork and read back its exact commit
and CI state. After the fork result is confirmed, create an upstream issue with
the baseline reproduction and impact, then open the upstream pull request with
`Fixes #<issue>`.
