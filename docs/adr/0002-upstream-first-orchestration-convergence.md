# ADR 0002: Prefer upstream AO lifecycle over a fork-local workflow engine

Date: 2026-07-29

Status: accepted

## Context

The fork added a Symphony-compatible `WORKFLOW.md` loader, a second durable
claim/retry state machine, Linear intake, GitHub App authentication, bot review
continuation, and worker credential filtering while upstream AO continued to
evolve its own session and feedback lifecycle.

By the convergence review, fork `main` and upstream `main` had diverged by 35
and 40 commits. The fork-only delta was about 6,177 added lines. Several of
those lines duplicated lifecycle responsibilities that upstream now owns:

- upstream v0.11.0 is the release baseline at `a10c98c8`;
- upstream `9c1f6e5b` completes accepted tmux message delivery after caller
  cancellation, replacing the fork's long-paste submission workaround; and
- upstream `2f6d98f2` removes the worker-idle outbox/nudge state machine.

The duplicate workflow layer created competing state writers and repeated
claim, retry, continuation, and terminal reconciliation paths. Operational
canaries then spent more effort repairing coordination protocol than proving
software delivery.

## Alternatives considered

### Continue the Symphony subset

This preserves the existing `WORKFLOW.md` contract and durable workflow rows,
but keeps two orchestration engines inside AO. Every upstream lifecycle change
would require compatibility work across both paths.

### Move all state into Linear

This would make Linear responsible for runtime session, CI, review, and merge
facts that are natively owned by AO and GitHub. It replaces one synchronization
problem with another and requires tracker writes that the current integration
does not need.

### Converge on upstream AO and keep only unique adapters

This removes the fork-local scheduler while retaining the local capabilities
that upstream does not yet provide.

## Decision

Use upstream commit `2f6d98f2` as the pinned convergence baseline. It contains
v0.11.0 plus the post-release message-delivery and idle-nudge changes reviewed
above.

AO keeps one execution lifecycle:

| Fact | Authority |
| --- | --- |
| Issue intent, assignment, and product-facing progress | Linear or GitHub Issues |
| Worker session, worktree, activity, and local delivery reactions | AO |
| Pull request, CI, review, mergeability, and merge | GitHub |

The fork retains only boundaries that remain unique:

- GitHub App installation-token authentication;
- daemon-only Linear credential filtering for tmux and ConPTY workers;
- read-only, project-scoped Linear tracker intake;
- actionable bot review continuation; and
- provider-update refresh for commented inline review threads.

Linear intake reuses upstream's tracker-intake observer. A project explicitly
configures `provider=linear`, a Linear project UUID in `repo`, and an assignee.
The adapter lists open matching issues and AO creates at most one live session
for the canonical `linear:<issue UUID>` identifier. AO does not write Linear
state or maintain a parallel workflow-run table.

Remove the following fork-local runtime contracts:

- `WORKFLOW.md` discovery, parsing, templating, and reload;
- `workflow_issue_runs` claim, retry, continuation, and reconciliation code;
- workflow concurrency budgets and state normalization policy;
- `--tracker-workflow` / `trackerIntake.workflowPath`; and
- the fork-specific Symphony compatibility documents and fixtures.

Migration `0037_workflow_issue_runs.sql` remains byte-for-byte unchanged
because it has already shipped on fork `main`. Upstream's conflicting migration
number moves to `0038`, and `0039` removes the obsolete workflow table. This
keeps both existing fork databases and fresh databases on a monotonic migration
path.

## Consequences

- The main execution path becomes the maintained upstream AO lifecycle.
- Upstream upgrades should begin with a pinned release or commit and a
  fork-delta review, not a new compatibility layer.
- Linear remains an intake and human progress surface, not AO's runtime
  database.
- Terminated sessions may be recreated only while the tracker issue is still
  open and eligible. Done or cancelled issues are excluded by the adapter.
- The removed workflow prompt templates and retry policy are intentionally not
  compatibility-preserved. Repositories using those fork-only fields must move
  task instructions into issue content or repository agent rules.
- A deployment upgrade still requires database migration checks, daemon health,
  worker credential isolation, and a representative PR continuation canary.

## Reopening conditions

Reconsider a durable external workflow engine only if representative workload
evidence shows that upstream AO sessions and platform-native PR state cannot
reliably survive the required claim, resume, or retry volume. Any replacement
must have one authoritative state owner, a smaller interface than the removed
layer, and a canary proving improved delivery rather than only static
conformance.
