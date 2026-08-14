# Downstream patch ledger

Status: archived for the direct upstream `v0.12.4` synchronization

The fork-only runtime and test-harness changes below are preserved by
`archive/fork-private-main-c55165a8` at
`c55165a8b042715e6e613028d3d3039fff79d90f`. They are not selectively carried
into the v0.12.4 runtime candidate. The archive branch is the recovery point;
this ledger is only an index of what was deliberately left behind.

| Archived patch | Former surface | Candidate disposition |
| --- | --- | --- |
| GitHub App installation tokens | `adapters/githubappauth`, daemon wiring, Go dependencies | archived; upstream v0.12.4 source is authoritative |
| Linear credential isolation | `runtime/runtimeenv`, tmux and ConPTY launch paths | archived; upstream v0.12.4 source is authoritative |
| Read-only Linear intake | tracker domain/config, adapter, observer, and CLI config | archived; upstream v0.12.4 source is authoritative |
| Bot-review continuation | project config and lifecycle reactions | archived; upstream v0.12.4 source is authoritative |
| Commented-review refresh | SCM observer provider timestamps and thread hashes | archived; upstream v0.12.4 source is authoritative |
| tmux integration-test isolation | `tmux_integration_test.go` and test environment | archived; upstream v0.12.4 source is authoritative |
| Symphony workflow scheduler | workflow issue-run runtime and state machine | retired and archived |

Published SQLite migrations are the exception required by the repository
contract. Their immutable files and the append-only reconciliation checks stay
in the candidate solely so existing fork databases can upgrade; they do not
authorize restoring the archived runtime behavior. See
[`upstream-migration-map.md`](upstream-migration-map.md) for the exact source
paths, destination paths, commits, and hashes.
