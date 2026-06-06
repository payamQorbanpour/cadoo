# Phase 3: API Docs / OpenAPI - Pattern Map

**Mapped:** 2026-06-05
**Files analyzed:** 8 new/modified files
**Analogs found:** 8 / 8

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/releasedocs/generators/apidocs/apidocs.go` | generator | request-response | `internal/releasedocs/generators/blog/blog.go` | exact |
| `internal/releasedocs/generators/apidocs/discover.go` | utility | request-response | `internal/releasedocs/template/template.go` (Resolve + isMissingFile) | role-match |
| `internal/releasedocs/generators/apidocs/parse.go` | utility | transform | `internal/releasedocs/template/template.go` (Render) | role-match |
| `internal/releasedocs/generators/apidocs/render_html.go` | utility | transform | `internal/releasedocs/template/template.go` (go:embed) | role-match |
| `internal/releasedocs/generators/apidocs/render_markdown.go` | utility | transform | `internal/releasedocs/generators/releasenotes/releasenotes.go` + `template/template.go` | role-match |
| `internal/releasedocs/generators/apidocs/apidocs_test.go` | test | — | `internal/releasedocs/generators/blog/blog_test.go` | exact |
| `internal/releasedocs/releasedocs.go` | model | — | self (extend `Artifact` struct) | exact |
| `internal/releasedocs/publishers/pages/pages.go` | publisher | request-response | self (3-line change to `Publish`) | exact |
| `internal/releasedocs/defaults/defaults.go` | config/wiring | — | self (add one import + one list entry) | exact |
| `internal/config/config.go` | config | — | self (extend `ReleaseArtifacts` struct) | exact |

---

## Pattern Assignments

### `internal/releasedocs/generators/apidocs/apidocs.go` (generator, request-response)

**Analog:** `internal/releasedocs/generators/blog/blog.go`

**Package declaration pattern** (lines 1–9):
```go
// Package apidocs implements the API-docs Generator for the release-docs
// subsystem. The generator fetches a committed OpenAPI/Swagger spec via
// FileFetcher, validates it, and emits three artifacts: the raw spec, a
// self-contained Redoc HTML reference, and a deterministic Markdown reference.
// The generator is fully deterministic (no LLM). When no valid spec exists,
// it skips all three artifacts with a logged reason (D-10).
package apidocs
```

**Imports pattern** (lines 10–18 of blog.go):
```go
import (
    "context"
    "fmt"
    "log/slog"

    "github.com/payamqorbanpour/cadoo/internal/config"
    "github.com/payamqorbanpour/cadoo/internal/releasedocs"
)
```
For apidocs, also add the new library imports in the third group:
```go
    "github.com/pb33f/libopenapi"
    "github.com/pb33f/libopenapi-validator/validator"
```

**Generator struct + New() pattern** (blog.go lines 24–27):
```go
// Generator implements releasedocs.Generator for the blog artifact kind.
// It is safe for concurrent use.
type Generator struct{}

