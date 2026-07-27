# Symphony compatibility gap design

Status: accepted for phased implementation; phases 0 through 2 are merged in
pull requests 2 through 4. The private canary credential boundary is delivered
as phase 2.5 before live acceptance testing.

## Baseline and intent

This design describes how to add a bounded OpenAI Symphony compatibility lane
to Agent Orchestrator (AO) without replacing AO's existing scheduler, session
runtime, worktree management, lifecycle observers, or user interfaces.

The reviewed baselines are:

- Agent Orchestrator `e3596af8029c8a99e3ee5b144e7222c246a5ca77`
- OpenAI Symphony repository
  `f8e8b8a670c799f6e0ade7a8c25c4bf4a4a56ec7`
  (`SPEC.md` blob `a6b44e162383e7241a76bce85afb7a8e8d704c45`)

This AO baseline includes the unified session lifecycle and presentation model,
workspace recovery metadata, migrations 0032 through 0036, and the repair for
applying missing out-of-order migrations. It also includes terminal-activity
reconciliation for stale Codex sessions and conservative preservation of failed
scratch workspaces for retry. The claim and attempt storage design must extend
those facts and migration rules rather than introduce a parallel session-state
model.

The Symphony specification content is unchanged from the previously reviewed
baseline; the newer repository head adds release tooling rather than contract
changes.

The first target is a declared `symphony-subset-v1`, not an unqualified
conformance claim. Unsupported fields or behaviors must fail validation
explicitly.

## Existing AO capabilities to retain

AO already supplies most of the execution and feedback plane:

- an opt-in GitHub issue intake observer with pagination, ETag caching,
  assignee filtering, and live-session deduplication;
- tracker-domain normalization and a tracker port;
- the session manager, isolated worktrees, agent adapters, and daemon lifecycle;
- durable SQLite facts and CDC-backed updates;
- PR, CI, and review observers that can nudge the owning session;
- CLI and Electron surfaces derived from the same daemon state.

The compatibility work must extend these components rather than introduce a
parallel scheduler.

## Gaps against the Symphony specification

| Gap | Current AO behavior | Required compatibility behavior |
| --- | --- | --- |
| Workflow contract | Tracker intake configuration and prompt construction are compiled into AO. | Load a repository-owned `WORKFLOW.md` with typed front matter, prompt template, validation, and safe reload. |
| Prompt rendering | GitHub issue prompts are hard-coded and capped at 4096 bytes. | Render the workflow prompt from normalized issue data with deterministic size and escaping rules. |
| Tracker providers | Runtime intake supports GitHub. | Keep GitHub and add Linear through the same tracker contract. |
| Concurrency | Existing session controls are project/session oriented. | Enforce bounded global, project, and optional tracker-state concurrency before claims are dispatched. |
| Claim authority | Live sessions are used for deduplication. | Persist one authoritative issue claim and attempt history so restart cannot duplicate work. |
| Retry and continuation | A terminated session may allow an active issue to spawn again. | Retry with bounded exponential backoff while the issue remains active; preserve attempt linkage and terminal reason. |
| Reconciliation | Polling discovers eligible issues. | Reconcile active claims with tracker state, stop work for terminal/ineligible issues, and recover orphaned claims. |
| Workspace hooks | AO owns worktree setup and cleanup. | Map declared hooks onto AO lifecycle points with timeouts, captured output, and explicit failure semantics. |
| Observability | Session and PR facts are visible. | Link issue, claim, attempt, retry deadline, and workflow revision into existing APIs and derived status. |
| Security | AO has an established local-daemon trust boundary. | Add least-privilege tracker credentials, permission validation, redacted logs, and explicit sandbox policy without exposing the daemon. |

## Proposed architecture

### 1. Workflow package

Add a backend package responsible only for:

- discovering the configured `WORKFLOW.md`;
- parsing and validating typed front matter;
- compiling the prompt template;
- producing a content-addressed workflow revision;
- retaining the last valid revision when a reload is invalid.

The package must not spawn sessions or call trackers. The first schema should
cover tracker selection, eligibility, prompt, concurrency, polling, retry,
workspace hooks, and agent execution settings. Unknown fields should be
reported; compatibility-critical unsupported fields should reject activation.

Existing per-project tracker intake remains the default. A project enters this
lane only when it explicitly selects the Symphony workflow mode and path.

### 2. Tracker adapters

Retain the tracker port and GitHub adapter. Extend normalized issue data only
where the workflow contract requires it, and add a Linear adapter behind the
same port.

Tracker reads determine eligibility and reconciliation. Tracker writes remain
agent-owned through the normal tool/runtime permissions unless a later,
separately reviewed capability explicitly authorizes daemon-side mutation.

### 3. Intake coordinator and durable claims

Evolve `observe/trackerintake` into a coordinator that consumes a validated
workflow and delegates execution to the existing session service.

Persist durable facts for:

- canonical issue identity and last observed tracker state;
- one current claim per issue and project;
- attempts and their owning AO session;
- retry count, next eligible time, and terminal reason;
- the workflow revision used for each attempt.

Use database constraints for single-claim authority and storage triggers for
CDC. Display labels remain derived read-model state. Do not persist a second
mutable UI status.

