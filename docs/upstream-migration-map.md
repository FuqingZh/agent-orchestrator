# Upstream migration map

Status: current

This ledger preserves the relationship between immutable upstream SQLite
migrations and the versions shipped by this fork. The fork must never modify a
migration already recorded by a deployed database. When a pinned upstream
release reuses one of those version numbers, copy the upstream migration to the
next unused fork version without changing its contents.

| Upstream source | Upstream commit | Fork path | SHA-256 |
| --- | --- | --- | --- |
| `0037_drop_worker_idle_outbox.sql` | `2f6d98f272afa2cd9ea142511fe3a9197d94d2c6` | `0038_drop_worker_idle_outbox.sql` | `abf41789032c9a9bc25e21364d07d3c19dc3ad5e76a6e327e195a87f32947342` |
| `0038_orchestrator_reengagement.sql` | `c5523a6d0e51251b79555b95ddc7d2be59da0f50` | `0040_orchestrator_reengagement.sql` | `f2b14364b6abad489e941dac5c9e40e2678c8f5054ddfcd734ff7d09ad6db3ca` |

The source and fork files in each row must remain byte-identical. Verification
checks the content hash, uniqueness of the numeric fork versions, fresh schema
creation, and upgrade from both upstream and fork database histories.

The workstation compatibility lineage deployed a different immutable `0040`,
`0040_add_session_launch_permissions.sql`, before this baseline was merged.
Those databases therefore skip `0040_orchestrator_reengagement.sql` by version
even though its table is absent. Migration
`0041_repair_orchestrator_reengagement.sql` idempotently converges both
histories: it is a no-op after the mapped upstream `0040`, and creates the
missing table and indexes after the deployed calibration `0040`. Its down path
intentionally preserves the table because logical ownership remains with
`0040` on fresh databases.

The next unused fork migration version after this compatibility repair is
`0042`. Future
upstream migrations are mapped only when a stable release is pinned; current
upstream `main` is not migration authority for this fork.
