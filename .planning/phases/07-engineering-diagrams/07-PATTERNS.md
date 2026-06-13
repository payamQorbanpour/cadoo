# Phase 7: Engineering Diagrams - Pattern Map

**Mapped:** 2026-06-13
**Files analyzed:** 6 (1 new package with 3+ files, 5 modified/extended)
**Analogs found:** 6 / 6 (all exact — Phase 7 is a mechanical clone of Phase 3 apidocs)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/releasedocs/generators/diagrams/diagrams.go` (NEW) | generator | transform / file-I/O (fetch→wrap→emit) | `internal/releasedocs/generators/apidocs/apidocs.go` | exact |
| `internal/releasedocs/generators/diagrams/sniff.go` (NEW) | utility | transform (byte inspection) | `internal/releasedocs/generators/apidocs/discover.go` (`isMissingFile` shape) | role-match |
| `internal/releasedocs/generators/diagrams/render.go` (NEW) | utility | transform (string assembly) | `internal/releasedocs/generators/apidocs/render_markdown.go` | role-match (simpler — fixed wrapper, no template) |
| `internal/releasedocs/generators/diagrams/diagrams_test.go` (NEW) | test | golden-file | `internal/releasedocs/generators/apidocs/apidocs_test.go` | exact |
| `internal/releasedocs/generators/diagrams/testdata/` + `testdata/golden/` (NEW) | test fixtures | file-I/O | `internal/releasedocs/generators/apidocs/testdata/` + `golden/` | exact |
| `internal/releasedocs/releasedocs.go` (MODIFY) | model/const | — | existing `KindAPIDocs` const | exact |
| `internal/config/config.go` (MODIFY) | config | — | `APIDocsConfig` inline-embed (config.go:118-132) | exact |
| `internal/releasedocs/defaults/defaults.go` (MODIFY) | wiring | — | `apidocs.New()` registration line (defaults.go:40) | exact |
| `internal/releasedocs/publishers/pages/pages_diagrams_test.go` (NEW) | test | CRUD/idempotency | `pages_apidocs_test.go` | exact |
| `.cadoo.yaml.example` (MODIFY) | config doc | — | `apiDocs:` block (lines 107-124) | exact |

---

## Pattern Assignments

### `internal/releasedocs/generators/diagrams/diagrams.go` (generator, transform/file-I/O)

**Analog:** `internal/releasedocs/generators/apidocs/apidocs.go` (the entire file is the template — clone its shape exactly)

**Imports + struct + constructor + Kind** (apidocs.go:9-28):
```go
import (
	"context"
	"log/slog"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
)

