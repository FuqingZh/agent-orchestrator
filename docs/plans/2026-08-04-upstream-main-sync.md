# AO upstream main snapshot synchronization

Status: completed — awaiting pull-request delivery

Date: 2026-08-04

Authority: [ADR 0004](../adr/0004-explicit-upstream-main-snapshots.md)

## Goal

Merge the explicitly authorized upstream `main` snapshot into the fork while
preserving shipped database history and the documented downstream adapter
boundaries. This plan changes source only; production deployment is out of
scope.

## Pinned inputs

| Input | Commit |
| --- | --- |
| Common ancestor | `c5523a6d0e51251b79555b95ddc7d2be59da0f50` |
| Fork baseline | `338a8f60c55a0824d2e7d9be054cef2773ab884b` |
| Upstream target | `5f3e6bcd5a47bb7312f80cfc3966464a8f948cda` |

At the start of the merge, the fork and upstream were 47 and 63 commits ahead
of their common ancestor respectively.

## Merge contract

- Merge upstream normally; do not rewrite either published history.
- Prefer upstream lifecycle behavior unless the downstream patch ledger owns
  the conflicting contract.
- Preserve bot-review continuation while adopting upstream's shared
  `MergeReadiness` and notification-resolution behavior.
- Preserve Linear intake, worker credential isolation, GitHub App auth,
  provider-update refresh, and tmux test isolation until their retirement
  conditions are met.
- Regenerate OpenAPI and TypeScript artifacts from Go source.
- Keep the worker containment plans separate from this baseline sync.

## Migration mapping

The fork already shipped versions 0037 through 0041. Map the new upstream
migrations without editing either source content or shipped fork files:

| Upstream | Fork |
| --- | --- |
| `0039_drop_orchestrator_reengagement.sql` | `0042_drop_orchestrator_reengagement.sql` |
| `0040_add_session_diff_base.sql` | `0043_add_session_diff_base.sql` |
| `0041_notification_resolution.sql` | `0044_notification_resolution.sql` |

Acceptance requires unique versions, recorded SHA-256 values, fresh database
creation, upgrade from upstream v0.11.1, upgrade from fork versions 0039 and
0041, compatibility with the deployed version-40 collision history, and an
idempotent second migration pass.

## Verification

Run focused conflict and migration tests first, then:

```bash
npm run lint
npm run frontend:typecheck
cd backend && go build ./...
cd backend && go test -race ./...
cd backend && go vet ./...
cd frontend && npm run typecheck:e2e
cd frontend && npm test
npm run sqlc
npm run api
git diff --check
git diff --cached --check
git diff --check origin/main...HEAD
git status --short
```

Use bounded Go and test concurrency on this host. Run an isolated daemon and
worker lifecycle canary before merge. GitHub's exact-head workflows remain the
authority for platform-specific and secret-dependent checks.

## Delivery

Deliver one compatibility PR from
`codex/sync-upstream-main-5f3e6bcd`. Do not enable auto-merge until the exact
head has passed CI and review. Do not deploy the result as part of this PR.

## Progress

- [x] Fetch and pin fork and upstream heads.
- [x] Merge upstream and resolve source/API conflicts.
- [x] Map colliding migrations 0039 through 0041 to fork 0042 through 0044.
- [x] Pass focused lifecycle, API, and migration tests.
- [x] Complete the canonical gate and isolated CLI lifecycle canary.
- [ ] Deliver and merge the synchronization PR.
- [ ] Rebase the worker-containment plan PR on the synchronized fork baseline.
