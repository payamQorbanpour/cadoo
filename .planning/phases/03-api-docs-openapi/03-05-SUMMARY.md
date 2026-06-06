---
phase: 03-api-docs-openapi
plan: "05"
subsystem: releasedocs
tags: [apidocs, openapi, defaults, integration, phase-proof, lint-fix]
dependency_graph:
  requires:
    - apidocs.GenerateMulti wired with discoverSpec + loadSpec (from 03-03)
    - buildRedocHTML + renderMarkdown implemented (from 03-04)
    - All test stubs from 03-02 in RED state awaiting activation
  provides:
    - TestGenerate_ValidationFailure_Skips activated and passing (D-10 validation skip)
    - apidocs.New() registered in DefaultGenerators (4th position, after blog)
    - .cadoo.yaml.example: complete releaseDocs block with apiDocs documentation
    - Full D-01..D-10 test coverage: 36 tests, 0 skips
    - Full suite (go test ./...): 446 tests pass across 69 packages
    - make lint: clean (0 issues)
  affects:
    - internal/releasedocs/defaults/defaults.go (apidocs registered)
    - .cadoo.yaml.example (releaseDocs + apiDocs config block)
    - internal/releasedocs/generators/apidocs/apidocs_test.go (validation skip activated)
    - internal/releasedocs/generators/apidocs/render_html.go (staticcheck fix)
    - internal/releasedocs/generators/apidocs/parse.go (gofmt fix)
    - internal/releasedocs/publishers/pages/pages_apidocs_test.go (gofmt fix)
tech_stack:
  added: []
  patterns:
    - apidocs.Generator registered at end of DefaultGenerators slice (runs after blog)
    - MultiGenerator dispatch: dispatcher prefers GenerateMulti over single-artifact Generate
    - D-08 "always" default coercion for apiDocs.when in .cadoo.yaml.example documented
    - D-02 fallback discovery list documented in specPath comment
key_files:
  created:
    - .planning/phases/03-api-docs-openapi/03-05-SUMMARY.md
  modified:
    - internal/releasedocs/defaults/defaults.go
    - .cadoo.yaml.example
    - internal/releasedocs/generators/apidocs/apidocs_test.go
    - internal/releasedocs/generators/apidocs/render_html.go
    - internal/releasedocs/generators/apidocs/parse.go
    - internal/releasedocs/publishers/pages/pages_apidocs_test.go
decisions:
  - "TestGenerate_ValidationFailure_Skips activated: libopenapi-validator does correctly reject OAS 3.0.3 spec missing info.version (valid=false, 'Document does not pass validation'); the test had been kept skipped in Plan 03 due to a misdiagnosis — the stub GenerateMulti returned nil,nil without ever calling loadSpec"
  - "render_html.go doc comment reworded to avoid SA9009 staticcheck false positive: the phrase '// go:embed so the generated...' in a package doc comment triggered staticcheck's ineffectual compiler directive warning; reworded to '(via the embed directive)' to eliminate the pattern"
  - "gofmt applied to parse.go and pages_apidocs_test.go as pre-existing violations; not modified functionally in this plan"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-06"
  tasks_completed: 2
  tasks_total: 3
  files_modified: 6
---

# Phase 3 Plan 05: Final Assembly + Full-Phase Proof Summary

## One-liner

Activates TestGenerate_ValidationFailure_Skips (all 36 D-01..D-10 tests now green), registers apidocs.New() in DefaultGenerators, and documents the full apiDocs config block in .cadoo.yaml.example; full suite (446 tests, 69 packages) passes and lint is clean.

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Activate TestGenerate_ValidationFailure_Skips — all D-01..D-10 green | f78dc92 | apidocs_test.go |
| 2 | Register apidocs in DefaultGenerators; document config; lint fixes | a0fbce1 | defaults.go, .cadoo.yaml.example, render_html.go, parse.go, pages_apidocs_test.go |
| 3 | Browser verification checkpoint | (awaiting human) | n/a |

## What Was Built

### Task 1: Activate TestGenerate_ValidationFailure_Skips

