---
phase: 03-api-docs-openapi
verified: 2026-06-06T00:00:00Z
status: passed
human_verification_note: "Offline Redoc HTML render confirmed by user during 03-05 blocking-human checkpoint (2026-06-06): /tmp/api-reference.html renders fully offline, no failed network requests."
score: 3/3 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Generate an api-reference.html from a petstore fixture and open it offline in a browser"
    expected: "The Redoc API reference renders completely (endpoints, schemas, parameters visible) with no network requests and no console errors"
    why_human: "Visual rendering and absence of runtime network requests cannot be asserted by go test; the NoCDN + Deterministic unit tests cover the testable invariants"
---

# Phase 03: API Docs / OpenAPI Verification Report

**Phase Goal:** For a supported framework, Cadoo derives an OpenAPI spec and a rendered API reference from the repo's code at release time and publishes them to pages.
**Verified:** 2026-06-06
**Status:** human_needed
**Re-verification:** No — initial verification

---

## Goal-Wording Delta: ROADMAP vs Plan Contract

This framing note is addressed explicitly per the verification brief.

**ROADMAP goal wording:** "Cadoo _derives_ an OpenAPI spec...from the repo's _code_" — language implying extraction from source annotations/routes.

**Plan contract (03-CONTEXT.md, D-01):** The 5 PLAN.md files deliberately scoped the work to _ingesting a committed spec_. D-01 is explicit: "The apidocs generator renders a committed spec — it does NOT derive OpenAPI from source code." The CONTEXT.md deferred-ideas section explicitly records "Derive OpenAPI from source code...Explicitly out of scope for Phase 3."

**Verdict on divergence:** The PLAN contract is the binding execution contract for this phase. The CONTEXT.md decisions (D-01 through D-10) were made by the user/planner with full awareness of the ROADMAP wording, as evidenced by D-03: "'supported framework set' reduces to 'supported spec versions' — since we render a committed spec, the framework that produced it is irrelevant." The success criteria in ROADMAP.md are evaluated against D-01's reinterpretation: SC-1's "from the code" = reading the spec file committed in the repo tree; SC-3's "outside the supported framework set" = unsupported spec version or absent spec file.

**Finding for the user:** The literal ROADMAP wording ("derives...from the repo's code") is not met — there is no annotation scraping, route-AST analysis, or framework detection. What is delivered is a committed-spec ingestor that is framework-agnostic by design. The plan-level decisions D-01/D-03 were the authoritative scoping mechanism, and those decisions are fully implemented. If future phases are expected to deliver source-derived extraction, that should be tracked as a separate roadmap phase (it is already recorded as a deferred idea in 03-CONTEXT.md).

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | For a repo in the supported spec-version set (Swagger 2.0, OAS 3.0.x, OAS 3.1.x), the apidocs generator produces three artifacts: the raw OpenAPI YAML spec, a self-contained Redoc HTML reference, and a deterministic Markdown reference | VERIFIED | `GenerateMulti` in `apidocs.go` orchestrates discover→parse→renderHTML→renderMarkdown and returns exactly 3 artifacts; `TestGenerate_ThreeArtifacts`, `TestGenerate_Swagger2`, `TestGenerate_OAS3`, `TestGenerate_OAS31` all pass (36 tests green) |
| 2 | The generated API docs and OpenAPI are published to pages at deterministic paths and are idempotent across re-runs | VERIFIED | `pages.go` uses `path.Join(dir, "releases", rc.ToRef, filename)` with `art.Filename` override; `TestPublish_APIDocs_Paths` asserts `.yaml`/`.html`/`.md` route to `docs/releases/v1.1.0/openapi.yaml`, `api-reference.html`, `api-reference.md`; `TestIdempotent_APIDocs` asserts two Publish calls hit identical paths (2 tests green); the same generator inputs produce byte-identical artifacts (`TestBuildRedocHTML_Deterministic`, `TestRenderMarkdown_Golden`) |
| 3 | A repo outside the supported set (no spec found, unsupported version, parse failure, validation failure, oversized spec) degrades gracefully — apidocs skipped with a logged reason, sibling generators unaffected | VERIFIED | `GenerateMulti` returns `(nil, nil)` on every failure path; `TestGenerate_NoSpec_Skips`, `TestGenerate_ParseFailure_Skips`, `TestGenerate_ValidationFailure_Skips`, `TestGenerate_UnsupportedVersion_Skips`, `TestLoadSpec_OOMGuard_Oversized` all pass; dispatcher wraps errors as non-nil only when a generator itself returns non-nil, which never happens on skip |

