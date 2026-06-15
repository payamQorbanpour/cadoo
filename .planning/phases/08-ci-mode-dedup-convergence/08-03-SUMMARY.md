---
phase: 08-ci-mode-dedup-convergence
plan: "03"
subsystem: vcs/orchestrator/tools
tags: [tdd, incremental-review, marker, security, infrastructure]
dependency_graph:
  requires:
    - 08-02 (PriorReview struct, PriorInline.Line/EndLine, NewFromPrior)
  provides:
    - RenderReviewedSHA + ParseReviewedSHA with 40-hex validation (internal/vcs/marker.go)
    - reviewedSHAPrefix/reviewedSHASuffix constants (internal/vcs/marker.go)
    - PriorReview.LastReviewedSHA field (internal/vcs/vcs.go)
    - DiffBetweener optional interface (internal/vcs/vcs.go)
    - renderConsolidated(sections, headSHA) with marker embed (internal/orchestrator/consolidate.go)
    - memoryStore.lastReviewedSHA field (internal/findings/findings.go)
    - NewFromPrior seeds lastReviewedSHA from pr.LastReviewedSHA (internal/findings/prior.go)
    - gitlab.Adapter.DiffBetween via Repositories.Compare (internal/vcs/gitlab/release.go)
    - github.Adapter.DiffBetween via CompareCommits (internal/vcs/github/release.go)
    - ListCadooArtifacts.LastReviewedSHA parse in both adapters
    - tools.Input.IncrementalFiles, IncrementalPacked, IsIncrementalRun (internal/tools/tools.go)
  affects:
    - internal/vcs/marker.go
    - internal/vcs/marker_test.go
    - internal/vcs/vcs.go
    - internal/orchestrator/consolidate.go
    - internal/orchestrator/consolidate_test.go
    - internal/orchestrator/reviewer.go
    - internal/findings/findings.go
    - internal/findings/prior.go
    - internal/tools/tools.go
    - internal/vcs/gitlab/gitlab.go
    - internal/vcs/gitlab/release.go
    - internal/vcs/github/github.go
    - internal/vcs/github/release.go
tech_stack:
  added: []
  patterns:
    - TDD RED/GREEN: failing marker tests committed before implementation
    - 40-hex regexp validation for attacker-influenceable input (ASVS V5 / T-08-C1)
    - Pitfall 5: marker placed INSIDE wrapper after SummaryWrapperBegin (T-08-C3)
    - (nil,nil) conservative fallback for non-ancestor DiffBetween errors (T-08-C2 / Assumption A3)
    - Optional-capability interface pattern (mirrors ReleaseRangeReader)
    - Compile-time interface assertions in both adapter release.go files
    - Additive Input fields following KBHits/Learnings/Issues comment+declaration style
key_files:
  created: []
  modified:
    - internal/vcs/marker.go
    - internal/vcs/marker_test.go
    - internal/vcs/vcs.go
    - internal/orchestrator/consolidate.go
    - internal/orchestrator/consolidate_test.go
    - internal/orchestrator/reviewer.go
    - internal/findings/findings.go
    - internal/findings/prior.go
    - internal/tools/tools.go
    - internal/vcs/gitlab/gitlab.go
    - internal/vcs/gitlab/release.go
    - internal/vcs/github/github.go
    - internal/vcs/github/release.go
decisions:
  - "ParseReviewedSHA validates exactly 40 lowercase hex chars via regexp; any other input returns \"\" → full-review fallback (ASVS V5 / T-08-C1)"
  - "reviewed-sha marker placed INSIDE wrapper after wrapperBegin so SummaryWrapperBegin remains the first wrapper token (Pitfall 5 / T-08-C3)"
  - "DiffBetween returns (nil,nil) on ANY API error including non-ancestor, force-push, or forged SHA — conservative fallback to full review (Assumption A3)"
  - "lastReviewedSHA stored as unexported field on memoryStore; Plan 04 will add Store.LastReviewedSHA() accessor + orchestrator wiring"
  - "tools.Input incremental fields are purely additive; no existing field removed or renamed (SPEC dual-context approach)"
metrics:
  duration: "25 minutes"
  completed: "2026-06-15T12:45:00Z"
  tasks_completed: 3
  files_modified: 13
---

# Phase 08 Plan 03: Incremental Review Infrastructure Summary

## One-liner

Implements the stateless incremental-review plumbing: the `<!-- cadoo:reviewed-sha:<sha> -->` marker with strict 40-hex validation, `PriorReview.LastReviewedSHA`, `DiffBetweener` optional interface in both adapters, `LastReviewedSHA` parse-back in both `ListCadooArtifacts`, and additive `tools.Input` incremental-context fields.

## What Was Built

### Task 1 — reviewed-sha marker + consolidate embed [TDD RED → GREEN]

