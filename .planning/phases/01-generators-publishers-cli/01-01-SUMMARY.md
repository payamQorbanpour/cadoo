---
phase: 01-generators-publishers-cli
plan: "01"
subsystem: releasedocs
tags: [interfaces, vcs, config, releasedocs, types, fake, testing]
dependency_graph:
  requires: []
  provides:
    - releasedocs.Generator
    - releasedocs.Publisher
    - releasedocs.FileFetcher
    - releasedocs.ReleaseContext
    - releasedocs.ReleaseJob
    - releasedocs.Artifact
    - releasedocs.ArtifactKind
    - releasedocs.PublishTarget
    - releasedocs.SemverBump
    - releasedocs.GeneratorRegistry
    - releasedocs.PublisherRegistry
    - releasedocs.ReleaseNotesBegin/End marker constants
    - releasedocs.SpliceReleaseBody
    - releasedocs.HasChangelogMarker
    - releasedocs.ChangelogMarker/Branch
    - vcs.ReleaseRangeReader
    - vcs.ReleasePublisher
    - vcs.BranchCommitter
    - vcs.Commit
    - vcs.MergedPR
    - vcs.Release
    - vcs.FileWrite
    - config.ReleaseDocs (schema + all Phase-1 keys)
    - releasedocstest.Fake (importable test double)
  affects:
    - internal/vcs/vcs.go
    - internal/config/config.go
tech_stack:
  added:
    - golang.org/x/mod v0.36.0 (semver operations for Phase-1 context builder)
  patterns:
    - Optional capability interfaces on vcs.Provider (PriorReviewReader pattern)
    - Locked HTML-comment markers for idempotent splicing
    - Functional-options fake with narrowing wrapper types for capability-toggle testing
key_files:
  created:
    - internal/releasedocs/releasedocs.go
    - internal/releasedocs/marker.go
    - internal/releasedocs/releasedocstest/fake.go
  modified:
    - internal/vcs/vcs.go
    - internal/config/config.go
    - go.mod
    - go.sum
decisions:
  - "config.ReleaseDocs added in Task 2 commit (not Task 3) because releasedocs.go references the type — required to compile. No architectural impact."
  - "NewFake returns (*Fake, vcs.Provider) tuple so tests can both access raw call counters and pass the narrowed interface to consumers."
  - "PublishTarget config struct (ReleasePublish.ReleaseBody etc.) uses a custom PublishTarget type with only Enabled field, distinct from releasedocs.PublishTarget enum — avoids name collision at call sites."
metrics:
  duration_minutes: 18
  completed_date: "2026-06-05"
  tasks_completed: 3
  files_created: 3
  files_modified: 4
---

# Phase 1 Plan 01: Contract Layer (vcs + releasedocs interfaces + config + test fake) Summary

**One-liner:** Defined Release Docs contract layer — Generator/Publisher/FileFetcher interfaces, ReleaseRangeReader/ReleasePublisher/BranchCommitter optional vcs capabilities, locked idempotency markers, config.ReleaseDocs schema, and importable releasedocstest.Fake with 16-combination capability-toggle wrappers.

## What Was Built

This plan delivers the Phase-1 contract layer that all subsequent plans (context builder, generators, publishers, CLI dispatcher) implement against:

### internal/vcs/vcs.go (modified)
- Added `Commit`, `MergedPR`, `Release`, `FileWrite` normalized types (mirror PullRequest/FileChange docstring + field style)
- Added three optional capability interfaces: `ReleaseRangeReader` (ListCommits/ListMergedPRs/LatestTagBefore), `ReleasePublisher` (GetReleaseByTag/UpdateReleaseBody), `BranchCommitter` (UpsertFile/OpenOrUpdatePR)
- Each interface documented as OPTIONAL with graceful degradation semantics (D-15)

### internal/releasedocs/releasedocs.go (created)
- `ArtifactKind` enum: KindChangelog, KindReleaseNotes
- `PublishTarget` enum: TargetReleaseBody, TargetChangelogPR
- `SemverBump` enum: BumpMajor/Minor/Patch/None
- `Artifact` struct: Kind + Content
- `ReleaseJob` struct with `Kind() string` method (River-ready)
- `ReleaseContext` struct: all fields per D-04 (nil-tolerant LLM)
- `FileFetcher` interface: own declaration so releasedocs never imports orchestrator (D-01)
- `Generator` interface: Kind/Enabled/Generate
- `Publisher` interface: Target/Publish
- `GeneratorRegistry` and `PublisherRegistry` (mirror tools.Registry shape)

