---
phase: 03-api-docs-openapi
plan: "03"
subsystem: releasedocs
tags: [apidocs, openapi, libopenapi, security, ssrf, oom, discovery, parse]
dependency_graph:
  requires:
    - discoverSpec via FileFetcher type-assert (from 03-01)
    - APIDocsConfig.SpecPath (from 03-01)
    - libopenapi v0.37.3 in go.mod (from 03-01)
    - libopenapi-validator v0.13.8 in go.mod (from 03-01)
    - Six test fixtures + fakeFetcher + test stubs (from 03-02)
    - GenerateMulti stub in apidocs.go (from 03-02)
  provides:
    - discover.go: discoverSpec with D-02 fallback list and 404 tolerance
    - parse.go: loadSpec with SSRF guard (AllowRemoteReferences=false), OOM guard (5 MB), version detection, OAS 3.x validation, Swagger 2.0 isolation
    - specModel + operationItem + paramItem structs (consumed by Plans 04–05 renderers)
    - isSupportedOAS3Version: 3.0.x + 3.1.x allowed, 3.2+ rejected
    - GenerateMulti wired with discover + parse; returns 3 artifacts (raw + stubs)
    - 14 test stubs activated (TODO(03-03) stubs removed); TestGenerate_ValidationFailure_Skips remains TODO(03-04)
  affects:
    - internal/releasedocs/generators/apidocs (new files discover.go, parse.go; updated apidocs.go, apidocs_test.go)
tech_stack:
  added: []
  patterns:
    - isMissingFile copied verbatim from template/template.go (404 + fs.ErrNotExist tolerance)
    - NewDocumentWithConfiguration with AllowRemoteReferences=false + AllowFileReferences=false (SSRF guard T-03-03)
    - maxSpecSize = 5 MiB pre-parse length check (OOM guard T-03-04)
    - isSupportedOAS3Version version range check (3.0.x, 3.1.x only; D-03, D-09)
    - parseSwagger2 isolated function with deprecation TODO (T-03-05)
    - Sorted path keys via sort.Strings for golden-file determinism
key_files:
  created:
    - internal/releasedocs/generators/apidocs/discover.go
    - internal/releasedocs/generators/apidocs/parse.go
  modified:
    - internal/releasedocs/generators/apidocs/apidocs.go
    - internal/releasedocs/generators/apidocs/apidocs_test.go
decisions:
  - "isSupportedOAS3Version added: libopenapi returns SpecType=openapi for 3.2.0 too, so explicit version-prefix check needed to reject unsupported OAS 3.2+ specs"
  - "parseSwagger2 isolated in a single function with deprecation TODO per RESEARCH Pitfall 1; all v2 API calls are contained so removal is a one-function change"
  - "GenerateMulti returns 3 artifacts (openapi.yaml with real content; html+md with empty stub bytes) so ThreeArtifacts and OAS3/OAS31/Swagger2 tests pass; Plans 04-05 fill in the stubs"
  - "TestGenerate_ValidationFailure_Skips kept skipped (TODO 03-04): the invalid spec used (missing info.version) currently passes libopenapi-validator; activating requires Plan 04 research into which fields the validator enforces"
metrics:
  duration: "~25 minutes"
  completed: "2026-06-06"
  tasks_completed: 2
  tasks_total: 2
  files_modified: 4
---

# Phase 3 Plan 03: Spec Ingestion (discover.go + parse.go) Summary

## One-liner

Implements discoverSpec (D-02 fallback list with 404 tolerance) and loadSpec (libopenapi parse with SSRF + OOM guards, version detect, OAS 3.x validation, Swagger 2.0 isolation); wires GenerateMulti; activates 14 test stubs from Plan 02.

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | discover.go — fallback spec discovery with 404 tolerance | 9845d41 | discover.go |
| 2 | parse.go — libopenapi parse + version detect + validation + security guards; wire GenerateMulti; activate tests | cecd101 | parse.go, apidocs.go, apidocs_test.go |

## What Was Built

### Task 1: discover.go