// New returns a new blog Generator.
func New() *Generator { return &Generator{} }
```
Mirror exactly for apidocs (substituting KindAPIDocs).

**Kind() pattern** (blog.go line 30):
```go
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindBlog }
```
For apidocs:
```go
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindAPIDocs }
```

**Enabled() with default-coercion** (blog.go lines 38–45):
```go
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
    artifactCfg := cfg.Artifacts.Blog
    // Coerce empty When to "minor_or_above" — blog's opinionated default.
    if artifactCfg.When == "" {
        artifactCfg.When = "minor_or_above"
    }
    return releasedocs.Enabled(artifactCfg, bump)
}
```
For apidocs, coerce empty `When` to `"always"` (D-08):
```go
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
    artifactCfg := cfg.Artifacts.APIDocs.ArtifactConfig
    if artifactCfg.When == "" {
        artifactCfg.When = "always"
    }
    return releasedocs.Enabled(artifactCfg, bump)
}
```

**Generate() nil-tolerant skeleton** (blog.go lines 53–80):
```go
func (g *Generator) Generate(ctx context.Context, rc releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
    skeleton := buildSkeleton(rc)

    // When rc.LLM is nil, return the skeleton verbatim (nil-tolerant D-11).
    if rc.LLM == nil {
        return releasedocs.Artifact{
            Kind:    releasedocs.KindBlog,
            Content: []byte(skeleton),
        }, nil
    }
    // ... LLM call ...
}
```
apidocs has no LLM dependency. Mirror the skip-with-log pattern:
```go
func (g *Generator) Generate(ctx context.Context, rc releasedocs.ReleaseContext) ([]releasedocs.Artifact, error) {
    specBytes, err := discoverSpec(ctx, rc)
    if err != nil {
        slog.Warn("apidocs: skipping — could not fetch spec",
            "repo", rc.Repo, "toRef", rc.ToRef, "err", err)
        return nil, nil // D-10: skip with logged reason, no error returned
    }
    // ... parse, render, return three artifacts ...
}
```
Note: `Generate` returns `[]releasedocs.Artifact` (multiple artifacts, not one). The `Generator` interface declares `Generate(...) (Artifact, error)` — the apidocs generator must either (a) change the interface to return a slice, or (b) return a single compound artifact and unpack on the publisher side. See the "Claude's Discretion" mapping note in the cross-cutting section below.

**Error / skip-with-log pattern** (blog.go lines 68–74):
```go
slog.Warn("blog: LLM narrative failed; falling back to skeleton",
    "repo", rc.Repo, "toRef", rc.ToRef, "err", err)
return releasedocs.Artifact{
    Kind:    releasedocs.KindBlog,
    Content: []byte(skeleton),
}, nil
```
For apidocs skip path:
```go
slog.Warn("apidocs: skipping", "repo", rc.Repo, "toRef", rc.ToRef, "reason", reason)
return nil, nil // D-10: sibling generators must not be affected
```

---

### `internal/releasedocs/generators/apidocs/discover.go` (utility, request-response)

**Analog:** `internal/releasedocs/template/template.go` — the `Resolve` + `isMissingFile` functions (lines 132–168)

**isMissingFile pattern** (template.go lines 132–142):
```go
func isMissingFile(err error) bool {
    if errors.Is(err, fs.ErrNotExist) {
        return true
    }
    msg := err.Error()
    return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}
```
Copy verbatim into `discover.go` as a package-private function.

**FileFetcher type-assert pattern** (template.go lines 150–167):
```go
ff, ok := rc.Provider.(releasedocs.FileFetcher)
if ok {
    src, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, overridePath)
    if err == nil {
        return template.New("override:" + overridePath).Parse(string(src))
    }
    if !isMissingFile(err) {
        _ = err // callers may inspect returned preset without this detail
    }
    // Missing file (404) → fall back silently.
}
// Provider does not implement FileFetcher → fall back.
```
For `discoverSpec`, wrap in a fallback loop:
```go
var fallbackPaths = []string{
    "openapi.yaml", "openapi.yml", "openapi.json",
    "docs/openapi.yaml", "api/openapi.yaml",
}