**RED (commit 6c0f74e):** Added three failing tests to `marker_test.go` (`TestRenderReviewedSHA`, `TestParseReviewedSHA`) and `consolidate_test.go` (`TestRenderConsolidatedEmbedsReviewedSHA`). Updated existing `renderConsolidated` call sites in `consolidate_test.go` to pass a second `""` arg (intentionally fails until GREEN). Verified build failure confirmed RED state.

**GREEN (commit 1405b0e):** Seven surgical changes:

1. **`internal/vcs/marker.go`**: Added `reviewedSHAPrefix`, `reviewedSHASuffix` constants adjacent to `SummaryWrapperBegin`, a `reviewedSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)` validator, and two exported functions:
   - `RenderReviewedSHA(sha string) string` — prefix + sha + suffix
   - `ParseReviewedSHA(body string) string` — locate substring, extract, validate 40-hex; return "" on tampered/malformed input (ASVS V5 / T-08-C1)

2. **`internal/vcs/vcs.go`**: Added `LastReviewedSHA string` to `PriorReview` with a docstring explaining the validation contract.

3. **`internal/orchestrator/consolidate.go`**: Changed `renderConsolidated` signature to `(sections []findings.Section, headSHA string)`; emits `vcs.RenderReviewedSHA(headSHA) + "\n"` immediately after `wrapperBegin` when `headSHA != ""` (T-08-C3: marker stays INSIDE wrapper so `SummaryWrapperBegin` remains the first wrapper token).

4. **`internal/orchestrator/reviewer.go`**: Updated production call site to pass `pr.HeadSHA`.

5. **`internal/findings/findings.go`**: Added `lastReviewedSHA string` field to `memoryStore` with a doc comment (Plan 04 will add the `Store.LastReviewedSHA()` accessor).

6. **`internal/findings/prior.go`**: In `NewFromPrior`, seeded `m.lastReviewedSHA = pr.LastReviewedSHA` adjacent to the `SummaryCommentID` guard.

All three named tests pass; 86 tests pass across `vcs`, `orchestrator`, `findings` packages.

### Task 2 — DiffBetweener interface + both adapter implementations [compile-time verified]

**Commit da2c5b8:**

1. **`internal/vcs/vcs.go`**: Added `DiffBetweener` optional interface mirroring `ReleaseRangeReader` doc-comment style, with a tri-value return contract:
   - `([]FileChange, nil)` — valid diff (empty slice = no changed files)
   - `(nil, nil)` — non-ancestor / force-push / unknown SHA → full-review fallback
   - `(nil, err)` — unexpected failure → full-review fallback

2. **`internal/vcs/gitlab/release.go`**: `DiffBetween` reuses `Repositories.Compare(repo, &glab.CompareOptions{From: ptr(oldSHA), To: ptr(newSHA)}, glab.WithContext(ctx))` from `ListCommits`, converts `cmp.Diffs ([]*glab.Diff)` to `[]vcs.FileChange`. Any API error returns `(nil, nil)`. Added `var _ vcs.DiffBetweener = (*Adapter)(nil)` to compile-time assertion block.

3. **`internal/vcs/github/release.go`**: `DiffBetween` reuses `CompareCommits(ctx, owner, name, oldSHA, newSHA, nil)` from `ListCommits`, converts `cmp.Files ([]*gogithub.CommitFile)` to `[]vcs.FileChange` using the same `f.GetFilename()` / `f.GetStatus()` pattern as `ListChangedFiles`. Any API error (including 404 non-ancestor) returns `(nil, nil)`. Added `var _ vcs.DiffBetweener = (*Adapter)(nil)` to compile-time assertion block.

4. **`internal/vcs/gitlab/gitlab.go`**: In `ListCadooArtifacts` summary-detection branch, added `out.LastReviewedSHA = vcs.ParseReviewedSHA(n.Body)`.

5. **`internal/vcs/github/github.go`**: In `ListCadooArtifacts` issue-comment loop, added `if out.LastReviewedSHA == "" { out.LastReviewedSHA = vcs.ParseReviewedSHA(c.Body) }` guarded by the existing `SummaryWrapperBegin` check.

All 495 tests pass; `go build ./...` enforces both compile-time assertions.

### Task 3 — Additive tools.Input incremental-context fields

**Commit 991bf8a:**

Added three fields to `tools.Input` following the `KBHits`/`Learnings`/`Issues` comment-then-declaration style:

```go
IncrementalFiles  []vcs.FileChange
IncrementalPacked contextengine.Compressed
IsIncrementalRun  bool
```

All three are nil/false on first run, after force-push, or when the provider lacks `DiffBetweener`. Inline tools prefer `IncrementalFiles/Packed` when `IsIncrementalRun` is true; summary-only tools always use `Files/Packed`. No existing field removed or renamed.

