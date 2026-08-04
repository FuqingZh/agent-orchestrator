# Upstream main synchronization verification

Date: 2026-08-04

Branch: `codex/sync-upstream-main-5f3e6bcd`

This record bounds the evidence for merging upstream `main` at
`5f3e6bcd5a47bb7312f80cfc3966464a8f948cda` into the fork baseline at
`338a8f60c55a0824d2e7d9be054cef2773ab884b`. It is source and isolated-runtime
evidence, not production deployment evidence.

## Merge-specific evidence

- Source conflicts preserved the downstream bot-review filter while adopting
  upstream shared merge-readiness and notification-resolution behavior.
- Upstream migrations 0039 through 0041 were mapped byte-for-byte to fork
  versions 0042 through 0044. Tests cover a fresh database and fork histories
  through versions 0039 and 0041; the next unused fork version is 0045.
- OpenAPI, TypeScript, and sqlc artifacts were regenerated without residual
  drift.
- A workspace-project regression found during the full gate was fixed: a child
  repository reports a default-branch ref only when that ref recomputes a base
  different from the persisted child SHA.

## Canonical gates

The candidate passed with host-bounded Go and Vitest concurrency:

- `npm run lint`;
- frontend typecheck and E2E typecheck;
- `go build ./...`, `go vet ./...`, and `go test -race ./...`;
- all 1,460 frontend Vitest tests;
- `npm run sqlc` and `npm run api`; and
- the tagged CLI E2E suite, including isolated daemon lifecycle coverage.

The first frontend test attempt lacked the landing package's locked
dependencies and failed only the five Markdown-twin tests because `cheerio`
was absent. After installing that package from its own lock file, the complete
frontend suite passed. npm reported existing dependency audit findings; this
sync does not rewrite dependency policy or apply an unreviewed audit fix.

## Evidence boundaries

GitHub Actions remains authoritative for platform-specific workflows and
secret-dependent checks. This source synchronization does not deploy a binary,
change production AO state, or close the known `setsid` process-escape gap.
The upstream snapshot adds best-effort Docker container reaping, but it does
not add systemd/cgroup process ownership or durable empty-containment cleanup.
