---
phase: 08-ci-mode-dedup-convergence
plan: "01"
subsystem: findings/orchestrator
tags: [tdd, bugfix, dedup, ci-mode]
dependency_graph:
  requires: []
  provides:
    - PostedFinding.StructuralKey field (internal/findings/findings.go)
    - ListPostedFindings selects structural_key from DB (internal/findings/findings.go)
    - memoryStore.list populates StructuralKey (internal/findings/findings.go)
    - resolveStalePriors direct StructuralKey compare with legacy fallback (internal/orchestrator/reviewer.go)
    - TestResolveStalePriorsMultiLineNotSelfResolved regression test (internal/orchestrator/reviewer_test.go)
    - TestMemoryStoreListCarriesStructuralKey unit test (internal/findings/findings_test.go)
  affects:
    - internal/findings/findings.go
    - internal/findings/findings_test.go
    - internal/orchestrator/reviewer.go
    - internal/orchestrator/reviewer_test.go
tech_stack:
  added: []
  patterns:
    - TDD RED/GREEN: failing regression test committed before the fix
    - Legacy fallback guard: empty StructuralKey degrades to first-line recompute (Pitfall-1)
    - DB null-safety: coalesce(structural_key,'') in ListPostedFindings SELECT
key_files:
  created: []
  modified:
    - internal/findings/findings.go
    - internal/findings/findings_test.go
    - internal/orchestrator/reviewer.go
    - internal/orchestrator/reviewer_test.go
decisions:
  - "resolveStalePriors uses p.StructuralKey when non-empty; falls back to first-line recompute for legacy records so no mass-resolution on first deploy (Pitfall-1 / T-08-A1)"
  - "StructuralKey field has inline doc comment to satisfy revive exported rule"
  - "ListPostedFindings uses coalesce(structural_key,'') matching existing null-safety pattern for all other columns"
metrics:
  duration: "5 minutes"
  completed: "2026-06-15T10:59:10Z"
  tasks_completed: 3
  files_modified: 4
---

# Phase 08 Plan 01: End-to-End StructuralKey Threading Summary

## One-liner

Threads `PostedFinding.StructuralKey` end-to-end from both memory and DB backends into `resolveStalePriors`, replacing a lossy first-line recompute that caused every still-valid multi-line thread to be auto-resolved.

## What Was Built

### Task 1 — Regression test (TDD RED)

Added `TestResolveStalePriorsMultiLineNotSelfResolved` to `reviewer_test.go`. Seeds a prior `PostedFinding` with a multi-line improve-style body, then calls `postInline` with the same comment and asserts zero `ResolveThread` calls. The test failed RED against the unfixed code (the bug was confirmed: `disc-multi-1` was spuriously resolved).

### Task 2 — StructuralKey field and backend threading (TDD GREEN for findings)

Three surgical changes to `internal/findings/findings.go`:

1. Added `StructuralKey string` field to `PostedFinding` with an inline doc comment (satisfies `revive exported` rule). The field has no json tag — `PostedFinding` is never JSON-encoded; only `findingRec` is persisted.
2. `ListPostedFindings` (DB path): appended `coalesce(structural_key, '')` to the SELECT column list and `&f.StructuralKey` to the matching `rows.Scan` call. The `structural_key` column already exists from migration 0005.
3. `memoryStore.list`: added `StructuralKey: r.StructuralKey` to the `PostedFinding` literal in the conversion loop.

Added `TestMemoryStoreListCarriesStructuralKey` to `findings_test.go` — verifies the memory-store path carries `StructuralKey` after a round-trip through `RecordFinding` + `ListPostedFindings`.

All 23 findings tests pass.

### Task 3 — Fix resolveStalePriors (TDD GREEN)

Replaced the buggy first-line recompute in `resolveStalePriors` (`reviewer.go`) with a direct compare:

```go
var pkey string
if p.StructuralKey != "" {
    pkey = p.StructuralKey
} else {
    pkey = findings.StructuralKey(p.Tool, vcs.InlineComment{
        File:     p.File,
        Severity: vcs.Severity(p.Severity),
        Body:     p.Title,
    })
}
```

The `p.StructuralKey == ""` branch is the Pitfall-1 guard (T-08-A1): legacy records (written before this field existed) fall back to the original first-line recompute so they are not all mass-resolved on the first post-deploy run.

`TestResolveStalePriorsMultiLineNotSelfResolved` turned GREEN. `TestPostInlineResolvesStalePriors` and `TestCIModeTwoRunIdempotency` continued to pass (single-line bodies are unaffected by the fix).

## Commits

| Task | Commit | Type | Description |
|------|--------|------|-------------|
| 1 | f6ec1c6 | test | Add failing regression test for multi-line self-resolution (RED) |
| 2 | 911a00a | feat | Add StructuralKey to PostedFinding and thread through both backends |
| 3 | fcedf4e | fix | Fix resolveStalePriors to compare StructuralKey directly with legacy fallback |

## Verification Results

- `go test -race -count=1 ./...`: 486 passed in 70 packages
- `go vet ./...`: clean
- `TestResolveStalePriorsMultiLineNotSelfResolved`: PASS (GREEN after Task 3)
- `TestPostInlineResolvesStalePriors`: PASS
- `TestCIModeTwoRunIdempotency`: PASS

## Deviations from Plan

None — plan executed exactly as written.

## Threat Model Coverage

| Threat ID | Mitigation | Status |
|-----------|------------|--------|
| T-08-A1 | Pitfall-1 guard: empty `p.StructuralKey` falls back to first-line recompute — no mass-resolution on legacy records | Implemented in Task 3 |
| T-08-A2 | Both CI (memoryStore) and DB (ListPostedFindings) paths converge on the fixed resolveStalePriors | Implemented in Task 2 + 3 |

## Known Stubs

None — all fields are wired and carry real values.

## Threat Flags

None — no new network endpoints, auth paths, or schema changes introduced. The `structural_key` column selected in `ListPostedFindings` already exists in the DB schema (migration 0005).

## Self-Check: PASSED

- `internal/findings/findings.go`: FOUND (modified)
- `internal/findings/findings_test.go`: FOUND (modified)
- `internal/orchestrator/reviewer.go`: FOUND (modified)
- `internal/orchestrator/reviewer_test.go`: FOUND (modified)
- Commit f6ec1c6: FOUND
- Commit 911a00a: FOUND
- Commit fcedf4e: FOUND
