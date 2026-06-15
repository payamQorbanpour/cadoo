---
phase: 01-generators-publishers-cli
plan: "05"
subsystem: releasedocs/generators
tags:
  - changelog
  - release-notes
  - golden-file
  - tdd
  - deterministic
  - llm-nil-tolerant
dependency_graph:
  requires:
    - 01-02  # releasedocs.Enabled gate, BuildGroupedModel, GroupedModel
    - 01-03  # template.Resolve, template.Render, preset templates
  provides:
    - internal/releasedocs/generators/changelog  # changelog.Generator
    - internal/releasedocs/generators/releasenotes  # releasenotes.Generator
    - testdata/basic.golden  # first golden-file fixture in the repo
  affects:
    - downstream dispatcher (plan 06) that will register these generators
tech_stack:
  added:
    - golden-file test convention (testdata/basic.golden with -update flag)
  patterns:
    - deterministic-first render (template.Resolve+Render, then optional LLM polish)
    - nil-tolerant LLM (rc.LLM == nil guard before Chat call)
    - TDD RED/GREEN cycle (failing tests committed before implementation)
key_files:
  created:
    - internal/releasedocs/generators/changelog/changelog.go
    - internal/releasedocs/generators/changelog/changelog_test.go
    - internal/releasedocs/generators/changelog/testdata/basic.golden
    - internal/releasedocs/generators/releasenotes/releasenotes.go
    - internal/releasedocs/generators/releasenotes/releasenotes_test.go
  modified: []
decisions:
  - "changelog generator returns deterministic render verbatim when rc.LLM==nil; polish pass is a separate step so golden tests are immune to LLM non-determinism"
  - "release-notes generator falls back to skeleton on LLM failure (non-fatal) to honor D-10"
  - "stripConventionalPrefix applied in changelog buildTemplateData to clean up entry titles for rendering"
  - "release-notes buildTemplateData does NOT strip prefixes — raw commit subjects preserved for LLM narrative input"
metrics:
  duration_minutes: 18
  completed_date: "2026-06-05"
  tasks_completed: 4
  files_created: 5
---

# Phase 01 Plan 05: Changelog and Release-Notes Generators Summary

Implemented two Phase-1 generators for the release-docs subsystem: a deterministic-first changelog generator with optional LLM polish and a release-notes generator that builds a deterministic skeleton and calls LLM once for a tone-aware narrative when a provider is present.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | RED: changelog failing tests | ee676fc | changelog_test.go, testdata/basic.golden (placeholder), releasenotes_test.go |
| 2 | RED: release-notes failing tests | ee676fc | (same commit — both test suites confirmed failing) |
| 3 | GREEN: changelog + release-notes implementation | dbb1649 | changelog.go, releasenotes.go, updated test + golden |

## What Was Built

### changelog generator (`internal/releasedocs/generators/changelog`)

- `Generator` struct implementing `releasedocs.Generator` interface.
- `Kind()` returns `KindChangelog`.
- `Enabled(cfg, bump)` delegates to `releasedocs.Enabled(cfg.Artifacts.Changelog, bump)` (Plan 02 gate).
- `Generate(ctx, rc)` renders `rc.GroupedModel` deterministically via `template.Resolve` + `template.Render` (Plan 03). When `rc.LLM != nil`, a single polish pass is performed via `polishWithLLM` (T-05-01: polish cannot add/remove entries). When `rc.LLM == nil`, the deterministic render is returned verbatim.
- `stripConventionalPrefix` removes `feat:`, `fix:`, `perf:` etc. from entry titles before rendering.
- Golden file `testdata/basic.golden` established as the repo's first golden-file convention. Regenerate via `-update` flag.

### release-notes generator (`internal/releasedocs/generators/releasenotes`)

- `Generator` struct implementing `releasedocs.Generator` interface.
- `Kind()` returns `KindReleaseNotes`.
- `Enabled(cfg, bump)` delegates to `releasedocs.Enabled(cfg.Artifacts.ReleaseNotes.ArtifactConfig, bump)`.
- `Generate(ctx, rc)` builds the deterministic skeleton from `rc.GroupedModel` using the tone-keyed preset (Plan 03: `template.Resolve` with `rc.Config.Artifacts.ReleaseNotes.Tone`). When `rc.LLM != nil`, calls `Chat` once via `narrateWithLLM` with a tone-appropriate system prompt (concise/detailed/marketing). When `rc.LLM == nil`, skeleton returned verbatim.
- `rc.Model` passed through to Chat with no second default-model path (D-17).

### Test coverage

- 12 changelog tests (golden, determinism, enabled gate table, nil-LLM skip).
- 14 release-notes tests (nil-LLM skeleton determinism, tone variants ×4, enabled gate table).
- All 26 tests pass under `-race -count=1`.

## Verification

```
go test ./internal/releasedocs/generators/... -race -count=1
# 26 passed
go vet ./internal/releasedocs/generators/...
# no issues
golangci-lint run ./internal/releasedocs/generators/...
# No issues found
```

Golden test is byte-stable across repeated runs (confirmed with `-count=3`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Removed unused stubLLM from changelog_test.go**
- **Found during:** GREEN phase lint check
- **Issue:** `stubLLM` struct and its `Chat` method were written in the RED phase test file but not used — `TestChangelogLLMPolishSkipped` only tests with nil LLM and doesn't need a fake provider. golangci-lint (`unused` rule) flagged it.
- **Fix:** Removed `stubLLM` type declaration and `Chat` method; removed unused `llm` import.
- **Files modified:** `internal/releasedocs/generators/changelog/changelog_test.go`
- **Commit:** dbb1649

None other — plan executed as written.

## Known Stubs

None. Both generators produce real output from the grouped model.

## Threat Flags

No new trust boundaries introduced. Both generators operate entirely within the existing `releasedocs` subsystem threat model (T-05-01, T-05-02, T-05-03 from plan's STRIDE register):
- T-05-01 (prompt injection): commit/PR text flows as delimited data in the prompt; the deterministic skeleton controls which entries appear — the LLM cannot fabricate or omit changelog entries.
- T-05-02 (non-deterministic output): mitigated — LLM polish is a separate step; golden tests run with nil LLM.
- T-05-03 (model string in logs): rc.Model passed through; no token logged.

## TDD Gate Compliance

- RED gate: commit `ee676fc` — `test(01-05): failing golden + enabled + nil-LLM tests`
- GREEN gate: commit `dbb1649` — `feat(01-05): deterministic-first changelog generator and release-notes generator`
- Both gates present in git log. No REFACTOR commit needed (code is clean post-GREEN).

## Self-Check: PASSED

Files exist:
- internal/releasedocs/generators/changelog/changelog.go: FOUND
- internal/releasedocs/generators/changelog/changelog_test.go: FOUND
- internal/releasedocs/generators/changelog/testdata/basic.golden: FOUND
- internal/releasedocs/generators/releasenotes/releasenotes.go: FOUND
- internal/releasedocs/generators/releasenotes/releasenotes_test.go: FOUND

Commits exist:
- ee676fc: FOUND (test RED)
- dbb1649: FOUND (feat GREEN)