func discoverSpec(ctx context.Context, rc releasedocs.ReleaseContext) ([]byte, error) {
    ff, ok := rc.Provider.(releasedocs.FileFetcher)
    if !ok {
        return nil, fmt.Errorf("apidocs: provider does not implement FileFetcher")
    }
    specPath := rc.Config.Artifacts.APIDocs.SpecPath
    if specPath != "" {
        return ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, specPath)
    }
    for _, p := range fallbackPaths {
        b, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, p)
        if err == nil {
            return b, nil
        }
        if isMissingFile(err) {
            continue // try next path
        }
        // Non-404 error: log and skip rather than abort (D-10/Pitfall 5)
        slog.Warn("apidocs: fetch attempt failed", "path", p, "err", err)
    }
    return nil, fmt.Errorf("apidocs: no spec found at any fallback path")
}
```

---

### `internal/releasedocs/generators/apidocs/parse.go` (utility, transform)

**Analog:** RESEARCH.md Pattern 1 (external library API) + releasenotes generator structure

This file has no close existing codebase analog since `pb33f/libopenapi` is a new dependency. Mirror the package structure from `releasenotes.go` but the core logic follows RESEARCH.md Pattern 1.

**Key pattern to implement** (RESEARCH.md Code Examples section, `loadSpec` function):
```go
func loadSpec(specBytes []byte) (*specInfo, error) {
    doc, err := libopenapi.NewDocument(specBytes)
    // ... switch info.SpecType { "openapi" → v3 path, "swagger" → v2 path } ...
}
```

**Security: disable remote $ref** — MUST use `libopenapi.NewDocumentWithConfiguration()`:
```go
cfg := datamodel.DocumentConfiguration{
    AllowRemoteReferences: false,
    AllowFileReferences:   false,
}
doc, err := libopenapi.NewDocumentWithConfiguration(specBytes, &cfg)
```

**Max-size guard** (before parsing, RESEARCH.md Security section):
```go
const maxSpecSize = 5 * 1024 * 1024 // 5 MB
if len(specBytes) > maxSpecSize {
    return nil, fmt.Errorf("apidocs: spec too large (%d bytes, limit %d)", len(specBytes), maxSpecSize)
}
```

---

### `internal/releasedocs/generators/apidocs/render_html.go` (utility, transform)

**Analog:** `internal/releasedocs/template/template.go` — the `go:embed` + `embed.FS` pattern (lines 19–20)

**go:embed pattern** (template.go lines 19–20):
```go
//go:embed presets/*.tmpl
var presetFS embed.FS
```
For apidocs HTML renderer:
```go
//go:embed assets/redoc.standalone.js
var redocBundle []byte
```
Use `[]byte` not `string` (avoids copy-on-use; RESEARCH.md Anti-Patterns).

**HTML template pattern** (RESEARCH.md Pattern 2):
```go
func buildRedocHTML(specJSON []byte, bundle []byte) []byte {
    const tpl = `<!DOCTYPE html>...
    <script>%s</script>
    <script>Redoc.init(%s,{},document.getElementById('redoc-container'))</script>
    ...`
    return []byte(fmt.Sprintf(tpl, bundle, specJSON))
}
```
`specJSON` must be sorted-key JSON for byte-identical output.

---

### `internal/releasedocs/generators/apidocs/render_markdown.go` (utility, transform)

**Analog:** `internal/releasedocs/generators/releasenotes/releasenotes.go` + `internal/releasedocs/template/template.go`

**Template data struct pattern** (template.go lines 38–78 — `Data`, `ChangeGroup`, `ChangeItem`):
```go
// Define apidocs-specific template data structs:
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
    Method      string
    Path        string
    Summary     string
    Description string
    Parameters  []ParamItem
}
```

**Render function pattern** (template.go lines 124–130):
```go
func Render(tmpl *template.Template, data any) (string, error) {
    var b strings.Builder
    if err := tmpl.Execute(&b, data); err != nil {
        return "", err
    }
    return b.String(), nil
}
```
Mirror exactly.

**go:embed preset template** (template.go lines 19–20, 28–32):
```go
//go:embed presets/apidocs.tmpl
var apidocsPreset []byte

