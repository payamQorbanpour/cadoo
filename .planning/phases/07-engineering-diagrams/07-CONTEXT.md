# Phase 7: Release-Docs Engineering Diagrams - Context

**Gathered:** 2026-06-13
**Status:** Ready for planning

<domain>
## Phase Boundary

Add a `diagrams` generator to the release-docs subsystem (`internal/releasedocs/generators/diagrams`) that, at release time, locates **committed** Mermaid diagram source files in the repo (paths the user lists in `.cadoo.yaml`, grouped by diagram type), wraps each in a markdown page with a `mermaid` fence, and publishes them to **pages** at deterministic, idempotent paths — reusing the Phase 2 pages publisher. The user chooses which diagram **types** (sequence, dependency, state, flowchart, class) by populating the corresponding config keys. A listed source that is missing or not Mermaid is skipped with a logged reason; the rest of the release-docs run is unaffected.

This is the direct parallel of Phase 3's `apidocs` generator (render a *committed* artifact, don't derive it), applied to engineering diagrams.

**In scope:** per-type config of explicit Mermaid source paths; single-file fetch of each source at the release tag; markdown page rendering (mermaid fence wrapper); graceful per-source skip; pages publishing of one page per diagram; deterministic, LLM-free output.

**Out of scope (explicitly NOT this phase — see Deferred Ideas):** deriving diagrams *from source code* (static AST/import-graph/struct analysis); LLM-generated diagrams; rendering Mermaid/PlantUML/Graphviz to SVG/PNG via an external tool; a VCS directory/tree-listing capability; convention-directory auto-discovery; embedding diagrams in the release body or changelog PR.

</domain>

<decisions>
## Implementation Decisions

