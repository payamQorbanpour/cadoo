---
phase: 08-ci-mode-dedup-convergence
plan: "04"
subsystem: orchestrator
tags: [incremental-review, dedup, fixed-point, ci-mode, findings-store, diff-between]

# Dependency graph
requires:
  - phase: 08-01
    provides: resolveStalePriors direct-compare + postInline dedup gate
  - phase: 08-03
    provides: DiffBetweener interface, PriorReview.LastReviewedSHA, tools.Input incremental fields, marker round-trip, memoryStore.lastReviewedSHA field

provides:
  - Store.LastReviewedSHA() nil-safe dispatch method (mem/pool/nil paths)
  - Incremental dispatch block in Dispatcher.Run (DiffBetweener type-assert + SHA guards)
  - changeSet-scoped resolveStalePriors (priors on untouched files persist)
  - Fixed-point convergence: unchanged-head re-run posts 0 new + 0 resolved
  - Three integration tests: fixed-point, incremental, non-ancestor-fallback

affects: [08-ci-mode-dedup-convergence, cadoo-ci-mode, orchestrator-run, findings-store]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Nil-safe Store dispatch: nil receiver → ""; mem path → field value; pool path → "" (DB is full-review per SPEC Open Question 2)"
    - "Optional-capability probe: type-assert provider.(vcs.DiffBetweener); absent → full-review fallback"
    - "Three-outcome incremental dispatch: err/nil → fallback; empty slice → summary-only refresh; non-empty → scoped IncrementalFiles/Packed + changeSet"
    - "changeSet gate in resolveStalePriors: when incrementalRun, skip priors whose File is absent from the change set"
    - "Same-commit short-circuit: guard sha != pr.HeadSHA before calling DiffBetween (T-08-C4)"

key-files:
  created: []
  modified:
    - internal/findings/findings.go
    - internal/orchestrator/reviewer.go
    - internal/orchestrator/reviewer_test.go

key-decisions:
  - "DB/pool store path always returns empty LastReviewedSHA (full review) per SPEC Open Question 2 — only CI-mode mem path uses incremental dispatch"
  - "DiffBetween(nil, nil) is the non-ancestor signal (Pitfall 4): treat as full-review fallback (IsIncrementalRun=false) to never silently skip changed code"
  - "resolveStalePriors scope gate placed before the pkey compare — priors on untouched files are neither resolved nor re-posted, preserving thread monotonicity"
  - "Posted dedup store never bypassed: incremental logic only restricts which files enter tools.Input; posting still flows through postInline → HasFinding (Pitfall 3)"

patterns-established:
  - "Fixed-point convergence pattern: seal with LastReviewedSHA stamp → recover on re-run → scope incremental diff → empty diff → no new posts → no resolved priors → fixed point"
  - "Incremental test harness: fake provider implements vcs.DiffBetweener with configurable return; sv.replay() carries parsed LastReviewedSHA for Run 2"

requirements-completed:
  - REQ-cidedup-incremental-review
  - REQ-cidedup-convergent-review

# Metrics
duration: ~90min (across two tasks + live dogfood checkpoint)
completed: 2026-06-15
---

# Phase 08 Plan 04: Part C Orchestration + Convergence Fixed-Point Summary

**Incremental review wired end-to-end via Store.LastReviewedSHA() + DiffBetweener dispatch in Run, with changeSet-scoped resolveStalePriors achieving a verified fixed point: unchanged-head re-run posts 0 new threads and resolves 0 existing.**

## Performance

- **Duration:** ~90 min
- **Started:** 2026-06-15T00:00:00Z
- **Completed:** 2026-06-15T00:00:00Z
- **Tasks:** 3 (2 auto + 1 checkpoint:human-verify)
- **Files modified:** 3

## Accomplishments

