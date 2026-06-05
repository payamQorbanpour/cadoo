---
phase: 03-api-docs-openapi
plan: "02"
subsystem: releasedocs
tags: [apidocs, openapi, test-scaffold, golden-tests, nyquist, tdd-red]
dependency_graph:
  requires:
    - Artifact.Filename field (from 03-01)
    - KindAPIDocs ArtifactKind (from 03-01)
    - MultiGenerator interface (from 03-01)
    - APIDocsConfig in config.ReleaseArtifacts (from 03-01)
    - libopenapi v0.37.3 in go.mod (from 03-01)
    - releasedocstest.Fake + NewFake() (existing)
  provides:
    - Six OpenAPI/Swagger test fixtures (v2, v3, v31, invalid, remote_ref, oversized)
    - apidocs.Generator stub satisfying Generator + MultiGenerator interfaces
    - Table-driven test scaffold covering all 03-VALIDATION.md test names
    - fakeFetcher: implements FileFetcher + vcs.Provider for no-real-VCS testing
    - TestPublish_APIDocs_Paths + TestIdempotent_APIDocs in pages publisher
  affects:
    - internal/releasedocs/generators/apidocs (new package, stub + tests)
    - internal/releasedocs/publishers/pages (apidocs path + idempotency tests)
tech_stack:
  added: []
  patterns:
    - t.Skip("TODO(03-NN)") pattern for RED-state stubs that reference real symbols
    - fakeFetcher embedding releasedocstest.Fake for combined FileFetcher+vcs.Provider
    - TEST_UPDATE_GOLDEN=1 env-var golden update guard (CI-safe)
    - fixtureAPIDocsRC builder for deterministic ReleaseContext in apidocs tests
key_files:
  created:
    - internal/releasedocs/generators/apidocs/apidocs.go
    - internal/releasedocs/generators/apidocs/apidocs_test.go
    - internal/releasedocs/generators/apidocs/testdata/petstore_v2.yaml
    - internal/releasedocs/generators/apidocs/testdata/petstore_v3.yaml
    - internal/releasedocs/generators/apidocs/testdata/petstore_v31.yaml
    - internal/releasedocs/generators/apidocs/testdata/invalid.yaml
    - internal/releasedocs/generators/apidocs/testdata/remote_ref.yaml
    - internal/releasedocs/generators/apidocs/testdata/oversized.yaml
    - internal/releasedocs/publishers/pages/pages_apidocs_test.go
  modified: []
decisions:
  - "Generator stub includes both Generator and MultiGenerator interface satisfaction (compile-time var _ assertions) so the dispatcher can type-assert either path"
  - "fakeFetcher embeds releasedocstest.Fake via vcs.Provider interface embedding (not struct embedding) to avoid method set conflicts while satisfying both FileFetcher and vcs.Provider"
  - "All not-yet-implemented test stubs use t.Skip with explicit TODO(03-NN) references instead of build tags — keeps package compilable and traceable to owning plans"
  - "apidocs.go Generate() method satisfies releasedocs.Generator interface (required by registry) but is unreachable in practice since dispatcher prefers GenerateMulti"
  - "oversized.yaml generated via Python script producing 5.01 MB (5,250,738 bytes) > 5 MB guard threshold"
metrics:
  duration: "~6 minutes"
  completed: "2026-06-06"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 9
---

# Phase 3 Plan 02: Wave 0 Test Scaffold (fixtures + fakeFetcher + test stubs) Summary

## One-liner

Six deterministic OpenAPI/Swagger test fixtures, an apidocs.Generator stub satisfying MultiGenerator, a reusable fakeFetcher implementing FileFetcher+vcs.Provider, and table-driven test stubs covering all 17 validation-mapped test names in 03-VALIDATION.md — all compiled and in RED state awaiting Plans 03-05.

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Author OpenAPI/Swagger test fixtures | 190ebcd | testdata/petstore_v2.yaml, testdata/petstore_v3.yaml, testdata/petstore_v31.yaml, testdata/invalid.yaml, testdata/remote_ref.yaml, testdata/oversized.yaml |
| 2 | Build apidocs test scaffold (fakeFetcher + table-driven stubs) | d6617a6 | apidocs.go, apidocs_test.go |
| 3 | Pages-publisher apidocs path + idempotency tests | cafc068 | pages_apidocs_test.go |

## What Was Built

### Task 1: Test Fixtures

Six minimal, deterministic fixtures for the apidocs test suite:

