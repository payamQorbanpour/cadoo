# Phase 7: Release-Docs Engineering Diagrams - Research

**Researched:** 2026-06-13
**Domain:** Go release-docs subsystem extension (deterministic artifact generation + pages publishing); Mermaid diagram source rendering
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** The diagrams generator **renders committed diagram sources** — it does NOT derive diagrams from source code and does NOT use the LLM. "From the repository" is satisfied by reading diagram source files that already live in the repo. Fits the existing single-file `FileFetcher.FetchFileFromRef(repo, ref, path)` capability with zero new VCS plumbing. Mirrors api-docs D-01.
- **D-02:** Source format for v1 is **Mermaid only**. A listed source that is not Mermaid (`.puml`, `.dot`) is graceful-skipped with a logged reason (see D-08).
- **D-03:** Diagram sources are discovered via **explicit paths listed in config**, grouped by type — NOT by directory scanning. Each path is fetched individually via `FileFetcher.FetchFileFromRef` at the release tag (`rc.ToRef`). No VCS tree-listing capability is added.
- **D-04:** The set of diagram **types** is fixed: `sequence`, `dependency`, `state`, `flowchart`, `class`. The user chooses which to produce by populating that type's path list; an empty/absent type key produces nothing for that type.
- **D-05:** Each diagram is published as a **markdown page that wraps the source in a ` ```mermaid ` fence** — NO rendering binary is invoked (honors the "no new external runtimes" constraint). Output is fully deterministic and byte-stable (golden-file testable). The github.com-renders-mermaid vs GitHub-Pages/Jekyll-may-not nuance is a **research item** (resolved below; does NOT change this decision).
- **D-06:** Generation is **fully deterministic and LLM-free**. No LLM call on any path; `rc.LLM == nil` changes nothing.
- **D-07:** A **single `diagrams` family block** under `releaseDocs.artifacts` with one `enabled` + one `when:` gating all diagram output as a family. The block embeds `ArtifactConfig` (inline) and adds per-type path lists.
- **D-08:** **Skip-with-logged-reason, per source file** (not all-or-nothing across the family). A source that is missing, not valid Mermaid, or unreadable is skipped with a clear `slog` reason; other diagrams and all sibling artifacts still complete. On a generator-level skip condition, `GenerateMulti` returns `(nil, nil)` — never a non-nil error.
- **D-09:** **Pages only**, **one page per diagram**, at deterministic idempotent paths via `Artifact.Filename` and the existing pages publisher — e.g. `releases/<tag>/diagrams/<type>/<name>.md`. No release-body or changelog-PR delivery in this phase. Pages remains opt-in via `publish.pages`.
- **D-10:** Introduce `KindDiagrams` and implement the **`MultiGenerator`** interface (one `GenerateMulti` pass emits one `Artifact` per resolved source file, each with its own `Filename`). Register in `defaults.DefaultGenerators()`.

### Claude's Discretion
- The `when:` default — lean **`always`** (republish each release so a tag-pinned snapshot always exists), matching api-docs D-08; confirm in planning.
- How "valid Mermaid" is checked — lean toward a **lightweight first-line keyword sniff** rather than a full Mermaid parse. Confirm in research.
- Whether to verify the file under a given type key actually contains that diagram kind (type↔keyword consistency warn/skip), or publish whatever the file contains.
- Whether the markdown page wrapper supports `preset`/`template` override layering or uses a single fixed wrapper.
- Exact published path scheme, page filename derivation from the source path, and golden-file fixtures.
- Concrete `MultiGenerator` vs per-`Kind` mapping details and the `DiagramsConfig` struct field names/types.

### Deferred Ideas (OUT OF SCOPE)
- Code-derived diagrams (static analysis: `go/packages` import graph, struct/interface class diagrams, call-graph sequence diagrams).
- LLM-generated diagrams from code or the release diff.
- Rendering to SVG/PNG via an external tool (mermaid-cli/Node, Graphviz `dot`, PlantUML/Java).
- PlantUML / Graphviz source formats (`.puml`, `.dot`).
- Convention-directory auto-discovery (scan `docs/diagrams/**`) — needs a VCS tree-listing capability.
- Embedding diagrams in the release body / changelog PR.
- Cadoo-provided starter/example diagram templates.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DIAG-01 | User can enable a `diagrams` artifact and choose which types (sequence, dependency, state, flowchart, class) via `.cadoo.yaml`. | `DiagramsConfig` embeds `ArtifactConfig` (`enabled`+`when`) plus five `[]string` per-type path-list fields. Mirror `APIDocsConfig` exactly (config.go:123). `Enabled(cfg,bump)` gates the whole family. |
| DIAG-02 | For each selected type, Cadoo derives a diagram from the repository at release time. | "Derive" is satisfied by D-01 (render committed source). Each configured path fetched via `FileFetcher.FetchFileFromRef(ctx, repo, rc.ToRef, path)` (releasedocs.go:148). One `Artifact` per resolved source. |
| DIAG-03 | Diagrams published to pages at deterministic idempotent paths. | Existing pages publisher (pages.go) honors `Artifact.Filename`; path = `{dir}/releases/{toRef}/{filename}` with `UpsertFile` idempotency. Set `Filename = "diagrams/<type>/<name>.md"`. |
| DIAG-04 | Per-type graceful degradation — skip with logged reason, never fail siblings. | Per-source skip = `slog.Warn` + `continue`; generator-level skip = `(nil, nil)`. Dispatcher appends `multi...` (dispatcher.go:112-118); never returns error on skip. |
| DIAG-05 | Deterministic-first, reproducible with LLM disabled. | No LLM on any path (D-06). Fixed Go `text/template`/string wrapper → byte-stable. Golden-file tests mirror apidocs (`TestRenderMarkdown_Golden`, `TEST_UPDATE_GOLDEN=1`). |
</phase_requirements>

## Summary

Phase 7 is a near-mechanical clone of the Phase 3 `apidocs` generator pattern, applied to committed Mermaid source files. Every interface, capability, and publishing path it needs already exists and shipped in v1.0: the `MultiGenerator` interface, `Artifact.Filename` routing, the `FileFetcher` single-file fetch capability, and a pages publisher that already honors arbitrary sub-path filenames idempotently (verified by `pages_apidocs_test.go`). There is **zero new plumbing** — the work is a new `internal/releasedocs/generators/diagrams/` package, a `DiagramsConfig` struct in `config.go`, one line in `defaults.DefaultGenerators()`, and `.cadoo.yaml.example` documentation.

The one genuinely external research item — D-05's "does a Pages-served static site render a mermaid fence?" — resolves clearly: **GitHub.com's markdown viewer renders ` ```mermaid ` fences natively, but a GitHub Pages (Jekyll) or GitLab Pages *served HTML site* does NOT** render a bare mermaid fence without a client-side mermaid.js include. This is a documented, long-standing GitHub limitation. However, this does **not** force a runtime dependency: the page can include a client-side `<script>` that pulls mermaid.js in the browser at view time (no build-time binary, no Node, no Graphviz — fully honoring the "no new external runtimes" constraint). The recommendation below is to emit a `.md` page with the mermaid fence as the primary artifact (renders perfectly when browsed on github.com / the repo's docs branch tree view), and to **not** chase Jekyll-served-site rendering in v1 — keeping the artifact byte-identical to what D-05 mandates. A fixed wrapper (no preset/template layering) is recommended.

**Primary recommendation:** Build `diagrams.Generator` as a `MultiGenerator` clone of `apidocs.Generator`. For each configured `(type, path)` pair: fetch at `rc.ToRef`, sniff the first significant line for the type-appropriate Mermaid keyword (skip-with-log on miss), wrap in a fixed ` ```mermaid ` fenced markdown page, and emit one `Artifact{Kind: KindDiagrams, Filename: "diagrams/<type>/<basename>.md", Content: …}`. No preset/template layering, no LLM, golden-file tested.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Per-type diagram source selection | Config (`internal/config`) | — | User-facing `.cadoo.yaml` schema; mirrors `APIDocsConfig`. |
| Fetch committed source at tag | VCS adapter (via `FileFetcher` on `vcs.Provider`) | releasedocs generator | Single-file read; capability already implemented by github+gitlab adapters. |
| Mermaid validity sniff | Diagrams generator (`internal/releasedocs/generators/diagrams`) | — | Pure-Go string inspection; no parser, no network. |
| Markdown page wrapping | Diagrams generator | — | Deterministic string/`text/template` assembly. |
| Path derivation + idempotent write | Pages publisher (`publishers/pages`) | — | Already honors `Artifact.Filename`; no changes needed. |
| Registration / wiring | `defaults` package | dispatcher | `DefaultGenerators()` slice append; dispatcher prefers `MultiGenerator`. |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `text/template` | 1.26 | Deterministic page wrapper (if templated) `[VERIFIED: go.mod]` | Already the release-docs rendering engine (`internal/releasedocs/template`, apidocs `render_markdown.go`). |
| Go stdlib `strings` / `bufio` | 1.26 | First-line keyword sniff over source bytes `[VERIFIED: go.mod]` | No external parser needed; pure-Go, deterministic. |
| Go stdlib `log/slog` | 1.26 | Skip-with-logged-reason (D-08) `[VERIFIED: codebase grep]` | Exact pattern used by apidocs `discover.go` / `apidocs.go`. |
| Go stdlib `path` | 1.26 | Basename derivation for `Artifact.Filename` `[VERIFIED: codebase grep]` | Pages publisher already uses `path.Join` + prefix guard. |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `embed` | stdlib 1.26 | Embed a fixed page-wrapper template, if templated `[VERIFIED: codebase grep]` | Mirror apidocs `//go:embed presets/apidocs.tmpl`. Optional — a `const` string wrapper is simpler and equally deterministic. |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| First-line keyword sniff | A real Mermaid parser | **No robust pure-Go Mermaid parser exists** `[ASSUMED]`. Mermaid's reference parser is JavaScript (mermaid-js). Pulling one in violates the "no new external runtimes" constraint and adds CGO/WASM complexity. The sniff is correct for the validity gate this phase needs. |
| Fixed `const` wrapper | `text/template` + embedded preset | Both deterministic. A `const` wrapper is simpler and has no embed/FuncMap surface (smaller security footprint — see template.go T-03-01 note). Recommended: fixed wrapper. |
| `.md` page only | `.html` page with inlined mermaid.js | HTML would render on a Jekyll-served Pages *site*, but pulls a vendored mermaid bundle (large, versioned, like the Redoc bundle) — out of D-05's "wrap in a fence" scope and adds maintenance weight. Defer (see Open Questions Q1). |

**Installation:** No new dependencies. All stdlib. `[VERIFIED: go.mod inspection — no mermaid/diagram packages present in module]`

## Package Legitimacy Audit

> Not applicable — this phase installs **no external packages**. All functionality uses the Go standard library (`text/template`, `strings`, `bufio`, `log/slog`, `path`, `embed`) plus the existing in-repo `internal/releasedocs` and `internal/config` packages.

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

## Architecture Patterns

### System Architecture Diagram

```
                 .cadoo.yaml (releaseDocs.artifacts.diagrams)
                         │  enabled, when, sequence[], dependency[],
                         │  state[], flowchart[], class[]
                         ▼
        ┌──────────────────────────────────────┐
        │ dispatcher.go  (Run loop)             │
        │  for gen in Generators:               │
        │    if !gen.Enabled(cfg,bump): skip    │
        │    if mg,ok := gen.(MultiGenerator):  │
        │       arts += mg.GenerateMulti(rc) ───┼──┐
        └──────────────────────────────────────┘  │
                                                   ▼
        ┌──────────────────────────────────────────────────────────┐
        │ diagrams.Generator.GenerateMulti(ctx, rc)                  │
        │                                                            │
        │  ff := rc.Provider.(FileFetcher)  ── absent? → (nil,nil)   │
        │                                                            │
        │  for type in [sequence,dependency,state,flowchart,class]:  │
        │    for path in cfg.<type>:                                 │
        │      bytes,err := ff.FetchFileFromRef(repo, rc.ToRef, path)│
        │        404/err  → slog.Warn; continue ────────────┐       │
        │      kind := sniffMermaid(bytes)                   │       │
        │        no keyword / mismatch → slog.Warn; continue─┤       │
        │      page := wrapMermaidFence(bytes)               │       │
        │      emit Artifact{KindDiagrams,                   │       │
        │              Filename:"diagrams/<type>/<name>.md", │       │
        │              Content: page}                        │       │
        │                                                    ▼       │
        │  return []Artifact  (possibly empty → (nil,nil))  (skips logged)
        └────────────────────────────┬───────────────────────────────┘
                                      ▼
        ┌──────────────────────────────────────────────────────────┐
        │ pages.Publisher.Publish(ctx, rc, arts)                     │
        │   for art: p = path.Join(dir,"releases",toRef,art.Filename)│
        │   prefix-guard p under {dir}/ ; UpsertFile(branch,p,content)│
        │   (idempotent overwrite on re-run)                         │
        └──────────────────────────────────────────────────────────┘
                                      ▼
        docs branch: docs/releases/v1.2.0/diagrams/sequence/login.md
                     docs/releases/v1.2.0/diagrams/class/domain.md
```

### Recommended Project Structure
```
internal/releasedocs/generators/diagrams/
├── diagrams.go          # Generator: Kind/Enabled/Generate/GenerateMulti (clone apidocs.go)
├── sniff.go             # sniffMermaid(): first-significant-line keyword detection
├── render.go            # wrapMermaidFence(): deterministic page assembly (fixed wrapper)
├── diagrams_test.go     # Enabled gate, per-source skip, golden-file tests
└── testdata/
    ├── login.mmd            # fixture: sequenceDiagram source
    ├── domain.mmd           # fixture: classDiagram source
    ├── not-mermaid.txt      # fixture: skip case
    └── golden/
        ├── sequence_login.golden
        └── class_domain.golden
```

### Pattern 1: MultiGenerator clone of apidocs
**What:** Implement both `Generator` (stub `Generate` returning empty) and `MultiGenerator` (`GenerateMulti` does the real work). Dispatcher type-asserts `MultiGenerator` first.
**When to use:** Whenever one logical generate emits N files — exactly this phase (N sources → N pages).
**Example:**
```go
// Source: internal/releasedocs/generators/apidocs/apidocs.go (mirror this shape)
type Generator struct{}
func New() *Generator { return &Generator{} }
func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindDiagrams }

func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
	ac := cfg.Artifacts.Diagrams.ArtifactConfig
	if ac.When == "" { ac.When = "always" } // D-08 default coercion
	return releasedocs.Enabled(ac, bump)
}

// Generate is the unreachable Generator-interface stub (dispatcher uses GenerateMulti).
func (g *Generator) Generate(_ context.Context, _ releasedocs.ReleaseContext) (releasedocs.Artifact, error) {
	return releasedocs.Artifact{Kind: releasedocs.KindDiagrams}, nil
}

func (g *Generator) GenerateMulti(ctx context.Context, rc releasedocs.ReleaseContext) ([]releasedocs.Artifact, error) {
	ff, ok := rc.Provider.(releasedocs.FileFetcher)
	if !ok {
		slog.Warn("diagrams: provider does not implement FileFetcher; skipping", "repo", rc.Repo)
		return nil, nil // D-08 skip
	}
	var arts []releasedocs.Artifact
	for _, t := range diagramTypes { // ordered slice for determinism
		for _, p := range t.paths(rc.Config) {
			b, err := ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, p)
			if err != nil {
				slog.Warn("diagrams: source not fetched, skipping",
					"type", t.name, "path", p, "toRef", rc.ToRef, "err", err)
				continue
			}
			if !sniffMermaid(b, t.name) {
				slog.Warn("diagrams: source not valid Mermaid for type, skipping",
					"type", t.name, "path", p, "toRef", rc.ToRef)
				continue
			}
			arts = append(arts, releasedocs.Artifact{
				Kind:     releasedocs.KindDiagrams,
				Filename: "diagrams/" + t.name + "/" + diagramName(p) + ".md",
				Content:  wrapMermaidFence(b),
			})
		}
	}
	return arts, nil // empty slice is fine; dispatcher appends multi... (nil-safe)
}

var _ releasedocs.Generator = (*Generator)(nil)
var _ releasedocs.MultiGenerator = (*Generator)(nil)
```

### Pattern 2: First-line keyword sniff (validity gate)
**What:** Read source bytes, skip leading frontmatter / `%%` comments / blank lines, take the first significant token, match against the diagram-type keyword table.
**When to use:** The D-08 "valid Mermaid" check, replacing a full parser.
**Example:**
```go
// keyword table — recommended type→accepted-keyword mapping (see Pitfall 1 + Q for dependency)
var mermaidKeywords = map[string][]string{
	"sequence":   {"sequenceDiagram"},
	"class":      {"classDiagram"},
	"state":      {"stateDiagram", "stateDiagram-v2"},
	"flowchart":  {"flowchart", "graph"},
	"dependency": {"flowchart", "graph", "erDiagram"}, // see Open Questions Q3
}

// sniffMermaid reports whether the first significant line of src begins with a
// keyword valid for diagramType. It tolerates leading whitespace, %% comments,
// and --- frontmatter blocks that valid Mermaid files may start with.
func sniffMermaid(src []byte, diagramType string) bool {
	first := firstSignificantToken(src) // skips blank lines, %% comments, --- frontmatter
	for _, kw := range mermaidKeywords[diagramType] {
		if strings.HasPrefix(first, kw) { return true }
	}
	return false
}
```

### Pattern 3: Fixed mermaid-fence wrapper (deterministic, byte-stable)
**What:** Wrap raw source bytes in a fenced mermaid markdown block. No timestamps, no random IDs.
**Example:**
```go
// wrapMermaidFence produces a deterministic markdown page. The source bytes are
// inserted verbatim between fence markers; a trailing newline is normalized so
// re-runs are byte-identical (golden-file stable, D-06).
func wrapMermaidFence(src []byte) []byte {
	body := bytes.TrimRight(src, "\n")
	var b bytes.Buffer
	b.WriteString("```mermaid\n")
	b.Write(body)
	b.WriteString("\n```\n")
	return b.Bytes()
}
```
> Note: a fixed `const`/`bytes.Buffer` wrapper is recommended over `text/template` — there is no dynamic field, so a template adds no value and one more embed/parse surface.

### Anti-Patterns to Avoid
- **Adding a VCS tree-listing capability** to auto-discover `docs/diagrams/**`: explicitly out of scope (D-03, Deferred). Use explicit config paths only.
- **Invoking mermaid-cli / Node / Graphviz** to pre-render SVG/PNG: violates the "no new external runtimes" constraint and D-05. The fence is the artifact.
- **Returning a non-nil error on a per-source failure**: would propagate up `dispatcher.go:115` and abort sibling generators. Always `continue` with a `slog.Warn` (D-08).
- **Per-type separate `enabled`/`when` toggles**: D-07 mandates a single family gate. Embed one `ArtifactConfig`.
- **Map iteration over diagram types** without ordering: Go map iteration is random → non-deterministic artifact order → flaky golden tests. Use an ordered slice (`diagramTypes`).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Mermaid validity | A full Mermaid grammar parser | First-line keyword sniff | No robust Go parser exists; full parse is out of scope for a "is this plausibly Mermaid?" gate. |
| Idempotent path write | A new commit/upsert routine | Existing `pages.Publisher` + `vcs.BranchCommitter.UpsertFile` | Already idempotent, prefix-guarded, tested (`pages_apidocs_test.go`). |
| Per-file fetch at tag | New VCS plumbing | `releasedocs.FileFetcher.FetchFileFromRef` | Already implemented by github + gitlab adapters; type-assert off `rc.Provider`. |
| Multi-artifact emit | A custom dispatcher branch | `MultiGenerator.GenerateMulti` | Dispatcher already prefers it (dispatcher.go:112). |
| Filename → sub-path routing | Publisher changes | `Artifact.Filename` (e.g. `"diagrams/sequence/login.md"`) | Pages publisher already routes arbitrary sub-paths and guards `../` escape. |
| 404 detection | New error typing | `isMissingFile`-style heuristic (template.go:136 / discover.go:33) | Mirrors the established pattern so the package never imports VCS adapters. |

**Key insight:** This phase ships **no new infrastructure**. Every "hard" capability (multi-artifact, sub-path publishing, single-file fetch, idempotency, graceful skip) was built and tested in Phases 2–3. The only net-new code is the diagrams generator package + one config struct.

## Common Pitfalls

### Pitfall 1: Mermaid first-line is not always the keyword
**What goes wrong:** A valid Mermaid file may begin with a `---` YAML frontmatter block (title/config), one or more `%%` comment lines, or leading blank lines before the actual `sequenceDiagram`/`flowchart TD` line. A naive `bytes[0:N]` check rejects valid files.
**Why it happens:** Mermaid supports frontmatter and `%%` comments at the top of a file.
**How to avoid:** `firstSignificantToken` must skip: (a) leading blank/whitespace-only lines, (b) `%%`-prefixed comment lines, and (c) a leading `---`…`---` frontmatter block in its entirety, then take the first non-empty token of the next line. Match keyword as a prefix (e.g. `flowchart TD` starts with `flowchart`; `stateDiagram-v2` is its own keyword so check it *before* `stateDiagram` or use prefix-match — `stateDiagram-v2` has prefix `stateDiagram`, so `stateDiagram` prefix-match already accepts both).
**Warning signs:** Golden test for a frontmatter'd fixture skips unexpectedly; valid diagrams missing from output.

### Pitfall 2: Non-deterministic artifact order from map iteration
**What goes wrong:** Iterating the five types via a `map[string][]string` yields random order across runs, so the emitted `[]Artifact` order varies, breaking any order-sensitive golden assertion and producing churny commits.
**How to avoid:** Define an ordered `var diagramTypes = []struct{name string; paths func(cfg) []string}{...}` and iterate that. Within a type, iterate the config slice in its given order (config order is stable).
**Warning signs:** Intermittent golden-test failures; diff noise on identical input.

### Pitfall 3: Filename collisions across types or duplicate basenames
**What goes wrong:** Two sources with the same basename under *different* types (`sequence/login.mmd` and `flowchart/login.mmd`) are fine because the type is in the path. But two sources with the same basename under the *same* type (`a/login.mmd` and `b/login.mmd` both listed under `sequence`) would both map to `diagrams/sequence/login.md` — the second silently overwrites the first via `UpsertFile`.
**How to avoid:** Recommended derivation: `Filename = "diagrams/" + type + "/" + strings.TrimSuffix(path.Base(srcPath), ".mmd") + ".md"`. For same-type basename collisions, **log a warning and skip the colliding later entry** (deterministic: first-listed wins), OR document that basenames within a type must be unique. Recommend the warn-and-skip guard for safety. (Cross-type same-basename is NOT a collision — the `<type>/` segment disambiguates.)
**Warning signs:** Fewer published pages than configured sources with no obvious skip reason.

### Pitfall 4: Path-escape via adversarial source path
**What goes wrong:** A config path like `../../../etc/x.mmd` could try to escape the docs dir.
**Why it's already handled:** The pages publisher computes `path.Join(dir,"releases",toRef,filename)` and rejects any result not prefixed with `{dir}/` (pages.go:104-109). Since `Filename` here is generator-constructed (`"diagrams/"+type+"/"+base`) and `path.Base` strips directory components, the emitted filename cannot contain `../`. Still, **do not** pass the raw config source path into `Filename` — derive from `path.Base` only.
**How to avoid:** Use `path.Base(srcPath)` for the filename segment, never the full configured path.

### Pitfall 5: `stateDiagram` vs `stateDiagram-v2` keyword matching
**What goes wrong:** If you match keywords with exact equality instead of prefix, `stateDiagram-v2` fails a `== "stateDiagram"` check.
**How to avoid:** Use `strings.HasPrefix(firstToken, kw)`. `stateDiagram-v2` has prefix `stateDiagram`, so a single `{"stateDiagram"}` prefix entry accepts both. (The example table lists both for clarity; prefix-match with just `stateDiagram` is sufficient.)

## Runtime State Inventory

> Not a rename/refactor/migration phase — greenfield additive feature. Section omitted (no stored data, service config, OS state, secrets, or build artifacts are renamed or migrated).

## Code Examples

### DiagramsConfig (mirror APIDocsConfig exactly)
```go
// Source: internal/config/config.go:118-132 (APIDocsConfig pattern)

// In ReleaseArtifacts (config.go:76):
//   Diagrams DiagramsConfig `yaml:"diagrams"`

// DiagramsConfig extends ArtifactConfig with diagrams-specific per-type source
// path lists. All diagram output is gated together by a single enabled + when:
// condition (D-07). The default when: is "always" (D-08).
type DiagramsConfig struct {
	ArtifactConfig `yaml:",inline"`
	// Sequence lists repository-relative paths to committed Mermaid sequence-diagram
	// sources, fetched at the release tag.
	Sequence []string `yaml:"sequence"`
	// Dependency lists Mermaid dependency-diagram (graph/flowchart/erDiagram) sources.
	Dependency []string `yaml:"dependency"`
	// State lists Mermaid state-diagram sources.
	State []string `yaml:"state"`
	// Flowchart lists Mermaid flowchart/graph sources.
	Flowchart []string `yaml:"flowchart"`
	// Class lists Mermaid class-diagram sources.
	Class []string `yaml:"class"`
}
```

### KindDiagrams constant (releasedocs.go)
```go
// Source: internal/releasedocs/releasedocs.go:20-36 (add to the const block)

// KindDiagrams identifies the diagrams generator family. A single GenerateMulti
// call emits one markdown-page Artifact per resolved Mermaid source file, each
// differentiated by its Filename field (e.g. "diagrams/sequence/login.md").
// The diagrams generator implements the MultiGenerator interface.
KindDiagrams ArtifactKind = "diagrams"
```

### defaults registration
```go
// Source: internal/releasedocs/defaults/defaults.go:35-42
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

### Golden-file test wiring (mirror apidocs_test.go)
```go
// Source: internal/releasedocs/generators/apidocs/apidocs_test.go:17-89, 568-608

var updateGolden = os.Getenv("TEST_UPDATE_GOLDEN") == "1"

// fakeFetcher embeds a releasedocstest.Fake provider and serves files from a map;
// absent paths return a "404 not found" error so isMissingFile tolerance applies.
// (Reuse the apidocs_test.go fakeFetcher shape verbatim.)

func TestRenderDiagram_Golden(t *testing.T) {
	t.Parallel()
	src := mustReadFixture(t, "login.mmd")
	rc := fixtureDiagramsRC(map[string][]byte{"docs/diagrams/login.mmd": src},
		/* sequence */ []string{"docs/diagrams/login.mmd"}, "", true, releasedocs.BumpMinor)
	arts, err := diagrams.New().GenerateMulti(context.Background(), rc)
	// ... assert one artifact, Filename "diagrams/sequence/login.md", compare to golden
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Mermaid only renders via mermaid-cli build step | github.com renders ` ```mermaid ` fences **natively** in markdown views, issues, PRs, release bodies | GitHub shipped native Mermaid Feb 2022 | A committed `.md` with a mermaid fence renders with zero tooling when browsed on github.com / a repo's branch file view. `[CITED: github.com community discussions]` |
| (n/a) | GitHub Pages **Jekyll** still does **not** whitelist a Mermaid plugin → a *served Pages site* does not render bare fences | unchanged as of 2026 | Pages-served HTML needs a client-side mermaid.js include; does not affect the `.md`-on-branch-tree view. `[CITED: github/pages-gem#835, community/discussions/65040]` |

**Deprecated/outdated:**
- Assuming "published to pages" == "rendered diagram in a browser on a Jekyll site": **false** for a bare fence. See Open Questions Q1 for the recommendation.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | No robust pure-Go Mermaid parser exists worth depending on. | Standard Stack / Don't Hand-Roll | Low. Even if one exists, the first-line sniff is the *intended* lightweight gate (D-08 discretion); a parser is explicitly not wanted. |
| A2 | `dependency` diagrams map to Mermaid `graph`/`flowchart` (and possibly `erDiagram`) keywords. | Sniff table / Open Q3 | Medium. Mermaid has no `dependencyDiagram` keyword. If the user's intent for "dependency" differs (e.g. they expect `erDiagram` only), the sniff must accept the right set. Recommended: accept `flowchart`, `graph`, `erDiagram` for `dependency` and confirm with user. |
| A3 | A `.md` mermaid-fence page is the desired v1 artifact (not an HTML page with inlined mermaid.js). | Open Q1 | Low–Medium. D-05 locks "wrap in a fence"; HTML rendering is a deferred enhancement. If the user specifically needs a self-rendering Pages *site*, revisit. |

## Open Questions

1. **Pages-rendering of a bare mermaid fence (D-05 nuance) — RESOLVED with recommendation.**
   - What we know: github.com's markdown viewer, issues, PRs, and **release bodies** render ` ```mermaid ` fences natively (since Feb 2022). A **GitHub Pages (Jekyll)** served site does **NOT** render a bare mermaid fence — Jekyll does not whitelist a Mermaid plugin (`github/pages-gem#835`). **GitLab Pages** serves whatever static files you commit; a raw `.md` served as a static file is not rendered to HTML at all unless a generator processes it. To render on a *served Pages site*, the page must include a client-side mermaid.js loader (e.g. `<pre class="mermaid">…</pre>` + `<script type="module"> import mermaid …; mermaid.initialize({startOnLoad:true})</script>`). That script runs in the **browser at view time** — it is NOT a build-time runtime, so it does **not** violate the "no new external runtimes" constraint.
   - What's unclear: Whether the docs branch this phase commits to is *served as a Pages site* or *browsed as files in the branch tree*. The existing pages publisher commits `.md` files to a `gh-pages`/`docs` branch; if the user simply browses those files on github.com they render perfectly with the bare fence. If they run them through Jekyll, they won't.
   - **Recommendation:** For v1, emit the **`.md` page with a bare ` ```mermaid ` fence** exactly as D-05 specifies. This is correct and renders on the github.com file view (the most common way committed release-docs are consulted), keeps the artifact byte-stable and golden-testable, and adds no vendored bundle. Do **not** attempt Jekyll-served-site rendering in v1. Document in `.cadoo.yaml.example` that a mermaid fence renders on github.com's file view and release bodies, and that a Jekyll/GitLab-Pages *served site* needs a client-side mermaid include (a deferred enhancement). This keeps D-05 intact and defers the HTML-with-mermaid.js variant cleanly.

2. **Wrapper preset/template layering (Claude's Discretion) — RESOLVED.**
   - Recommendation: **Use a single fixed wrapper (no preset/template override).** The page has exactly one variable — the raw source bytes — and no per-tone or per-layout variation. Adding `preset`/`template` resolution (mirroring `template.Resolve`) buys nothing and adds an embed + FuncMap-free parse surface for no benefit. The embedded `ArtifactConfig` still carries `Preset`/`Template` fields (they come with the inline embed) but the diagrams generator should ignore them in v1. Ties to Q1: since the wrapper is a fixed bare fence, there's no rendering-target variation to template over.

3. **`dependency` keyword mapping — needs user confirmation.**
   - What we know: Mermaid has **no** `dependencyDiagram` keyword. A "dependency diagram" is conventionally expressed as a `graph`/`flowchart` (boxes + arrows) or, for data/entity dependencies, an `erDiagram`.
   - What's unclear: which keyword(s) the user intends `dependency` sources to use.
   - Recommendation: Accept `flowchart`, `graph`, and `erDiagram` as valid for the `dependency` type (superset). If type↔keyword consistency checking is enabled (see Q4), warn-but-publish on a `dependency` file that is actually a `flowchart` keyword, since they overlap. Confirm the intended set in planning/discuss.

4. **Type↔keyword consistency (Claude's Discretion) — RESOLVED with recommendation.**
   - Recommendation: **Soft consistency — publish whatever the file contains as long as it sniffs as *some* valid Mermaid diagram; warn on type mismatch but do not skip.** Rationale: the type key drives the *published path* (`diagrams/<type>/…`), which is a user-organizational choice; the file content is the source of truth. Skipping on mismatch would surprise users who put a `flowchart` under `dependency` (a legitimate overlap, see Q3). So: sniff must confirm the first significant line is a recognized Mermaid keyword *at all* (reject `.puml`/`.dot`/prose per D-02); if it's a recognized keyword but not in the type's expected set, emit `slog.Warn("diagrams: type/keyword mismatch", …)` and still publish. This satisfies D-02 (reject non-Mermaid) and D-08 (skip non-Mermaid) while being lenient on type labeling. (If the planner prefers strict skip-on-mismatch, that's also defensible — flag for the user.)

5. **Path scheme + filename derivation (Claude's Discretion) — RESOLVED.**
   - Recommendation: `Artifact.Filename = "diagrams/" + type + "/" + strings.TrimSuffix(path.Base(srcPath), ".mmd") + ".md"`. Strip a trailing `.mmd` (and tolerate `.mermaid`); if the basename has neither extension, just append `.md`. The pages publisher then writes `{dir}/releases/{toRef}/diagrams/<type>/<name>.md` — consistent with how apidocs sets `Filename` to a bare relative path and the publisher prepends `{dir}/releases/{toRef}/`. Cross-type same-basename is fine (type segment disambiguates); same-type same-basename → warn-and-skip the later entry (Pitfall 3).

6. **Golden-file test strategy — RESOLVED (documented).**
   - apidocs golden tests (`apidocs_test.go:568-650`) use: a package-level `updateGolden = os.Getenv("TEST_UPDATE_GOLDEN") == "1"` flag; a `fakeFetcher` test double embedding a `releasedocstest.NewFake()` provider and serving files from a `map[string][]byte` (absent → `"404 not found"` error); `mustReadFixture` reading from `testdata/`; golden files under `testdata/golden/*.golden`; when `updateGolden`, `os.WriteFile` the golden and return, else `os.ReadFile` + byte-compare. Mirror this exactly. Recommended fixtures: a `sequenceDiagram` source, a `classDiagram` source, a frontmatter-prefixed source (Pitfall 1), a `%%`-comment-prefixed source, and a non-Mermaid `.txt` (skip case). Golden files: one per published page (`sequence_login.golden`, `class_domain.golden`).

## Environment Availability

> Skip — no external tools/services/runtimes are required. The phase is pure-Go, stdlib-only, using existing in-repo capabilities. The dogfood target (SC-5) requires committing a couple of `.mmd` files in Cadoo's own repo and running the existing release-docs path; no new binary is installed.

## Validation Architecture

> `.planning/config.json` was checked: `workflow.nyquist_validation` was not located as an explicit `false`, so this section is included (absent/true = enabled).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (1.26) `[VERIFIED: codebase]` |
| Config file | none — `go test` convention |
| Quick run command | `go test -run TestDiagrams -count=1 ./internal/releasedocs/generators/diagrams/...` |
| Full suite command | `make test` (`go test -race -count=1 ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DIAG-01 | Enable gate + per-type selection honored | unit | `go test -run TestDiagrams_Enabled ./internal/releasedocs/generators/diagrams/...` | ❌ Wave 0 |
| DIAG-02 | Each configured source → one artifact fetched at ToRef | unit | `go test -run TestDiagrams_GenerateMulti ./...diagrams/...` | ❌ Wave 0 |
| DIAG-03 | Deterministic idempotent pages paths | unit (publisher) | `go test -run 'TestPublish_Diagrams|TestIdempotent' ./internal/releasedocs/publishers/pages/...` | ❌ Wave 0 (extend existing pages tests) |
| DIAG-04 | Per-source skip with logged reason, siblings unaffected | unit | `go test -run TestDiagrams_Skip ./...diagrams/...` | ❌ Wave 0 |
| DIAG-05 | LLM-off deterministic golden output | unit (golden) | `go test -run TestDiagrams_Golden ./...diagrams/...` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -count=1 ./internal/releasedocs/generators/diagrams/... ./internal/config/...`
- **Per wave merge:** `make test`
- **Phase gate:** `make ci` (vet + test + build) green before `/gsd:verify-work`.

### Wave 0 Gaps
- [ ] `internal/releasedocs/generators/diagrams/diagrams_test.go` — covers DIAG-01, DIAG-02, DIAG-04, DIAG-05 (reuse the `fakeFetcher` shape from `apidocs_test.go`).
- [ ] `internal/releasedocs/generators/diagrams/testdata/` + `testdata/golden/` fixtures.
- [ ] `internal/releasedocs/publishers/pages/pages_diagrams_test.go` — DIAG-03 path + idempotency (mirror `pages_apidocs_test.go`).
- [ ] Framework install: none — stdlib `testing` already present.

## Security Domain

> `security_enforcement` treated as enabled (not explicitly `false` in config). This is a low-risk additive feature; the relevant surface is untrusted repo content flowing into committed pages.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No auth surface; runs inside existing release-docs pipeline. |
| V3 Session Management | no | — |
| V4 Access Control | no | Reuses provider auth; no new access paths. |
| V5 Input Validation | yes | Source path + source content are repo-controlled. Validate keyword sniff; derive filenames via `path.Base` only; rely on the pages publisher's existing `../` prefix-guard (pages.go:104). |
| V6 Cryptography | no | No crypto. |

### Known Threat Patterns for Go release-docs / Mermaid pages

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal via adversarial config source path → write outside docs dir | Tampering | Derive `Filename` from `path.Base(srcPath)` (strips dirs); pages publisher rejects any joined path not prefixed `{dir}/` (existing guard, pages.go:104-109). |
| Mermaid source containing fence-breakout (e.g. a literal ` ``` ` line) corrupts the wrapping page | Tampering | The mermaid fence body is repo-committed by the maintainer (same trust level as any committed file). A source containing ` ``` ` would break its own page render but cannot escape the artifact. Optional hardening: skip-with-log if the source contains a fence terminator. Recommend documenting; not blocking for v1. |
| Non-Mermaid content published as a "diagram" | Tampering/Spoofing | First-line keyword sniff rejects non-Mermaid sources (`.puml`/`.dot`/prose) → skip with log (D-02/D-08). |
| Resource exhaustion from a huge source file | DoS | Sources are single-file fetched from the repo; existing fetch/commit limits apply. No recursive resolution (unlike apidocs `$ref`). Low risk. |

## Sources

### Primary (HIGH confidence)
- Codebase: `internal/releasedocs/releasedocs.go` — `MultiGenerator`, `Artifact{Kind,Content,Filename}`, `FileFetcher`, `ArtifactKind` const block, `Generators` registry.
- Codebase: `internal/releasedocs/generators/apidocs/{apidocs.go,discover.go,render_markdown.go,apidocs_test.go,render_html.go}` — the generator analog: `Enabled` coercion, `GenerateMulti`, `(nil,nil)` skip, golden-file test wiring, fixed HTML wrapper.
- Codebase: `internal/releasedocs/generators/blog/blog.go` — `Enabled` default-coercion + nil-tolerant `Generate`.
- Codebase: `internal/releasedocs/publishers/pages/{pages.go,pages_apidocs_test.go}` — `Artifact.Filename` sub-path routing, `{dir}/releases/{toRef}/{filename}` path, prefix guard, `UpsertFile` idempotency proof.
- Codebase: `internal/releasedocs/defaults/defaults.go` — `DefaultGenerators()` wiring point.
- Codebase: `internal/releasedocs/template/template.go` — `text/template` + embedded preset + `Resolve` override pattern (and the no-FuncMap T-03-01 security note).
- Codebase: `internal/config/config.go:55-132` — `ReleaseDocs`/`ReleaseArtifacts`/`ArtifactConfig`/`APIDocsConfig` inline-embed pattern to mirror for `DiagramsConfig`.
- Codebase: `internal/releasedocs/dispatcher.go:104-125` — MultiGenerator-preferred loop; error vs skip semantics.
- Codebase: `.cadoo.yaml.example:86-135` — `releaseDocs.artifacts.apiDocs` block to mirror for `diagrams`.
- Codebase: `go.mod` — Go 1.26; no mermaid/diagram module dependency present.

### Secondary (MEDIUM confidence)
- GitHub community: native Mermaid rendering on github.com markdown/issues/PRs/release bodies; Jekyll Pages does NOT whitelist Mermaid → bare fences don't render on a served Pages site. (`github/pages-gem#835`, `community/discussions/65040`, `community/discussions/13761`).
- mermaid-js docs: client-side `<pre class="mermaid">` + `mermaid.initialize({startOnLoad:true})` is the static-HTML render path (browser-time, not build-time).
- mermaid-js syntax: first-line keywords `sequenceDiagram` / `classDiagram` / `stateDiagram`(`-v2`) / `flowchart`|`graph` / `erDiagram`.

### Tertiary (LOW confidence)
- "No robust pure-Go Mermaid parser exists" — A1, training-knowledge assumption; not material because the sniff is the intended approach.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all stdlib + existing in-repo packages, directly verified in source.
- Architecture: HIGH — a mechanical mirror of the verified apidocs `MultiGenerator` pattern; every reused capability inspected in code and tests.
- Pitfalls: HIGH — derived from the actual pages publisher guard, dispatcher error semantics, and Go map-ordering behavior.
- Pages-rendering (D-05): MEDIUM-HIGH — github.com native rendering and Jekyll non-rendering are well-documented; the recommendation (emit bare `.md` fence, defer Jekyll-site rendering) is conservative and keeps D-05 intact.

**Research date:** 2026-06-13
**Valid until:** 2026-07-13 (stable — stdlib + internal patterns; the Mermaid/GitHub-Pages facts are slow-moving).