`internal/releasedocs/generators/apidocs/apidocs_test.go`:
- Removed `t.Skip("TODO(03-04): activate once GenerateMulti calls validateSpec (Plan 04)")`.
- The test uses a spec with `openapi: 3.0.3` and `info: {title: Bad}` (missing required `info.version`).
- Root cause of prior skip: in Plan 03, `GenerateMulti` was a stub returning `(nil, nil)` — the test passed for the wrong reason (zero artifacts because the stub returned nothing, not because the validator rejected the spec). The test was then kept skipped out of caution, carrying the TODO until Plan 04/05 could confirm the validator works.
- Investigation: `libopenapi-validator.ValidateDocument()` correctly returns `valid=false`, `errs[0].Message="Document does not pass validation"` for missing `info.version`. The `loadV3` function in `parse.go` gates on `!valid && len(valErrs) > 0` and returns a reasoned error; `GenerateMulti` then skips with `(nil, nil)` + `slog.Warn` as required by D-10.
- Result: All 36 tests in the apidocs package PASS, 0 SKIP.

### Task 2: Register apidocs + Document Config + Lint Fixes

`internal/releasedocs/defaults/defaults.go`:
- Added import `"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/apidocs"` in the cadoo-internal third import group (goimports order).
- Appended `apidocs.New()` as the 4th (last) entry in `DefaultGenerators()`.
- Updated function doc comment to list all 4 generators including the multi-artifact GenerateMulti dispatch path.

`.cadoo.yaml.example`:
- Added complete `releaseDocs:` block at the end of the file.
- `artifacts.apiDocs:` section documents:
  - `enabled: false` (master switch, default off)
  - `when: always` with inline comment noting this is the D-08 default
  - `specPath: ""` with inline comment documenting the D-02 fallback list (openapi.yaml → openapi.yml → openapi.json → docs/openapi.yaml → api/openapi.yaml) and the "release tag, not main branch" rule
- Full failure-mode description in the comment block (D-10 graceful skip, siblings unaffected).
- Also documents `changelog`, `releaseNotes`, `blog`, and `publish` sub-blocks for completeness (CLAUDE.md: every supported key must be documented).

`internal/releasedocs/generators/apidocs/render_html.go`:
- Fixed SA9009 staticcheck false positive: the package doc comment contained the phrase `// go:embed so the generated HTML...` which staticcheck flagged as an "ineffectual compiler directive due to extraneous space". Reworded the doc comment to use `(via the embed directive)` to eliminate the `// go:embed` pattern from prose.

`internal/releasedocs/generators/apidocs/parse.go`:
- Applied `gofmt` — pre-existing import alignment violation (single blank line between stdlib and third-party groups). No functional change.

`internal/releasedocs/publishers/pages/pages_apidocs_test.go`:
- Applied `gofmt` — struct literal field alignment. No functional change.

## Verification Results

```
TestGenerate_ValidationFailure_Skips                         PASS (was SKIP)
go test -race ./internal/releasedocs/generators/apidocs/...  36 PASS, 0 SKIP
go test -race ./internal/releasedocs/... ./internal/config/...  200 PASS
go test -race -count=1 ./...                                 446 PASS (69 packages)
go build ./...                                               CLEAN
go vet ./...                                                 CLEAN
make lint (golangci-lint run ./...)                          0 issues
grep 'apidocs.New()' internal/releasedocs/defaults/defaults.go   FOUND
grep 'apiDocs:' .cadoo.yaml.example                          FOUND
grep 'specPath:' .cadoo.yaml.example                         FOUND
grep 'releaseDocs:' .cadoo.yaml.example                      FOUND
```

### D-01..D-10 Coverage Matrix (final state)

| Decision | Test | Status |
|----------|------|--------|
| D-01 spec from committed file | TestGenerate_FetchesAtToRef | PASS |
| D-02 fallback discovery | TestDiscoverSpec | PASS |
| D-04 three artifacts | TestGenerate_ThreeArtifacts | PASS |
| D-05 HTML determinism | TestBuildRedocHTML_Deterministic | PASS |
| D-05 HTML offline | TestBuildRedocHTML_NoCDN | PASS |
| D-06 Markdown determinism | TestRenderMarkdown_Golden | PASS |
| D-07 config gating | TestEnabled | PASS |
| D-08 always coercion | TestEnabled (empty-coerced-always subtests) | PASS |
| D-09 Swagger 2.0 | TestGenerate_Swagger2 | PASS |
| D-09 OAS 3.0 | TestGenerate_OAS3 | PASS |
| D-09 OAS 3.1 | TestGenerate_OAS31 | PASS |
| D-10 no spec | TestGenerate_NoSpec_Skips | PASS |
| D-10 parse fail | TestGenerate_ParseFailure_Skips | PASS |
| D-10 validation fail | TestGenerate_ValidationFailure_Skips | PASS (activated in this plan) |
| D-10 unsupported version | TestGenerate_UnsupportedVersion_Skips | PASS |
| T-03-03 SSRF | TestGenerate_NoRemoteRef | PASS |
| Cross-cut: Filename routing | TestPublish_APIDocs_Paths | PASS |
| Cross-cut: idempotency | TestIdempotent_APIDocs | PASS |
| Cross-cut: backward compat | TestRenderMarkdown_Golden_V2 | PASS |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TestGenerate_ValidationFailure_Skips: misdiagnosed in Plan 03 as "validator doesn't reject" — actually it does**
- **Found during:** Task 1 investigation
- **Issue:** Plan 03 kept the test skipped with the note "libopenapi-validator.ValidateDocument() did NOT return valid=false for this input". This was incorrect. The real reason the test couldn't be activated in Plan 03 was that `GenerateMulti` was still a stub returning `(nil, nil)` — the test passed for the wrong reason (0 artifacts because the stub returned nothing). When the stub was replaced with the real implementation in Plans 03-04, the validator path became reachable. Verified via a throwaway Go main that confirmed `valid=false` for the missing-`info.version` spec.
- **Fix:** Removed `t.Skip` — test now activates and passes correctly.
- **Files modified:** `apidocs_test.go`
- **Commit:** f78dc92