On startup and every poll, the coordinator:

1. refreshes eligible tracker items;
2. reconciles existing claims before dispatching new work;
3. releases or stops claims that became terminal or ineligible;
4. recovers orphaned attempts;
5. dispatches within all concurrency limits;
6. schedules eligible retries with bounded exponential backoff and jitter.

The session manager remains the only execution engine. The coordinator stores
the issue-to-session relationship and uses existing lifecycle signals for
continuation, PR, CI, and review feedback.

### 4. Hooks and execution settings

Map workflow hooks to explicit AO lifecycle boundaries:

- before workspace preparation;
- after workspace preparation;
- before an agent attempt;
- after an agent attempt;
- before cleanup.

Run hooks in the workspace sandbox with a configured timeout, output cap,
redaction, and cancellation propagation. A hook failure must have a typed
effect: reject claim, fail attempt, retry attempt, or warn and continue.

AO agent adapters remain an implementation-defined execution layer for the
first subset. Direct Codex app-server parity is not required for phase one;
the compatibility declaration must document this difference.

### 5. API and UI

Expose issue, claim, attempt, workflow revision, retry deadline, and terminal
reason through the daemon API. Feed updates through the existing CDC path.

The CLI and Electron frontend should consume those facts and derive concise
states such as queued, running, waiting for retry, waiting for review, and
terminal. Neither client becomes scheduling authority.

## Delivery phases

### Phase 0: executable contract

- Add versioned workflow types, loader, validator, and prompt fixtures.
- Check in representative Symphony examples as conformance fixtures.
- Publish the supported-field matrix for `symphony-subset-v1`.
- Add reload and invalid-configuration tests.

No runtime behavior changes in this phase.

### Phase 1: workflow-driven GitHub intake

- Drive the existing GitHub observer from the validated workflow.
- Replace the hard-coded prompt builder with workflow rendering.
- Enforce global and per-project concurrency.
- Preserve existing intake configuration as a compatibility path.

### Phase 2: durable orchestration

- Add claim, attempt, and retry facts plus CDC triggers.
- Reconcile restart, tracker transition, cancellation, and orphaned sessions.
- Continue active issues without duplicate sessions or unbounded spawn churn.
- Surface orchestration state through API, CLI, and Electron.

### Phase 2.5: least-privilege GitHub canary identity

- Authenticate tracker intake and SCM observation with a GitHub App
  installation token that refreshes automatically.
- Restrict the canary installation to its private repository and the minimum
  repository permissions needed by the test.
- Reject partial configuration and unsafe private-key paths without falling
  back to a broader host credential.
- Keep static token and `gh` credential paths only as explicit compatibility
  fallbacks outside the App-configured boundary.

### Phase 3: Linear

- Add the Linear tracker adapter and credential/config validation.
- Run the same contract tests against mock GitHub and mock Linear servers.
- Canary one private Linear project with read-only daemon intake and
  agent-owned tracker writes.

### Phase 4: hardening and upstream split

- Run kill/restart, rate-limit, network-loss, and malformed-workflow canaries.
- Separate generally useful changes into reviewable upstream PRs:
  workflow loader/schema, generic coordinator, durable claims, then Linear.
- Keep personal deployment policy and container restrictions fork-only.

## Canary acceptance

A canary is successful only when all of the following are demonstrated:

1. Four eligible issues run with a configured maximum concurrency of four.
2. Killing and restarting the daemon produces no duplicate active claim.
3. Moving an issue to an ineligible or terminal state stops or cleans up work
   according to policy.
4. A normal worker exit while an issue remains active schedules a bounded
   retry or continuation linked to the same claim.
5. PR, CI, and review events reach the owning session without manual prompting.
6. Invalid workflow reloads leave the last valid revision active and visible.
7. Tracker rate limits and transient failures back off without losing claims.
8. The daemon and workers run in a resource-limited container with no host
   Docker socket, no host SSH agent, least-privilege credentials, and capped
   logs.

Required repository validation includes backend unit and race tests, frontend
lint/tests for affected views, generated-client verification when API schemas
change, migration tests, and an end-to-end mock-tracker smoke.

## Non-goals

- Rewriting AO around the Symphony reference implementation.
- Replacing AO's CLI, Electron frontend, session manager, or agent adapters.
- Claiming full Symphony conformance in the first delivery.
- Implementing remote-SSH execution or arbitrary daemon-side tracker writes.
- Automatically merging or deploying pull requests.

## Decisions

- Use AO's SQLite store for durable claim authority even though the Symphony
  specification does not require a database; restart safety is a stronger AO
  invariant.
- Keep tracker state as an external fact and AO display state as derived data.
- Use explicit opt-in so current AO projects retain their existing behavior.
- Treat the Symphony specification as the workflow and orchestration contract,
  not as a mandate to copy its implementation.

## First implementation slice

The first implementation PR should contain only phase 0:

- workflow schema and loader;
- prompt compilation/rendering;
- compatibility matrix and fixtures;
- focused unit tests.

It must not add a second polling loop, database migration, Linear client, or UI
surface. That boundary makes the contract reviewable before runtime behavior
is changed.