**Score:** 3/3 truths verified (automated checks)

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/releasedocs/releasedocs.go` | `Artifact.Filename`, `KindAPIDocs`, `MultiGenerator` interface | VERIFIED | All three present; `KindAPIDocs = "apidocs"`, `MultiGenerator.GenerateMulti` declared; compile-time interface assertions in `apidocs.go` confirm the Generator also satisfies MultiGenerator |
| `internal/releasedocs/dispatcher.go` | `MultiGenerator` type-assertion spreading `[]Artifact` | VERIFIED | Lines 112-118: `if mg, ok := gen.(MultiGenerator); ok { multi, err := mg.GenerateMulti(ctx, rc); ... arts = append(arts, multi...) }` |
| `internal/releasedocs/publishers/pages/pages.go` | `art.Filename` override with traversal guard | VERIFIED | Lines 96-109: `filename := art.Filename; if filename == "" { filename = string(art.Kind)+".md" }; p := path.Join(...); if !strings.HasPrefix(p, expectedPrefix) { skip }` |
| `internal/config/config.go` | `APIDocsConfig` with embedded `ArtifactConfig` + `SpecPath` field | VERIFIED | Lines 123-132: `type APIDocsConfig struct { ArtifactConfig yaml:",inline"; SpecPath string yaml:"specPath" }` |
| `internal/releasedocs/generators/apidocs/assets/redoc.standalone.js` | Vendored Redoc bundle (go:embed target) | VERIFIED | 1,097,271 bytes; `.redoc-version` records version=2.5.3, sha256, MIT license |
| `internal/releasedocs/generators/apidocs/discover.go` | `discoverSpec` with fallback loop + isMissingFile | VERIFIED | Implements D-02 ordered fallback list; exact-path when `specPath` set; 404 tolerance; non-404 log-and-continue |
| `internal/releasedocs/generators/apidocs/parse.go` | `loadSpec` with OOM guard + SSRF guard + version detection | VERIFIED | `AllowRemoteReferences:false`, `AllowFileReferences:false`; `maxSpecSize = 5*1024*1024` checked before parse; OAS 3.x validated via libopenapi-validator; Swagger 2.0 isolated in `parseSwagger2` with deprecation TODO |
| `internal/releasedocs/generators/apidocs/render_html.go` | `go:embed` Redoc bundle + `buildRedocHTML` with sorted-key JSON | VERIFIED | `//go:embed assets/redoc.standalone.js`; `yamlToJSON` walks `yaml.Node` tree with sorted `mappingToJSON` keys; no `json.Marshal(map[string]any)` |
| `internal/releasedocs/generators/apidocs/render_markdown.go` | `renderMarkdown` via `text/template`, injection-escaped | VERIFIED | `//go:embed presets/apidocs.tmpl`; `escapeField` applies backslash-escape of Markdown chars + `html/template.HTMLEscapeString` + newline→`<br>`; `TestEscapeField_MarkdownControlChars` passes |
| `internal/releasedocs/generators/apidocs/presets/apidocs.tmpl` | Deterministic Markdown preset template | VERIFIED | 349 bytes; `//go:embed presets/apidocs.tmpl` wires it |
| `internal/releasedocs/generators/apidocs/apidocs.go` | `Kind/Enabled/Generate(compliance)/GenerateMulti` | VERIFIED | `GenerateMulti` returns 3 artifacts or `(nil, nil)`; `Enabled` coerces empty `When` to `"always"` (D-08); compile-time assertions at bottom of file |
| `internal/releasedocs/defaults/defaults.go` | `apidocs.New()` in `DefaultGenerators` | VERIFIED | Line 40: `apidocs.New()` appended last after `blog.New()` |
| `internal/releasedocs/generators/apidocs/testdata/petstore_v2.yaml` | Swagger 2.0 fixture | VERIFIED | 2.8K; `swagger: "2.0"` |
| `internal/releasedocs/generators/apidocs/testdata/petstore_v3.yaml` | OAS 3.0 fixture | VERIFIED | 6.2K; `openapi: 3.0.x` |
| `internal/releasedocs/generators/apidocs/testdata/petstore_v31.yaml` | OAS 3.1 fixture | VERIFIED | 3.6K; `openapi: 3.1.0` |
| `internal/releasedocs/generators/apidocs/testdata/invalid.yaml` | Malformed YAML fixture | VERIFIED | 164 bytes |
| `internal/releasedocs/generators/apidocs/testdata/remote_ref.yaml` | Remote $ref fixture | VERIFIED | 1.3K |
| `internal/releasedocs/generators/apidocs/testdata/golden/markdown_v3.golden` | OAS 3.0 Markdown golden | VERIFIED | 832 bytes |
| `internal/releasedocs/generators/apidocs/testdata/golden/markdown_v2.golden` | Swagger 2.0 Markdown golden | VERIFIED | 448 bytes |
| `internal/releasedocs/generators/apidocs/apidocs_test.go` | Table-driven scaffold with `fakeFetcher` | VERIFIED | 21.8K; all validation-mapped test names from 03-VALIDATION.md present |
| `internal/releasedocs/publishers/pages/pages_apidocs_test.go` | `TestPublish_APIDocs_Paths` + `TestIdempotent_APIDocs` | VERIFIED | Both tests pass |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `dispatcher.go` | `MultiGenerator.GenerateMulti` | `gen.(MultiGenerator)` type assertion | VERIFIED | Line 112: `if mg, ok := gen.(MultiGenerator); ok` |
| `pages.go` | `art.Filename` | Filename override with fallback to `string(art.Kind)+".md"` | VERIFIED | Lines 96-100: guards present |
| `render_html.go` | `assets/redoc.standalone.js` | `//go:embed assets/redoc.standalone.js` | VERIFIED | Line 21 |
| `render_markdown.go` | `presets/apidocs.tmpl` | `//go:embed presets/apidocs.tmpl` + `text/template.Execute` | VERIFIED | Lines 47, 94-95 |
| `apidocs.go` | `discoverSpec + loadSpec + buildRedocHTML + renderMarkdown` | `GenerateMulti` orchestration | VERIFIED | Lines 64, 72, 88, 101 |
| `defaults.go` | `apidocs.New()` | Appended to `DefaultGenerators` slice | VERIFIED | Line 40 |
| `parse.go` | `libopenapi.NewDocumentWithConfiguration` | `AllowRemoteReferences:false, AllowFileReferences:false` | VERIFIED | Lines 91-92 |
| `discover.go` | `releasedocs.FileFetcher` | Type-assert `rc.Provider.(releasedocs.FileFetcher)` then `FetchFileFromRef` | VERIFIED | Lines 53, 61, 66 |

