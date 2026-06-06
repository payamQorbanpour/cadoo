---
phase: 02-webhook-auto-trigger-state
plan: "01"
subsystem: vcs-interfaces, releasedocs-publishers, config
tags:
  - vcs
  - optional-capability-interface
  - cr-01-fix
  - config-schema
  - tdd

dependency_graph:
  requires:
    - 01-07 (releasebody publisher baseline, Fake test helper)
  provides:
    - vcs.TagReleasePublisher interface (consumed by plans 02-02, 02-05)
    - CR-01 fix: GitLab releasebody publish now routes through UpdateReleaseBodyByTag
    - config.ReleaseDocs.Artifacts.Blog (consumed by plan 02-03)
    - config.ReleaseDocs.Publish.Pages as PagesPublishTarget with Branch/Dir (consumed by plan 02-04)
    - KindBlog + TargetPages constants in releasedocs package (consumed by 02-03, 02-04)
  affects:
    - internal/vcs (new optional interface)
    - internal/config (Pages type change — whole-module build verified clean)
    - internal/releasedocs/publishers/releasebody (CR-01 fix + new tests)

tech_stack:
  added: []
  patterns:
    - Optional capability interface pattern (mirrors ReleasePublisher, BranchCommitter)
    - Type-assert TagReleasePublisher only when rel.ID==0 AND rel.TagName!="" (threat T-02-01 mitigated)

key_files:
  created: []
  modified:
    - internal/vcs/vcs.go
    - internal/vcs/gitlab/release.go
    - internal/vcs/github/release.go
    - internal/releasedocs/publishers/releasebody/releasebody.go
    - internal/releasedocs/publishers/releasebody/releasebody_test.go
    - internal/config/config.go
    - internal/releasedocs/releasedocs.go

decisions:
  - "TagReleasePublisher type-assertion guarded by rel.ID==0 AND rel.TagName!='' (not by provider.Kind()) — keeps the publisher provider-agnostic and avoids importing vcs/gitlab or vcs/github"
  - "GitHub adapter implements TagReleasePublisher by resolving via GetReleaseByTag then delegating to numeric-ID UpdateReleaseBody — uniform type-assertion across providers without any special-casing"
  - "PagesPublishTarget replaces PublishTarget for Pages field — more specific type prevents accidental omission of Branch/Dir in pages publisher (02-04)"

metrics:
  duration_minutes: 4
  completed_date: "2026-06-05"
  tasks_completed: 3
  files_modified: 7
---

# Phase 02 Plan 01: TagReleasePublisher Interface + CR-01 Fix + Config Schema Summary

**One-liner:** vcs.TagReleasePublisher optional interface fixes GitLab releasebody hard-fail (CR-01); Blog artifact config and PagesPublishTarget struct unblock parallel Wave 2 plans.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Add TagReleasePublisher interface + assert on both adapters | a7e9a2b | vcs.go, gitlab/release.go, github/release.go |
| 2 (RED) | Add failing tests for CR-01 GitLab tag-path fix | 530bbc3 | releasebody_test.go |
| 2 (GREEN) | CR-01 fix in releasebody publisher | 3f65e4d | releasebody.go, releasebody_test.go |
| 3 | Config schema (Blog + PagesPublishTarget) + constants | 3919769 | config.go, releasedocs.go, releasebody_test.go |

## Decisions Made

1. `TagReleasePublisher` declared in `vcs.go` as an OPTIONAL capability interface alongside `ReleasePublisher` and `BranchCommitter`. The releasebody publisher uses a type assertion only when `rel.ID == 0 && rel.TagName != ""`, which is the GitLab invariant from `GetReleaseByTag`. This avoids provider-specific code in the publisher package (no import cycle).

2. GitHub's `UpdateReleaseBodyByTag` resolves the release by tag (via the existing `GetReleaseByTag`) then calls `UpdateReleaseBody` with the numeric ID. This is a thin delegation; both adapters satisfy `TagReleasePublisher` uniformly, allowing the releasebody publisher to type-assert once without branching on provider kind.

3. `PagesPublishTarget` is a new struct (not an extension of `PublishTarget`) so the pages publisher in plan 02-04 gets compile-time access to `Branch` and `Dir` without type gymnastics.

## Deviations from Plan

None — plan executed exactly as written.

## TDD Gate Compliance

Task 2 followed the RED/GREEN cycle:
- RED commit `530bbc3`: 4 new tests, 2 failing (`TestGitLabPath`, `TestFallbackError`) — confirmed by test run
- GREEN commit `3f65e4d`: implementation added, all 6 tests pass

## Verification

- `go build ./...` exits 0 — Pages type change propagates cleanly across whole module
- `go test -race -count=1 ./internal/releasedocs/publishers/releasebody/... ./internal/config/... ./internal/vcs/...` — 42 tests pass
- `golangci-lint run ./internal/vcs/... ./internal/config/... ./internal/releasedocs/...` — 0 issues

## Known Stubs

None — all new fields are pure schema additions (zero-value disabled, as intended). No UI rendering, no placeholder text.

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries beyond what the plan's threat model covers. T-02-01 (tag name from GetReleaseByTag, provider-validated) mitigated as planned.

## Self-Check

Files created/modified:
- [x] internal/vcs/vcs.go — FOUND
- [x] internal/vcs/gitlab/release.go — FOUND
- [x] internal/vcs/github/release.go — FOUND
- [x] internal/releasedocs/publishers/releasebody/releasebody.go — FOUND
- [x] internal/releasedocs/publishers/releasebody/releasebody_test.go — FOUND
- [x] internal/config/config.go — FOUND
- [x] internal/releasedocs/releasedocs.go — FOUND

Commits:
- [x] a7e9a2b — FOUND
- [x] 530bbc3 — FOUND
- [x] 3f65e4d — FOUND
- [x] 3919769 — FOUND

## Self-Check: PASSED
