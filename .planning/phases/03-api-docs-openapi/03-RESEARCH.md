# Phase 3: API Docs / OpenAPI — Research

**Researched:** 2026-06-05
**Domain:** Go OpenAPI parsing/validation (pb33f/libopenapi), Redoc HTML rendering (go:embed), artifact model extension, generator family integration
**Confidence:** HIGH (codebase analysis) / MEDIUM (external library details verified via pkg.go.dev + official sources)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Render a committed spec — NOT derive from source code. Read the spec file via `FileFetcher.FetchFileFromRef`.
- **D-02:** Spec discovery: `apiDocs.specPath` if set; else ordered fallback: `openapi.yaml` → `openapi.yml` → `openapi.json` → `docs/openapi.yaml` → `api/openapi.yaml`. Fetched at `rc.ToRef`.
- **D-03:** Supported set = supported spec versions (Swagger 2.0 + OpenAPI 3.0.x + 3.1.x), not web framework.
- **D-04:** Three artifacts per run: (1) raw spec, (2) self-contained HTML (Redoc), (3) deterministic Markdown.
- **D-05:** HTML offline/no-CDN: Redoc bundle vendored and embedded via `go:embed`. Byte-identical across re-runs.
- **D-06:** Markdown reference via `text/template`, mirroring `internal/releasedocs/template` pattern.
- **D-07:** Single `apiDocs` config block: `enabled` + `when:` gates all three as a family.
- **D-08:** Default `when: always`. `specPath` defaults to `""`.
- **D-09:** Validator spanning Swagger 2.0 + OpenAPI 3.0.x + 3.1.x: favor `pb33f/libopenapi`.
- **D-10:** Skip-with-logged-reason on any failure (no spec, parse failure, validation failure, unsupported version). Never publish invalid docs. Always continue sibling artifacts.

### Claude's Discretion

- Raw spec passthrough vs re-serialized for the `.yaml` artifact (lean toward raw passthrough).
- How three outputs map onto `Artifact`/`ArtifactKind` (one Kind emitting multiple files vs. multiple Kinds).
- Exact Markdown layout, Redoc bundle version, golden-file fixtures.
- Whether to introduce `KindOpenAPI`/`KindAPIDocs` constants vs. reuse a family Kind.

### Deferred Ideas (OUT OF SCOPE)

- Derive OpenAPI from source code (swaggo-style, route-AST analysis).
- LLM-assisted spec derivation or enrichment.
- Swagger-UI as renderer / interactive "try it" console.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| REQ-release-artifact-generation (api-docs + openapi) | After a release, generate API docs and OpenAPI spec from the repo's committed spec file. Acceptance: derived from code (spec file), deterministic, idempotent publish to pages at deterministic paths. | libopenapi parses Swagger 2.0 + OAS 3.0/3.1; Redoc bundle embeds offline; pages publisher extended with per-artifact filename; generator family pattern mirrors blog/changelog. |
| REQ-per-artifact-toggles | `enabled` + `when:` condition gates per-artifact (all three apidocs outputs as a family). | Mirrors `releasedocs.Enabled()` gate; `apiDocs` config block extends `config.ReleaseArtifacts`. |
| REQ-configurable-templates | Preset + override template layers. | `internal/releasedocs/template` pattern re-used for Markdown reference. |
| REQ-publish-destinations (pages) | Publish to docs branch at deterministic paths, idempotent across re-runs. | Cross-cutting constraint resolved: `Artifact.Filename` field added; pages publisher uses it instead of hardcoded `.md`. |

</phase_requirements>

---

## Summary

Phase 3 adds an `apidocs` generator to `internal/releasedocs/generators/apidocs` that (a) fetches a committed OpenAPI/Swagger spec via `FileFetcher.FetchFileFromRef`, (b) validates it using `pb33f/libopenapi` (spanning Swagger 2.0 + OAS 3.0/3.1), and (c) emits three artifacts: the raw spec bytes, a self-contained Redoc HTML bundle, and a deterministic Markdown reference rendered with `text/template`. All three are published to pages at deterministic paths via the existing Phase 2 pages publisher, with one minimal cross-cutting change: `Artifact` gains an optional `Filename` field so each artifact can carry its own extension.

**CRITICAL FINDING — libopenapi Swagger 2.0 v2 model deprecation:** The `pb33f/libopenapi` `BuildV2Model()` function exists and parses Swagger 2.0, but the project's official docs explicitly state: "the v2 model package in libopenapi is no longer maintained and will be removed in a future version. DO NOT take a dependency on it." [CITED: pb33f.io/libopenapi/swagger/] This means the plan must account for this: validation via `libopenapi-validator` does NOT support Swagger 2.0 (it is OpenAPI 3+ only). The implementation must use a two-path approach: for Swagger 2.0, `NewDocument()` + `BuildV2Model()` for parsing and path iteration (accepting the deprecation risk); for OAS 3.x, `BuildV3Model()` + `libopenapi-validator`. The apidocs generator should log a clear warning when it encounters a Swagger 2.0 spec, since the v2 model will eventually be removed.

