# Fork private modifications archive: v0.12.1 baseline

Status: archival only; this history is not part of the v0.12.2 release candidate.

Date: 2026-08-10

## Authoritative references

- Fork main at the time of archival: `c55165a8b042715e6e613028d3d3039fff79d90f`
- Local archive reference: `archive/fork-private-main-c55165a8`
- Fork/source common ancestor: `1df40e93772c2c48e916870d9c3ddf8f29a69f84`
- Direct synchronization target: upstream release `v0.12.2` at `b6609ae610e809309be86fce56c0845cc45628cb`
- Upstream main observed separately: `3e2dbf4249793e127f4e26b2499738ba3ca8fa1a`

The archive reference preserves the complete fork-main commit graph and is the
recovery point for the fork-owned runtime, API, frontend, workflow, and
integration changes. It is intentionally kept separate from the direct release
synchronization branch.

## Disposition

The v0.12.2 synchronization candidate takes upstream release behavior as the
source of truth. Fork-only runtime changes are not selectively carried forward.
Already-published SQLite migration files remain only where required by the
repository's migration compatibility rules; retaining immutable migration
history is not a reactivation of the archived fork behavior.

No production AO binary, service, database, Dashboard, or deployment state was
changed by this archival operation.