func loadMarkdownTemplate() (*template.Template, error) {
    return template.New("apidocs.tmpl").Parse(string(apidocsPreset))
}
```

**Sorted iteration pattern** (RESEARCH.md Pattern 3):
```go
// v3Model.Model.Paths.PathItems.FromOldest() preserves spec-file order.
// For additional determinism (golden-file tests), sort path keys:
var pathKeys []string
for pathName := range v3Model.Model.Paths.PathItems.KeysFromOldest() {
    pathKeys = append(pathKeys, pathName)
}
sort.Strings(pathKeys)
```

---

### `internal/releasedocs/generators/apidocs/apidocs_test.go` (test)

**Analog:** `internal/releasedocs/generators/blog/blog_test.go`

**Package declaration** (blog_test.go line 1):
```go
package apidocs_test
```

**Fixture builder pattern** (blog_test.go lines 39–65):
```go
func fixtureBlogRC(llmProvider llm.Provider, bump releasedocs.SemverBump, blogEnabled bool, when string) releasedocs.ReleaseContext {
    cfg := config.ReleaseDocs{
        Artifacts: config.ReleaseArtifacts{
            Blog: config.ArtifactConfig{Enabled: blogEnabled, When: when},
        },
    }
    // ...
    return releasedocs.ReleaseContext{ ... }
}
```
For apidocs, build a `fixtureAPIDocsRC` that injects a `fakeFetcher` stub implementing `releasedocs.FileFetcher` — no real VCS call needed. Mirror the struct:
```go
type fakeFetcher struct {
    files map[string][]byte // path → content; nil means 404
}
func (f *fakeFetcher) FetchFileFromRef(_ context.Context, _, _, path string) ([]byte, error) {
    b, ok := f.files[path]
    if !ok {
        return nil, fmt.Errorf("404 not found: %s", path)
    }
    return b, nil
}
// fakeFetcher must also implement vcs.Provider (embed minimalProvider or use a combined fake)
```

**Table-driven Enabled test pattern** (blog_test.go lines 80–112 — `TestBlogEnabledMinorOrAboveDefault`):
```go
tests := []struct {
    name string
    bump releasedocs.SemverBump
    want bool
}{...}
for _, tc := range tests {
    tc := tc
    t.Run(tc.name, func(t *testing.T) {
        t.Parallel()
        // ...
    })
}
```
Mirror for `TestAPIDocs_EnabledAlwaysDefault`.

**Determinism test pattern** (blog_test.go lines 203–230 — `TestBlogGenerateNilLLMSkeleton`):
```go
got1, err := g.Generate(context.Background(), rc)
// ...
got2, err := g.Generate(context.Background(), rc)
// ...
if string(got1.Content) != string(got2.Content) {
    t.Errorf("Generate is not deterministic: ...")
}
```
Mirror for `TestBuildRedocHTML_Deterministic` and `TestRenderMarkdown_Golden`.

**Golden file test approach** — no existing golden test in the codebase. Use the standard `testdata/golden/*.golden` pattern:
```go
// TestRenderMarkdown_Golden tests deterministic output with -update flag.
const updateGolden = false // set via flag or env

func TestRenderMarkdown_Golden(t *testing.T) {
    specBytes, _ := os.ReadFile("testdata/petstore_v3.yaml")
    got := renderMarkdown(specBytes)
    goldenPath := "testdata/golden/markdown_v3.golden"
    if updateGolden {
        os.WriteFile(goldenPath, []byte(got), 0644)
    }
    want, _ := os.ReadFile(goldenPath)
    if got != string(want) {
        t.Errorf("Markdown output does not match golden file.\ngot:\n%s\nwant:\n%s", got, want)
    }
}
```

---

### `internal/releasedocs/releasedocs.go` (model — extend `Artifact` struct)

**Analog:** Self — lines 64–69 of `releasedocs.go`

**Current `Artifact` struct** (releasedocs.go lines 64–69):
```go
// Artifact is the output of a Generator: a typed blob of content plus any
// metadata a Publisher needs to route and splice the content correctly.
type Artifact struct {
    // Kind identifies which generator produced this artifact.
    Kind ArtifactKind
    // Content is the rendered artifact bytes (Markdown for all current kinds).
    Content []byte
}
```

**Change: add `Filename` field** (RESEARCH.md Pattern 4):
```go
type Artifact struct {
    // Kind identifies which generator produced this artifact.
    Kind ArtifactKind
    // Content is the rendered artifact bytes.
    Content []byte
    // Filename, when non-empty, overrides the default "{kind}.md" path
    // computed by publishers. Use this for artifacts that are not Markdown
    // (e.g. "openapi.yaml", "api-reference.html", "api-reference.md").
    // When empty, publishers fall back to string(art.Kind)+".md" for
    // backward compatibility with existing changelog/releasenotes/blog artifacts.
    Filename string
}
```

**Also add new ArtifactKind constant** (releasedocs.go lines 20–29):
```go
// KindAPIDocs identifies the apidocs generator family (spec + HTML + Markdown).
// A single Generate() call emits three artifacts all sharing this Kind, each
// differentiated by their Filename field.
KindAPIDocs ArtifactKind = "apidocs"
```

**Backward-compatibility guarantee:** All existing `Artifact` values have `Filename: ""` (zero value). The pages publisher's fallback to `string(art.Kind)+".md"` when `Filename == ""` preserves all existing paths. No existing tests need updating.

---

### `internal/releasedocs/publishers/pages/pages.go` (publisher — 3-line change)

**Analog:** Self — lines 74–99 of `pages.go`

**Current path-construction loop** (pages.go lines 74–99):
```go
for _, art := range arts {
    if len(art.Content) == 0 {
        continue
    }

    // Build the path using path.Join to clean separators and ".." segments.
    p := path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")

    // Guard: path.Join cleans ".." but does not prevent escape from the base
    // directory. Reject any path that does not start with the expected prefix.
    expectedPrefix := dir + "/"
    if !strings.HasPrefix(p, expectedPrefix) {
        slog.Warn("pages: computed path escapes base dir; skipping artifact",
            "path", p, "dir", dir, "toRef", rc.ToRef, "kind", art.Kind)
        continue
    }

    commitMsg := "docs: release " + rc.ToRef + " " + string(art.Kind)

    if err := bc.UpsertFile(ctx, rc.Repo, branch, commitMsg, vcs.FileWrite{
        Path:    p,
        Content: art.Content,
    }); err != nil {
        return fmt.Errorf("pages: UpsertFile path %q: %w", p, err)
    }
}
```

**Change: replace line 81** (the hardcoded `.md` formula):
```go
// Before (line 81):
p := path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")

// After (3-line replacement per RESEARCH.md Pattern 4):
filename := art.Filename
if filename == "" {
    filename = string(art.Kind) + ".md"
}
p := path.Join(dir, "releases", rc.ToRef, filename)
```

**`expectedPrefix` guard remains unchanged** (pages.go lines 84–90) — it already guards against path traversal by checking `strings.HasPrefix(p, expectedPrefix)`. The `Filename` field could carry `../` if a generator were malicious, but the `expectedPrefix` check catches that. No additional changes needed.

---

### `internal/releasedocs/defaults/defaults.go` (wiring — add one entry)

**Analog:** Self — lines 12–36 of `defaults.go`

**Current imports + DefaultGenerators** (defaults.go lines 12–36):
```go
import (
    "github.com/payamqorbanpour/cadoo/internal/releasedocs"
    "github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/blog"
    "github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/changelog"
    "github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/releasenotes"
    // ...
)

func DefaultGenerators() []releasedocs.Generator {
    return []releasedocs.Generator{
        changelog.New(),
        releasenotes.New(),
        blog.New(),
    }
}
```

**Change: add apidocs import and list entry:**
```go
import (
    // ... existing imports ...
    "github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/apidocs"
)

func DefaultGenerators() []releasedocs.Generator {
    return []releasedocs.Generator{
        changelog.New(),
        releasenotes.New(),
        blog.New(),
        apidocs.New(), // added — runs last; emits three Filename-differentiated artifacts
    }
}
```

---

### `internal/config/config.go` (config — extend `ReleaseArtifacts`)

**Analog:** Self — lines 76–112 of `config.go`

**Current `ReleaseArtifacts` struct** (config.go lines 76–85):
```go
type ReleaseArtifacts struct {
    Changelog    ArtifactConfig     `yaml:"changelog"`
    ReleaseNotes ReleaseNotesConfig `yaml:"releaseNotes"`
    Blog         ArtifactConfig     `yaml:"blog"`
}
```

**Change: add `APIDocs` field:**
```go
type ReleaseArtifacts struct {
    Changelog    ArtifactConfig     `yaml:"changelog"`
    ReleaseNotes ReleaseNotesConfig `yaml:"releaseNotes"`
    Blog         ArtifactConfig     `yaml:"blog"`
    // APIDocs configures the API documentation artifact family (spec + HTML + Markdown).
    // All three outputs are gated together by a single enabled + when: condition (D-07).
    APIDocs      APIDocsConfig      `yaml:"apiDocs"`
}
```

**New `APIDocsConfig` struct** — mirrors `ReleaseNotesConfig` extension pattern (config.go lines 107–112):
```go
// APIDocsConfig extends ArtifactConfig with apidocs-specific settings.
// Mirror of ReleaseNotesConfig extension pattern (lines 107–112).
type APIDocsConfig struct {
    ArtifactConfig `yaml:",inline"`
    // SpecPath is the repository-relative path to the committed OpenAPI/Swagger spec.
    // When empty, the generator tries the conventional fallback list (D-02):
    // openapi.yaml → openapi.yml → openapi.json → docs/openapi.yaml → api/openapi.yaml.
    SpecPath string `yaml:"specPath"`
}
```

**`ReleaseNotesConfig` reference** (config.go lines 107–112):
```go
type ReleaseNotesConfig struct {
    ArtifactConfig `yaml:",inline"`
    Tone string `yaml:"tone"`
}
```
`APIDocsConfig` follows the exact same inline-embed + extra-field pattern.

---

## Shared Patterns

### 1. Generator Interface Compliance (Kind / Enabled / Generate)

**Source:** `internal/releasedocs/releasedocs.go` lines 145–155
**Apply to:** `apidocs/apidocs.go`
```go
type Generator interface {
    Kind() ArtifactKind
    Enabled(cfg config.ReleaseDocs, bump SemverBump) bool
    Generate(ctx context.Context, rc ReleaseContext) (Artifact, error)
}
```
Note: if apidocs emits three `Artifact` values per `Generate()`, the interface must be extended to `([]Artifact, error)` — or apidocs returns a single artifact with all content. The planner must resolve this "Claude's Discretion" item. The simplest backward-compatible approach: add a separate `MultiGenerator` interface OR return `([]Artifact, error)` from a modified interface. Existing generators return one artifact; apidocs returns three.

### 2. Enabled() Gate

**Source:** `internal/releasedocs/context.go` lines 126–148
**Apply to:** `apidocs/apidocs.go` `Enabled()` method
```go
func Enabled(artifactCfg config.ArtifactConfig, bump SemverBump) bool {
    if !artifactCfg.Enabled {
        return false
    }
    switch artifactCfg.When {
    case "", "always": return true
    case "major":      return bump == BumpMajor
    // ... etc.
    }
}
```
apidocs delegates to this shared function after coercing empty `When` to `"always"`.

### 3. Skip-with-Log (D-10)

**Source:** `internal/releasedocs/generators/blog/blog.go` lines 68–74 (LLM-error fallback)
**Apply to:** `apidocs/apidocs.go` `Generate()` — all failure paths
```go
slog.Warn("<package>: <reason>",
    "repo", rc.Repo, "toRef", rc.ToRef, "err", err)
return releasedocs.Artifact{}, nil  // or nil slice — no error returned
```
Never return a non-nil error from `Generate()` for skip conditions (spec not found, parse failure, validation failure). Return nil/empty + log.

### 4. go:embed

**Source:** `internal/releasedocs/template/template.go` lines 19–20
**Apply to:** `apidocs/render_html.go` (Redoc bundle), `apidocs/render_markdown.go` (preset template)
```go
//go:embed presets/*.tmpl
var presetFS embed.FS

// OR for a single file:
//go:embed assets/redoc.standalone.js
var redocBundle []byte
```
Use `[]byte` variable (not `embed.FS`) for the single JS file. Use `embed.FS` only if multiple files need to be addressed by name at runtime.

### 5. FileFetcher Type-Assert

**Source:** `internal/releasedocs/template/template.go` lines 151–166
**Apply to:** `apidocs/discover.go`
```go
ff, ok := rc.Provider.(releasedocs.FileFetcher)
if !ok {
    // degrade gracefully
}
src, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, path)
```

### 6. isMissingFile 404 Tolerance

**Source:** `internal/releasedocs/template/template.go` lines 132–142
**Apply to:** `apidocs/discover.go` (fallback discovery loop)
```go
func isMissingFile(err error) bool {
    if errors.Is(err, fs.ErrNotExist) {
        return true
    }
    msg := err.Error()
    return strings.Contains(msg, "404") || strings.Contains(msg, "not found")
}
```

### 7. Path-Traversal Guard (pages publisher)

**Source:** `internal/releasedocs/publishers/pages/pages.go` lines 84–90
**Apply to:** `pages.go` after the Filename extension change — guard still applies to the computed path, whether Filename is set or not
```go
expectedPrefix := dir + "/"
if !strings.HasPrefix(p, expectedPrefix) {
    slog.Warn("pages: computed path escapes base dir; skipping artifact", ...)
    continue
}
```

### 8. Test Fixture + Parallel Pattern

**Source:** `internal/releasedocs/generators/blog/blog_test.go` lines 68–75 (TestBlogKind)
**Apply to:** all `apidocs_test.go` test functions
```go
func TestAPIDocs_Kind(t *testing.T) {
    t.Parallel()
    g := apidocs.New()
    if got := g.Kind(); got != releasedocs.KindAPIDocs {
        t.Errorf("Kind() = %q; want %q", got, releasedocs.KindAPIDocs)
    }
}
```

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `apidocs/parse.go` (libopenapi integration) | utility | transform | No existing Go OpenAPI parsing in codebase; library is new. Use RESEARCH.md Pattern 1 + Code Examples as the reference. |
| `apidocs/assets/redoc.standalone.js` | embedded asset | — | Binary/JS asset, not a Go source pattern; downloaded from npm/GitHub Releases per RESEARCH.md Wave 0 instruction. |
| `apidocs/presets/apidocs.tmpl` | template | — | New artifact kind; no existing apidocs preset. Mirror `presets/changelog.tmpl` structure (range over groups/items). |
| `apidocs/testdata/*.yaml` + `golden/*.golden` | test fixtures | — | New test fixtures; no existing OpenAPI fixture in codebase. |

---

## Critical Planning Notes

1. **Generator interface return type:** `releasedocs.Generator.Generate()` returns a single `Artifact`. apidocs emits three. The planner must decide between: (a) adding a `MultiGenerator` interface alongside `Generator`, (b) changing `Generator` to return `([]Artifact, error)` — this is a breaking change requiring all three existing generators to update their signatures, or (c) having apidocs wrap all three outputs in one `Artifact.Content` and unpack in the publisher. Option (a) is cleanest; option (b) requires touching `blog.go`, `releasenotes.go`, `changelog.go`, `releasedocs.go`, and the dispatcher. RESEARCH.md implies three separate `Artifact` values.

2. **Dispatcher awareness:** `internal/releasedocs/dispatcher.go` calls `generator.Generate()` and passes the result to publishers. When apidocs returns multiple artifacts, the dispatcher must spread them correctly. The planner must include the dispatcher file in the modification scope.

3. **Wave 0 dependency tasks** (must happen before any code compiles):
   - `go get github.com/pb33f/libopenapi@v0.37.3`
   - `go get github.com/pb33f/libopenapi-validator@latest`
   - Download Redoc bundle: `npm install redoc@2.5.3 && cp node_modules/redoc/bundles/redoc.standalone.js internal/releasedocs/generators/apidocs/assets/`

4. **Linter:** `golangci-lint v2` with `exported` rule on. Every exported symbol in new files needs a docstring. Check with `make lint` after each file.

---

## Metadata

**Analog search scope:** `internal/releasedocs/`, `internal/config/`, `internal/orchestrator/reviewer.go`
**Files scanned:** 10 source files + 2 test files
**Pattern extraction date:** 2026-06-05