- `petstore_v2.yaml` — Swagger 2.0 (`swagger: "2.0"`) with two tags (pets/store), 5 operations, parameters, definitions. Covers D-09 Swagger 2.0 path.
- `petstore_v3.yaml` — OAS 3.0.3 with two tags, 6 operations, requestBody, components/schemas. Primary fixture for golden tests (D-05/D-06).
- `petstore_v31.yaml` — OAS 3.1.0 petstore subset with 4 operations and components. Covers D-09 OAS 3.1 path.
- `invalid.yaml` — Syntactically malformed YAML that fails to parse. Exercises D-10 ParseFailure skip path.
- `remote_ref.yaml` — OAS 3.0 spec with a remote URL `$ref` (`https://example.com/schemas/Pet.yaml`) AND a file `$ref` (`./external.yaml#/...`). Exercises T-03-03 SSRF guard — parser must NOT resolve these when `AllowRemoteReferences=false` and `AllowFileReferences=false`.
- `oversized.yaml` — 5,250,738 bytes (5.01 MB), exceeding the 5 MB max-size guard. Exercises T-03-04 OOM guard (D-10 skip).

All fixtures are deterministic (no timestamps).

### Task 2: Generator Stub + Test Scaffold

`internal/releasedocs/generators/apidocs/apidocs.go`:
- `Generator struct{}` + `New() *Generator` — mirrors blog.go pattern exactly.
- `Kind()` returns `KindAPIDocs`.
- `Enabled()` coerces empty `When` to `"always"` (D-08) before delegating to `releasedocs.Enabled`.
- `Generate()` satisfies `releasedocs.Generator` interface (required by registry); unreachable in practice since dispatcher prefers `GenerateMulti`.
- `GenerateMulti()` is a stub returning `(nil, nil)` with a `TODO(03-03)` comment. Plans 03-05 fill in `discoverSpec`, `parseSpec`, `buildRedocHTML`, `renderMarkdown`.
- Compile-time assertions: `var _ releasedocs.Generator = (*Generator)(nil)` and `var _ releasedocs.MultiGenerator = (*Generator)(nil)`.

`internal/releasedocs/generators/apidocs/apidocs_test.go`:
- `package apidocs_test` — external test package (mirrors blog_test.go).
- `fakeFetcher` struct: holds `files map[string][]byte`; implements `releasedocs.FileFetcher` (absent paths return `"404 not found"` error for isMissingFile tolerance); embeds `vcs.Provider` via interface embedding wrapping a `releasedocstest.Fake`.
- `fixtureAPIDocsRC(files, specPath, when, enabled, bump)` — builds deterministic `ReleaseContext` with `APIDocsConfig` set and `fakeFetcher` as `Provider`.
- `updateGolden` guard: `TEST_UPDATE_GOLDEN=1` env var, default false. CI safe.
- All 17 test names from `03-VALIDATION.md` Per-Task Verification Map are present:

| Test | Status |
|------|--------|
| TestAPIDocs_Kind | PASS (active) |
| TestEnabled (15 subtests) | PASS (active) — covers disabled, empty-to-always coercion, explicit always/major/minor_or_above |
| TestDiscoverSpec | SKIP (TODO 03-03) |
| TestGenerate_FetchesAtToRef | SKIP (TODO 03-03) |
| TestGenerate_ThreeArtifacts | SKIP (TODO 03-03) |
| TestGenerate_RawSpecPassthrough | SKIP (TODO 03-03) |
| TestGenerate_Swagger2 | SKIP (TODO 03-03) |
| TestGenerate_OAS3 | SKIP (TODO 03-03) |
| TestGenerate_OAS31 | SKIP (TODO 03-03) |
| TestGenerate_NoSpec_Skips | SKIP (TODO 03-03) |
| TestGenerate_ParseFailure_Skips | SKIP (TODO 03-03) |
| TestGenerate_ValidationFailure_Skips | SKIP (TODO 03-04) |
| TestGenerate_UnsupportedVersion_Skips | SKIP (TODO 03-03) |
| TestGenerate_NoRemoteRef | SKIP (TODO 03-03) |
| TestBuildRedocHTML_Deterministic | SKIP (TODO 03-05) |
| TestBuildRedocHTML_NoCDN | SKIP (TODO 03-05) |
| TestRenderMarkdown_Golden | SKIP (TODO 03-05) |

### Task 3: Pages Publisher apidocs Tests