**Primary recommendation:** Use `github.com/pb33f/libopenapi` for all spec parsing with separate v2/v3 code paths; use `github.com/pb33f/libopenapi-validator` for OAS 3.x schema validation only; vendor the Redoc v2.5.3 standalone bundle as a `//go:embed` file; extend `Artifact` with an optional `Filename` string so pages publisher can route `.yaml`/`.html`/`.md` without breaking existing tests.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Spec discovery and fetching | Generator (apidocs) | FileFetcher (VCS adapter) | Generator type-asserts FileFetcher off rc.Provider; VCS adapter does the HTTP fetch |
| Spec parsing + version detection | Generator (apidocs) | libopenapi | All spec I/O is generator-internal; libopenapi is an implementation detail |
| Spec validation (OAS 3.x) | Generator (apidocs) | libopenapi-validator | Validator lives inside Generate(); skip-on-failure path handled here |
| Spec validation (Swagger 2.0) | Generator (apidocs) | libopenapi BuildV2Model | Limited: parse success = "valid enough"; no separate schema validator for v2 |
| HTML rendering (Redoc) | Generator (apidocs) | go:embed (vendored bundle) | Generator builds the HTML string; embedded JS bundle is a static asset |
| Markdown rendering | Generator (apidocs) | text/template + rdtemplate pattern | Mirrors releasenotes/changelog; sorted path/op iteration |
| Artifact routing / extension | pages.Publisher | Artifact.Filename field | Publisher uses Filename if set, falls back to string(art.Kind)+".md" |
| Config toggle | config.ReleaseArtifacts.APIDocs | releasedocs.Enabled() | Single `apiDocs` block; gates all three outputs as a family |
| Registration | defaults.DefaultGenerators() | — | Adds apidocs.New() to the ordered slice |

---

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/pb33f/libopenapi` | v0.37.3 | Parse Swagger 2.0 + OAS 3.0/3.1, detect version, iterate paths/ops/schemas | Only Go library spanning all three versions in one module; MIT; actively maintained; enterprise-grade [CITED: pkg.go.dev/github.com/pb33f/libopenapi] |
| `github.com/pb33f/libopenapi-validator` | v0.13.8+ | Validate OAS 3.x documents against the OpenAPI schema (ValidateDocument) | Official companion validator from same author; MIT; OpenAPI 3+ only (Swagger 2.0 excluded per docs) [CITED: pkg.go.dev/github.com/pb33f/libopenapi-validator] |
| Redoc standalone bundle | v2.5.3 (vendored JS) | Offline self-contained HTML API reference | MIT; `<300 KB` gzipped; Redoc.init() accepts inline spec object; vendored as static asset via go:embed [CITED: github.com/Redocly/redoc/blob/main/LICENSE] |
| `text/template` (stdlib) | Go 1.26 (stdlib) | Deterministic Markdown reference from parsed spec | Zero-dependency; existing pattern in internal/releasedocs/template; golden-file testable |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `embed` (stdlib) | Go 1.16+ | Embed Redoc bundle and Markdown preset templates into binary | Used at compile time for air-gapped/no-egress; no runtime downloads |
| `gopkg.in/yaml.v3` | already in go.mod | Parse/marshal YAML spec for Markdown extraction if needed | Already a direct dependency; do not add a second YAML library |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `pb33f/libopenapi` | `getkin/kin-openapi` | kin-openapi is OpenAPI 3.x only — cannot handle Swagger 2.0 (confirmed [CITED: pkg.go.dev/github.com/getkin/kin-openapi/openapi3]). D-09 requires Swagger 2.0. |
| Vendored Redoc bundle | Swagger-UI | Swagger-UI is explicitly deferred (CONTEXT.md deferred list). |
| Vendored Redoc bundle | CDN `<script>` tag | CDN violates D-05 (no-egress, air-gapped customers, Helm no-egress mode). |
| `libopenapi-validator` for v2 | Custom JSON-Schema validator | No production-quality Go Swagger 2.0 validator exists; warn and accept parse-success as "valid enough" for v2. |

**Installation:**
```bash
go get github.com/pb33f/libopenapi@v0.37.3
go get github.com/pb33f/libopenapi-validator
```

Redoc bundle is not a Go package — it is downloaded once from the npm registry or GitHub releases and committed under `internal/releasedocs/generators/apidocs/assets/redoc.standalone.js`.

**Version verification:**
```bash
# Both pb33f packages are Go modules — verify via go list after go get
go list -m github.com/pb33f/libopenapi
go list -m github.com/pb33f/libopenapi-validator
```

---

## Package Legitimacy Audit

> slopcheck was not available at research time. All packages below are tagged `[ASSUMED]` and the planner must gate each install behind a `checkpoint:human-verify` task.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `github.com/pb33f/libopenapi` | Go modules / pkg.go.dev | ~3 yrs | — | github.com/pb33f/libopenapi | [ASSUMED] | Approved — verified on pkg.go.dev + official docs site pb33f.io; MIT license confirmed |
| `github.com/pb33f/libopenapi-validator` | Go modules / pkg.go.dev | ~2 yrs | — | github.com/pb33f/libopenapi-validator | [ASSUMED] | Approved — companion module from same author; verified on pkg.go.dev; MIT |
| `redoc` (npm — vendored JS only, not a Go dep) | npm | ~9 yrs | high | github.com/Redocly/redoc | [ASSUMED] | Approved — well-known OSS project; MIT confirmed; bundle vendored locally |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none
*Slopcheck was unavailable — planner must add a `checkpoint:human-verify` task before each `go get`.*

---

## Architecture Patterns

### System Architecture Diagram

```
Release tag (VCS)
      |
      v
