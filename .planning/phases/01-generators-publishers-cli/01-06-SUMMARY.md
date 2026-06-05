---
phase: "01-generators-publishers-cli"
plan: "06"
subsystem: "releasedocs/publishers"
tags: ["publisher", "idempotency", "markers", "vcs", "tdd"]
dependency_graph:
  requires: ["01-01", "01-04"]
  provides: ["releasebody publisher", "changelogpr publisher"]
  affects: ["internal/releasedocs/publishers"]
tech_stack:
  added: []
  patterns:
    - "marker splice/preserve (mirrors applyPRBody, spliceCadooBody)"
    - "stateless read-back idempotency (mirrors postSummary, priorStore)"
    - "optional capability type-assert + graceful degradation (D-15)"
    - "inline minimalProvider for degradation tests (fake embed workaround)"
key_files:
  created:
    - internal/releasedocs/publishers/releasebody/releasebody.go
    - internal/releasedocs/publishers/releasebody/releasebody_test.go
    - internal/releasedocs/publishers/changelogpr/changelogpr.go
    - internal/releasedocs/publishers/changelogpr/changelogpr_test.go
  modified: []
decisions:
  - "Use inline minimalProvider (not releasedocstest.NewFake(OmitX())) for degradation tests because wrapper embedding promotes all *Fake methods, defeating capability type-assertions (documented in plan-02)"
  - "OpenOrUpdatePR handles update-else-create internally at the BranchCommitter provider level using the deterministic branch as the invariant key"
  - "changelogpr.Publish prepends new section to existing CHANGELOG.md content; FetchFileFromRef failure → best-effort (section only, no prepend)"
metrics:
  duration: "5 minutes"
  completed: "2026-06-05T13:08:20Z"
  tasks_completed: 4
  files_changed: 4
---

# Phase 01 Plan 06: Publishers (releasebody + changelogpr) Summary

Marker-wrapped release-body upsert and single marker-keyed CHANGELOG.md PR publisher, both idempotent via stateless marker reconstruction and gracefully degrading when VCS capability is absent.

## What Was Built

### releasebody Publisher (`internal/releasedocs/publishers/releasebody/`)

`Publisher.Publish` type-asserts `vcs.ReleasePublisher` on the context provider. If absent, logs a `slog.Warn` and returns nil (D-15). Otherwise:

1. Finds the `KindReleaseNotes` artifact in the input slice.
2. Calls `GetReleaseByTag(rc.ToRef)` to read the current release body.
3. Calls `releasedocs.SpliceReleaseBody(current, section)` to inject the Cadoo-managed block between `ReleaseNotesBegin`/`ReleaseNotesEnd` markers while preserving user content outside the markers (D-12).
4. Calls `UpdateReleaseBody` ONLY if the spliced body differs from the current body (no-op guard; mirrors `applyPRBody`).

State is reconstructed entirely from the live release body marker — no DB (D-14).

### changelogpr Publisher (`internal/releasedocs/publishers/changelogpr/`)

`Publisher.Publish` type-asserts `vcs.BranchCommitter`. If absent, logs a `slog.Warn` and returns nil (D-15). Otherwise:

1. Finds the `KindChangelog` artifact.
2. Attempts to read the existing `CHANGELOG.md` from `rc.ToRef` via `releasedocs.FileFetcher`. On failure, logs a warning and proceeds best-effort (may miss prior content this run; mirrors ci.go priorStore degrade).
3. Prepends the new changelog section to any existing CHANGELOG content.
4. Calls `UpsertFile` on the deterministic branch `cadoo/changelog/vX.Y.Z` (D-13).
5. Calls `OpenOrUpdatePR` with a PR body containing the hidden marker `<!-- cadoo:changelog:vX.Y.Z -->`. The `BranchCommitter` contract guarantees update-else-create semantics using the deterministic branch as the invariant key — single-PR invariant prevents PR spam.

### TDD Gate Compliance

| Phase | Commit | Gate |
|-------|--------|------|
| RED - releasebody | `f96356c` | test(01-06): failing splice-preserve + degrade tests |
| GREEN - releasebody | `25b62ff` | feat(01-06): releasebody marker-upsert publisher |
| RED - changelogpr | `f54c0e1` | test(01-06): failing single-PR + degrade tests |
| GREEN - changelogpr | `124e3d6` | feat(01-06): changelogpr single-PR publisher |

## Verification

- `go test ./internal/releasedocs/publishers/... -race -count=1`: 5 passed (2 packages)
- `go test ./internal/releasedocs/... -race -count=1`: 75 passed (5 packages, including all prior plans)
- `go vet ./internal/releasedocs/publishers/...`: 0 issues
- `make lint`: 0 issues

### Tests

| Test | Package | Assertion |
|------|---------|-----------|
| `TestSplicePreserves` | releasebody | User content outside markers preserved; idempotent splice; no-op when unchanged |
| `TestReleaseBodyDegrades` | releasebody | Missing ReleasePublisher → nil return, no write |
| `TestSinglePR` | changelogpr | Deterministic branch; PR body contains marker; stable across two runs |
| `TestChangelogPRDegrades` | changelogpr | Missing BranchCommitter → nil return |
| `TestChangelogReadBackDegrade` | changelogpr | FetchFileFromRef failure → proceeds best-effort, UpsertFile still called |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] releasedocstest.NewFake(OmitX()) doesn't block type assertions**

- **Found during:** RED phase of releasebody (TestReleaseBodyDegrades)
- **Issue:** `releasedocstest.NewFake(OmitReleasePublisher())` returns a wrapper struct that embeds `*Fake` via Go struct embedding, which promotes ALL `*Fake` methods to the wrapper type. This means any wrapper type-asserted to `vcs.ReleasePublisher` (or `vcs.BranchCommitter`) will succeed regardless of the `OmitX()` option — the omit flag has no effect at runtime.
- **Root cause:** Already documented in plan-02 SUMMARY. The narrower wrappers in fake.go only have compile-time `vcs.Provider` assertions; they never shadow or restrict the promoted capability methods.
- **Fix:** Used an inline `minimalProvider` struct that implements ONLY `vcs.Provider` with all methods returning zero values. Type assertions to `vcs.ReleasePublisher` and `vcs.BranchCommitter` correctly return `(nil, false)` on this type.
- **Applied to:** Both `releasebody_test.go` and `changelogpr_test.go` degradation tests.
- **Files modified:** `releasebody_test.go`, `changelogpr_test.go`
- **Commits:** `25b62ff`, `124e3d6`

## Known Stubs

None. Both publishers are fully wired to their VCS capability interfaces.

## Threat Surface Scan

No new network endpoints or auth paths introduced. Both publishers write through the `vcs.BranchCommitter` / `vcs.ReleasePublisher` interfaces already covered by the plan's threat model (T-06-01 through T-06-05):

- T-06-01 (PR spam): mitigated by deterministic branch + hidden marker
- T-06-02 (clobber user content): mitigated by `SpliceReleaseBody` user-content preservation
- T-06-04 (repeated writes): mitigated by no-op-if-unchanged guard in releasebody

## Self-Check: PASSED

All 4 files created. All 4 commits verified:
- `f96356c` test(01-06): failing splice-preserve + degrade tests for releasebody
- `25b62ff` feat(01-06): releasebody marker-upsert publisher
- `f54c0e1` test(01-06): failing single-PR + degrade tests for changelogpr
- `124e3d6` feat(01-06): changelogpr single-PR publisher
