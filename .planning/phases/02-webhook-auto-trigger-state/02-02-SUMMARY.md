---
phase: 02-webhook-auto-trigger-state
plan: "02"
subsystem: database
tags: [postgres, pgx, goose, idempotency, release-docs]

requires:
  - phase: 01-core-review-pipeline
    provides: findings.Store nil-tolerance pattern and pgxpool wiring

provides:
  - release_docs_state table (migration 0006) keyed on (provider, repo_full_name, to_tag, artifact_kind)
  - internal/releasedocs/state.Store with nil-tolerant Record/Lookup backed by pgxpool
  - DB idempotency layer for release-docs dispatcher re-runs across process restarts

affects:
  - 02-06-PLAN.md (dispatcher wiring that consumes state.Store)
  - any plan that publishes release artifacts and needs edit-in-place semantics

tech-stack:
  added: []
  patterns:
    - "nil *Store no-op pattern: guard `if s == nil || s.pool == nil` at top of each method — matches findings.Store contract"
    - "ON CONFLICT DO UPDATE for idempotent upsert on composite key"
    - "Isolation by plain-string kind parameter — sub-package takes string, not a parent-package type, to prevent import cycle"

key-files:
  created:
    - db/migrations/0006_release_docs_state.sql
    - internal/releasedocs/state/state.go
    - internal/releasedocs/state/state_test.go
  modified: []

key-decisions:
  - "artifact_kind stored as plain TEXT string in DB and taken as plain string in Go to prevent import cycle between internal/releasedocs and internal/releasedocs/state"
  - "org_id stored as TEXT (not FK) for multi-tenancy, matching posted_findings pattern"
  - "nil pool Store treated same as nil *Store — no-op — so callers need zero nil-guards"
  - "DB round-trip test guarded by DATABASE_URL env var; pure unit tests cover nil-tolerance without DB"

patterns-established:
  - "Sub-package isolation: child packages of a dispatcher package must not import the parent — pass primitive types (string, int) across the boundary"
  - "Nil-safe store pattern: every exported method starts with `if s == nil || s.pool == nil { return zero }`"

requirements-completed:
  - REQ-release-docs-idempotency

duration: 35min
completed: 2026-06-05
---

# Phase 02 Plan 02: release_docs_state Table and nil-tolerant Store Summary

**Goose migration 0006 adds release_docs_state table keyed on (provider, repo_full_name, to_tag, artifact_kind); nil-tolerant state.Store wraps it with ON CONFLICT DO UPDATE upsert and zero coupling to the releasedocs package.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-06-05T00:00:00Z
- **Completed:** 2026-06-05T00:35:00Z
- **Tasks:** 2 (+ 1 human-verify checkpoint)
- **Files modified:** 3

## Accomplishments

- Goose migration 0006 creates `release_docs_state` with BIGSERIAL PK, multi-tenant `org_id TEXT`, unique composite key `(provider, repo_full_name, to_tag, artifact_kind)`, lookup index, and clean `Down` block — round-trips up→down→up confirmed by operator against live Postgres
- `internal/releasedocs/state.Store` implements `Record` (INSERT ON CONFLICT DO UPDATE) and `Lookup` (SELECT by composite key), both nil-receiver-safe, backed by pgxpool
- Package takes `kind` as a plain `string` parameter and imports nothing from `internal/releasedocs` — the isolation guarantee that prevents an import cycle when the dispatcher wires this in plan 02-06

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 0006_release_docs_state.sql** - `5b75412` (chore)
2. **Task 2: state.Store — DB-backed, nil-tolerant Record/Lookup** - `a571f82` (feat)

**Plan metadata:** _(docs commit below)_

## Files Created/Modified

- `db/migrations/0006_release_docs_state.sql` — Goose Up/Down migration defining release_docs_state table with composite unique key and lookup index
- `internal/releasedocs/state/state.go` — Store type, New constructor, Record and Lookup methods; nil-tolerant; parameterized SQL only (T-02-03 mitigation)
- `internal/releasedocs/state/state_test.go` — nil-receiver tests (4 unit), DB round-trip test skipped when DATABASE_URL unset

## Decisions Made

- **Plain string kind, not ArtifactKind type:** Avoids import cycle — `internal/releasedocs` is the parent package of `state`, so `state` cannot import it. Kind values flow as plain strings across the boundary.
- **org_id as TEXT, not FK:** Matches the `posted_findings` multi-tenancy convention. No FK avoids schema coupling to an orgs table that may not exist in all deployment modes.
- **Nil pool = nil receiver semantics:** A `New(nil)` Store behaves identically to a nil `*Store`, so callers in cadoo-cli (no DB) require zero nil-guards.

## Deviations from Plan

None — plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None — no external service configuration required. The migration is applied via `make migrate`.

## Threat Surface Scan

No new network endpoints or auth paths introduced. All SQL uses parameterized queries ($1, $2, ...) — T-02-03 (SQL injection) fully mitigated. No new trust boundaries beyond what the plan's threat model covers.

## Next Phase Readiness

- `release_docs_state` table is live (migration confirmed up→down→up by operator)
- `state.Store` is ready to be wired into the release-docs dispatcher in plan 02-06
- Zero import cycle risk verified: `grep -c "internal/releasedocs\"" internal/releasedocs/state/state.go` returns 0

---
*Phase: 02-webhook-auto-trigger-state*
*Completed: 2026-06-05*