**2. [Rule 1 - Bug] SA9009 staticcheck false positive on render_html.go package doc comment**
- **Found during:** Task 2 lint run
- **Issue:** `make lint` reported `SA9009: ineffectual compiler directive due to extraneous space` on line 3 of `render_html.go`. The package doc comment contained `"via go:embed so the generated HTML..."` — staticcheck interpreted `// go:embed` (with a leading space in `//  go:embed`) as an attempt at a compiler directive. It was prose, not a directive.
- **Fix:** Reworded: `"(via the embed directive) so the generated HTML..."` — eliminates the `// go:embed` substring from prose.
- **Files modified:** `render_html.go`
- **Commit:** a0fbce1

**3. [Rule 1 - Bug] gofmt violations on parse.go and pages_apidocs_test.go**
- **Found during:** Task 2 lint run
- **Issue:** Pre-existing gofmt violations in both files (import alignment in parse.go; struct literal alignment in pages_apidocs_test.go). Neither file was modified functionally in this plan, but `make lint` (which runs on all packages) reported them.
- **Fix:** Applied `gofmt -w` to both files.
- **Files modified:** `parse.go`, `pages_apidocs_test.go`
- **Commit:** a0fbce1

## Known Stubs

None. All three GenerateMulti artifacts carry real content:
- `openapi.yaml`: raw spec bytes (from Plan 03)
- `api-reference.html`: full Redoc HTML (from Plan 04)
- `api-reference.md`: deterministic Markdown (from Plan 04)

The only remaining item is Task 3 (browser verification checkpoint) which requires human confirmation that the offline HTML renders correctly.

## Threat Flags

No new threat surface introduced. All threat model items from the plan's `<threat_model>` are addressed:

| Threat ID | Status |
|-----------|--------|
| T-03-06 (invalid spec published as docs) | Mitigated — GenerateMulti returns (nil, nil) on any discover/parse/validate/render failure; TestGenerate_ValidationFailure_Skips now active and passing |
| T-03-01 (3 artifacts' Filenames → pages paths) | Mitigated — Filenames are fixed literals; TestPublish_APIDocs_Paths confirms path routing |
| T-03-03 (HTML offline rendering) | Mitigated — go:embed-inlined bundle; TestBuildRedocHTML_NoCDN enforces no external CDN src= |

## Self-Check

| Check | Result |
|-------|--------|
| internal/releasedocs/defaults/defaults.go (imports apidocs, apidocs.New() last) | FOUND |
| .cadoo.yaml.example (releaseDocs + apiDocs + specPath documented) | FOUND |
| internal/releasedocs/generators/apidocs/apidocs_test.go (t.Skip removed) | VERIFIED |
| internal/releasedocs/generators/apidocs/render_html.go (SA9009 fix) | VERIFIED |
| commit f78dc92 (activate validation test) | FOUND |
| commit a0fbce1 (register + document + lint fixes) | FOUND |
| go test -race ./internal/releasedocs/generators/apidocs/... 36 tests, 0 skips | PASS |
| go test -race -count=1 ./... 446 tests | PASS |
| go build ./... clean | PASS |
| go vet ./... clean | PASS |
| make lint clean (0 issues) | PASS |
| TestGenerate_ValidationFailure_Skips PASS (not SKIP) | PASS |
| DefaultGenerators includes apidocs.New() | PASS |
| .cadoo.yaml.example includes apiDocs: block | PASS |

## Self-Check: PASSED
