---
phase: 02-webhook-auto-trigger-state
plan: "04"
subsystem: releasedocs/publishers/pages
tags: [pages-publisher, branch-committer, idempotent, graceful-degradation, path-traversal]
dependency_graph:
  requires:
    - 02-01  # TargetPages constant, PagesPublishTarget config, BranchCommitter in vcs
  provides:
    - pages.Publisher implementing releasedocs.Publisher
  affects: []
tech_stack:
  added: []
  patterns:
    - "path.Join for traversal-safe path construction (T-02-07 mitigation)"
    - "type-assert optional capability then degrade (D-15 pattern)"
    - "UpsertFile as idempotency mechanism (same deterministic path on re-runs)"
key_files:
  created:
    - internal/releasedocs/publishers/pages/pages.go
    - internal/releasedocs/publishers/pages/pages_test.go
  modified: []
decisions:
  - "Used path.Join (not fmt.Sprintf) for all committed file paths to neutralize rc.ToRef path-traversal (T-02-07)"
  - "Mirrored minimalProvider pattern from changelogpr_test.go for capability-absent degradation test (releasedocstest.NewFake wrapping prevents accurate type-assertion negation)"
metrics:
  duration: "2m 43s"
  completed: "2026-06-05"
  tasks_completed: 2
  files_created: 2
---

# Phase 02 Plan 04: Pages Publisher Summary

Pages publisher commits each release artifact to a configured docs branch at deterministic paths via `vcs.BranchCommitter.UpsertFile`. Path traversal is neutralized by `path.Join`. Re-runs overwrite idempotently.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | TargetPages constant + pages.Publisher implementation | acab117 | internal/releasedocs/publishers/pages/pages.go |
| 2 | Tests — deterministic paths, idempotent overwrite, degradation, disabled | b7eb6bd | internal/releasedocs/publishers/pages/pages_test.go |

## What Was Built

**`internal/releasedocs/publishers/pages/pages.go`** — `pages.Publisher` implementing `releasedocs.Publisher`:
- `Target()` returns `releasedocs.TargetPages` (the constant from 02-01)
- `Publish()` iterates over artifacts and calls `bc.UpsertFile` for each non-empty one at `{dir}/releases/{toRef}/{kind}.md`
- Paths built with `path.Join` (never raw `fmt.Sprintf` with `rc.ToRef`) — T-02-07 mitigation
- Branch defaults to `"gh-pages"`, dir defaults to `"docs"` when config fields are empty
- Graceful degradation: when `BranchCommitter` absent, logs `slog.Warn` and returns nil (D-15)
- No-op when `publish.pages.enabled` is false
- Empty-content artifacts are skipped (no UpsertFile call)

**`internal/releasedocs/publishers/pages/pages_test.go`** — 7 test functions:
- `TestTarget` — verifies `Target()` returns `TargetPages`
- `TestDeterministicPaths` — verifies exact path strings `"docs/releases/v1.2.3/changelog.md"`, `release_notes.md`, `blog.md`
- `TestConfiguredBranchAndDir` — cfg.Branch="docs-site", cfg.Dir="site" → branch and path respected
- `TestIdempotentOverwrite` — two consecutive Publish calls produce identical paths
- `TestCapabilityAbsent` — provider without BranchCommitter yields nil error, zero UpsertFile calls
- `TestDisabled` — cfg.Enabled=false yields nil error, zero UpsertFile calls
- `TestEmptyContentSkipped` — empty content artifact produces no UpsertFile call

## Verification Results

```
go test -race -count=1 ./internal/releasedocs/publishers/pages/... → 7 passed
make lint → 0 issues
grep -c "path.Join" pages.go → 5 (traversal mitigation verified)
grep -c "releasedocs.TargetPages" pages.go → 1 (02-01 constant consumed, not redeclared)
grep -c "vcs/github|vcs/gitlab|orchestrator|internal/tools" pages.go → 0 (D-01 respected)
```

## Deviations from Plan

### Auto-fixed Issues

None — plan executed exactly as written.

**Note on `minimalProvider`:** The plan suggests reusing `releasedocstest.NewFake(OmitBranchCommitter())` for the capability-absent test, but the `releasedocstest.Fake` wrapper embeds `*Fake` which promotes all methods (including `UpsertFile`). This means type-assertions to `vcs.BranchCommitter` on the wrapped value still succeed. This was the same issue documented in the changelogpr plan's SUMMARY. Following the established pattern, a local `minimalProvider` struct implementing only `vcs.Provider` was used for the degradation test — identical to changelogpr_test.go approach.

## Known Stubs

None — all functionality is fully implemented and wired.

## Threat Flags

No new threat surface introduced. T-02-07 path-traversal mitigation is in place via `path.Join`.

## Self-Check: PASSED

- [x] `internal/releasedocs/publishers/pages/pages.go` exists
- [x] `internal/releasedocs/publishers/pages/pages_test.go` exists
- [x] Commit acab117 exists (feat(02-04))
- [x] Commit b7eb6bd exists (test(02-04))
- [x] Tests pass (7/7)
- [x] Lint clean (0 issues)