[apidocs.Generator.Generate()]
      |
      +---> [1] FileFetcher.FetchFileFromRef(repo, toRef, specPath)
      |             |
      |             +-- path not found (all fallbacks exhausted) --> SKIP (log reason)
      |             +-- parse error  --------------------------> SKIP (log reason)
      |             |
      |             v
      |         raw spec bytes
      |
      +---> [2] libopenapi.NewDocument(bytes)
      |             |
      |             +-- GetSpecInfo().SpecType == OpenApi2 --> BuildV2Model() --> v2paths
      |             +-- GetSpecInfo().SpecType == OpenApi3 --> BuildV3Model() + ValidateDocument()
      |             +-- unknown version -----------------------> SKIP (log reason)
      |             +-- validation errors (OAS 3.x only) -----> SKIP (log reason)
      |
      +---> [3a] Artifact{Kind: KindAPIDocs, Filename: "openapi.yaml", Content: rawSpecBytes}
      |
      +---> [3b] buildRedocHTML(rawSpecBytes, embeddedBundle) --> Artifact{..., Filename: "api-reference.html"}
      |
      +---> [3c] renderMarkdown(parsedPaths, template) ---------> Artifact{..., Filename: "api-reference.md"}
      |
      v
[pages.Publisher.Publish()]
      |
      +-- for each Artifact: UpsertFile at {dir}/releases/{toRef}/{art.Filename}
```

### Recommended Project Structure

```
internal/releasedocs/generators/apidocs/
├── apidocs.go               # Generator struct, Kind/Enabled/Generate
├── apidocs_test.go          # table-driven unit tests, golden files
├── discover.go              # spec discovery (fallback path list), FetchFileFromRef calls
├── parse.go                 # libopenapi NewDocument, version detection, validation, path iteration
├── render_html.go           # buildRedocHTML() — inline spec + embedded bundle → HTML string
├── render_markdown.go       # renderMarkdown() — text/template over sorted paths/ops
├── assets/
│   └── redoc.standalone.js  # vendored Redoc v2.5.3 (committed; go:embed target)
├── presets/
│   └── apidocs.tmpl         # Markdown preset template
└── testdata/
    ├── petstore_v2.yaml     # Swagger 2.0 fixture
    ├── petstore_v3.yaml     # OAS 3.0 fixture
    ├── petstore_v31.yaml    # OAS 3.1 fixture
    ├── invalid.yaml         # malformed YAML fixture
    └── golden/
        ├── markdown_v3.golden
        └── markdown_v2.golden
```

### Pattern 1: libopenapi Parse + Version Detection

```go
// Source: pkg.go.dev/github.com/pb33f/libopenapi (verified API)
import (
    "github.com/pb33f/libopenapi"
    "github.com/pb33f/libopenapi/what-changed/model"
    "github.com/pb33f/libopenapi-validator/validator"
)

func parseAndValidate(specBytes []byte) error {
    doc, err := libopenapi.NewDocument(specBytes)
    if err != nil {
        return fmt.Errorf("apidocs: parse spec: %w", err)
    }

    info := doc.GetSpecInfo()
    switch info.SpecType {
    case "openapi": // utils.OpenApi3 constant
        v3, errs := doc.BuildV3Model()
        if len(errs) > 0 {
            return fmt.Errorf("apidocs: build v3 model: %v", errs[0])
        }
        docValidator, _ := validator.NewValidator(doc)
        valid, valErrs := docValidator.ValidateDocument()
        if !valid {
            return fmt.Errorf("apidocs: spec validation failed: %v", valErrs[0].Message)
        }
        // iterate: v3.Model.Paths.PathItems.FromOldest()
    case "swagger": // utils.OpenApi2 constant
        // WARNING: BuildV2Model is deprecated, will be removed in a future version.
        // Use only for parsing and path iteration; no schema validation available.
        slog.Warn("apidocs: Swagger 2.0 spec — v2 model is deprecated in libopenapi; limited validation")
        v2, errs := doc.BuildV2Model()
        if len(errs) > 0 {
            return fmt.Errorf("apidocs: build v2 model: %v", errs[0])
        }
        _ = v2 // iterate paths
    default:
        return fmt.Errorf("apidocs: unsupported spec version %q", info.Version)
    }
    return nil
}
```

### Pattern 2: Redoc Self-Contained HTML (Offline)

The self-contained HTML pattern inlines the spec as a JSON literal inside a `<script>` block and passes it directly to `Redoc.init()` (no URL, no CDN). The Redoc standalone bundle is also inlined (not referenced via CDN `src`).

```go
// Source: Redoc standalone.tsx API (verified: Redoc.init accepts JSON object, not only URL)
// go:embed assets/redoc.standalone.js
var redocBundle []byte

func buildRedocHTML(specBytes []byte, bundle []byte) ([]byte, error) {
    // Convert YAML spec to JSON for inline injection (Redoc.init accepts JSON object).
    // Use encoding/json or gopkg.in/yaml.v3 to unmarshal YAML then re-marshal as JSON.
    specJSON, err := yamlToJSON(specBytes) // deterministic: no timestamps, no random IDs
    if err != nil {
        return nil, err
    }
    // Template: inline bundle + inline spec JSON.
    // The bundle string is static (pinned vendor file), the spec JSON is deterministic,
    // so the entire HTML output is byte-identical given the same inputs.
    const htmlTmpl = `<!DOCTYPE html>
<html>
  <head>
    <title>API Reference</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>body{margin:0;padding:0;}</style>
  </head>
  <body>
    <div id="redoc-container"></div>
    <script>%s</script>
    <script>Redoc.init(%s,{},document.getElementById('redoc-container'))</script>
  </body>
</html>`
    html := fmt.Sprintf(htmlTmpl, bundle, specJSON)
    return []byte(html), nil
}
```

**Determinism note:** `yamlToJSON` must produce sorted-key JSON (use `encoding/json` with struct types or a custom marshaler that sorts map keys). Do NOT use `json.Marshal` on `map[string]any` — Go maps have non-deterministic iteration order.

### Pattern 3: Markdown Template Data (Sorted Iteration)

Go maps are unordered. `libopenapi` path items are stored in an ordered-map type (`pb33f/ordered-map/v2`) that exposes a `FromOldest()` iterator guaranteed to preserve insertion order (spec file order). For paths not in spec-order in the source, apply `sort.Strings` on path keys before iteration.

```go
// Source: pkg.go.dev/github.com/pb33f/libopenapi (verified: FromOldest() ordered iteration)
type APIDocsData struct {
    Title   string
    Version string
    Tags    []TagSection
}

