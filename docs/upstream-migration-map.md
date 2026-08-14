# Upstream migration map

Status: current for direct upstream `v0.12.4` synchronization
(`c01ef2d39783c1bc9f99ed75abfb7e90e6c7d3bf`)

The runtime source follows upstream `v0.12.4`. The migration files below are
the only fork-line compatibility retained in the candidate. Already-published
fork migrations are immutable; upstream migrations that collide with an
unpublished fork number are copied byte-for-byte to the next available number.

## Fork-line immutable history

These files are retained because fork databases may have recorded their
versions. They do not restore the fork runtime behavior that created them.

| Version | File | Role |
| ---: | --- | --- |
| 37 | `0037_workflow_issue_runs.sql` | shipped fork history; later dropped |
| 38 | `0038_drop_worker_idle_outbox.sql` | upstream 0037 content, mapped |
| 39 | `0039_drop_workflow_issue_runs.sql` | shipped fork history; later dropped |
| 40 | `0040_orchestrator_reengagement.sql` | upstream 0038 content, mapped |
| 41 | `0041_repair_orchestrator_reengagement.sql` | repair for deployed fork version-40 collision |
| 42 | `0042_drop_orchestrator_reengagement.sql` | upstream 0039 content, mapped |
| 43 | `0043_add_session_diff_base.sql` | upstream 0040 content, mapped |
| 44 | `0044_notification_resolution.sql` | upstream 0041 content, mapped |
| 45 | `0045_review_run_unique_per_harness.sql` | upstream 0042 content, mapped |
| 46 | `0046_add_session_pinned.sql` | upstream 0043 content, mapped |
| 47 | `0047_backfill_review_run_batch_id.sql` | upstream 0044 content, mapped |
| 48 | `0048_agent_model_catalog.sql` | upstream 0047 content, mapped |
| 49 | `0049_reconcile_v0121_native_lineage.sql` | append-only native-lineage reconciliation |

## Upstream source and candidate mapping

The SHA-256 column is over the complete SQL file. Mapped rows must remain
byte-identical to the source file at the recorded upstream commit.