type Generator struct{}
func New() *Generator { return &Generator{} }
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindDiagrams }
```
> Note the cadoo-internal import group is third (goimports `local-prefixes` rule, CLAUDE.md). Diagrams adds `"bytes"`/`"strings"`/`"path"` to group 1 for the sniff+wrap helpers.

**`Enabled(cfg, bump)` gate with "always" default coercion** (apidocs.go:33-40 — copy verbatim, swap `APIDocs`→`Diagrams`):
```go
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	artifactCfg := cfg.Artifacts.Diagrams.ArtifactConfig
	// D-08: coerce empty When to "always" — diagrams run on every release by default.
	if artifactCfg.When == "" {
		artifactCfg.When = "always"
	}
	return releasedocs.Enabled(artifactCfg, bump)
}
```
> `releasedocs.Enabled` is the shared gate at `internal/releasedocs/context.go:126` — do NOT reimplement bump matching.

**Unreachable `Generate` stub** (apidocs.go:46-51 — required to satisfy the `Generator` interface; dispatcher uses `GenerateMulti`):
```go
func (g *Generator) Generate(_ context.Context, _ releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	return releasedocs.Artifact{Kind: releasedocs.KindDiagrams}, nil
}
```

**`GenerateMulti` — graceful `(nil,nil)` skip + per-artifact emit** (apidocs.go:62-114 is the structural template). Diagrams differs in that it loops `(type, path)` pairs and does the FileFetcher type-assert inline (apidocs delegates to `discoverSpec`). Key load-bearing patterns to copy:

1. **FileFetcher type-assert → `(nil,nil)` on absence** (mirror discover.go:53-56 but return `(nil,nil)`, not an error, since this is the family-level skip):
```go
ff, ok := rc.Provider.(releasedocs.FileFetcher)
if !ok {
	slog.Warn("diagrams: provider does not implement FileFetcher; skipping", "repo", rc.Repo)
	return nil, nil // D-08 family-level skip
}
```
2. **Per-source fetch at `rc.ToRef` + skip-with-log (`continue`, never error)** — `FetchFileFromRef` signature from releasedocs.go:152:
```go
b, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, p)
if err != nil {
	slog.Warn("diagrams: source not fetched, skipping",
		"type", t.name, "path", p, "toRef", rc.ToRef, "err", err)
	continue
}
```
3. **`Artifact` emit with `Filename` sub-path** (apidocs.go:81-85 sets `Filename: "openapi.yaml"`; diagrams sets a nested sub-path):
```go
arts = append(arts, releasedocs.Artifact{
	Kind:     releasedocs.KindDiagrams,
	Filename: "diagrams/" + t.name + "/" + diagramName(p) + ".md",
	Content:  wrapMermaidFence(b),
})
```
4. **Return** — `return arts, nil` (an empty/nil slice is fine; dispatcher.go:117 does `arts = append(arts, multi...)` which is nil-safe).

**Compile-time interface assertions** (apidocs.go:118-119 — copy verbatim):
```go
var _ releasedocs.Generator = (*Generator)(nil)
var _ releasedocs.MultiGenerator = (*Generator)(nil)
```

**Determinism pitfall (RESEARCH Pitfall 2):** iterate diagram types via an **ordered slice**, never a `map`. Define:
```go
var diagramTypes = []struct {
	name  string
	paths func(config.DiagramsConfig) []string
}{
	{"sequence", func(c config.DiagramsConfig) []string { return c.Sequence }},
	{"dependency", func(c config.DiagramsConfig) []string { return c.Dependency }},
	{"state", func(c config.DiagramsConfig) []string { return c.State }},
	{"flowchart", func(c config.DiagramsConfig) []string { return c.Flowchart }},
	{"class", func(c config.DiagramsConfig) []string { return c.Class }},
}
```

---

### `internal/releasedocs/generators/diagrams/sniff.go` (utility, transform)

**Analog:** `internal/releasedocs/generators/apidocs/discover.go` (for the package-level lookup-table + pure-Go inspection style; `isMissingFile` at discover.go:33-39 shows the heuristic-string pattern this package mirrors).

**Pattern — `firstSignificantToken` + keyword table** (new code; RESEARCH Pattern 2). Must skip leading blank lines, `%%` comment lines, and a `---`…`---` frontmatter block (Pitfall 1), then prefix-match (Pitfall 5 — `stateDiagram-v2` has prefix `stateDiagram`):
```go
var mermaidKeywords = map[string][]string{
	"sequence":   {"sequenceDiagram"},
	"class":      {"classDiagram"},
	"state":      {"stateDiagram"}, // prefix-match also accepts stateDiagram-v2
	"flowchart":  {"flowchart", "graph"},
	"dependency": {"flowchart", "graph", "erDiagram"}, // RESEARCH Q3 — confirm set
}

func sniffMermaid(src []byte, diagramType string) bool {
	first := firstSignificantToken(src)
	for _, kw := range mermaidKeywords[diagramType] {
		if strings.HasPrefix(first, kw) {
			return true
		}
	}
	return false
}
```
> RESEARCH Q4 recommendation: soft consistency — if the first line is *some* recognized Mermaid keyword but not in this type's set, `slog.Warn` a mismatch and still publish (reject only non-Mermaid). Planner to confirm strict-vs-soft.

---

### `internal/releasedocs/generators/diagrams/render.go` (utility, transform)

**Analog:** `internal/releasedocs/generators/apidocs/render_markdown.go` — but **deliberately simpler**. apidocs uses `text/template` + an embedded preset (`//go:embed presets/apidocs.tmpl`, render_markdown.go:47-48) because it has structured data. Diagrams has exactly one variable (the raw bytes), so RESEARCH Q2 recommends a **fixed `const`/`bytes.Buffer` wrapper, NO template, NO embed**:
```go
func wrapMermaidFence(src []byte) []byte {
	body := bytes.TrimRight(src, "\n")
	var b bytes.Buffer
	b.WriteString("```mermaid\n")
	b.Write(body)
	b.WriteString("\n```\n")
	return b.Bytes()
}
```
> Byte-stable output (no timestamps/IDs) → golden-file testable, satisfies D-05/D-06.
> `diagramName(p)` filename derivation (RESEARCH Q5): `strings.TrimSuffix(path.Base(srcPath), ".mmd")` — use `path.Base` ONLY (Pitfall 4: never pass the raw config path into `Filename`).

---

### `internal/releasedocs/generators/diagrams/diagrams_test.go` (test, golden-file)

**Analog:** `internal/releasedocs/generators/apidocs/apidocs_test.go` — clone the test harness verbatim.

**`updateGolden` flag + `fakeFetcher` test double** (apidocs_test.go:20, 22-52 — copy verbatim, rename to diagrams):
```go
var updateGolden = os.Getenv("TEST_UPDATE_GOLDEN") == "1"

