# Downstream patch ledger

Status: current for the v0.11.2 fork baseline

This ledger classifies every intentional runtime or test-harness difference
between upstream `v0.11.2` and this fork. A downstream patch remains only while
its contract is required and no stable upstream release supplies equivalent
behavior.

| Patch | Status | Owned surface | Contract evidence | Retirement condition |
| --- | --- | --- | --- | --- |
| GitHub App installation tokens | retained | `adapters/githubappauth`, daemon GitHub wiring, Go dependencies | adapter and daemon wiring tests cover complete, partial, invalid, fallback, and refresh behavior | a stable upstream release supports installation tokens with the same fail-closed and fallback contract |
| Linear credential isolation | retained | `runtime/runtimeenv`, tmux and ConPTY launch paths | unit and tmux integration tests prove `AO_LINEAR_API_KEY` and `AO_LINEAR_OAUTH_TOKEN` remain daemon-only | Linear credentials no longer enter the daemon environment or upstream supplies an equivalent worker boundary |
| Read-only Linear intake | temporary | tracker domain/config, Linear adapter, tracker observer and CLI config | adapter, observer, daemon, and CLI tests cover project/assignee scoping, canonical identity, open-state filtering, deduplication, and no write path | an upstream stable release ships an equivalent Linear provider; compare first with #3319 or its accepted successor |
| Bot review continuation | retained | project config, lifecycle reactions, SCM observation model | lifecycle and CLI round-trip tests cover allowlist, denylist, deduplication, edit handling, and bounded nudges | upstream routes configurable actionable bot feedback without treating all automation as human review |
| Commented-review refresh | retained | SCM observer provider timestamp and thread semantic hashes | observer and integration tests cover provider-only updates and preservation of unrelated review state | upstream refreshes inline `COMMENTED` feedback on provider updates with equivalent polling bounds |
| tmux integration-test isolation | retained test harness | `tmux_integration_test.go` | v0.11.2 integration tests still use the default tmux server and call `kill-server`; the fork assigns a test-scoped `TMUX_TMPDIR` | upstream isolates its integration tmux server from user sessions |

The Symphony workflow scheduler and its runtime state machine are retired.
Migrations `0037` and `0039` remain only because they have shipped; they do not
authorize restoring the removed runtime. The long-prompt submission workaround
is also retired: no runtime delta remains after merging upstream's accepted
message-delivery implementation.

Migration numbering is tracked separately in
[`upstream-migration-map.md`](upstream-migration-map.md). Documentation,
generated API artifacts, immutable migration history, and mechanical removal of
trailing whitespace from an upstream SVG are not additional product patches.
Migration `0041` is a schema-convergence repair for an already deployed fork
version collision, not a new runtime capability.
