---
phase: 03-api-docs-openapi
plan: "01"
subsystem: releasedocs
tags: [apidocs, openapi, libopenapi, redoc, model-extension, multi-artifact]
dependency_graph:
  requires: []
  provides:
    - Artifact.Filename field (backward-compatible extension)
    - KindAPIDocs ArtifactKind constant
    - MultiGenerator interface + dispatcher spread
    - pages publisher Filename-aware routing with traversal guard
    - APIDocsConfig in config.ReleaseArtifacts
    - libopenapi v0.37.3 in go.mod
    - libopenapi-validator v0.13.8 in go.mod
    - Redoc v2.5.3 bundle vendored (go:embed target)
  affects:
    - internal/releasedocs (model + dispatcher)
    - internal/releasedocs/publishers/pages (path routing)
    - internal/config (config schema)
tech_stack:
  added:
    - github.com/pb33f/libopenapi@v0.37.3
    - github.com/pb33f/libopenapi-validator@v0.13.8
    - Redoc v2.5.3 standalone bundle (vendored JS, go:embed target)
  patterns:
    - MultiGenerator optional interface for generators emitting multiple artifacts
    - Dispatcher type-assertion for MultiGenerator before single-artifact Generate fallback
    - Artifact.Filename zero-value backward compat (empty → {kind}.md)
    - pages publisher traversal guard applies to computed path regardless of Filename source
key_files:
  created:
    - internal/releasedocs/generators/apidocs/assets/redoc.standalone.js
    - internal/releasedocs/generators/apidocs/.redoc-version
  modified:
    - go.mod
    - go.sum
    - internal/releasedocs/releasedocs.go
    - internal/releasedocs/dispatcher.go
    - internal/releasedocs/publishers/pages/pages.go
    - internal/config/config.go
decisions:
  - "MultiGenerator optional interface (not modifying base Generator) — preserves backward compat without touching blog/changelog/releasenotes"
  - "KindAPIDocs = apidocs (single Kind for all 3 artifacts, differentiated by Filename) — avoids Kind enum bloat"
  - "libopenapi-validator pinned at v0.13.8 (latest at go get time, not @latest to ensure reproducibility)"
  - "Redoc v2.5.3 vendored from npm bundles/redoc.standalone.js path (confirmed non-CDN, offline-safe)"
metrics:
  duration: "~25 minutes"
  completed: "2026-06-06"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 8
---

# Phase 3 Plan 01: Wave 0 Foundation (libopenapi + Redoc + model/wiring) Summary

## One-liner

Adds libopenapi v0.37.3 + libopenapi-validator v0.13.8 to go.mod, vendors the Redoc v2.5.3 standalone bundle, and makes four cross-cutting model/wiring changes — `Artifact.Filename`, `KindAPIDocs`, `MultiGenerator` interface + dispatcher spread, pages publisher Filename-aware routing, and `APIDocsConfig` — that Plans 02-05 compile against.

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 2 | Add libopenapi deps and vendor Redoc bundle | c169d65 | go.mod, go.sum, assets/redoc.standalone.js, .redoc-version |
| 3 | Extend Artifact + KindAPIDocs + MultiGenerator + dispatcher spread | 6b3c392 | releasedocs.go, dispatcher.go |
| 4 | Pages publisher honors Artifact.Filename + APIDocsConfig in config | 5e7525e | pages/pages.go, config/config.go |

Note: Task 1 was a `gate="blocking-human"` package legitimacy checkpoint. It was pre-approved by the human before this agent was spawned and is treated as satisfied.

## What Was Built

### Task 2: Dependencies + Redoc Bundle

- `go get github.com/pb33f/libopenapi@v0.37.3` — adds the OpenAPI/Swagger parser (Swagger 2.0 + OAS 3.0/3.1).
- `go get github.com/pb33f/libopenapi-validator@v0.13.8` — adds the OAS 3.x schema validator.
- `go mod tidy` — no replace directives introduced (Pitfall 6 check passed).
- `internal/releasedocs/generators/apidocs/assets/redoc.standalone.js` — vendored Redoc v2.5.3 standalone bundle (1,097,271 bytes uncompressed) from npm `redoc@2.5.3 bundles/redoc.standalone.js`.
- `internal/releasedocs/generators/apidocs/.redoc-version` — sidecar provenance file: version 2.5.3, sha256 `1320f442151c57c447d3b70c7ffc6c4f86d08464020fe34c8cc5d3164e9944f0`, license MIT, re-vendor instructions.
- Binary size note: embedded bundle adds ~1.2 MB uncompressed. Accepted for server binaries; not embedded in cadoo-cli.