### Data-Flow Trace (Level 4)

The apidocs generator is a publisher, not a data-rendering UI component — its data source is the VCS (committed spec bytes) and its outputs are committed files. The data flow is fully synchronous and tested end-to-end:

| Path | Data Variable | Source | Produces Real Data | Status |
|------|---------------|--------|-------------------|--------|
| `discoverSpec` → `loadSpec` | `specBytes []byte` | `ff.FetchFileFromRef(ctx, repo, toRef, path)` → VCS adapter | Yes — bytes from actual file fetch (or fake in tests) | FLOWING |
| `loadSpec` → `buildRedocHTML` | `*specModel` | libopenapi parse + path extraction | Yes — sorted path model | FLOWING |
| `buildRedocHTML` | `redocBundle []byte` | `//go:embed assets/redoc.standalone.js` | Yes — 1.097 MB bundle at compile time | FLOWING |
| `renderMarkdown` | `apiDocsData` | `buildTemplateData(model)` | Yes — escaped spec fields | FLOWING |
| All 3 artifacts | `[]releasedocs.Artifact` | `GenerateMulti` return | Pages publisher commits via `UpsertFile` | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| 36 apidocs tests pass (D-01..D-10 + security + golden) | `go test -race -run 'TestAPIDocs\|TestEnabled\|TestDiscoverSpec\|TestGenerate\|TestBuildRedoc\|TestRenderMarkdown' ./internal/releasedocs/generators/apidocs/...` | 36 passed | PASS |
| Pages apidocs path + idempotency tests pass | `go test -race -run 'TestPublish_APIDocs_Paths\|TestIdempotent_APIDocs' ./internal/releasedocs/publishers/pages/...` | 2 passed | PASS |
| Full releasedocs + config suite (no Phase 1/2 regression) | `go test -race ./internal/releasedocs/... ./internal/config/...` | 224 passed in 13 packages | PASS |
| Full suite (470 tests) | `go test -race -count=1 ./...` | 470 passed in 69 packages | PASS |
| Build succeeds | `go build ./...` | Success | PASS |
| go vet clean | `go vet ./internal/releasedocs/... ./internal/config/...` | No issues | PASS |
| libopenapi modules resolved | `go list -m github.com/pb33f/libopenapi github.com/pb33f/libopenapi-validator` | `v0.37.3` + `v0.13.8` | PASS |
| CR-01 YAML numeric forms produce valid JSON | `go test -race -run TestScalarToJSON_NumericForms ./internal/releasedocs/generators/apidocs/...` | Passed (hex 0xAF, octal 0o17, underscore 1_000, .inf, .nan all re-encoded) | PASS |
| WR-01 depth guard prevents stack overflow | `go test -race -run TestNodeToJSON_DepthGuard ./internal/releasedocs/generators/apidocs/...` | Passed | PASS |
| WR-02 Markdown injection mitigation (now fixed) | `go test -race -run TestEscapeField_MarkdownControlChars ./internal/releasedocs/generators/apidocs/...` | Passed (`\|`, `\n`, `#` escaped) | PASS |
| WR-04 OOM guard tested programmatically (WR-05 fixed) | `go test -race -run 'TestLoadSpec_OOMGuard' ./internal/releasedocs/generators/apidocs/...` | 2 passed (oversized + boundary) | PASS |