## Commits

| Task | Commit | Type | Description |
|------|--------|------|-------------|
| 1 RED | 6c0f74e | test | Add failing tests for reviewed-sha marker and consolidated embed (RED) |
| 1 GREEN | 1405b0e | feat | reviewed-sha marker, PriorReview.LastReviewedSHA, consolidated embed |
| 2 | da2c5b8 | feat | DiffBetweener interface + both adapter implementations + LastReviewedSHA parse |
| 3 | 991bf8a | feat | Add IncrementalFiles, IncrementalPacked, IsIncrementalRun to tools.Input |

## Verification Results

- `go build ./...`: PASS
- `go test -race -count=1 ./...`: 495 passed in 70 packages
- `go vet ./...`: clean
- `golangci-lint run ./internal/vcs/... ./internal/orchestrator/... ./internal/findings/... ./internal/tools/...`: 0 issues
- `TestRenderReviewedSHA`: PASS
- `TestParseReviewedSHA`: PASS (6 sub-cases including uppercase/non-hex/wrong-length rejection)
- `TestRenderConsolidatedEmbedsReviewedSHA`: PASS (marker inside wrapper; ParseReviewedSHA round-trips; empty headSHA emits no marker)
- `git diff db/migrations`: empty (no schema changes)
- Compile-time assertions: `var _ vcs.DiffBetweener = (*Adapter)(nil)` in both `release.go` files enforced by `go build`

## Deviations from Plan

None — plan executed exactly as written.

## Threat Model Coverage

| Threat ID | Mitigation | Status |
|-----------|------------|--------|
| T-08-C1 | `ParseReviewedSHA` validates exactly 40 lowercase hex chars; non-hex/wrong-length/uppercase → "" → full-review fallback | Implemented in Task 1; tested by `TestParseReviewedSHA` |
| T-08-C2 | `DiffBetween` invoked with already-authorized `repo`; SHA only selects which intra-repo diff; non-ancestor/unknown SHA → API error → `(nil,nil)` fallback | Implemented in Task 2 |
| T-08-C3 | Marker placed INSIDE wrapper after `SummaryWrapperBegin`; existing `strings.Contains(body, SummaryWrapperBegin)` detection still triggers; asserted by `TestRenderConsolidatedEmbedsReviewedSHA` | Implemented in Task 1 |
| T-08-SC | No new package installs (RESEARCH audit: reuses existing go-github / go-gitlab) | N/A |

## Known Stubs

None — `lastReviewedSHA` is seeded from real VCS data via `ParseReviewedSHA`; `DiffBetween` calls real VCS APIs. Plan 04 will add the `Store.LastReviewedSHA()` accessor and orchestrator wiring that consumes these fields — that is by design, not a stub.

## Threat Flags

None — `DiffBetween` calls the VCS provider's compare API using the already-authorized `repo` parameter. No new network endpoints, auth paths, schema changes, or trust boundaries introduced beyond what the plan's threat model documents.

## Self-Check: PASSED

Files confirmed present:
- `internal/vcs/marker.go`: FOUND (RenderReviewedSHA + ParseReviewedSHA added)
- `internal/vcs/marker_test.go`: FOUND (TestRenderReviewedSHA + TestParseReviewedSHA added)
- `internal/vcs/vcs.go`: FOUND (PriorReview.LastReviewedSHA + DiffBetweener added)
- `internal/orchestrator/consolidate.go`: FOUND (renderConsolidated signature updated)
- `internal/orchestrator/consolidate_test.go`: FOUND (TestRenderConsolidatedEmbedsReviewedSHA added)
- `internal/orchestrator/reviewer.go`: FOUND (pr.HeadSHA passed to renderConsolidated)
- `internal/findings/findings.go`: FOUND (memoryStore.lastReviewedSHA field added)
- `internal/findings/prior.go`: FOUND (lastReviewedSHA seeded in NewFromPrior)
- `internal/tools/tools.go`: FOUND (IncrementalFiles/Packed/IsIncrementalRun added)
- `internal/vcs/gitlab/gitlab.go`: FOUND (LastReviewedSHA parse in ListCadooArtifacts)
- `internal/vcs/gitlab/release.go`: FOUND (DiffBetween + compile-time assertion)
- `internal/vcs/github/github.go`: FOUND (LastReviewedSHA parse in ListCadooArtifacts)
- `internal/vcs/github/release.go`: FOUND (DiffBetween + compile-time assertion)

Commits confirmed:
- 6c0f74e: FOUND (test RED)
- 1405b0e: FOUND (feat Task 1 GREEN)
- da2c5b8: FOUND (feat Task 2)
- 991bf8a: FOUND (feat Task 3)
