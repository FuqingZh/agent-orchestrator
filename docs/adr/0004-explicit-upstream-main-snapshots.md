# ADR 0004: Allow explicitly pinned upstream main snapshots

Date: 2026-08-04

Status: accepted

## Context

ADR 0003 synchronized the fork to the stable upstream v0.11.2 release and
treated upstream `main` as observation-only. After that merge, the fork moved
47 commits ahead while upstream moved 63 commits ahead. The next contribution
track, worker process containment, must be designed and tested against current
upstream code rather than an increasingly stale release snapshot.

The user explicitly authorized synchronizing upstream `main` at
`5f3e6bcd5a47bb7312f80cfc3966464a8f948cda`. This is a source-baseline change,
not authorization to deploy the resulting binary or migrate the live database.

## Decision

The development fork may take an upstream `main` snapshot when a human
explicitly authorizes it and the synchronization pins the exact commit.

Each snapshot synchronization must:

- use a normal merge from fork `main`, preserving both histories;
- never rebase or force-push the fork's published `main`;
- keep shipped fork migrations immutable and map colliding upstream migrations
  to the next unused fork versions without changing their contents;
- audit every remaining runtime delta against the downstream patch ledger;
- regenerate derived API artifacts from their source contracts;
- run the repository's canonical gates and a representative isolated canary;
- update durable baseline, migration, and downstream-delta documentation; and
- deliver through a compatibility-sensitive pull request.

This is not continuous tracking. A later upstream head requires another
explicitly pinned synchronization and fresh verification. Upstream remains the
authority for the core AO lifecycle; the fork retains only documented adapter
or compatibility boundaries.

Production deployment remains separately authorized and transactional. A
merged source snapshot does not change the installed daemon, database,
Dashboard, systemd units, or rollback generation.

## Consequences

- Contribution work can start from a current upstream ancestor without
  discarding the fork's tested adapters.
- Snapshot PRs may be larger than stable-release PRs and therefore require a
  migration matrix, downstream-delta audit, full CI, and exact-head review.
- Migration mappings remain append-only even when upstream later removes the
  feature introduced by an earlier mapped migration.
- Stable tags remain preferable for production rollouts, but are no longer the
  only permitted source merge target for the development fork.

## Reopening conditions

Reconsider this policy if snapshot synchronization repeatedly creates
unreviewable changes, if the fork's downstream delta can be eliminated, or if
AO publishes a stable cadence that is current enough for upstream contribution
work without merging `main` snapshots.
