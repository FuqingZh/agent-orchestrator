# Symphony compatibility matrix

Status: `symphony-subset-v1`, Phase 0 contract

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
| `tracker.kind` | Parsed and preflighted | The first runtime adapter is `github` in Phase 1; `linear` remains Phase 3. |
| `tracker.required_labels`, active states, terminal states | Parsed and normalized | Enforcement begins in Phase 1. |
| `polling.interval_ms` | Supported | Default 30000; positive values only. Runtime application begins in Phase 1. |
| `workspace.root` | Supported | Default temp root, relative-to-workflow resolution, `~`, and explicit `$VAR`; missing variables fail validation. |
| Four workspace hooks plus timeout | Parsed and validated | Hook execution begins after the workflow-driven intake slice. |
| Global, turn, retry, and per-state concurrency fields | Parsed and validated | Invalid per-state entries are ignored as required; enforcement begins in Phase 1/P2. |
| Codex command, policies, and timeouts | Parsed | Policy values remain pass-through. AO agent adapters remain the Phase 1 execution layer. |
| Config revision | Supported | SHA-256 of the exact workflow file bytes. |
| Dynamic reload | Contract supported | `Reloader` detects revision changes at caller tick boundaries and retains the last valid config. Runtime tick wiring begins in Phase 1. |

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
| P1 workflow-driven GitHub intake | Not yet implemented | Mock GitHub intake, strict rendered prompt, required-label filtering, global/per-state concurrency, legacy-config compatibility. |
| P2 durable claim/retry/reconciliation | Not yet implemented | Single-claim constraint, restart recovery, terminal/ineligible cancellation, bounded retry, orphan recovery, CDC facts. |
| P3 Linear adapter | Not yet implemented | Adapter contract tests and isolated real Linear canary. |

## P1/P2 test gates

P1 must not ship until deterministic tests demonstrate:

1. a valid workflow revision drives the existing GitHub observer;
2. four eligible issues never exceed configured global or state limits;
3. invalid reload preserves the last valid runtime config;
4. unknown prompt variables fail only the affected attempt;
5. existing `trackerIntake` projects keep their current behavior.

P2 must not ship until deterministic restart tests demonstrate:

1. one durable active claim per project and canonical issue;
2. daemon reconstruction cannot spawn a duplicate active attempt;
3. normal and abnormal exits schedule the documented continuation/backoff;
4. terminal and ineligible tracker transitions release or cancel correctly;
5. retries retain issue, session, workflow revision, attempt, due time, and
   terminal reason;
6. storage triggers, rather than store methods, feed the existing CDC path.