### Probe Execution

No probe scripts declared or present for this phase (`scripts/*/tests/probe-*.sh` absent). Phase verification was done via `go test` which is the project's canonical validation mechanism (per 03-VALIDATION.md).

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| REQ-release-artifact-generation (api-docs + openapi) | 03-01, 03-02, 03-03, 03-04, 03-05 | After a release, Cadoo generates api-docs + openapi from the committed spec | SATISFIED | `GenerateMulti` emits `openapi.yaml` (raw bytes), `api-reference.html` (Redoc), `api-reference.md` (Markdown); all three published via pages publisher |
| REQ-per-artifact-toggles | 03-05 | `Enabled(cfg, bump)` gates all three outputs as a family | SATISFIED | `Enabled` reads `cfg.Artifacts.APIDocs.ArtifactConfig`, coerces empty `When` to `"always"`, delegates to `releasedocs.Enabled`; `TestEnabled` exercises all bump × when combinations |
| REQ-configurable-templates | 03-04 | Preset Markdown template; override via `template:` field | SATISFIED | `presets/apidocs.tmpl` is the embedded preset; `APIDocsConfig.ArtifactConfig.Template` field available for override (standard releasedocs template loading applies); `TestRenderMarkdown_Golden` validates preset output |
| REQ-publish-destinations (pages) | 03-01 | Pages publisher routes apidocs artifacts to deterministic paths | SATISFIED | `pages.go` Filename override routes `.yaml`/`.html`/`.md`; `TestPublish_APIDocs_Paths` + `TestIdempotent_APIDocs` pass |

