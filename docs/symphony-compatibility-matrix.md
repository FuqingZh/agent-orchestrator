# Symphony compatibility matrix

Status: `symphony-subset-v1`, Phase 1 intake

Spec baseline:
[`openai/symphony@f8e8b8a6`](https://github.com/openai/symphony/blob/f8e8b8a670c799f6e0ade7a8c25c4bf4a4a56ec7/SPEC.md)

This matrix declares only behavior implemented or deliberately bounded in
Agent Orchestrator. It is not a full Symphony conformance claim.

## Workflow and config

| Contract | Phase 0 status | Boundary |
| --- | --- | --- |
| Explicit workflow path, otherwise `WORKFLOW.md` in cwd | Supported | `workflow.ResolvePath` |
| Optional YAML front matter plus trimmed Markdown prompt | Supported | Typed errors distinguish missing file, malformed YAML, and non-map front matter. |
| Preserve unknown top-level and `tracker.provider` keys | Supported | Unknown keys remain in the raw definition and are ignored by the typed core view. |
| `tracker.kind` | Parsed, preflighted, and wired | The first runtime adapter is `github`; `linear` remains Phase 3. |
| `tracker.required_labels`, active states, terminal states | Runtime wired | Required labels and active states gate GitHub intake; durable reconciliation terminates and releases terminal or ineligible runs. |
| `polling.interval_ms` | Runtime wired | Default 30000; the fastest active workflow interval drives AO's shared intake loop, while legacy intake retains its daemon fallback. |
| `workspace.root` | Supported | Default temp root, relative-to-workflow resolution, `~`, and explicit `$VAR`; missing variables fail validation. |
| Four workspace hooks plus timeout | Parsed and validated | Hook execution begins after the workflow-driven intake slice. |
| Global, turn, retry, and per-state concurrency fields | Partially enforced | P1 bounds global/project dispatch and same-pass state dispatch. P2 persists claims and retries; max-turn execution remains open. |
| Codex command, policies, and timeouts | Parsed | Policy values remain pass-through. AO agent adapters remain the Phase 1 execution layer. |
| Config revision | Supported | SHA-256 of the exact workflow file bytes. |
| Dynamic reload | Runtime wired | GitHub intake reloads defensively before dispatch, retains the last valid config, and reapplies the effective shared polling interval. |

## Prompt templates

| Contract | Phase 0 status | Boundary |
| --- | --- | --- |
| Strict `issue.*` and `attempt` interpolation | Supported | All normalized Symphony issue fields are available. |
| Unknown variables | Supported | Fails with `template_render_error`. |
| Unknown filters | Supported | Filters are not part of this subset and fail closed. |
| Liquid tags | Unsupported | Tags fail with `template_parse_error`; no silent interpretation. |
| Empty prompt fallback | Supported | Uses the minimal Symphony fallback prompt. |

## Runtime phases

| Phase | State | Required proof |
| --- | --- | --- |
| P0 executable workflow contract | Implemented by this slice | Package tests, race test, vet, repository lint. |
| P1 workflow-driven GitHub intake | Implemented and smoke-tested | Tests cover rendered prompts, required labels, global/project dispatch, same-pass state limits, reload fallback, workflow polling cadence, legacy behavior, and real GitHub-adapter wiring against a mock server. |
| P2 durable claim/retry/reconciliation | Implemented and timing-smoke-tested | SQLite tests cover single-claim, restart recovery, terminal/ineligible cancellation, bounded retry, direct by-ID retry dispatch, stale-claim recovery, trigger-fed CDC, and near-deadline continuation. |
| P3 Linear adapter | Not yet implemented | Adapter contract tests and isolated real Linear canary. |

## P1/P2 test gates

P1 deterministic tests demonstrate:

1. a valid workflow revision drives the existing GitHub observer;
2. four eligible issues never exceed configured global or state limits;
3. invalid reload preserves the last valid runtime config;
4. unknown prompt variables fail only the affected attempt;
5. existing `trackerIntake` projects keep their current behavior.

P1 closeout includes an isolated mock-GitHub daemon smoke and applies the
fastest active workflow's `polling.interval_ms` to AO's shared intake loop.

P2 must not ship until deterministic restart tests demonstrate:

1. one durable active claim per project and canonical issue;
2. daemon reconstruction cannot spawn a duplicate active attempt;
3. normal and abnormal exits schedule the documented continuation/backoff;
4. terminal and ineligible tracker transitions release or cancel correctly;
5. retries retain issue, session, workflow revision, attempt, due time, and
   terminal reason;
6. storage triggers, rather than store methods, feed the existing CDC path.

The deterministic P2 gates above are covered, including an SQLite close/reopen
continuation smoke. A loop-level timing smoke verifies that a normal exit wakes
near the documented one-second continuation delay, and due retries dispatch
from the tracker item refreshed by ID even when it is absent from the candidate
list. The due-row transition back to `claimed` remains atomic.