### Derivation Strategy
- **D-01:** The diagrams generator **renders committed diagram sources** — it does NOT derive diagrams from source code and does NOT use the LLM. "From the repository" is satisfied by reading diagram source files that already live in the repo. This is deterministic, language-agnostic, requires no LLM, and fits the existing single-file `FileFetcher.FetchFileFromRef(repo, ref, path)` capability with zero new VCS plumbing. Directly mirrors api-docs **D-01** (render the committed spec, don't derive from code).
- **D-02:** Source format for v1 is **Mermaid only**. A listed source that is not Mermaid (e.g. `.puml`, `.dot`) is graceful-skipped with a logged reason (see D-08). Mermaid is the only format GitHub/GitLab render natively from a markdown fence, which is what makes the "no rendering runtime" decision (D-05) possible.

### Discovery / Selection ("choosable by user")
- **D-03:** Diagram sources are discovered via **explicit paths listed in config**, grouped by type — NOT by directory scanning. Each path is fetched individually via `FileFetcher.FetchFileFromRef` at the release tag (`rc.ToRef`), consistent with "config from the tag tree, never main." This deliberately avoids adding a VCS tree-listing capability (the plumbing Phase 3 also avoided). Fully explicit = fully "choosable."
- **D-04:** The set of diagram **types** is fixed: `sequence`, `dependency`, `state`, `flowchart`, `class`. The user chooses which to produce by populating that type's path list in config; an empty/absent type key produces nothing for that type.

### Renderer / Output Format
- **D-05:** Each diagram is published as a **markdown page that wraps the source in a ` ```mermaid ` fence** — NO rendering binary is invoked (honors the PROJECT.md "no new external runtimes" constraint). Output is fully deterministic and byte-stable given the same source + wrapper (golden-file testable). The github.com-renders-mermaid vs GitHub-Pages/Jekyll-may-not nuance is a **research item** (does NOT change this decision).
- **D-06:** Generation is **fully deterministic and LLM-free** (a step beyond "nil-tolerant," matching api-docs). No LLM call on any path; `rc.LLM == nil` changes nothing.

### Config / Toggle
- **D-07:** A **single `diagrams` family block** under `releaseDocs.artifacts` with one `enabled` + one `when:` gating all diagram output as a family — mirroring api-docs **D-07** (one `Enabled(cfg, bump)` gate for the whole generator). The block embeds `ArtifactConfig` (inline) and adds per-type path lists. Illustrative shape:
  ```yaml
  diagrams:
    enabled: false        # family master switch (default false, opt-in like apiDocs)
    when: always          # default "always" (lean) — see Claude's Discretion
    sequence:   [docs/diagrams/login.mmd]
    dependency: [docs/diagrams/packages.mmd]
    state:      []
    flowchart:  [docs/diagrams/release-flow.mmd]
    class:      [docs/diagrams/domain.mmd]
  ```

### Graceful Degradation (DIAG-04)
- **D-08:** **Skip-with-logged-reason, per source file** (not all-or-nothing across the family — each listed file is independent). A source that is missing at its path, not valid Mermaid, or otherwise unreadable is skipped with a clear `slog` reason (e.g. `diagrams: docs/diagrams/login.mmd not found at <tag>, skipping`); other diagrams and all sibling artifacts (changelog/release-notes/blog/api-docs) still complete. On a generator-level skip condition, `GenerateMulti` returns `(nil, nil)` — never a non-nil error — consistent with api-docs **D-10**.

### Publishing
- **D-09:** **Pages only**, **one page per diagram**, at deterministic idempotent paths via `Artifact.Filename` and the existing pages publisher — e.g. `releases/<tag>/diagrams/<type>/<name>.md`. No release-body or changelog-PR delivery in this phase. Pages remains opt-in via `publish.pages` like every other artifact.

### Generator Shape
- **D-10:** Introduce `KindDiagrams` and implement the **`MultiGenerator`** interface (one `GenerateMulti` pass emits one `Artifact` per resolved source file, each with its own `Filename`) — exactly the api-docs pattern. Register in `internal/releasedocs/defaults/defaults.go` `DefaultGenerators()`.

### Claude's Discretion
- The `when:` default — lean **`always`** (republish each release so a tag-pinned snapshot always exists), matching api-docs D-08; confirm in planning.
- How "valid Mermaid" is checked before publishing: lean toward a **lightweight first-line keyword sniff** (`sequenceDiagram` / `classDiagram` / `stateDiagram` / `flowchart`|`graph` / etc.) rather than a full Mermaid parse (no robust Go Mermaid parser is assumed). Confirm in research.
- Whether to verify the file under a given type key actually contains that diagram kind (type↔keyword consistency warn/skip), or publish whatever the file contains.
- Whether the markdown page wrapper supports `preset` / `template` override layering (the embedded `ArtifactConfig` exposes `Preset`/`Template`) or uses a single fixed wrapper.
- Exact published path scheme, page filename derivation from the source path, and golden-file fixtures.
- Concrete `MultiGenerator` vs per-`Kind` mapping details and the `DiagramsConfig` struct field names/types.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` — Phase 7 section: goal + 5 success criteria (per-type choosable diagrams, deterministic idempotent pages publishing, graceful per-type skip, LLM-off reproducibility, dogfood).
- `.planning/REQUIREMENTS.md` — Milestone v1.1 section: `DIAG-01` (enable + choose types via `.cadoo.yaml`), `DIAG-02` (derive a diagram per selected type), `DIAG-03` (deterministic idempotent pages paths), `DIAG-04` (per-type graceful skip), `DIAG-05` (deterministic-first, LLM-off reproducible).

### Pattern precedent (read first — this phase is its parallel)
- `.planning/phases/03-api-docs-openapi/03-CONTEXT.md` — the api-docs context. D-01 (render committed, don't derive), D-02 (single-file fetch + no tree listing), D-07 (single family gate), D-10 (skip-with-logged-reason, `(nil,nil)`), and the `Artifact.Filename` / pages cross-cutting constraint are all directly reused here.

### Code analogs to mirror (release-docs subsystem)
- `internal/releasedocs/releasedocs.go` — `Generator` + **`MultiGenerator`** interfaces, `Artifact{Kind,Content,Filename}`, `ReleaseContext`, `FileFetcher` (single-file source access), `ArtifactKind`/`PublishTarget` constants (add `KindDiagrams`).
- `internal/releasedocs/generators/apidocs/apidocs.go` (+ `discover.go`, `render_markdown.go`, `apidocs_test.go`) — the closest generator analog: `MultiGenerator`, `Enabled(cfg,bump)`, graceful skip, deterministic rendering, golden-file tests.
- `internal/releasedocs/generators/blog/blog.go` — `Enabled` default-coercion + nil-tolerant `Generate` reference.
- `internal/releasedocs/template/` — deterministic Go `text/template` + preset pattern for the markdown page wrapper (if preset/override is supported).
- `internal/releasedocs/publishers/pages/pages.go` — the pages publisher this phase publishes through; honors `Artifact.Filename` for non-`.md`/sub-path artifacts.
- `internal/releasedocs/defaults/defaults.go` — `DefaultGenerators()` wiring point (register the diagrams generator).
- `internal/config/config.go` — `ReleaseDocs` / `ReleaseArtifacts` / `ArtifactConfig` structs to extend with the `diagrams` block (mirror the `apiDocs` `APIDocsConfig` inline-embed pattern).
- `.cadoo.yaml.example` — `releaseDocs.artifacts` block to document the new `diagrams` keys.

### External (confirm in research)
- Mermaid native-rendering surfaces: github.com markdown viewer / release bodies render `mermaid` fences; GitHub Pages (Jekyll) and GitLab Pages rendering behavior — confirm what "published to pages" actually renders, and whether a client-side mermaid include is needed for a Pages-served site (D-05 nuance).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `FileFetcher.FetchFileFromRef(repo, ref, path)` — exactly the capability to fetch each committed Mermaid source at the release tag; type-assert off `rc.Provider`, degrade gracefully if absent.
- `MultiGenerator` interface + `Artifact.Filename` — already exist (added in Phase 3 for api-docs); the diagrams generator emits N artifacts with per-file names/sub-paths with no model changes.
- Pages publisher (Phase 2/3) — already writes per-artifact files idempotently and honors `Artifact.Filename`; diagrams publish through it unchanged.
- `internal/releasedocs/template` — deterministic Go-template engine + preset pattern, available for the page wrapper.

### Established Patterns
- Render committed artifacts, don't derive from code (api-docs D-01) — diagrams follow it exactly.
- Single-file fetch + explicit config paths, **no VCS tree listing** (api-docs D-02) — diagrams keep this constraint.
- Deterministic-first, fully LLM-free for this generator (api-docs went fully deterministic).
- Per-family `enabled` + `when:` gate via `Enabled(cfg, bump)` (Phase 1/3).
- Graceful skip = log reason + `(nil, nil)`, never fail siblings (api-docs D-10 → DIAG-04).

### Integration Points
- `internal/config/config.go`: add the `diagrams` block (embed `ArtifactConfig`, add per-type path-list fields) to `ReleaseArtifacts`.
- `internal/releasedocs/defaults/defaults.go`: register the diagrams generator in `DefaultGenerators()`.
- `internal/releasedocs/generators/diagrams/`: new package (generator + tests + any preset wrapper template).

</code_context>

<specifics>
## Specific Ideas

- Config is the api-docs `apiDocs` block's sibling: a single `diagrams` family block with `enabled` + `when:` plus five per-type path lists (`sequence`, `dependency`, `state`, `flowchart`, `class`).
- Illustrative published paths (one page per diagram): `releases/<tag>/diagrams/sequence/login.md`, `releases/<tag>/diagrams/class/domain.md`.
- Each published page is a markdown file whose body is the source wrapped in a ` ```mermaid … ``` ` fence (GitHub renders it inline).
- Dogfood target (SC-5): commit a couple of Mermaid sources in Cadoo's own repo and produce diagram pages end-to-end.

</specifics>

<deferred>
## Deferred Ideas

- **Code-derived diagrams** (static analysis: Go `go/packages` import/dependency graph, struct/interface class diagrams, call-graph sequence diagrams). Deterministic but language-specific and large; the "derive from source" path Phase 3 also deferred. Revisit as a future phase if "render committed sources" proves insufficient.
- **LLM-generated diagrams** from code or the release diff. Conflicts with the deterministic / LLM-off-reproducible house rule; a future, clearly-separated opt-in.
- **Rendering to SVG/PNG via an external tool** (mermaid-cli/Node, Graphviz `dot`, PlantUML/Java) — supports non-Mermaid sources and Jekyll-served Pages, but adds a runtime dependency (against the PROJECT.md constraint). Revisit if Mermaid-as-text rendering proves inadequate on target hosts.
- **PlantUML / Graphviz source formats** (`.puml`, `.dot`) — gated on the rendering-tool decision above; v1 is Mermaid-only.
- **Convention-directory auto-discovery** (e.g. scan `docs/diagrams/**`) — needs a new VCS tree-listing capability; explicitly avoided in v1.
- **Embedding diagrams in the release body / changelog PR** (Mermaid renders inline on GitHub release pages) — pages-only in v1; release-body delivery is a later enhancement.
- **Cadoo-provided starter/example diagram templates** for users to copy — a docs/onboarding nicety, not this phase.

</deferred>

---

*Phase: 07-engineering-diagrams*
*Context gathered: 2026-06-13*
