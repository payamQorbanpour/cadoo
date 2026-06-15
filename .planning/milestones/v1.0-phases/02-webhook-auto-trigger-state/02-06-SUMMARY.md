---
phase: 02-webhook-auto-trigger-state
plan: "06"
subsystem: releasedocs
tags: [releasedocs, worker, dispatch, idempotency, blog, pages, state]
dependency_graph:
  requires: [02-01, 02-02, 02-03, 02-04, 02-05]
  provides: [release-worker-wiring, posted-store-hook, default-generators-publishers]
  affects: [cmd/cadoo-worker, internal/releasedocs/dispatcher, internal/releasedocs/defaults]
tech_stack:
  added: []
  patterns:
    - PostedStore decoupling interface (releasedocs owns interface; state sub-package satisfies it — no import cycle)
    - TDD RED/GREEN pattern for PostedStore hook
    - nil-tolerant store field (D-14 stateless marker mode preserved)
key_files:
  created: []
  modified:
    - internal/releasedocs/defaults/defaults.go
    - internal/releasedocs/dispatcher.go
    - internal/releasedocs/dispatcher_test.go
    - cmd/cadoo-worker/main.go
decisions:
  - Use PostedStore interface in releasedocs package (not importing state sub-package) to avoid import cycle
  - Record errors are slog.Warn + continue (best-effort; publish already succeeded)
  - buildReleaseDispatcher uses same VCS pool construction pattern as buildDispatcher
  - In-memory runMemory path wraps releaseDispatcher.Run in a HandlerFunc for jobs.Handler compatibility
metrics:
  duration_minutes: 5
  completed_date: "2026-06-05"
  tasks_completed: 3
  files_modified: 4
---

# Phase 02 Plan 06: Worker Wiring + PostedStore Hook Summary

**One-liner:** cadoo-worker now consumes release_docs River jobs via a releasedocs.Dispatcher with blog+pages defaults and DB-backed PostedStore for cross-restart idempotency.

## What Was Built

### Task 1: Extend default slices with blog + pages (commit e8360c1)

Added `blog.New()` to `DefaultGenerators` (changelog -> releasenotes -> blog canonical order) and `pages.Publisher{}` to `DefaultPublishers` (releasebody -> changelogpr -> pages). Updated docstrings. No import cycle: defaults imports sub-packages; sub-packages import releasedocs, never defaults.

### Task 2: Add nil-tolerant PostedStore hook to Dispatcher - TDD (commits 028126e, b200df0)

Defined `PostedStore` interface in `internal/releasedocs/dispatcher.go` with a single `Record` method whose signature matches `state.Store.Record`. The interface decouples releasedocs from the state sub-package - no import and no cycle. Added `Store PostedStore` field to `Dispatcher` (nil = stateless marker mode, D-14).

After all publishers succeed, `Run` iterates produced artifacts and calls `Store.Record(ctx, job.Org, string(job.Provider), job.Repo, job.ToRef, string(art.Kind), job.ToRef)`. Record errors are slog.Warn (best-effort) and do not fail the run.

Three new tests:
- `TestPostedStoreNilNoOp`: nil Store -> no panic, no Record calls, normal behavior
- `TestPostedStoreRecordsOnSuccess`: one Record per artifact with correct (org, provider, repo, toTag, kind, externalID) args
- `TestPostedStoreNoRecordOnPublishError`: publisher error -> zero Record calls

### Task 3: Wire releasedocs dispatcher into cadoo-worker (commit 8e2e42a)

Added `buildReleaseDispatcher(s, pool)` that builds a VCS pool (same pattern as `buildDispatcher`), constructs `releasedocs.Dispatcher` with `defaults.DefaultGenerators()` / `defaults.DefaultPublishers()`, and sets `state.New(pool)` as `Store` when `pool != nil` (DB-backed idempotency) or leaves Store nil (stateless).

Updated `runRiver` to accept `releaseDispatcher *releasedocs.Dispatcher` and pass it as the 3rd arg to `riverq.New`. The `releaseWorker` in riverq was already wired in plan 02-05 - this plan closes the loop by passing a non-nil dispatcher.

Updated `runMemory` to register the `release_docs` kind with a `HandlerFunc` that unmarshals and calls `releaseDispatcher.Run`. When no VCS is configured the kind is left unregistered (graceful no-op).

## Verification Results

- `go build ./...` - all five binaries build successfully
- `go test -race -count=1 ./internal/releasedocs/...` - 160 tests pass across 11 packages
- `go vet ./...` - no issues

## Deviations from Plan

### Auto-additions

**1. [Rule 2 - Missing Critical Functionality] Added fmt import to cmd/cadoo-worker/main.go**
- **Found during:** Task 3
- **Issue:** `runMemory` needed `fmt.Errorf` for the HandlerFunc wrapper
- **Fix:** Added `"fmt"` to the import block
- **Files modified:** `cmd/cadoo-worker/main.go`
- **Commit:** 8e2e42a

**2. [Rule 2 - Missing Critical Functionality] Added Handle compatibility via HandlerFunc wrapper**
- **Found during:** Task 3
- **Issue:** `releasedocs.Dispatcher` has no `Handle(ctx, json.RawMessage) error` method (unlike `orchestrator.Dispatcher`) so it cannot be registered directly with the in-memory queue
- **Fix:** Used `jobs.HandlerFunc` in `runMemory` to unmarshal + forward to `releaseDispatcher.Run` - no change to the dispatcher itself
- **Files modified:** `cmd/cadoo-worker/main.go`
- **Commit:** 8e2e42a

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes beyond those declared in the plan's threat model. T-02-12 (SQL injection via parameterized pgx queries in state.Store.Record) and T-02-13 (repudiation logging via Record) are mitigated by the existing state sub-package implementation from 02-02.

## TDD Gate Compliance

- RED gate: commit 028126e (`test(02-06): add failing tests...`) - build failed with "unknown field Store"
- GREEN gate: commit b200df0 (`feat(02-06): add nil-tolerant PostedStore hook...`) - 160 tests pass
- REFACTOR: not needed (implementation was minimal and clean on first pass)

## Self-Check: PASSED

Files created/modified exist:
- internal/releasedocs/defaults/defaults.go - FOUND
- internal/releasedocs/dispatcher.go - FOUND
- internal/releasedocs/dispatcher_test.go - FOUND
- cmd/cadoo-worker/main.go - FOUND

Commits exist:
- e8360c1 - FOUND (feat: extend default slices)
- 028126e - FOUND (test: failing PostedStore tests)
- b200df0 - FOUND (feat: PostedStore implementation)
- 8e2e42a - FOUND (feat: worker wiring)
