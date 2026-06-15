---
phase: 08-ci-mode-dedup-convergence
plan: "02"
subsystem: findings/vcs
tags: [tdd, bugfix, dedup, ci-mode, resolved-threads]
dependency_graph:
  requires:
    - 08-01 (PostedFinding.StructuralKey, resolveStalePriors fix)
  provides:
    - PriorInline.Line and PriorInline.EndLine fields (internal/vcs/vcs.go)
    - findingRec.Resolved and findingRec.Line fields (internal/findings/findings.go)
    - ResolvedSuppressThreshold const = 0.3 (internal/findings/findings.go)
    - memoryStore.has resolved-prior branch with line-overlap + lower Jaccard (internal/findings/findings.go)
    - NewFromPrior seeds Resolved and Line into findingRec (internal/findings/prior.go)
    - gitlab.go ListCadooArtifacts populates Line/EndLine from n.Position.NewLine (internal/vcs/gitlab/gitlab.go)
    - github.go GraphQL query adds line/originalLine; node struct uses *int; nil-safe deref (internal/vcs/github/github.go)
    - TestMemoryStoreHasResolvedSuppressesSameFile + two guardrail tests (internal/findings/findings_test.go)
    - TestNewFromPriorCarriesResolvedAndLine (internal/findings/prior_test.go)
  affects:
    - internal/vcs/vcs.go
    - internal/findings/findings.go
    - internal/findings/findings_test.go
    - internal/findings/prior.go
    - internal/findings/prior_test.go
    - internal/vcs/gitlab/gitlab.go
    - internal/vcs/github/github.go
tech_stack:
  added: []
  patterns:
    - TDD RED/GREEN: failing suppression tests committed before the widened has() implementation
    - Line-overlap + lower Jaccard gate for resolved-prior sticky suppression
    - Nil-safe *int deref for GitHub GraphQL nullable line field (Pitfall 6)
    - int64 → int cast for GitLab NotePosition.NewLine field
key_files:
  created: []
  modified:
    - internal/vcs/vcs.go
    - internal/findings/findings.go
    - internal/findings/findings_test.go
    - internal/findings/prior.go
    - internal/findings/prior_test.go
    - internal/vcs/gitlab/gitlab.go
    - internal/vcs/github/github.go
decisions:
  - "ResolvedSuppressThreshold=0.3: lower than SimilarTitleThreshold (0.5) so reworded restatements of resolved threads are caught even with substantial LLM paraphrase"
  - "Line-overlap check guards c.LineStart <= r.Line <= c.LineEnd; guard r.Line > 0 && c.LineStart > 0 degrades to Jaccard-only when anchor unknown (Pitfall 6 / T-08-B3 accepted)"
  - "findingRec.Resolved and .Line carry omitempty JSON tags — zero-value fields do not bloat persisted JSON"
  - "int(n.Position.NewLine) cast: GitLab NotePosition.NewLine is int64; vcs.PriorInline.Line is int"
  - "github.go: *int for thread.Line and thread.OrigLine to handle GraphQL nullable; anchorLine = 0 when nil"
  - "DB HasFinding path unchanged: no resolved column, no migration — CI-mode (memory store) only per SPEC"
metrics:
  duration: "10 minutes"
  completed: "2026-06-15T11:15:56Z"
  tasks_completed: 3
  files_modified: 7
---

# Phase 08 Plan 02: Resolved-Thread Sticky Suppression Summary

## One-liner

Captures VCS anchor line (GitLab `n.Position.NewLine`, GitHub `reviewThread.line`) and resolved flag end-to-end into `findingRec`, then widens `memoryStore.has` with a line-overlap OR lower-Jaccard (0.3) gate so user-resolved threads stay gone even when the LLM rewords the finding.

## What Was Built

### Task 1 — Structural plumbing (vcs, prior, adapters) [TDD GREEN for data flow]

Five surgical additions that carry anchor line and resolved flag from both VCS adapters through `NewFromPrior` into `findingRec`:

1. **`internal/vcs/vcs.go`**: Added `Line int` and `EndLine int` to `PriorInline` with inline doc comments satisfying the `revive exported` rule.