### Anti-Patterns Found

Code review (03-REVIEW.md) identified 1 critical + 5 warnings. All were resolved before phase submission per REVIEW.md frontmatter `resolution: all_fixed`. The verifier re-checked the three most important:

| File | Issue | Resolution | Verified |
|------|-------|-----------|---------|
| `render_html.go` | CR-01: YAML hex/octal/inf/nan emitted as raw invalid JSON | Fixed: `scalarToJSON` decodes through `yaml.Node.Decode` then `json.Marshal`; `TestScalarToJSON_NumericForms` passes | YES |
| `render_html.go` | WR-01: `nodeToJSON` had no depth guard (stack-overflow) | Fixed: `maxNodeDepth = 1000` check added; `TestNodeToJSON_DepthGuard` passes | YES |
| `render_markdown.go` | WR-02: `escapeField` only HTML-escaped, not Markdown chars | Fixed: `mdBackslashReplacer` added for `\|`, `` ` ``, `*`, `_`, `#`, `[`, `]`; `TestEscapeField_MarkdownControlChars` passes | YES |
| `parse.go` | WR-03: comment said "sorted" but params were in source order | Fixed: comment corrected to "ordered list of parameters in spec source order" | YES (comment matches code) |
| `testdata/oversized.yaml` | WR-04/WR-05: 5.25 MB blob committed but unreferenced | Fixed: blob removed; `oom_test.go` generates oversized input programmatically with `bytes.Repeat`; `TestLoadSpec_OOMGuard_Oversized` + `TestLoadSpec_OOMGuard_Boundary` pass | YES |

Remaining INFO items from the review (IN-01 dead bool arm, IN-02 heuristic 404, IN-03 dead Generate path, IN-04 duplicate JSON keys) are non-blocking quality notes; none prevent goal achievement.

**Anti-pattern scan on phase-modified files:**

No `TBD`, `FIXME`, or `XXX` markers found in any of the 11 modified files. One `TODO` marker exists at `parse.go:213` ("TODO: libopenapi v2 model is deprecated and will be removed; revisit when libopenapi drops it") — this is an intentional, documented deprecation notice with clear audit trail (T-03-05 accept/isolated), not an unresolved debt marker.

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `parse.go` | 213 | `TODO: libopenapi v2 model is deprecated...` | INFO | Intentional deprecation notice with documented rationale (T-03-05); not an unresolved gap |

### Human Verification Required

#### 1. Offline Redoc HTML Rendering

**Test:** Generate an `api-reference.html` from a petstore fixture (use the golden-update path from `apidocs_test.go TestBuildRedocHTML_Deterministic`, or write a small throwaway `go test` helper that calls `buildRedocHTML` over `testdata/petstore_v3.yaml` and writes the output to `/tmp/api-reference.html`). Disable network access (airplane mode or offline browser profile). Open the file in a browser.

**Expected:** The Redoc API reference renders completely — endpoint paths, descriptions, parameter tables, and response schemas are all visible — with no network requests in the browser console (no red failed requests), no blank page, and no JavaScript errors.

**Why human:** Visual rendering and the absence of runtime CDN requests cannot be asserted by `go test`. The `TestBuildRedocHTML_NoCDN` unit test confirms no `cdn.redoc.ly` or external `src=` appears in the HTML text, and `TestBuildRedocHTML_Deterministic` confirms byte-identity. But confirming that the inlined 1.097 MB Redoc bundle actually initializes and renders the inlined spec JSON correctly in a real browser is a visual check. This is documented as the only manual-only verification in `03-VALIDATION.md`.

---

## Gaps Summary

No automated gaps found. All 3 success criteria are verified by green tests. The only open item is the browser rendering human check (SC-1 "rendered API reference" visual confirmation), which is why `status: human_needed`.

---

_Verified: 2026-06-06_
_Verifier: Claude (gsd-verifier)_