- Added `Store.LastReviewedSHA() string` to `internal/findings/findings.go` with nil-safe dispatch mirroring the `SummaryID` pattern: nil receiver → ""; mem path returns `mem.lastReviewedSHA`; pool/DB path → "" (full-review per SPEC)
- Wired incremental dispatch block in `Dispatcher.Run` (`reviewer.go`): type-asserts `provider.(vcs.DiffBetweener)`, guards `sha != "" && sha != pr.HeadSHA`, handles three DiffBetween outcomes (err/nil-diff → fallback; empty → summary-only; non-empty → scoped IncrementalFiles/Packed + fileSet changeSet)
- Extended `resolveStalePriors` with `changeSet map[string]struct{}, incrementalRun bool` so priors on untouched files skip resolution, preventing churn on unchanged code
- Added three integration tests: `TestCIModeFixedPointUnchangedHead`, `TestCIModeIncrementalChangedLines`, `TestDiffBetweenFallbackOnNonAncestor` — all GREEN after Task 2; 499 tests passing
- Live dogfood checkpoint approved: second unchanged-head `cadoo ci` run posted 0 new threads and resolved 0 existing (fixed point confirmed on a live PR/MR)

## Task Commits

Each task was committed atomically:

1. **Task 1: Store.LastReviewedSHA() + failing fixed-point / incremental / fallback tests (RED)** - `c437a6d` (test)
2. **Task 2: Incremental dispatch in Run + changeSet-scoped resolveStalePriors (GREEN)** - `3b9e67c` (feat), `c208b62` (style)
3. **Task 3: Dogfood convergence fixed point on live PR/MR** - APPROVED by user (no code commit — manual verification checkpoint)

## Files Created/Modified

- `internal/findings/findings.go` - Added `Store.LastReviewedSHA() string` nil-safe dispatch; `memoryStore.lastReviewedSHA` field was seeded in Plan 03
- `internal/orchestrator/reviewer.go` - Incremental dispatch block in `Run` (DiffBetweener type-assert, SHA guards, three-outcome handling, `fileSet` helper); `resolveStalePriors` signature extended with `changeSet + incrementalRun`; call site in `postInline` updated to pass both
- `internal/orchestrator/reviewer_test.go` - Three new integration tests; fake provider extended to implement `vcs.DiffBetweener` with configurable returns; `sv.replay()` extended to parse and carry `LastReviewedSHA` from stamped summary body

## Decisions Made

- DB/pool `Store` path returns `""` from `LastReviewedSHA()` (always full review) per SPEC Open Question 2 — only the CI-mode in-memory path exercises incremental dispatch
- `DiffBetween` returning `(nil, nil)` is the canonical non-ancestor signal (Pitfall 4): treated as full-review fallback (`IsIncrementalRun=false`) to guarantee no changed code is silently skipped — asserted by `TestDiffBetweenFallbackOnNonAncestor`
- Same-commit short-circuit guard (`sha != pr.HeadSHA`) placed before the DiffBetweener type-assert to avoid a needless provider round-trip on unchanged-head re-runs (T-08-C4)
- `Posted` dedup store never bypassed: incremental scoping only changes WHICH files enter `tools.Input`; posting still flows through `postInline → HasFinding` (Pitfall 3 / T-08-C7)

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None — the `sv.replay()` extension required careful parsing of the `<!-- cadoo:reviewed-sha:<hex> -->` marker to carry `LastReviewedSHA` into the second run's seeded store, but this was anticipated in the plan's `<read_first>` guidance.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Part C orchestration is complete. All three parts of Phase 08 (A: no self-resolution, B: sticky suppression, C: incremental dispatch) are now implemented and verified.
- The phase gate (`make test && make lint && make vet`) passes with 499 tests.
- Live dogfood confirms the convergence fixed point holds on a real PR/MR.
- Ready for Phase 08 wrap-up / any remaining plans in the phase.

---
*Phase: 08-ci-mode-dedup-convergence*
*Completed: 2026-06-15*

## Self-Check: PASSED

- `internal/findings/findings.go` exists and contains `Store.LastReviewedSHA` (modified in Task 1/2)
- `internal/orchestrator/reviewer.go` exists and contains `DiffBetweener` type-assert (modified in Task 2)
- `internal/orchestrator/reviewer_test.go` exists and contains `TestCIModeFixedPointUnchangedHead` (added in Task 1)
- Commits verified: `c437a6d` (test RED), `3b9e67c` (feat GREEN), `c208b62` (style) all present in `git log --oneline -5`
- Live dogfood checkpoint: user confirmed "approved" — 0 new + 0 resolved on unchanged-head second run
