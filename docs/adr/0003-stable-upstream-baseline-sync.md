# ADR 0003: Synchronize the fork from pinned stable upstream releases

Date: 2026-08-03

Status: accepted

## Context

ADR 0002 removed the fork-local workflow engine and established upstream AO as
the owner of the execution lifecycle. The fork now needs a repeatable way to
take upstream releases without rebuilding a second product or losing the few
downstream compatibility boundaries that remain necessary.

The last common baseline is upstream `v0.11.1` at `2f6d98f2`. Fork `main` is
`2a3eb0e8`; it contains the upstream-first convergence plus the retained
adapters. Upstream `v0.11.2` is the next stable release at `c5523a6d`. Relative
to that release, the two lines contain 31 upstream-side and 40 fork-side
commits. The release adds `0038_orchestrator_reengagement.sql`, whose version
collides with the fork's immutable shipped migration `0038`. Upstream `main`
continues with further colliding migration numbers.

The fork has shipped these migration numbers:

- `0037_workflow_issue_runs.sql`;
- `0038_drop_worker_idle_outbox.sql`, equivalent to upstream v0.11.2's
  `0037_drop_worker_idle_outbox.sql`; and
- `0039_drop_workflow_issue_runs.sql`.

Renaming or editing them would break databases that have already recorded the
versions. Copying upstream migrations by filename would create duplicate
versions.

Upstream also has active Linear intake work, including
[#3319](https://github.com/Untrivial-ai/agent-orchestrator/pull/3319) and
[#2651](https://github.com/Untrivial-ai/agent-orchestrator/pull/2651). Neither is
a released contract yet, so the fork cannot depend on it for this baseline.

## Decision

Synchronize production-bound fork baselines from immutable stable upstream
tags. For the current tranche, pin upstream `v0.11.2` at
`c5523a6d0e51251b79555b95ddc7d2be59da0f50`. Treat upstream `main` and nightly
builds as observation inputs, not merge targets.

Integrate the tag with a normal merge on a branch created from fork `main`.
Preserve both histories: do not rebase fork `main`, force-push, or reconstruct
the fork by cherry-picking its complete history onto upstream. Resolve the
merge by the authorities below:

| Surface | Authority |
| --- | --- |
| AO lifecycle, Dashboard, worker/worktree/session behavior | upstream release |
| Fork database migration numbers already shipped | fork history |
| GitHub App installation-token authentication | retained downstream boundary |
| Worker filtering of Linear tracker credentials | retained downstream boundary |
| Read-only, project-scoped Linear intake | temporary downstream boundary |
| Actionable bot review continuation | retained downstream boundary |
| Provider-update refresh for commented review threads | retained downstream boundary |

Treat every other fork-side runtime delta as a deletion candidate. In
particular, do not restore the removed Symphony workflow scheduler. Remove the
fork's old long-prompt submission workaround where upstream message delivery
now supplies the same contract. Retain the tmux test-isolation change only if a
focused v0.11.2 reproduction still demonstrates the original failure.

Keep the shipped fork migrations byte-for-byte unchanged. Continue to omit
upstream's `0037_drop_worker_idle_outbox.sql` from the resulting fork tree and
record it as semantically represented by fork migration `0038`. For v0.11.2,
rename upstream `0038_orchestrator_reengagement.sql` to fork
`0040_orchestrator_reengagement.sql` without changing its contents. Preserve a
source-file and content-hash ledger for both mappings. Before taking a later
upstream baseline, map every further collision to the next unused fork number;
based on the current upstream sequence, upstream `0039/0040` would become fork
`0041/0042`, subject to verification against that future pinned release.

Keep source convergence separate from host deployment. A merged and validated
source baseline does not authorize replacement of the installed compatibility
binary, migration of the live database, or service restart.

Keep worker process containment separate from this synchronization. The
`setsid` cleanup gap and proposed systemd/cgroup boundary remain an upstream
design and contribution track; they are not a reason to fork more lifecycle
code into the baseline merge.

## Consequences

- Fork history remains auditable and upstream ancestry remains visible.
- A stable release supplies a bounded review and canary surface.
- Downstream code is justified by an explicit compatibility boundary and has a
  deletion condition.
- Linear intake remains supported now, but must be compared with and retired in
  favor of an upstream release once an equivalent contract ships.
- Migration compatibility is a first-class gate rather than an incidental
  merge-conflict resolution.
- Production deployment remains a separately authorized, backed-up, reversible
  host operation.

## Reopening conditions

Reconsider the stable-release-only policy when a production-blocking fix exists
only on upstream `main` or a nightly build and a bounded canary demonstrates
that waiting for the next release is riskier than taking the pinned commit.

Reconsider a retained downstream patch when an upstream stable release ships
equivalent behavior and the fork contract tests pass with the patch removed.