`internal/releasedocs/generators/apidocs/discover.go`:
- `fallbackPaths var` — ordered D-02 list: `openapi.yaml`, `openapi.yml`, `openapi.json`, `docs/openapi.yaml`, `api/openapi.yaml`.
- `isMissingFile(err error) bool` — copied verbatim from `template/template.go` (checks `fs.ErrNotExist` + "404"/"not found" string matches). Package-private.
- `discoverSpec(ctx, rc) ([]byte, error)`:
  - Type-asserts `rc.Provider.(releasedocs.FileFetcher)` — error if absent.
  - Non-empty `SpecPath`: fetches exactly that path at `rc.ToRef`, no fallback (D-02).
  - Empty `SpecPath`: iterates `fallbackPaths`; returns first success; `isMissingFile` errors continue; non-404 errors are `slog.Warn`-logged and skipped (Pitfall 5).
  - All paths exhausted: returns `"apidocs: no spec found at any fallback path"`.

### Task 2: parse.go + apidocs.go wiring + test activation

`internal/releasedocs/generators/apidocs/parse.go`:
- `maxSpecSize = 5 * 1024 * 1024` (5 MiB) — OOM guard constant (T-03-04).
- `paramItem` struct — Name + In for parameter extraction.
- `operationItem` struct — Method, Path, Summary, Description, Parameters.
- `specModel` struct — Version, Title, Paths (sorted operationItem slice).
- `isSupportedOAS3Version(version string) bool` — returns true for `3.0.*` and `3.1.*` prefixes only. Added because libopenapi classifies `openapi: 3.2.0` as `SpecType="openapi"`, so explicit version-range check is needed (D-03, D-09).
- `loadSpec(specBytes []byte) (*specModel, error)`:
  1. OOM guard: `len(specBytes) > maxSpecSize` → reasoned error.
  2. SSRF guard: `NewDocumentWithConfiguration(bytes, &DocumentConfiguration{AllowRemoteReferences:false, AllowFileReferences:false})` (T-03-03).
  3. Switch on `info.SpecType`: `utils.OpenApi3` → `isSupportedOAS3Version` check → `loadV3`; `utils.OpenApi2` → `parseSwagger2`; default → reasoned error.
- `loadV3(doc, version)` — `BuildV3Model`, then `libopenapi-validator.NewValidator + ValidateDocument`, then sorted path extraction via `KeysFromOldest()`.
- `extractV3Paths` / `v3PathItemToOps` — collect keys, `sort.Strings`, emit operationItem per HTTP verb (GET/PUT/POST/DELETE).
- `parseSwagger2(doc, version)`:
  - `TODO: libopenapi v2 model is deprecated and will be removed…` comment.
  - `slog.Warn` on entry (T-03-05 deprecation warning).
  - `BuildV2Model`; sorted path extraction; no schema validation (libopenapi-validator is OAS 3.x-only).
- `extractV2Paths` / `v2PathItemToOps` — same pattern as V3.

`internal/releasedocs/generators/apidocs/apidocs.go`:
- `GenerateMulti` now calls `discoverSpec` → `loadSpec` → builds 3 artifacts.
- Skip conditions return `(nil, nil)` with `slog.Warn` (D-10).
- `openapi.yaml` artifact carries raw fetched bytes (D-03).
- `api-reference.html` and `api-reference.md` artifacts carry `[]byte{}` stubs with `TODO(03-04)` and `TODO(03-05)` comments respectively.

`internal/releasedocs/generators/apidocs/apidocs_test.go`:
- 14 `t.Skip("TODO(03-03):…")` calls removed and replaced with full test bodies.
- Activated: TestDiscoverSpec/{explicit,fallback,all-404}, TestGenerate_FetchesAtToRef, TestGenerate_ThreeArtifacts, TestGenerate_RawSpecPassthrough, TestGenerate_OAS3, TestGenerate_OAS31, TestGenerate_Swagger2, TestGenerate_NoSpec_Skips, TestGenerate_ParseFailure_Skips, TestGenerate_UnsupportedVersion_Skips, TestGenerate_NoRemoteRef.
- `TestGenerate_ValidationFailure_Skips` remains `t.Skip("TODO(03-04):…")` — see Deviations.

## Verification Results