type TagSection struct {
    Name       string
    Operations []OperationItem
}

type OperationItem struct {
    Method      string // GET, POST, etc.
    Path        string
    Summary     string
    Description string
    Parameters  []ParamItem
}
```

Iteration pattern using libopenapi's ordered-map:
```go
// v3Model.Model.Paths.PathItems.FromOldest() preserves spec-file order
// For additional determinism (golden-file tests), collect path keys and sort:
var pathKeys []string
for pathName := range v3Model.Model.Paths.PathItems.KeysFromOldest() { ... }
sort.Strings(pathKeys)
```

### Pattern 4: Artifact + Pages Publisher Extension (Cross-Cutting Constraint)

The current pages publisher path formula is:
```go
p := path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")
```

The `apidocs` generator needs `.yaml`, `.html`, `.md`. The minimal fix is to add an optional `Filename` field to `Artifact`:

```go
// In releasedocs.go
type Artifact struct {
    Kind     ArtifactKind
    Content  []byte
    // Filename, when non-empty, overrides the default "{kind}.md" path
    // computed by publishers. Use this for artifacts that are not Markdown
    // (e.g. "openapi.yaml", "api-reference.html", "api-reference.md").
    Filename string
}
```

In `pages.Publisher.Publish()`, replace:
```go
p := path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")
```
with:
```go
filename := art.Filename
if filename == "" {
    filename = string(art.Kind) + ".md"
}
p := path.Join(dir, "releases", rc.ToRef, filename)
```

**Backward-compatibility:** All existing artifacts (changelog, releasenotes, blog) have `Filename: ""` (zero value), so they continue to produce `{kind}.md` paths. No existing tests change their expected paths. The existing `TestDeterministicPaths` test remains green.

### Anti-Patterns to Avoid

- **Using libopenapi-validator for Swagger 2.0:** The validator only supports OAS 3.x. Calling `NewValidator(doc)` on a v2 doc will either error or produce meaningless results. Use `BuildV2Model()` success/failure as the Swagger 2.0 "validation" gate.
- **json.Marshal on map[string]any for HTML spec injection:** Non-deterministic key order breaks golden-file tests. Marshal via the parsed libopenapi high-level model's `MarshalYAML()` then convert, or use `encoding/json` with a stable-key approach.
- **Bypassing the `.Filename` field for pages extension:** Don't add new `ArtifactKind` constants like `KindAPIDocs_YAML` just to carry extension info. The `Filename` field approach is cleaner and preserves the existing `Kind` enum for generator registration.
- **Calling `FetchFileFromRef` without a 404-tolerance wrapper:** Use the same `isMissingFile()` pattern from `dispatcher.go:179-188` when probing fallback paths.
- **Storing the Redoc bundle in `go:embed` as a `string` variable:** Use `[]byte` or `embed.FS` to avoid a copy-on-use.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| OpenAPI YAML/JSON parsing | Custom YAML parser + path walker | `pb33f/libopenapi` | Handles JSON references (`$ref`), multi-document, anchors, 3.1 JSON Schema dialect |
| OAS 3.x schema validation | Custom JSON Schema validator | `pb33f/libopenapi-validator` | JSON Schema 2020-12 compliance for OAS 3.1; 100s of validation cases |
| Swagger 2.0 validation | Custom schema validator | Accept BuildV2Model success; log deprecation warning | No maintained pure-Go Swagger 2.0 schema validator |
| HTML rendering | Custom React/Svelte component | Redoc (vendored) | Production-quality rendering; MIT; handles complex schema display |
| Ordered map iteration | Custom sort wrapper | libopenapi's `FromOldest()` (ordered-map) | Library already preserves spec-file order |

**Key insight:** OpenAPI reference resolution (`$ref`) is the hardest part — a spec can reference remote files, local files, or internal anchors. libopenapi handles all of this; hand-rolling would miss dozens of edge cases.

---

## Common Pitfalls

### Pitfall 1: libopenapi Swagger 2.0 v2 Model Deprecation
**What goes wrong:** Implementing the full Swagger 2.0 path using `BuildV2Model()` and then hitting a libopenapi major version bump that removes the function entirely.
**Why it happens:** The libopenapi docs explicitly say "DO NOT take a dependency on it."
**How to avoid:** Isolate all v2-model calls behind a single `parseSwagger2()` function. Add a `TODO: libopenapi v2 model is deprecated; revisit when libopenapi removes it` comment. Treat Swagger 2.0 as a best-effort path.
**Warning signs:** libopenapi changelog mentions v2 model removal.

### Pitfall 2: Non-Deterministic HTML Output
**What goes wrong:** Two runs with the same spec produce different HTML bytes (e.g. different JSON key order), breaking golden-file tests.
**Why it happens:** `json.Marshal(map[string]any{...})` iterates Go maps in non-deterministic order.
**How to avoid:** Convert YAML→JSON via a stable path: use `libopenapi`'s render (which uses the ordered-map) or use `gopkg.in/yaml.v3` to decode to a `yaml.Node` then encode to JSON using a node walker that sorts map keys.
**Warning signs:** Golden file test flakes across test runs.

### Pitfall 3: Pages Publisher `.md` Hardcode Breaks Existing Tests
**What goes wrong:** Adding the `Filename` field to `Artifact` without updating the pages publisher causes `.yaml` and `.html` artifacts to still get the `.md` extension.
**Why it happens:** The publisher's path formula `string(art.Kind)+".md"` is not conditional.
**How to avoid:** The minimal edit is 3 lines in `pages.go` (guard + use `art.Filename` when non-empty). Existing tests all pass because `Filename` is `""` for legacy kinds.
**Warning signs:** `TestDeterministicPaths` in `pages_test.go` starts failing if the publisher is changed to unconditionally use `art.Filename`.

### Pitfall 4: Redoc Bundle Size in Binary
**What goes wrong:** The embedded `redoc.standalone.js` adds ~300 KB (gzipped) to the binary size.
**Why it happens:** go:embed adds the uncompressed file size to the binary.
**How to avoid:** The raw bundle is ~1.2 MB uncompressed. Accept this: the binary is a server process, not a CLI one-shot tool, and the bundle is embedded once. Document the size in the commit.
**Warning signs:** Build CI size check fails if a size limit is enforced.

### Pitfall 5: Spec Fallback Discovery — 404 Tolerance
**What goes wrong:** FetchFileFromRef returns a network-level error (e.g. auth failure) instead of a 404, causing the fallback loop to stop too early.
**Why it happens:** `isMissingFile()` matches "404"/"not found" strings; a rate-limit or auth error looks different.
**How to avoid:** In the fallback discovery loop, use `isMissingFile(err)` to skip to the next path; for non-404 errors, log them and skip rather than abort (align with D-10: skip with logged reason).
**Warning signs:** Generator skips when only a transient auth error occurred.

### Pitfall 6: Import Cycle via libopenapi-validator
**What goes wrong:** `libopenapi-validator` brings in `goccy/go-yaml` and `santhosh-tekuri/jsonschema/v6` as deps, which may conflict with `gopkg.in/yaml.v3` (already in go.mod).
**Why it happens:** Two different YAML libraries coexist; `go.sum` handles it, but `yaml.v3` and `go-yaml` are different and non-interchangeable.
**How to avoid:** Use `gopkg.in/yaml.v3` (existing dep) for all spec-reading in the generator itself; let `libopenapi-validator` use its own YAML library internally. Do not try to share parsed YAML between the two.
**Warning signs:** `go mod tidy` fails or produces unexpected `replace` directives.

---

## Code Examples

### Minimal "parse → detect version → validate → iterate paths" Sketch

```go
// Source: verified against pkg.go.dev/github.com/pb33f/libopenapi and
//         pkg.go.dev/github.com/pb33f/libopenapi-validator
package apidocs