2. **`internal/findings/findings.go`**:
   - Added `const ResolvedSuppressThreshold = 0.3` with docstring adjacent to `SimilarTitleThreshold`.
   - Added `Resolved bool \`json:"resolved,omitempty"\`` and `Line int \`json:"line,omitempty"\`` to `findingRec` following the `ExternalID omitempty` convention.

3. **`internal/findings/prior.go`**: `NewFromPrior`'s recs append now assigns `Resolved: pi.Resolved` and `Line: pi.Line`.

4. **`internal/vcs/gitlab/gitlab.go`**: `ListCadooArtifacts` anchored-note branch (inside `n.Position != nil` guard) now sets `Line: int(n.Position.NewLine), EndLine: int(n.Position.NewLine)`. The `int()` cast is required because `glab.NotePosition.NewLine` is `int64`.

5. **`internal/vcs/github/github.go`**:
   - Extended GraphQL query: `reviewThreads ... nodes{ id isResolved line originalLine ...}`.
   - Added `Line *int \`json:"line"\`` and `OrigLine *int \`json:"originalLine"\`` to the anonymous node struct (nullable per GitHub GraphQL schema — Pitfall 6).
   - Nil-safe deref: `if th.Line != nil { anchorLine = *th.Line }` (0 when nil — safe degradation).
   - `out.Inline` append sets `Line: anchorLine, EndLine: anchorLine`.

Build: green. 57 vcs + findings tests pass.

### Task 2 — Failing table-tests for resolved-thread sticky suppression [TDD RED]

Added four tests targeting the new resolved-prior suppression logic. Before Task 3, two sub-cases fail (RED) because `memoryStore.has` has no resolved branch:

**`internal/findings/findings_test.go`**:

- `TestMemoryStoreHasResolvedSuppressesSameFile` — two sub-cases using bodies with Jaccard ≈ 0.40 (above `ResolvedSuppressThreshold=0.3`, below `SimilarTitleThreshold=0.5`): (a) same anchor line as prior, (b) new comment line-range brackets prior anchor. Both expect suppression → FAIL before Task 3.
- `TestMemoryStoreHasResolvedDoesNotSuppressDifferentLine` — resolved prior at line 10, unrelated finding at line 50 with Jaccard=0 → NOT suppressed (PASS).
- `TestMemoryStoreHasResolvedJaccardBelowThreshold` — resolved prior with no anchor, unrelated finding Jaccard=0 → NOT suppressed (PASS).

**`internal/findings/prior_test.go`**:

- `TestNewFromPriorCarriesResolvedAndLine` — seeds a `Resolved=true, Line=25` prior via `NewFromPrior`; verifies a same-line restatement (Jaccard≈0.40) is suppressed AND a different-line unrelated finding is not → first assertion FAILS before Task 3.

### Task 3 — Widen memoryStore.has [TDD GREEN]

One surgical change to `memoryStore.has` in `internal/findings/findings.go`:

The existing inner loop block is split on `r.Resolved`:

```
!r.Resolved: existing rule — exact StructuralKey OR Jaccard >= SimilarTitleThreshold (0.5)
 r.Resolved: line-overlap (c.LineStart <= r.Line <= c.LineEnd) guarded by r.Line > 0 && c.LineStart > 0
             OR Jaccard >= ResolvedSuppressThreshold (0.3)
```

The line-overlap guard `r.Line > 0 && c.LineStart > 0` implements Pitfall 6 safe degradation: when the anchor is unknown (GitHub nullable line → 0), the geometric check is skipped and suppression falls back to Jaccard only — no false-positive suppression of unrelated findings.

All 6 sticky-suppression and guardrail tests are GREEN. Full suite: 492 tests pass across 70 packages. `go vet ./...` clean. `golangci-lint run ./...` 0 issues.

## Commits

| Task | Commit | Type | Description |
|------|--------|------|-------------|
| 1 | 5a80bcd | feat | Add Line/EndLine to PriorInline; seed Resolved+Line in NewFromPrior |
| 2 | 99d6af7 | test | Add failing sticky-suppression tests for resolved prior (RED) |
| 3 | 2cadc63 | feat | Widen memoryStore.has with resolved-prior sticky-suppression branch |