```
go build ./internal/releasedocs/generators/apidocs/...   CLEAN
go vet ./internal/releasedocs/generators/apidocs/...     CLEAN
go test -race ./internal/releasedocs/generators/apidocs/... PASS (31 tests)
grep AllowRemoteReferences parse.go                       FOUND (AllowRemoteReferences: false)
grep AllowFileReferences parse.go                        FOUND (AllowFileReferences: false)
grep maxSpecSize parse.go                                FOUND (5 * 1024 * 1024)
go build ./...                                           CLEAN
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] isSupportedOAS3Version guard added for OAS 3.2+ rejection**
- **Found during:** Task 2 — TestGenerate_UnsupportedVersion_Skips failed (got 3 artifacts, want 0)
- **Issue:** libopenapi returns `SpecType = "openapi"` for `openapi: 3.2.0`. The switch `case utils.OpenApi3` matched, so 3.2.0 flowed through as a valid OAS 3.x spec instead of being rejected.
- **Fix:** Added `isSupportedOAS3Version(version)` function checking `strings.HasPrefix(version, "3.0.")` || `strings.HasPrefix(version, "3.1.")`. The OAS 3.x branch now calls this guard before `loadV3`; non-matching versions return a reasoned error.
- **Files modified:** `parse.go`
- **Commit:** cecd101

### Kept Skipped (by design)

**TestGenerate_ValidationFailure_Skips** (TODO 03-04):
- The test inline spec is `openapi: 3.0.3` with `info: {title: Bad}` missing `version`.
- At implementation time, `libopenapi-validator.ValidateDocument()` did NOT return `valid=false` for this input — the validator considers it valid enough (no required violation detected at the document level for missing `info.version` in OAS 3.0 schema).
- This is consistent with the plan note "D-10 skip: validation fail" being assigned to Plan 04 (`TODO(03-04)`), which is expected to research and fix the validation test fixture.
- No change made; test remains skipped with the original TODO reference.

## Known Stubs

| Stub | File | Reason |
|------|------|--------|
| `api-reference.html` artifact content is `[]byte{}` | apidocs.go:73 | buildRedocHTML implemented in Plan 04 (TODO 03-04) |
| `api-reference.md` artifact content is `[]byte{}` | apidocs.go:79 | renderMarkdown implemented in Plan 05 (TODO 03-05) |

These stubs do not prevent this plan's goal (discover + parse) from being achieved. The three-artifact shape is correct; content is completed by Plans 04 and 05.

## Threat Flags

No new threat surface introduced beyond what is documented in the plan's threat model. All T-03-03 (SSRF) and T-03-04 (OOM) mitigations are implemented and verified:

| Threat ID | Status |
|-----------|--------|
| T-03-03 (SSRF via $ref) | Mitigated — AllowRemoteReferences=false + AllowFileReferences=false in NewDocumentWithConfiguration |
| T-03-04 (OOM via oversized spec) | Mitigated — len(specBytes) > maxSpecSize check before any parsing |
| T-03-05 (Swagger 2.0 deprecated model) | Accepted/isolated — parseSwagger2 function with deprecation TODO + slog.Warn |
| T-03-06 (malformed/invalid spec) | Mitigated — parse errors and OAS 3.x validation failures return reasoned errors → generator skips |

## Self-Check: PASSED

| Check | Result |
|-------|--------|
| internal/releasedocs/generators/apidocs/discover.go | FOUND |
| internal/releasedocs/generators/apidocs/parse.go | FOUND |
| internal/releasedocs/generators/apidocs/apidocs.go (updated) | FOUND |
| internal/releasedocs/generators/apidocs/apidocs_test.go (updated) | FOUND |
| commit 9845d41 (discover.go) | FOUND |
| commit cecd101 (parse.go + wire + test activation) | FOUND |
| go build ./... clean | PASS |
| go vet ./internal/releasedocs/generators/apidocs/... clean | PASS |
| go test -race ./internal/releasedocs/generators/apidocs/... 31 tests pass | PASS |
| grep AllowRemoteReferences parse.go | PASS |
| grep AllowFileReferences parse.go | PASS |
| grep maxSpecSize parse.go | PASS |
| TestDiscoverSpec (4 subtests) passes | PASS |
| TestGenerate_OAS3/OAS31/Swagger2 passes | PASS |
| TestGenerate_ParseFailure_Skips passes | PASS |
| TestGenerate_UnsupportedVersion_Skips passes | PASS |
| TestGenerate_NoRemoteRef passes | PASS |
| TestGenerate_ValidationFailure_Skips skipped (TODO 03-04) | EXPECTED |
