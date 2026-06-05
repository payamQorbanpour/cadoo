---
phase: 01-generators-publishers-cli
plan: 02
subsystem: releasedocs
tags: [semver, conventional-commits, changelog, grouped-model, golang.org/x/mod]

# Dependency graph
requires:
  - phase: 01-generators-publishers-cli
    plan: 01
    provides: "releasedocs core types/interfaces (ReleaseContext, SemverBump, Generator, Publisher, vcs.Commit, vcs.MergedPR, vcs.ReleaseRangeReader, config.ReleaseDocs)"
provides:
  - "ComputeBump(fromRef, toRef) — semver bump computation via golang.org/x/mod/semver with v-prefix normalization and malformed-tag safety"
  - "BuildGroupedModel(commits, prs, cfg) — deterministic conventional/labels grouped change model with canonical section ordering"
  - "GroupedModel + ChangeSection + ChangeEntry — exported types representing the ordered grouped change model"
  - "BuildContext(ctx, provider, job, cfg, llm, model) — ReleaseContext builder: range read, FromRef resolution, Bump computation, GroupedModel (built once)"
  - "Enabled(artifactCfg, bump) — per-artifact enabled+when: gate (D-08)"
  - "ReleaseContext.GroupedModel field — pre-built model passed to every Generator (D-09 build-once)"