## Verification Results

- `go test -race -count=1 ./...`: 492 passed in 70 packages
- `go vet ./...`: clean
- `golangci-lint run ./...`: 0 issues
- `TestMemoryStoreHasResolvedSuppressesSameFile`: PASS (2/2 sub-cases)
- `TestMemoryStoreHasResolvedDoesNotSuppressDifferentLine`: PASS
- `TestMemoryStoreHasResolvedJaccardBelowThreshold`: PASS
- `TestNewFromPriorCarriesResolvedAndLine`: PASS (both assertions)
- `git diff db/migrations`: empty (no schema changes — CI-mode only per SPEC)

## Deviations from Plan

**[Rule 1 - Bug] int64 → int cast for GitLab NotePosition.NewLine**

- **Found during:** Task 1
- **Issue:** `glab.NotePosition.NewLine` is `int64` but `vcs.PriorInline.Line` is `int`. Direct assignment failed with "cannot use int64 as int".
- **Fix:** Added `int(n.Position.NewLine)` cast in both `Line` and `EndLine` assignments in `gitlab.go`.
- **Files modified:** `internal/vcs/gitlab/gitlab.go`
- **Commit:** 5a80bcd

**[Rule 1 - Bug] Test bodies initially above SimilarTitleThreshold**

- **Found during:** Task 2 RED phase verification
- **Issue:** First draft of `TestMemoryStoreHasResolvedSuppressesSameFile` used body pairs with Jaccard ≥ 0.5, so they were already suppressed by the existing open-prior path — tests were GREEN before Task 3 implementation.
- **Fix:** Replaced body pairs with "goroutine leak in handler" vs "goroutine leak on shutdown timeout" (Jaccard ≈ 0.40) — above ResolvedSuppressThreshold (0.3) but below SimilarTitleThreshold (0.5), ensuring genuine RED before Task 3.
- **Files modified:** `internal/findings/findings_test.go`, `internal/findings/prior_test.go`
- **Commit:** 99d6af7

## Threat Model Coverage

| Threat ID | Mitigation | Status |
|-----------|------------|--------|
| T-08-B1 | Suppression scoped to (tool, file) AND (line-overlap OR Jaccard >= 0.3); TestMemoryStoreHasResolvedDoesNotSuppressDifferentLine proves non-suppression at different lines | Implemented in Task 3 |
| T-08-B2 | Line-overlap check bounds suppression to overlapping lines; below-threshold test asserts non-suppression | Implemented in Task 3 |
| T-08-B3 | GitHub nullable line → `Line=0`; `r.Line > 0` guard degrades to Jaccard-only safely | Implemented in Task 1 (github.go nil deref) |
| T-08-SC | No new package installs | N/A |

## Known Stubs

None — all fields carry real values populated from VCS APIs.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes introduced. The GraphQL query extension (`line originalLine`) retrieves existing GitHub API fields and is scoped to the same already-authorized pull request query.

## Self-Check: PASSED

Files modified (confirmed present):
- `internal/vcs/vcs.go`: FOUND (Line/EndLine fields added)
- `internal/findings/findings.go`: FOUND (ResolvedSuppressThreshold, findingRec fields, memoryStore.has widened)
- `internal/findings/prior.go`: FOUND (Resolved/Line seeded in NewFromPrior)
- `internal/findings/findings_test.go`: FOUND (4 new test functions)
- `internal/findings/prior_test.go`: FOUND (TestNewFromPriorCarriesResolvedAndLine)
- `internal/vcs/gitlab/gitlab.go`: FOUND (Line/EndLine from n.Position.NewLine)
- `internal/vcs/github/github.go`: FOUND (GraphQL line/originalLine, *int fields, anchorLine)

Commits confirmed:
- 5a80bcd: FOUND (feat(08-02): add Line/EndLine to PriorInline)
- 99d6af7: FOUND (test(08-02): add failing sticky-suppression tests)
- 2cadc63: FOUND (feat(08-02): widen memoryStore.has)