type fakeFetcher struct {
	files        map[string][]byte // path → content; absent → 404 error
	vcs.Provider                   // embedded provider satisfies vcs.Provider
}

func (f *fakeFetcher) FetchFileFromRef(_ context.Context, _, _, path string) ([]byte, error) {
	b, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("404 not found: %s", path)
	}
	return b, nil
}

func newFakeFetcher(files map[string][]byte) *fakeFetcher {
	_, provider := releasedocstest.NewFake() // internal/releasedocs/releasedocstest/fake.go:120
	return &fakeFetcher{files: files, Provider: provider}
}
```

**`fixtureDiagramsRC` builder** (mirror `fixtureAPIDocsRC` apidocs_test.go:57-79 — build `config.DiagramsConfig` instead of `APIDocsConfig`):
```go
func fixtureDiagramsRC(files map[string][]byte, dc config.DiagramsConfig) releasedocs.ReleaseContext {
	cfg := config.ReleaseDocs{Artifacts: config.ReleaseArtifacts{Diagrams: dc}}
	return releasedocs.ReleaseContext{
		Repo: "owner/repo", Org: "org1",
		FromRef: "v1.0.0", ToRef: "v1.1.0",
		Bump: releasedocs.BumpMinor,
		Config: cfg, Provider: newFakeFetcher(files),
	}
}
```

**`mustReadFixture`** (apidocs_test.go:82-89 — copy verbatim, reads `testdata/<name>`).

**Golden-file test wiring** (apidocs_test.go:571-608 — the canonical `TEST_UPDATE_GOLDEN` write-or-compare loop):
```go
goldenPath := "testdata/golden/sequence_login.golden"
if updateGolden {
	if err := os.WriteFile(goldenPath, art.Content, 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	return
}
want, err := os.ReadFile(goldenPath)
if err != nil { t.Fatalf("read golden %q: %v", goldenPath, err) }
if string(art.Content) != string(want) {
	t.Errorf("does not match golden %q", goldenPath)
}
```

**`findArtifact(arts, filename)` helper** (apidocs_test.go:652-657 — copy verbatim; match on `Filename` like `"diagrams/sequence/login.md"`).

**Required test functions (REQUIREMENTS map, RESEARCH §Validation):**
- `TestDiagrams_Kind` (apidocs_test.go:92) → DIAG sanity
- `TestDiagrams_Enabled` (apidocs_test.go:101, table-driven) → DIAG-01
- `TestDiagrams_GenerateMulti` (fetch at ToRef → N artifacts) → DIAG-02
- `TestDiagrams_Skip` (missing path + non-Mermaid `.txt` → `continue`, siblings unaffected) → DIAG-04
- `TestDiagrams_Golden` (byte-stable, `TEST_UPDATE_GOLDEN=1`) → DIAG-05

---

### `internal/releasedocs/generators/diagrams/testdata/` (test fixtures)

**Analog:** `apidocs/testdata/` (fixtures) + `apidocs/testdata/golden/markdown_v2.golden` / `markdown_v3.golden` (golden).

Recommended fixtures (RESEARCH Q6): `login.mmd` (sequenceDiagram), `domain.mmd` (classDiagram), a frontmatter-prefixed source (Pitfall 1), a `%%`-comment-prefixed source, `not-mermaid.txt` (skip case). Golden files one-per-page: `testdata/golden/sequence_login.golden`, `testdata/golden/class_domain.golden`.

---

### `internal/releasedocs/releasedocs.go` (model/const) — MODIFY

**Analog:** existing `KindAPIDocs` const (releasedocs.go:29-35). Add to the same `const` block (releasedocs.go:20-36):
```go
// KindDiagrams identifies the diagrams generator family. A single GenerateMulti
// call emits one markdown-page Artifact per resolved Mermaid source file, each
// differentiated by its Filename field (e.g. "diagrams/sequence/login.md").
// The diagrams generator implements the MultiGenerator interface.
KindDiagrams ArtifactKind = "diagrams"
```
> No interface changes — `MultiGenerator`, `Artifact.Filename`, `FileFetcher` all already exist (Phase 3). This is the ONLY edit to releasedocs.go.
> Every exported symbol needs a docstring (revive `exported` is on — CLAUDE.md).

---

### `internal/config/config.go` (config) — MODIFY

**Analog:** `APIDocsConfig` inline-embed (config.go:118-132) + its registration in `ReleaseArtifacts` (config.go:85-88).

**Step 1 — add field to `ReleaseArtifacts`** (after config.go:88):
```go
// Diagrams configures the engineering-diagrams artifact family. The user lists
// committed Mermaid source paths per diagram type; all output is gated together
// by a single enabled + when: condition (D-07). Wired in Phase 7.
Diagrams DiagramsConfig `yaml:"diagrams"`
```

**Step 2 — add `DiagramsConfig` struct** (mirror `APIDocsConfig`'s `ArtifactConfig` inline-embed at config.go:123-124; RESEARCH §Code Examples):
```go
type DiagramsConfig struct {
	ArtifactConfig `yaml:",inline"` // enabled + when: + (unused) preset/template
	Sequence   []string `yaml:"sequence"`
	Dependency []string `yaml:"dependency"`
	State      []string `yaml:"state"`
	Flowchart  []string `yaml:"flowchart"`
	Class      []string `yaml:"class"`
}
```
> The embedded `ArtifactConfig` carries `Preset`/`Template` (config.go:100-106); diagrams **ignores them** in v1 (RESEARCH Q2 — fixed wrapper). Each exported field needs a docstring.

---

### `internal/releasedocs/defaults/defaults.go` (wiring) — MODIFY

**Analog:** `apidocs.New()` registration (defaults.go:40) and its import (defaults.go:14).

Add import to the generators group (defaults.go:14-17):
```go
"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/diagrams"
```
Append to `DefaultGenerators()` slice (after defaults.go:40):
```go
func DefaultGenerators() []releasedocs.Generator {
	return []releasedocs.Generator{
		changelog.New(),
		releasenotes.New(),
		blog.New(),
		apidocs.New(),
		diagrams.New(), // Phase 7
	}
}
```

---

### `internal/releasedocs/publishers/pages/pages_diagrams_test.go` (test) — NEW (extend pages tests)

**Analog:** `pages_apidocs_test.go` (the whole file). No change to `pages.go` itself — the publisher already routes arbitrary `Artifact.Filename` sub-paths and is idempotent (pages.go:96-100, 113-117).

**Path-routing test** (mirror `TestPublish_APIDocs_Paths` pages_apidocs_test.go:16-74). Use `releasedocstest.NewFake()` + `enabledPages()` (pages_test.go:49), assert `fake.UpsertFileCalls` and `fake.CapturedFiles` paths:
```go
arts := []releasedocs.Artifact{
	{Kind: releasedocs.KindDiagrams, Filename: "diagrams/sequence/login.md", Content: []byte("```mermaid\nsequenceDiagram\n```\n")},
	{Kind: releasedocs.KindDiagrams, Filename: "diagrams/class/domain.md", Content: []byte("```mermaid\nclassDiagram\n```\n")},
}
// want: docs/releases/<toRef>/diagrams/sequence/login.md  (and .../class/domain.md)
```

**Idempotency test** (mirror `TestIdempotent_APIDocs` pages_apidocs_test.go:80-157): publish twice, assert second-run paths exactly match first-run paths.

> The pages publisher computes `path.Join(dir,"releases",rc.ToRef,filename)` then guards the `{dir}/` prefix (pages.go:100-109). The generator-constructed `Filename` (built from `path.Base`) cannot contain `../`, so the guard is satisfied by construction.

---

### `.cadoo.yaml.example` (config doc) — MODIFY

**Analog:** the `apiDocs:` block (lines 116-124, with its explanatory comment block 107-115). Add a sibling `diagrams:` block inside `releaseDocs.artifacts:` (after line 124):
```yaml
    # Engineering-diagrams artifact family — renders committed Mermaid source files
    # (one markdown page per source, wrapped in a ```mermaid fence) and publishes to
    # pages at releases/<tag>/diagrams/<type>/<name>.md. All output is gated by the
    # single enabled + when: condition below (D-07). A listed source that is missing
    # or not valid Mermaid is skipped with a logged reason; siblings are unaffected (D-08).
    # Note: a mermaid fence renders on github.com's file view and release bodies; a
    # Jekyll/GitLab-Pages *served* site needs a client-side mermaid include (deferred).
    diagrams:
      enabled: false
      when: always          # default "always" — runs on every release (D-08)
      sequence:   []        # repo-relative paths to committed Mermaid sequenceDiagram sources
      dependency: []        # graph/flowchart/erDiagram sources
      state:      []        # stateDiagram sources
      flowchart:  []        # flowchart/graph sources
      class:      []        # classDiagram sources
```

---

## Shared Patterns

### Graceful skip (D-08 / DIAG-04)
**Source:** apidocs.go:65-69 (family-level `(nil,nil)`), discover.go:70-77 (per-item `continue`+`slog.Warn`).
**Apply to:** `diagrams.go` `GenerateMulti`.
- Family-level skip (no FileFetcher) → `slog.Warn(...)` + `return nil, nil`.
- Per-source failure (404, non-Mermaid) → `slog.Warn(...)` + `continue`.
- **Never** return a non-nil error on a skip — dispatcher.go:114-115 wraps any error and aborts ALL sibling generators (RESEARCH Anti-Pattern).

### 404 tolerance heuristic
**Source:** `isMissingFile` (discover.go:33-39) — mirrors `template/template.go:isMissingFile`; checks `fs.ErrNotExist` + `"404"`/`"not found"` substrings so the package never imports VCS adapters.
**Apply to:** diagrams per-source fetch (optional — diagrams can skip on any fetch error, so the heuristic is only needed if it wants to distinguish 404 from transient errors in the log message).

### `Enabled` "always" default coercion (D-07)
**Source:** apidocs.go:33-40 + blog.go:38-44 + the shared `releasedocs.Enabled` (context.go:126).
**Apply to:** `diagrams.go` `Enabled`. Read `cfg.Artifacts.Diagrams.ArtifactConfig`, coerce empty `When`→`"always"`, delegate to `releasedocs.Enabled`. Do NOT reimplement bump matching.

### `Artifact.Filename` sub-path routing + idempotency
**Source:** pages.go:96-100 (filename fallback), :100 (`path.Join`), :104-109 (prefix guard), :113-117 (`UpsertFile`).
**Apply to:** generator sets `Filename = "diagrams/<type>/<base>.md"`; publisher needs no change. Idempotency is provided by `UpsertFile` overwriting in place.

### Golden-file test harness
**Source:** apidocs_test.go:20 (`updateGolden`), :22-52 (`fakeFetcher`), :82-89 (`mustReadFixture`), :571-608 (write-or-compare), :652-657 (`findArtifact`); `releasedocstest.NewFake()` (fake.go:120) for the embedded provider.
**Apply to:** `diagrams_test.go`.

---

## No Analog Found

None. Every file has an exact or near-exact analog. Phase 7 introduces zero new infrastructure — `MultiGenerator`, `Artifact.Filename`, `FileFetcher`, the pages publisher, and `releasedocs.Enabled` all shipped in Phases 2-3.

The only genuinely net-new code (no direct analog, but small and RESEARCH-specified) is the Mermaid keyword **sniff** (`sniff.go`) and the fixed **fence wrapper** (`render.go`) — both pure-Go stdlib, with templates provided in RESEARCH Patterns 2 and 3.

---

## Metadata

**Analog search scope:** `internal/releasedocs/` (generators/apidocs, generators/blog, publishers/pages, defaults, releasedocstest, dispatcher.go, context.go, releasedocs.go), `internal/config/config.go`, `.cadoo.yaml.example`.
**Files scanned:** 13 source files read; apidocs package is the primary template.
**Key shared utility:** `releasedocs.Enabled` (context.go:126) — the canonical gate; `releasedocstest.NewFake` (fake.go:120) — the canonical test provider with `UpsertFileCalls`/`CapturedFiles` recording.
**Pattern extraction date:** 2026-06-13
