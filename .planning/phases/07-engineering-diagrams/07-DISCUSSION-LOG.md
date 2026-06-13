# Phase 7: Release-Docs Engineering Diagrams - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-13
**Phase:** 07-engineering-diagrams
**Areas discussed:** Diagram source / derivation, Discovery, Rendering format, Config + publish shape

---

## Diagram source / derivation

| Option | Description | Selected |
|--------|-------------|----------|
| Render committed sources | User commits diagram source files; Cadoo discovers + publishes them. Deterministic, language-agnostic, zero code-analysis — the api-docs D-01 pattern. | ✓ |
| Auto-derive from code (static analysis) | Cadoo analyzes code to generate diagrams (import graph, struct class diagrams). Deterministic but language-specific & large; sequence/state/flowchart hard. Phase 3 deferred this. | |
| LLM-generate from code/diff | LLM emits Mermaid/PlantUML per type. Flexible/language-agnostic but non-deterministic — conflicts with DIAG-05. | |

**User's choice:** Render committed sources
**Notes:** Keeps full consistency with api-docs D-01 (render the committed artifact, don't derive). "Choosable types" = which committed sources to publish.

---

## Discovery

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit paths in config | User lists diagram source files in `.cadoo.yaml` grouped by type; each fetched via existing single-file `FileFetcher` — no new VCS plumbing (api-docs D-02). | ✓ |
| Convention dir + tree listing | Drop files in `docs/diagrams/**`, auto-discover all. More ergonomic but REQUIRES a new VCS tree-listing capability Phase 3 avoided. | |
| Fixed fallback list per type | Try a fixed set of conventional paths per type, publish first that exists. No config but rigid (one fixed-name file per type). | |

**User's choice:** Explicit paths in config
**Notes:** Avoids adding VCS directory listing; fully explicit selection. Dissolves the "what each diagram depicts" scope question — the user authors exactly what each file shows.

---

## Rendering format

| Option | Description | Selected |
|--------|-------------|----------|
| Mermaid-as-text, no rendering | Publish each source as a markdown page with a `mermaid` fence; GitHub/GitLab render natively. No rendering binary, deterministic. Non-Mermaid sources graceful-skip. | ✓ |
| Render to SVG/PNG via a tool | mermaid-cli/Graphviz/PlantUML produce static images. Any format, any host — but new runtime dependency (against PROJECT.md constraint). | |
| Raw passthrough (any format) | Copy source files verbatim, no fences, no rendering. Trivial but not visually rendered anywhere. | |

**User's choice:** Mermaid-as-text, no rendering
**Notes:** Honors "no new external runtimes"; fully deterministic / golden-file testable. Pages/Jekyll-vs-github.com rendering nuance flagged as a research item.

---

## Config + publish shape

| Option | Description | Selected |
|--------|-------------|----------|
| Pages only (per-file) | One page per diagram at deterministic paths (`releases/<tag>/diagrams/<type>/<name>.md`) via the existing pages publisher. Mirrors api-docs per-artifact files. | ✓ |
| Pages + embed in release body | Also splice Mermaid into the release body (renders inline on GitHub). Better discoverability, risk of release-body bloat. | |
| Pages, single combined page | One combined `diagrams.md` per release. Simpler index, less granular. | |

**User's choice:** Pages only (per-file)
**Notes:** Config locked as a single `diagrams` family block (one `enabled` + `when:`, api-docs D-07 parallel) with per-type path lists; populated keys = the choosable types.

---

## Claude's Discretion

- `when:` default for diagrams (lean `always`, like api-docs D-08).
- Mermaid validity check approach (lightweight first-line keyword sniff vs full parse).
- Type↔keyword consistency checking (warn/skip on mismatch vs publish as-is).
- Page-wrapper template (preset/override layering vs single fixed wrapper).
- Exact published path scheme, page filename derivation, golden-file fixtures.
- `MultiGenerator` vs per-`Kind` mapping and `DiagramsConfig` struct details.

## Deferred Ideas

- Code-derived diagrams (static analysis).
- LLM-generated diagrams.
- Rendering to SVG/PNG via an external tool.
- PlantUML / Graphviz source formats (`.puml`, `.dot`).
- Convention-directory auto-discovery (needs VCS tree listing).
- Embedding diagrams in the release body / changelog PR.
- Cadoo-provided starter/example diagram templates.