import (
    "fmt"
    "log/slog"
    "sort"

    "github.com/pb33f/libopenapi"
    "github.com/pb33f/libopenapi-validator/validator"
)

// specInfo holds the parsed model and version string for use in rendering.
type specInfo struct {
    version string // "2.0", "3.0.x", "3.1.x"
    title   string
    v3Paths []parsedPath // populated for OAS 3.x
    v2Paths []parsedPath // populated for Swagger 2.0
}

// loadSpec parses specBytes, validates (OAS 3.x only), and returns specInfo.
// Returns an error (with reason) on any failure — callers skip+log per D-10.
func loadSpec(specBytes []byte) (*specInfo, error) {
    doc, err := libopenapi.NewDocument(specBytes)
    if err != nil {
        return nil, fmt.Errorf("parse: %w", err)
    }
    info := doc.GetSpecInfo()
    si := &specInfo{version: info.Version}

    switch info.SpecType {
    case "openapi": // OAS 3.x
        v3Model, errs := doc.BuildV3Model()
        if len(errs) > 0 {
            return nil, fmt.Errorf("build v3 model: %v", errs[0])
        }
        si.title = v3Model.Model.Info.Title

        // Validate document against OpenAPI schema.
        docVal, _ := validator.NewValidator(doc)
        if valid, valErrs := docVal.ValidateDocument(); !valid && len(valErrs) > 0 {
            return nil, fmt.Errorf("validation: %s", valErrs[0].Message)
        }

        // Iterate paths in spec-file order (ordered-map FromOldest).
        var keys []string
        for pathName := range v3Model.Model.Paths.PathItems.KeysFromOldest() {
            keys = append(keys, pathName)
        }
        sort.Strings(keys) // additional sort for golden determinism
        for _, p := range keys {
            item, _ := v3Model.Model.Paths.PathItems.Get(p)
            si.v3Paths = append(si.v3Paths, extractV3Path(p, item))
        }

    case "swagger": // Swagger 2.0
        slog.Warn("apidocs: Swagger 2.0 spec detected; libopenapi v2 model is deprecated",
            "version", info.Version)
        v2Model, errs := doc.BuildV2Model()
        if len(errs) > 0 {
            return nil, fmt.Errorf("build v2 model: %v", errs[0])
        }
        si.title = v2Model.Model.Info.Title
        // Iterate v2 paths (limited — no schema validation)
        for pathName, item := range v2Model.Model.Paths.PathItems {
            si.v2Paths = append(si.v2Paths, extractV2Path(pathName, item))
        }
        sort.Slice(si.v2Paths, func(i, j int) bool { return si.v2Paths[i].path < si.v2Paths[j].path })

    default:
        return nil, fmt.Errorf("unsupported spec version %q (want swagger 2.0, openapi 3.x)", info.Version)
    }
    return si, nil
}
```

### Self-Contained HTML (Redoc Inline Bundle + Inline Spec)

```go
// Source: Redoc standalone.tsx — Redoc.init(specOrSpecUrl, options, element)
// specOrSpecUrl accepts a JSON object (not only a URL string).

