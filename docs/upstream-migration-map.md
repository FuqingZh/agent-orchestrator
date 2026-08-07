# Upstream migration map

Status: current for upstream `v0.12.1`

This ledger preserves the relationship between immutable upstream SQLite
migrations and the versions shipped by this fork. The fork must never modify a
migration already recorded by a deployed database. When a pinned upstream
release reuses one of those version numbers, copy the upstream migration to the
next unused fork version without changing its contents.

| Upstream source | Upstream commit | Fork path | SHA-256 |
| --- | --- | --- | --- |
| `0037_drop_worker_idle_outbox.sql` | `2f6d98f272afa2cd9ea142511fe3a9197d94d2c6` | `0038_drop_worker_idle_outbox.sql` | `abf41789032c9a9bc25e21364d07d3c19dc3ad5e76a6e327e195a87f32947342` |
| `0038_orchestrator_reengagement.sql` | `c5523a6d0e51251b79555b95ddc7d2be59da0f50` | `0040_orchestrator_reengagement.sql` | `f2b14364b6abad489e941dac5c9e40e2678c8f5054ddfcd734ff7d09ad6db3ca` |
| `0039_drop_orchestrator_reengagement.sql` | `ef4d6c124226c715bef3d02777b89bf201dd4b96` | `0042_drop_orchestrator_reengagement.sql` | `01b2baa49b6fcc0c461f05e8b8bcf07a7f971ff8fcaee80425b53d0c8b752cf4` |
| `0040_add_session_diff_base.sql` | `e8cc5f3e2689a698a38504a99fe773a04af240e5` | `0043_add_session_diff_base.sql` | `1b1001d774bcb30aec24de8803bac0090b12c1fa3252d8f9b45ed74e0f9596f9` |
| `0041_notification_resolution.sql` | `1bd62cdfdd14cdd286e985be1528fa264d1659e2` | `0044_notification_resolution.sql` | `4aed8877163cd39674716564262376449a3a61a5959468e6c33d2b308c34e112` |
| `0042_review_run_unique_per_harness.sql` | `66240ab24ea78d1e6e2b1baa34c6796a1a7494dc` | `0045_review_run_unique_per_harness.sql` | `679218370fde19c9f534395037055f74acd84877b119bc4d9d9c46471a405074` |
| `0043_add_session_pinned.sql` | `bfb69fbe7f362d7a142576eb374b756c7d177992` | `0046_add_session_pinned.sql` | `bfb32829e648bf051051d88b85fd8c459b95cd87b485d2b7f211043936427a68` |
| `0044_backfill_review_run_batch_id.sql` | `ad153c1f5630dd7c0191e14a9d72c0c31fa0dd9e` | `0047_backfill_review_run_batch_id.sql` | `e8263c0522e9c1946fc550cc0cc4223abf87268882ee512d06b18c9c80361598` |
| `0047_agent_model_catalog.sql` | `4babacf95e8ac54595f2a21849762f82263452c1` | `0048_agent_model_catalog.sql` | `cafb59d968d0941e04219dfabefc7e8440cbfb51ef4695be9904361e2bb9c4da` |

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

The explicitly authorized upstream `main` snapshot later removes the table
through mapped migration `0042`; the repair remains necessary so every shipped
fork history first converges before that idempotent removal. Mapped migrations
`0043` and `0044` then add diff-base and notification-resolution facts.

The v0.12.1 release reuses fork versions `0042`, `0043`, and `0044` for new
review and session facts, and uses `0047` for the model catalog. Those four
release files are preserved byte-for-byte under fork versions `0045` through
`0048` above.

The append-only v0.12.1 lineage reconciliation uses `0049`; the next unused
`0049_reconcile_v0121_native_lineage.sql` has SHA-256
`c16feb4ae9cf674163f81b81ac24a44de9fbb96ee297474773fa8968a6ee3247` and
bridges the native v0.12.1 version rows without modifying any shipped
migration. The next unused fork migration version is `0050`. Future upstream migrations
are mapped only as part of another explicitly pinned baseline synchronization.