### Task 3: Model + Dispatcher Contracts

`internal/releasedocs/releasedocs.go`:
- `Artifact.Filename string` — optional field; non-empty overrides publishers' default `{kind}.md` path; zero value is backward compat.
- `KindAPIDocs ArtifactKind = "apidocs"` — identifies the apidocs family (3 artifacts per GenerateMulti call, differentiated by Filename).
- `MultiGenerator` interface — optional; `GenerateMulti(ctx, rc) ([]Artifact, error)`; the dispatcher prefers it when a generator implements it. Base `Generator` interface signature is byte-identical to before.

`internal/releasedocs/dispatcher.go` generate loop:
- Type-asserts `gen.(MultiGenerator)` first; calls `GenerateMulti` and appends via `arts = append(arts, multi...)` when present.
- Falls back to single-artifact `Generate` path for non-MultiGenerator generators (unchanged behavior).
- Error wrapping format preserved: `fmt.Errorf("releasedocs: generator %s: %w", gen.Kind(), err)`.

### Task 4: Pages Publisher + Config

`internal/releasedocs/publishers/pages/pages.go`:
- Replaced hardcoded `string(art.Kind)+".md"` with 3-line guard:
  1. `filename := art.Filename`
  2. `if filename == "" { filename = string(art.Kind) + ".md" }`
  3. `p := path.Join(dir, "releases", rc.ToRef, filename)`
- The `expectedPrefix` traversal guard (T-02-07, T-03-01) is preserved and applies to the computed path regardless of whether `Filename` is set — adversarial filenames with `../` are still rejected.
- All existing tests (TestDeterministicPaths, TestConfiguredBranchAndDir, TestIdempotentOverwrite, etc.) pass with `Filename: ""` artifacts producing identical `{kind}.md` paths.

`internal/config/config.go`:
- `APIDocsConfig` struct — embeds `ArtifactConfig` inline (yaml:",inline") + adds `SpecPath string` (yaml:"specPath"). Mirrors `ReleaseNotesConfig` pattern exactly.
- `ReleaseArtifacts.APIDocs APIDocsConfig` field (yaml:"apiDocs") added.
- SpecPath default `""` → apidocs generator uses fallback discovery list (D-02).

## Verification Results

```
go build ./...                                              OK
go vet ./internal/releasedocs/...                          OK
go test -race ./internal/releasedocs/...                   ALL PASS (9 packages)
go test ./internal/config/...                              OK
grep Filename releasedocs.go                               PASS
grep KindAPIDocs releasedocs.go                            PASS
grep MultiGenerator releasedocs.go                         PASS
grep GenerateMulti dispatcher.go                           PASS
grep APIDocsConfig config.go                               PASS
grep art.Filename pages.go                                 PASS
test -s assets/redoc.standalone.js                         PASS (1097271 bytes)
grep pb33f/libopenapi go.mod                               PASS (v0.37.3)
grep pb33f/libopenapi-validator go.mod                     PASS (v0.13.8)
No replace directives in go.mod                            PASS
```

## Deviations from Plan

None — plan executed exactly as written. Task 1 was pre-approved by the human; Tasks 2, 3, 4 executed atomically with individual commits.

## Known Stubs

None. The cross-cutting model changes are complete and functional. No apidocs generator code (parse/render/generate) was written in this plan — that is the scope of Plans 02-05 which depend on these contracts.

## Threat Flags

No new threat surface introduced. All mitigations from the plan's threat model are implemented:

| Threat ID | Status |
|-----------|--------|
| T-03-01 (Tampering via art.Filename → pages path) | Mitigated — `expectedPrefix` guard preserved and applied to Filename-computed path |
| T-03-02 (Vendored redoc.standalone.js) | Mitigated — sha256 recorded in .redoc-version; MIT license verified at human checkpoint |
| T-03-SC (go get of pb33f packages) | Mitigated — human checkpoint verified before install |

## Self-Check: PASSED

All created/modified files exist on disk and all task commits are reachable in git history.

| Check | Result |
|-------|--------|
| internal/releasedocs/releasedocs.go | FOUND |
| internal/releasedocs/dispatcher.go | FOUND |
| internal/releasedocs/publishers/pages/pages.go | FOUND |
| internal/config/config.go | FOUND |
| assets/redoc.standalone.js | FOUND |
| .redoc-version | FOUND |
| commit c169d65 (deps + bundle) | FOUND |
| commit 6b3c392 (model + dispatcher) | FOUND |
| commit 5e7525e (pages + config) | FOUND |