//go:embed assets/redoc.standalone.js
var redocBundle []byte

func buildRedocHTML(specJSON []byte, bundle []byte) []byte {
    const tpl = `<!DOCTYPE html>
<html>
  <head>
    <title>API Reference</title>
    <meta charset="utf-8"/>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>body{margin:0;padding:0;}</style>
  </head>
  <body>
    <div id="redoc-container"></div>
    <script>%s</script>
    <script>Redoc.init(%s,{},document.getElementById('redoc-container'))</script>
  </body>
</html>`
    return []byte(fmt.Sprintf(tpl, bundle, specJSON))
}
```

`specJSON` must be sorted-key JSON for byte-identical output. Derive it from the libopenapi high-level model's rendered YAML (which uses the ordered-map), converted to JSON.

### ArtifactConfig Extension for apiDocs

```go
// In internal/config/config.go — extend ReleaseArtifacts:
type ReleaseArtifacts struct {
    Changelog    ArtifactConfig `yaml:"changelog"`
    ReleaseNotes ReleaseNotesConfig `yaml:"releaseNotes"`
    Blog         ArtifactConfig `yaml:"blog"`
    // APIDocs configures the API documentation artifact family (spec + HTML + Markdown).
    APIDocs      APIDocs Config `yaml:"apiDocs"`
}

// APIDocsConfig holds the per-artifact settings for the apidocs generator family.
type APIDocsConfig struct {
    ArtifactConfig `yaml:",inline"`
    // SpecPath is the repository-relative path to the committed OpenAPI/Swagger spec.
    // When empty, the generator tries the conventional fallback list (D-02).
    SpecPath string `yaml:"specPath"`
}
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Swagger 2.0 (`swagger:`) specs | OpenAPI 3.0/3.1 (`openapi:`) | OAS 3.0 released 2017, 3.1 in 2021 | Swagger 2.0 is legacy; libopenapi's v2 model is deprecated |
| CDN-hosted Redoc (`<script src="cdn">`) | Vendored bundle + go:embed | go:embed added Go 1.16 (2021) | Offline/air-gapped support without CDN dependency |
| `kin-openapi` (OpenAPI 3.x Go library) | `pb33f/libopenapi` (Swagger 2.0 + OAS 3.x) | libopenapi first release ~2022 | Single library spans all versions; kin-openapi is 3.x-only |
| Regenerating spec from source (swaggo, etc.) | Render committed spec | Phase 3 design decision | Deterministic, framework-agnostic, no annotation scraping |