affects: [01-03, 01-04, 01-05, 01-06, 01-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deterministic section ordering via sort.SliceStable on canonical position index (Pitfall 3)"
    - "TDD: RED (failing test) → GREEN (implementation) → verify cycle"
    - "Hand-rolled Conventional Commit parser using strings.HasPrefix/strings.Cut (~30 lines, A5)"
    - "Enabled gate: enabled flag + when: condition matrix (D-08) in a single pure function"
    - "grouping.source=llm logs warning and falls back to conventional (OQ-1 resolution)"
    - "minimalProvider test double for capability-absent degradation tests (embedding limitation workaround)"

key-files:
  created:
    - internal/releasedocs/changemodel.go
    - internal/releasedocs/context.go
    - internal/releasedocs/changemodel_test.go
    - internal/releasedocs/context_test.go
  modified:
    - internal/releasedocs/releasedocs.go

key-decisions:
  - "ComputeBump treats malformed fromRef as BumpMajor (first-release) and malformed toRef as BumpNone rather than panicking (T-02-03 threat mitigation)"
  - "BuildGroupedModel omits sections with no entries (clean output for golden-file tests)"
  - "fallbackSection prefers 'Other' over last-section in canonical list for unmapped commits/PRs"
  - "GroupedModel added to ReleaseContext (not a separate struct) so generators receive the pre-built model without I/O (D-09 build-once principle)"
  - "minimalProvider inline test double used instead of releasedocstest.Fake for degradation tests — Fake's embedding promotes all methods including ReleaseRangeReader, preventing true capability-absent type assertions"
  - "llm grouping source: accepted in config enum, logs warning, falls back to conventional (Open Question 1 resolution)"

patterns-established:
  - "ComputeBump: normalize → IsValid check → Major/MajorMinor comparison (prevents panics on malformed tags)"
  - "BuildGroupedModel: posOf map for O(1) section lookup, sort.SliceStable for canonical order, skip empty sections"
  - "Enabled: pure function with explicit when: switch, logs unknown values rather than failing"

requirements-completed:
  - REQ-release-artifact-generation
  - REQ-per-artifact-toggles

# Metrics
duration: 35min
completed: 2026-06-05
---

# Phase 01 Plan 02: Grouped Change Model + Semver Bump + Enabled Gate Summary

**Deterministic grouped change model with semver bump computation, Conventional Commit parser, labels grouping, and per-artifact Enabled gate — the pure transform core that makes changelog golden-file tests possible**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-06-05T13:00:00Z
- **Completed:** 2026-06-05T13:35:00Z
- **Tasks:** 1 (TDD: RED + GREEN)
- **Files modified:** 5

## Accomplishments

- `ComputeBump` correctly handles all semver bump cases including first-release (empty fromRef → BumpMajor), malformed tags (safe degradation, T-02-03), tag normalization (v-prefix), and major/minor/patch detection
- `BuildGroupedModel` produces deterministic, golden-file-testable output: `sort.SliceStable` on canonical section position index ensures identical ordering on every run regardless of Go map iteration
- Hand-rolled Conventional Commit parser handles `feat:`, `fix:`, `perf:`, `feat!:`, `fix!:`, and `BREAKING CHANGE:` in body — ~40 lines, no library
- Labels grouping correctly assigns merged PRs by configured label→section map with "Other" fallback for unmapped labels and no-label PRs
- `Enabled` gate fully implements the D-08 matrix: enabled flag + `when:` conditions (always/major/minor/patch/minor_or_above/patch_or_above)
- `BuildContext` is the single entry point that assembles the full `ReleaseContext` with all fields including `GroupedModel` (built once, D-09)
- 58 tests pass under `-race -count=1`; 104 pass under `-count=2` (determinism verified)

## Task Commits

1. **RED (failing tests)** - `99c7c65` (test)
2. **GREEN (implementation)** - `9d92379` (feat)

## Files Created/Modified

- `internal/releasedocs/changemodel.go` - GroupedModel type + BuildGroupedModel + ComputeBump + helper functions
- `internal/releasedocs/context.go` - BuildContext + Enabled gate
- `internal/releasedocs/releasedocs.go` - Added GroupedModel field to ReleaseContext
- `internal/releasedocs/changemodel_test.go` - TestGroupedModel (conventional + labels + llm fallback + ordering)
- `internal/releasedocs/context_test.go` - TestBump + TestEnabledMatrix + TestBuildContext

## Decisions Made

- **malformed-tag handling**: ComputeBump returns BumpMajor (not error) for malformed fromRef (first-release treatment) and BumpNone for malformed toRef — no panics, per T-02-03 threat mitigation
- **GroupedModel in ReleaseContext**: Added as a struct field so the dispatcher builds it once and every generator reads the same pre-built model (D-09)
- **minimalProvider test double**: releasedocstest.Fake uses struct embedding which promotes all methods including ReleaseRangeReader ones, so type assertions always succeed regardless of `OmitRangeReader()`. Created an inline `minimalProvider` that genuinely doesn't implement the interface, enabling accurate degradation test
- **llm source fallback**: accepted in the config enum (`grouping.source: llm`), logs a `slog.Warn`, and falls back to conventional — matches graceful-degradation philosophy and Open Question 1 resolution

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed releasedocstest.Fake capability-absent test**
- **Found during:** GREEN phase (TestBuildContext degradation test)
- **Issue:** `releasedocstest.NewFake(OmitRangeReader())` returns a wrapper that embeds `*Fake`, which promotes all `*Fake` methods including `ListCommits`/`ListMergedPRs`/`LatestTagBefore`. This means `provider.(vcs.ReleaseRangeReader)` always succeeds on any wrapper, defeating the capability-absent test.
- **Fix:** Changed the degradation test to use an inline `minimalProvider` struct that implements only `vcs.Provider` — a genuinely non-implementing type where the type assertion correctly fails
- **Files modified:** `internal/releasedocs/context_test.go`
- **Verification:** `go test -run TestBuildContext/degrades` passes; test correctly asserts error from BuildContext
- **Committed in:** `9d92379` (part of GREEN commit)

---

**Total deviations:** 1 auto-fixed (Rule 1 - test correctness fix)
**Impact on plan:** The fix was necessary for the degradation test to be meaningful. No scope creep; the implementation behavior (returning an error when ReleaseRangeReader absent) is correct per the plan.

## Issues Encountered

- The worktree was initialized at `2760682` (older main commit) without the Plan 01-01 work. Merged `feat/release-docs-spec` (containing Plan 01-01 commits) via `git merge feat/release-docs-spec --no-edit` before starting implementation. Fast-forward merge succeeded cleanly.

## Known Stubs

None. All functions are fully implemented.

## Threat Flags

None. The implementation correctly mitigates T-02-03 (malformed semver tags yield safe BumpNone/BumpMajor rather than panic).

## Self-Check: PASSED

- `internal/releasedocs/changemodel.go` — exists
- `internal/releasedocs/context.go` — exists
- `internal/releasedocs/changemodel_test.go` — exists
- `internal/releasedocs/context_test.go` — exists
- `internal/releasedocs/releasedocs.go` — modified (GroupedModel field added)
- RED commit `99c7c65` — verified in git log
- GREEN commit `9d92379` — verified in git log
- All 58 tests pass: `go test ./internal/releasedocs/... -run 'TestBump|TestGroupedModel|TestEnabledMatrix' -race -count=1`
- `go vet ./internal/releasedocs/...` — clean
- `make lint` — 0 issues

## Next Phase Readiness

- `BuildGroupedModel` and `GroupedModel` types are ready for Plan 03 (changelog generator) and Plan 04 (release-notes generator) to consume
- `BuildContext` is ready for the dispatcher (Plan 05) to call
- `Enabled` gate is ready for every generator's `Enabled(cfg, bump)` method
- No blockers; all exported symbols have docstrings per `exported` revive rule

---
*Phase: 01-generators-publishers-cli*
*Completed: 2026-06-05*