`internal/releasedocs/publishers/pages/pages_apidocs_test.go`:
- `TestPublish_APIDocs_Paths`: builds three `KindAPIDocs` artifacts with `Filename` set to `openapi.yaml`, `api-reference.html`, `api-reference.md`; asserts `UpsertFile` called 3 times with paths `docs/releases/v2.0.0/openapi.yaml`, `.../api-reference.html`, `.../api-reference.md` respectively. Covers T-03-01 traversal guard cross-cut.
- `TestIdempotent_APIDocs`: calls `Publish` twice with identical inputs; asserts both runs target identical path sets (6 total UpsertFile calls, first 3 == last 3). Covers D-13/D-14 idempotency for apidocs.
- Both tests reuse the existing `releasedocstest.NewFake()` + `enabledPages()` fixtures from `pages_test.go`.
- All existing pages tests (7 tests) remain green.

## Verification Results

```
go vet ./internal/releasedocs/generators/apidocs/...          CLEAN
go test -run TestAPIDocs_Kind ./internal/releasedocs/generators/apidocs/...   PASS
go test ./internal/releasedocs/generators/apidocs/...         PASS (2 pass, 15 skip)
go test -race ./internal/releasedocs/publishers/pages/...     ALL PASS (9 tests)
go build ./...                                                 CLEAN
```

## Deviations from Plan

None — plan executed exactly as written. The `fakeFetcher` embeds `vcs.Provider` as an interface field (initialized with `releasedocstest.NewFake()`) rather than defining all methods inline — this is cleaner and reuses the already-complete `releasedocstest.Fake` that has all required method implementations.

## Known Stubs

The following test stubs reference symbols implemented in Plans 03-05. They compile and skip with clear TODO references:

| Stub | TODO Reference | Activates When |
|------|---------------|----------------|
| TestDiscoverSpec subtests | 03-03 | discoverSpec wired in GenerateMulti |
| TestGenerate_FetchesAtToRef | 03-03 | GenerateMulti fully implemented |
| TestGenerate_ThreeArtifacts | 03-03 | GenerateMulti returns 3 artifacts |
| TestGenerate_RawSpecPassthrough | 03-03 | GenerateMulti returns raw spec artifact |
| TestGenerate_Swagger2/OAS3/OAS31 | 03-03 | Version-specific parsing implemented |
| TestGenerate_NoSpec_Skips | 03-03 | discoverSpec wired |
| TestGenerate_ParseFailure_Skips | 03-03 | parseSpec wired |
| TestGenerate_ValidationFailure_Skips | 03-04 | validateSpec wired |
| TestGenerate_UnsupportedVersion_Skips | 03-03 | Version check implemented |
| TestGenerate_NoRemoteRef | 03-03 | AllowRemoteReferences=false guard in place |
| TestBuildRedocHTML_Deterministic | 03-05 | buildRedocHTML implemented |
| TestBuildRedocHTML_NoCDN | 03-05 | buildRedocHTML implemented |
| TestRenderMarkdown_Golden | 03-05 | renderMarkdown implemented |

## Threat Flags

No new threat surface introduced. This plan creates test fixtures and stubs only — no production code paths, no network calls, no new trust boundaries. All threat fixtures (remote_ref.yaml, oversized.yaml) are test inputs only.

## Self-Check: PASSED

| Check | Result |
|-------|--------|
| internal/releasedocs/generators/apidocs/apidocs.go | FOUND |
| internal/releasedocs/generators/apidocs/apidocs_test.go | FOUND |
| testdata/petstore_v2.yaml | FOUND |
| testdata/petstore_v3.yaml | FOUND |
| testdata/petstore_v31.yaml | FOUND |
| testdata/invalid.yaml | FOUND |
| testdata/remote_ref.yaml | FOUND |
| testdata/oversized.yaml | FOUND (5,250,738 bytes > 5,242,880 limit) |
| internal/releasedocs/publishers/pages/pages_apidocs_test.go | FOUND |
| commit 190ebcd (fixtures) | FOUND |
| commit d6617a6 (generator stub + test scaffold) | FOUND |
| commit cafc068 (pages apidocs tests) | FOUND |
| TestAPIDocs_Kind passes | PASS |
| TestEnabled (15 subtests) passes | PASS |
| TestPublish_APIDocs_Paths passes | PASS |
| TestIdempotent_APIDocs passes | PASS |
| go vet ./internal/releasedocs/... clean | PASS |
| go test -race ./internal/releasedocs/publishers/pages/... green | PASS |
