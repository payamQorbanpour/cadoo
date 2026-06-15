# Phase 3: API Docs / OpenAPI - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-05
**Phase:** 03-api-docs-openapi
**Areas discussed:** Extraction strategy, Supported framework set, Renderer for API reference, Detection + graceful degradation

---

## Extraction Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Render a committed spec | Fetch the repo's own openapi.yaml/json, validate, render. Deterministic, framework-agnostic, fits single-file FileFetcher, no LLM. | ✓ |
| Derive from source annotations | Parse swaggo-style comments from Go source; needs tree-listing/clone + annotation convention. | |
| Route-AST analysis | Statically analyze chi/gin route registrations; most framework-specific and fragile. | |

**User's choice:** Render a committed spec
**Notes:** Resolves the STATE.md open item on extraction strategy. "From the code" = from the spec file committed in the repo. Reframes "supported framework set" into "supported spec versions."

### Spec path resolution (follow-up)

| Option | Description | Selected |
|--------|-------------|----------|
| Configured path + conventional fallbacks | Exact specPath if set; else try openapi.yaml/.yml/.json, docs/, api/; first wins. | ✓ |
| Single configured path only | Only specPath (default openapi.yaml), no fallback. | |

**User's choice:** Configured path + conventional fallbacks
**Notes:** Zero-config for common layouts, overridable. None found → skip with reason.

---

## Renderer for API Reference

| Option | Description | Selected |
|--------|-------------|----------|
| Self-contained Redoc HTML | One static HTML, Redoc JS vendored (offline, no CDN). | |
| Deterministic Markdown reference | Go-template Markdown from the spec; tiny, diff-friendly. | |
| Both HTML + Markdown | Emit vendored-Redoc HTML AND Markdown as separate artifacts. | ✓ |

**User's choice:** Both HTML + Markdown
**Notes:** Three published artifacts per release: openapi.yaml + api-reference.html (vendored Redoc, offline) + api-reference.md. Surfaces the pages-publisher hardcoded-`.md`-extension constraint.

### Config toggle / when: (follow-up)

| Option | Description | Selected |
|--------|-------------|----------|
| One toggle, default 'always' | Single apiDocs block gating all 3 outputs; when: always (regenerate every release). | ✓ |
| One toggle, default 'minor_or_above' | Same shape, default minor/major only (blog-style). | |

**User's choice:** One toggle, default 'always'
**Notes:** API surface can change in any release, so default to every release. specPath default "" → fallbacks.

---

## Detection + Graceful Degradation

| Option | Description | Selected |
|--------|-------------|----------|
| Skip with logged reason | Any failure (no spec / parse / validate / unsupported version) → skip whole apidocs family, log reason, continue run. | ✓ |
| Publish spec as-is, skip rendering | Publish raw spec even if validation fails; skip only HTML/MD. | |

**User's choice:** Skip with logged reason
**Notes:** All-or-nothing per apidocs family. Never publishes invalid docs; never fails sibling artifacts (SC-3).

### Supported spec versions (follow-up)

| Option | Description | Selected |
|--------|-------------|----------|
| OpenAPI 3.0 + 3.1 | Modern 3.x only; kin-openapi validator. | |
| OpenAPI 3.0 + 3.1 + Swagger 2.0 | Spans 2.0 + 3.x; pb33f/libopenapi validator; broader legacy coverage. | ✓ |
| OpenAPI 3.0 only | Narrowest; 3.1 + 2.0 skip. | |

**User's choice:** OpenAPI 3.0 + 3.1 + Swagger 2.0
**Notes:** Broad legacy coverage chosen over a narrower validation surface; favors a 2.0+3.x-spanning library (confirm in research).

---

## Claude's Discretion

- Raw-passthrough vs re-serialized `openapi.yaml` artifact (lean raw).
- Artifact/ArtifactKind mapping for 3 outputs (one Kind multi-file vs multiple Kinds).
- Markdown layout, Redoc bundle version, golden-file fixtures.

## Deferred Ideas

- Derive OpenAPI from source code (annotations / route-AST) — future phase.
- LLM-assisted spec derivation/enrichment.
- Swagger-UI / interactive "try it" console as an alternative renderer.