| Upstream source | Upstream commit | Candidate path | SHA-256 |
| --- | --- | --- | --- |
| `0037_drop_worker_idle_outbox.sql` | `2f6d98f272afa2cd9ea142511fe3a9197d94d2c6` | `0038_drop_worker_idle_outbox.sql` | `abf41789032c9a9bc25e21364d07d3c19dc3ad5e76a6e327e195a87f32947342` |
| `0038_orchestrator_reengagement.sql` | `79a70e82fa455824568e25b9530a8e2241314cec` | `0040_orchestrator_reengagement.sql` | `f2b14364b6abad489e941dac5c9e40e2678c8f5054ddfcd734ff7d09ad6db3ca` |
| `0039_drop_orchestrator_reengagement.sql` | `ef4d6c124226c715bef3d02777b89bf201dd4b96` | `0042_drop_orchestrator_reengagement.sql` | `01b2baa49b6fcc0c461f05e8b8bcf07a7f971ff8fcaee80425b53d0c8b752cf4` |
| `0040_add_session_diff_base.sql` | `e8cc5f3e2689a698a38504a99fe773a04af240e5` | `0043_add_session_diff_base.sql` | `1b1001d774bcb30aec24de8803bac0090b12c1fa3252d8f9b45ed74e0f9596f9` |
| `0041_notification_resolution.sql` | `1bd62cdfdd14cdd286e985be1528fa264d1659e2` | `0044_notification_resolution.sql` | `4aed8877163cd39674716564262376449a3a61a5959468e6c33d2b308c34e112` |
| `0042_review_run_unique_per_harness.sql` | `66240ab24ea78d1e6e2b1baa34c6796a1a7494dc` | `0045_review_run_unique_per_harness.sql` | `679218370fde19c9f534395037055f74acd84877b119bc4d9d9c46471a405074` |
| `0043_add_session_pinned.sql` | `bfb69fbe7f362d7a142576eb374b756c7d177992` | `0046_add_session_pinned.sql` | `bfb32829e648bf051051d88b85fd8c459b95cd87b485d2b7f211043936427a68` |
| `0044_backfill_review_run_batch_id.sql` | `ad153c1f5630dd7c0191e14a9d72c0c31fa0dd9e` | `0047_backfill_review_run_batch_id.sql` | `e8263c0522e9c1946fc550cc0cc4223abf87268882ee512d06b18c9c80361598` |
| `0047_agent_model_catalog.sql` | `4babacf95e8ac54595f2a21849762f82263452c1` | `0048_agent_model_catalog.sql` | `cafb59d968d0941e04219dfabefc7e8440cbfb51ef4695be9904361e2bb9c4da` |
| `0048_review_agent_session_id.sql` | `d15fd8277c1b63a1a7ebdb55735b9ed6091cf118` | `0050_review_agent_session_id.sql` | `440eac3ebdd773e4fa70824d5137ac3b1704d706618d7d947da88407c6d23a7e` |
| `0049_review_per_harness.sql` | `d15fd8277c1b63a1a7ebdb55735b9ed6091cf118` | `0051_review_per_harness.sql` | `720fb0658b813368d3ae9aab914634c85638582207f6f506fa9139a21527ff1a` |
| `0052_model_usage.sql` | `ae0541324accf9f91f8f7de48ebafa2b5692ce36` | `0052_model_usage.sql` | `949759bf63f657c3caf137e4c771d0ae8b9f0390a050278e3a22e7421726cacd` |
| `0053_allow_muse_harness.sql` | `ef8cdb30938b13c49e0837e87a3a44a4af9bc116` | `0053_allow_muse_harness.sql` | `c2b51ab364ad16adb55fc8a3ec96bc64799b525413ab22b7699c7b6dfd1cad78` |
| `0054_allow_kimchi_harness.sql` | `0c98a7a8adfef4f73f58b4ecf2f080a59ea0fef7` | `0054_allow_kimchi_harness.sql` | `34a8717ee151c4303a533667718715e24641ea317be83617f3b462edc17737c2` |
| `0066_chat_session_mode.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0066_chat_session_mode.sql` | `ead224c711003cd908e541877da7471ae9543d677c27f384a3e653fefc337f4c` |
| `0067_app_settings.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0067_app_settings.sql` | `9ea7872219ca96f87c912238b486ae4e9436d6f0512460085a7b4130f614e128` |
| `0068_conversation_turn_settings.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0068_conversation_turn_settings.sql` | `682d992e547853c18ba339840ee30c0c388b1d13ad2bafa90676be6c64ba1b75` |
| `0069_conversation_compaction.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0069_conversation_compaction.sql` | `033e8e43c907d00095e99697d38bdaf0fe89df93964cc96338aec551a2eaa724` |
| `0070_command_output_and_diffs.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0070_command_output_and_diffs.sql` | `4a5adf4785c577b4797a59eeeb0c079da39036d383321046d51c7d2677fc4023` |
| `0071_conversation_usage.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0071_conversation_usage.sql` | `fd9b70757705d1a1d08526ac55405f50307e3967f7fbacf0b8c57e8ba77dd388` |
| `0072_conversation_history_ops.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0072_conversation_history_ops.sql` | `5d50117620407028b172426528689ce4bd4e417a9739b8d60f9293a77621734a` |
| `0073_conversation_provider_state.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0073_conversation_provider_state.sql` | `6c89428ef2da5254d0153c7cb61409e737a1fdafe4509708426a2611552bd5a1` |
| `0074_activity_kinds_mcp_and_auto_review.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0074_activity_kinds_mcp_and_auto_review.sql` | `1271188877dc4c4bfb2545fb69d59d2b98042b90d5b408c08d207a17e571559d` |
| `0075_conversation_user_input.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0075_conversation_user_input.sql` | `f01ae95ce6a6e972fe908ed13220f5540c8533f7ef17b2778aa8ba57236bb08d` |
| `0076_conversation_delivery_content_and_cost.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0076_conversation_delivery_content_and_cost.sql` | `cf9ad10d23b4e2bf2ae6f851a4e6c65cd040f6cd9a5f84ce977bbc67d312e5f6` |
| `0077_cancelled_conversation_activities.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0077_cancelled_conversation_activities.sql` | `8a694770a46859808ccf5e806fd351157723d02ee4c213562dded8797cc5be16` |
| `0078_session_interface_transitions.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0078_session_interface_transitions.sql` | `b85f520fbd8e3ba5fee20e641e24f6ba2166ff65950c1a8b88838050eb681113` |
| `0079_session_interface_transition_delivery.sql` | `7239bb95383c55be391c19ff749b71153eb554c1` | `0079_session_interface_transition_delivery.sql` | `6fa09d17110e5c897d1fc8f9e4eed932737e25de78ccb57e68979e87fd239cca` |
| `0080_review_per_harness.sql` | `d15fd8277c1b63a1a7ebdb55735b9ed6091cf118` | `0080_review_per_harness.sql` | `56b280912add643b94a34e1a323f458e06ca900b93bc9076e3a37cf515ef3387` |
| `0081_browser_capability_verifier.sql` | `c9e1c676baee8ab2e860a8110f2539a460e73a11` | `0081_browser_capability_verifier.sql` | `aef4c4efb367a2b15a4e2d78a6c9d03543c73359573e7308e0e6d8758880676a` |
| `0082_allow_prime_agent_harness.sql` | `6c72814f9849e95ad85fe42e88fee1c540d590ad` | `0082_allow_prime_agent_harness.sql` | `36dc2d339db5f10106188a94afd33dea7a5560fd25d6047c8cea586864a90e25` |
| `0083_reconcile_kimchi_prime_agent_harnesses.sql` | `0c98a7a8adfef4f73f58b4ecf2f080a59ea0fef7` | `0083_reconcile_kimchi_prime_agent_harnesses.sql` | `adf02802f66c15d286574f4c854b7cd2a2e9e5494a1f8a15602840659ebe88ce` |
| `0084_add_session_auto_inject_review.sql` | `f65c48e296e20a816221a4003c75a5f0387967ec` | `0084_add_session_auto_inject_review.sql` | `30203959a0a623e62424e7446a8f8c8f9aac1174059c83634a778a4ca15de84c` |
| `0085_agent_switching.sql` | `01cb67fe11734d725b1a84deb130fe58b40203d6` | `0085_agent_switching.sql` | `b3871aaf81c982886f3385f276aed543abdce029a62057f1a6708a3b5c643bc3` |
| `0086_workspace_repo_default_branch.sql` | `0c60f21360f1718c75394649e8e3634c0b690e65` | `0086_workspace_repo_default_branch.sql` | `97b86b2e4f1d6e25e6c3d80e527ab7d84dadb4674419aebae0b2827b0e7aa1cc` |
| `0087_conversation_branches.sql` | `5778b74db45e4516f918ea4ba502bcda22b05b52` | `0087_conversation_branches.sql` | `c7e7e1e41bcc83361db7fb68df5b646a5aa976f5230ec9f354a7c35e92dfb98e` |
| `0088_add_auto_inject_ci_toggle.sql` | `461a6df56994f5e959eb3515199a614c31182ce0` | `0088_add_auto_inject_ci_toggle.sql` | `9dff9f00dd75d99af4d01b8132320eeec27def6ed8aeb3648779582e308f76f2` |

The candidate has exactly one migration filename per numeric version. The
v0.12.4 additions above are source-identical and require no fork renumbering.
Observed upstream `main` migrations after `v0.12.4` remain outside this stable
baseline and are not reserved or cherry-picked here.