**Deprecated/outdated:**
- `BuildV2Model()` in libopenapi: deprecated, will be removed. Do not build new features on top of it — keep usage isolated and behind a version check.
- `redoc-cli` npm package: deprecated; replaced by `@redocly/cli`. Irrelevant since we vendor the bundle directly rather than using the CLI.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `libopenapi-validator` `ValidateDocument()` does NOT support Swagger 2.0 (only OAS 3.x) | Standard Stack, Code Examples | If it secretly does support v2, we could use it for v2 validation too — low risk, conservative approach is still correct |
| A2 | libopenapi v0.37.3 `info.SpecType` returns `"openapi"` for OAS 3.x and `"swagger"` for v2 (string comparison) | Code Examples | If constants differ (e.g. `"oas3"` vs `"openapi"`), switch cases fail silently. Must verify `utils.OpenApi2` / `utils.OpenApi3` constant values at implementation time. |
| A3 | Redoc `Redoc.init(specObject, ...)` accepts a pre-parsed JSON object (not only string URL) | Architecture Patterns | Confirmed via standalone.tsx source: `typeof specOrSpecUrl === 'string'` branch → otherwise treated as object. Risk: LOW. |
| A4 | Redoc bundle file path in npm package is `bundles/redoc.standalone.js` (not `dist/`) | Standard Stack | If path differs in v2.5.3, vendor step fails. Must verify via npm after pinning version. |
| A5 | `go.yaml.in/yaml/v4` (libopenapi's YAML lib) does not conflict with `gopkg.in/yaml.v3` (already in go.mod) | Common Pitfalls | Different import paths, both can coexist in one module. Low risk. |

---

## Open Questions (RESOLVED)

> All four resolved into plan task actions during planning (03-01..03-05): libopenapi-validator `@latest` pin and Redoc vendor path → 03-01; `utils.OpenApi2/OpenApi3` constant usage → 03-03; YAML→JSON sorted-key conversion → 03-04.


1. **SpecType constant values for version switch**
   - What we know: The switch on `info.SpecType` uses string constants from `utils` package.
   - What's unclear: Exact string values of `utils.OpenApi2` and `utils.OpenApi3` (could be `"swagger"`, `"openapi"`, or numeric-like).
   - Recommendation: Import `github.com/pb33f/libopenapi/utils` at implementation time and use the named constants, not bare strings.

2. **libopenapi-validator latest version after v0.13.8**
   - What we know: pkg.go.dev showed v0.13.8 but noted "not the latest version."
   - What's unclear: Current latest version tag.
   - Recommendation: Run `go get github.com/pb33f/libopenapi-validator@latest` at implementation time to pin the correct version.

3. **Redoc bundle vendoring process**
   - What we know: Bundle is at `node_modules/redoc/bundles/redoc.standalone.js` after `npm install redoc@2.5.3`.
   - What's unclear: Whether the bundle file path changed in v2.5.x.
   - Recommendation: Wave 0 task: `npm install redoc@2.5.3 && cp node_modules/redoc/bundles/redoc.standalone.js internal/releasedocs/generators/apidocs/assets/`. Commit the file. Document the provenance (version + sha256) in a sidecar `.redoc-version` file.

4. **YAML → JSON conversion for deterministic spec injection**
   - What we know: gopkg.in/yaml.v3 decodes YAML to `any`; `encoding/json.Marshal` on a `map[string]any` is non-deterministic.
   - What's unclear: Best approach — use libopenapi's own render path (which leverages ordered-map), or decode to typed struct then marshal.
   - Recommendation: Use libopenapi's `document.Render()` to get YAML bytes, then convert to JSON using a yaml-node walker that sorts map keys, or use the ordered-map's JSON marshaler if one exists.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | All | per go.mod | 1.26 | — |
| `gopkg.in/yaml.v3` | existing dep | per go.mod | already present | — |
| `github.com/pb33f/libopenapi` | apidocs generator | NOT in go.mod yet | — | Wave 0 `go get` task |
| `github.com/pb33f/libopenapi-validator` | OAS 3.x validation | NOT in go.mod yet | — | Wave 0 `go get` task |
| `node` / `npm` | Vendor Redoc bundle (one-time) | need to verify | — | Download bundle from GitHub Releases directly |

**Missing dependencies with no fallback:**
- libopenapi and libopenapi-validator must be added to go.mod before implementation.
- Redoc bundle must be vendored before the `//go:embed` directive compiles.

**Missing dependencies with fallback:**
- npm: if unavailable, download the bundle from `https://github.com/Redocly/redoc/releases/download/v2.5.3/redoc.standalone.js` directly (GitHub Releases publish the standalone JS alongside each release).

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | `go test` (stdlib, no external test framework required) |
| Config file | none — uses `make test` |
| Quick run command | `go test -race -run TestAPIDocs ./internal/releasedocs/generators/apidocs/...` |
| Full suite command | `make test` (`go test -race -count=1 ./...`) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| D-01 (spec from committed file) | FetchFileFromRef called at rc.ToRef, result used as spec | unit | `go test -run TestGenerate_FetchesAtToRef ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-02 (fallback discovery) | Tries paths in order; uses first found; stops at first success | unit (table-driven) | `go test -run TestDiscoverSpec ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-04 (three artifacts emitted) | Generate returns 3 artifacts with correct Filename values | unit | `go test -run TestGenerate_ThreeArtifacts ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-05 (HTML offline determinism) | Same spec + bundle → byte-identical HTML | golden file | `go test -run TestBuildRedocHTML_Deterministic ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-05 (HTML offline: no CDN) | Generated HTML contains no `cdn.redoc.ly` or external script src | unit | `go test -run TestBuildRedocHTML_NoCDN ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-06 (Markdown determinism) | Same parsed spec → byte-identical Markdown (golden file) | golden file | `go test -run TestRenderMarkdown_Golden ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-07 (apiDocs config gating) | `Enabled(cfg, bump)` returns false when apiDocs.Enabled=false | unit | `go test -run TestEnabled ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-09 (Swagger 2.0 support) | Generator processes a Swagger 2.0 spec and emits 3 artifacts | unit | `go test -run TestGenerate_Swagger2 ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-09 (OAS 3.0 support) | Generator processes an OAS 3.0 spec | unit | `go test -run TestGenerate_OAS3 ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-09 (OAS 3.1 support) | Generator processes an OAS 3.1 spec | unit | `go test -run TestGenerate_OAS31 ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-10 (skip on no spec found) | All fallback paths 404 → Generate returns no artifacts, no error | unit | `go test -run TestGenerate_NoSpec_Skips ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-10 (skip on parse failure) | Malformed YAML → Generate returns no artifacts, no error | unit | `go test -run TestGenerate_ParseFailure_Skips ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-10 (skip on validation failure) | Invalid OAS 3.x spec → Generate returns no artifacts, no error | unit | `go test -run TestGenerate_ValidationFailure_Skips ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| D-10 (skip on unsupported version) | OAS 3.2 or unknown version → skip with log | unit | `go test -run TestGenerate_UnsupportedVersion_Skips ./internal/releasedocs/generators/apidocs/...` | Wave 0 |
| Cross-cutting: Artifact.Filename | pages.Publisher routes .yaml + .html + .md to correct paths | unit | `go test -run TestPublish_APIDocs_Paths ./internal/releasedocs/publishers/pages/...` | Wave 0 |
| Cross-cutting: backward compat | Existing changelog/releasenotes/blog paths unchanged (Filename="") | regression | `go test ./internal/releasedocs/publishers/pages/...` (existing suite must stay green) | Exists |
| Idempotency | Two Publish calls with same inputs hit same UpsertFile paths | unit | `go test -run TestIdempotent_APIDocs ./internal/releasedocs/publishers/pages/...` | Wave 0 |
| Raw spec passthrough | openapi.yaml artifact content == fetched bytes (no re-serialization) | unit | `go test -run TestGenerate_RawSpecPassthrough ./internal/releasedocs/generators/apidocs/...` | Wave 0 |

### Critical Golden Files Needed

1. `testdata/golden/markdown_v3.golden` — Markdown output for a minimal OAS 3.0 spec (e.g. petstore).
2. `testdata/golden/markdown_v2.golden` — Markdown output for a Swagger 2.0 spec.
3. No HTML golden file needed per se — the `NoCDN` and `Deterministic` tests are sufficient; a full HTML golden would be brittle (bundle size varies).

### Sampling Rate

- **Per task commit:** `go test -race -run TestAPIDocs ./internal/releasedocs/generators/apidocs/...`
- **Per wave merge:** `make test` (full suite, includes pages backward-compat regression)
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `internal/releasedocs/generators/apidocs/apidocs_test.go` — covers D-01 through D-10
- [ ] `internal/releasedocs/generators/apidocs/testdata/petstore_v2.yaml` — Swagger 2.0 fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/petstore_v3.yaml` — OAS 3.0 fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/petstore_v31.yaml` — OAS 3.1 fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/invalid.yaml` — malformed YAML fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/golden/markdown_v3.golden` — Markdown golden file
- [ ] `internal/releasedocs/generators/apidocs/assets/redoc.standalone.js` — vendored bundle
- [ ] `go get github.com/pb33f/libopenapi && go get github.com/pb33f/libopenapi-validator` — Wave 0 dependency task

---

## Security Domain

`security_enforcement` not explicitly set to false in `.planning/config.json` → security section included.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Spec is fetched via existing authenticated FileFetcher — no new auth surface |
| V3 Session Management | no | Generator is stateless |
| V4 Access Control | no | No new endpoints or auth checks |
| V5 Input Validation | yes | Spec bytes from VCS are untrusted input; validate via libopenapi before use |
| V6 Cryptography | no | No cryptographic operations |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal via `art.Kind`/`Filename` in pages publisher | Tampering | Existing `expectedPrefix` guard in `pages.go:85-90`; must also apply to Filename-derived paths |
| Malicious spec with `$ref` pointing to internal network paths | Information Disclosure | libopenapi resolves `$ref` by default; use `datamodel.DocumentConfiguration{AllowRemoteReferences: false}` for spec parsing in apidocs |
| Oversized spec causing OOM | Denial of Service | Add a max-size check on raw spec bytes before parsing (e.g. 5 MB limit) |
| Spec YAML with executable content injected into Markdown output | Injection | Go `text/template` auto-escapes only in `html/template`; for Markdown output, use `text/template` with `template.HTMLEscapeString` on string fields that come from the spec |

**Critical security note — `$ref` remote resolution:** libopenapi by default follows remote `$ref` URLs. A spec authored by a malicious repo could reference `file:///etc/passwd` or internal network URLs. The apidocs generator MUST pass `datamodel.DocumentConfiguration{AllowRemoteReferences: false, AllowFileReferences: false}` to `libopenapi.NewDocumentWithConfiguration()`.

---

## Sources

### Primary (HIGH confidence)

- `internal/releasedocs/releasedocs.go` — `Generator` interface, `Artifact` struct (read directly)
- `internal/releasedocs/publishers/pages/pages.go` — hardcoded `.md` path formula (read directly)
- `internal/releasedocs/generators/blog/blog.go` — closest generator analog (read directly)
- `internal/releasedocs/defaults/defaults.go` — registration pattern (read directly)
- `internal/config/config.go` — `ReleaseArtifacts` struct to extend (read directly)
- `internal/releasedocs/template/template.go` — `go:embed` + `text/template` pattern (read directly)

### Secondary (MEDIUM confidence)

- [pkg.go.dev/github.com/pb33f/libopenapi](https://pkg.go.dev/github.com/pb33f/libopenapi) — module path, version v0.37.3, `GetSpecInfo()`/`BuildV3Model()`/`BuildV2Model()` API
- [pkg.go.dev/github.com/pb33f/libopenapi-validator](https://pkg.go.dev/github.com/pb33f/libopenapi-validator) — `ValidateDocument()` API, OAS 3.x only
- [pb33f.io/libopenapi/swagger/](https://pb33f.io/libopenapi/swagger/) — v2 model deprecation warning ("DO NOT take a dependency on it")
- [pb33f.io/libopenapi/validation/](https://pb33f.io/libopenapi/validation/) — `ValidateDocument()` return values
- [pkg.go.dev/github.com/getkin/kin-openapi/openapi3](https://pkg.go.dev/github.com/getkin/kin-openapi/openapi3) — confirmed 3.x-only, no Swagger 2.0
- [github.com/Redocly/redoc LICENSE](https://github.com/Redocly/redoc/blob/main/LICENSE) — MIT confirmed
- [github.com/Redocly/redoc standalone.tsx](https://github.com/Redocly/redoc/blob/main/src/standalone.tsx) — `Redoc.init(specOrSpecUrl, ...)` accepts JSON object
- [github.com/Redocly/redoc releases](https://github.com/Redocly/redoc/releases) — v2.5.3 confirmed current stable
- [pb33f/libopenapi go.mod](https://github.com/pb33f/libopenapi/blob/main/go.mod) — Go 1.25 minimum, ordered-map dep
- [pb33f/libopenapi-validator go.mod](https://github.com/pb33f/libopenapi-validator/blob/main/go.mod) — deps list

### Tertiary (LOW confidence)

- WebSearch results for Redoc bundle size (~300 KB gzipped, ~1.2 MB uncompressed) — unverified exact size for v2.5.3

---

## Metadata

**Confidence breakdown:**
- Standard stack: MEDIUM — verified on pkg.go.dev and official docs; package legitimacy audit blocked by slopcheck unavailability
- Architecture: HIGH — derived directly from reading real source files
- Pitfalls: HIGH — cross-cutting constraint, libopenapi v2 deprecation, and non-determinism risk are all grounded in real code
- External library APIs: MEDIUM — verified via pkg.go.dev + official docs; some SpecType constant values need runtime verification

**Research date:** 2026-06-05
**Valid until:** 2026-07-05 (30 days for stable; libopenapi moves quickly, re-check on major version bump)