### internal/releasedocs/marker.go (created)
- `ReleaseNotesBegin` / `ReleaseNotesEnd` locked exported constants
- `ChangelogMarker(toRef)` → `<!-- cadoo:changelog:<toRef> -->`
- `ChangelogBranch(toRef)` → `cadoo/changelog/<toRef>`
- `SpliceReleaseBody(original, section)` — replace-inner-else-append idempotent splice
- `HasChangelogMarker(body, toRef)` — strings.Contains grep for PR idempotency check

### internal/config/config.go (modified)
- Added `ReleaseDocs ReleaseDocs` field to `Repo` struct with `yaml:"releaseDocs"` tag
- Added full Phase-1 config schema: `ReleaseDocs`, `ReleaseArtifacts`, `ArtifactConfig`, `ReleaseNotesConfig`, `ReleaseGrouping`, `ReleasePublish`, `PublishTarget` structs
- Phase-2/3 keys (`pages`) included as forward-compatible fields with docstrings noting future wiring
- No new parser needed — `config.Parse` (yaml.Unmarshal) handles automatically

### internal/releasedocs/releasedocstest/fake.go (created)
- `Fake` struct implementing all 5 interfaces: vcs.Provider + 3 optional capabilities + releasedocs.FileFetcher
- `NewFake(opts ...Option) (*Fake, vcs.Provider)` with functional options (OmitRangeReader, OmitReleasePublisher, OmitBranchCommitter, OmitFileFetcher, WithKind)
- 15 narrowing wrapper types (+ full Fake = 16 combinations) so type-assertions fail exactly for omitted capabilities
- Exported call counters: ListCommitsCalls, UpdateReleaseBodyCalls, etc.
- Exported captured values: CapturedReleaseBody, CapturedPRBody, CapturedBranch, CapturedFiles
- Configurable return values: Commits, MergedPRs, LatestTag, Release, PRNumber, FileContent
- Compile-time assertions for Fake and all 15 wrapper types against vcs.Provider

### go.mod / go.sum
- Added `golang.org/x/mod v0.36.0` as a direct dependency (verified official Go module, checkpoint T-01-SC)

## Task Commits

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 0 | Verify golang.org/x/mod (approved by orchestrator) | N/A — pre-approved | — |
| 1 | vcs capability interfaces + normalized release types + semver dep | 5ba635d | internal/vcs/vcs.go, go.mod, go.sum |
| 2 | releasedocs core types/interfaces + FileFetcher + marker constants | d4acaee | internal/releasedocs/releasedocs.go, internal/releasedocs/marker.go, internal/config/config.go |
| 3 | config.ReleaseDocs schema + shared importable fake provider | 6cf9166 | internal/releasedocs/releasedocstest/fake.go |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] config.ReleaseDocs added in Task 2 commit instead of Task 3**
- **Found during:** Task 2 compilation
- **Issue:** `internal/releasedocs/releasedocs.go` references `config.ReleaseDocs` in the `ReleaseContext` struct and `Generator.Enabled` signature. Since `config.ReleaseDocs` was planned for Task 3, the build failed with "undefined: config.ReleaseDocs".
- **Fix:** Added `config.ReleaseDocs` and all nested types to `internal/config/config.go` as part of the Task 2 commit. Task 3 focused solely on the `releasedocstest/fake.go` file as intended.
- **Files modified:** internal/config/config.go
- **Commit:** d4acaee (included with Task 2)
- **Architectural impact:** None — this is just commit ordering. The Task 3 verification criteria (grep -q "ReleaseDocs" config.go) still passes.

## Known Stubs

None — this plan delivers interface/type definitions and a test fake. No data flows to UI rendering, no placeholder text used.

## Threat Flags

None — new files are pure type declarations, constants, and a test helper. No new network endpoints, auth paths, file access patterns, or DB schema changes introduced.

## Self-Check: PASSED

All created files exist on disk. All task commits (5ba635d, d4acaee, 6cf9166) verified in git log.
