# Phase 3: API Docs / OpenAPI - Context

**Gathered:** 2026-06-05
**Status:** Ready for planning

<domain>
## Phase Boundary

Add an `apidocs` generator to the release-docs subsystem (`internal/releasedocs/generators/apidocs`) that, at release time, locates the repo's **committed** OpenAPI/Swagger spec, validates it, and produces three artifacts — the spec itself, a self-contained HTML API reference, and a deterministic Markdown reference — then publishes all three to pages at deterministic, idempotent paths (reusing the Phase 2 pages publisher). Repos with no valid spec degrade gracefully: apidocs is skipped with a logged reason and the rest of the release-docs run is unaffected.

**In scope:** spec discovery from the repo tree, OpenAPI/Swagger validation, HTML + Markdown rendering, config toggle, graceful skip, pages publishing of the three artifacts.

**Out of scope (explicitly NOT this phase):** deriving an OpenAPI spec *from source code* (annotation scraping, route-AST analysis) — see Deferred Ideas; cloning/checking out repos; adding a VCS tree-listing capability beyond what single-file fetch + a fixed fallback path list requires.

</domain>

<decisions>
## Implementation Decisions

### Extraction Strategy
- **D-01:** The apidocs generator **renders a committed spec** — it does NOT derive OpenAPI from source code. "From the code" is satisfied by reading the spec file that lives in the repo. This is deterministic, framework-agnostic, requires no LLM, and fits the existing single-file `FileFetcher.FetchFileFromRef(repo, ref, path)` capability with zero new VCS plumbing. (Resolves the STATE.md open item: "exact OpenAPI extraction strategy and initial supported language/framework.")
- **D-02:** Spec discovery: if `apiDocs.specPath` is set in config, fetch **exactly** that path (no fallback). If `specPath` is empty (default), try a fixed ordered fallback list and use the first that exists: `openapi.yaml` → `openapi.yml` → `openapi.json` → `docs/openapi.yaml` → `api/openapi.yaml`. The spec is fetched at the release tag (`rc.ToRef`), consistent with "config from the tag tree, never main."
- **D-03:** "Supported framework set" reduces to "supported spec versions" — since we render a committed spec, the framework that produced it is irrelevant. The narrow supported set is defined by spec version (D-09), not by web framework.

### Renderer / Artifacts
- **D-04:** apidocs emits **three** artifacts per run: (1) the OpenAPI spec, (2) a self-contained **HTML** API reference rendered with **Redoc**, (3) a deterministic **Markdown** API reference.
- **D-05:** The HTML must be viewable **offline** (no runtime CDN `<script>`). The Redoc bundle is vendored/embedded into the binary (e.g. `go:embed`) and inlined into the emitted HTML, so it works for no-egress / air-gapped customers (Helm no-egress mode). The HTML must be deterministic — byte-identical given the same spec + vendored bundle (golden-file testable).
- **D-06:** The Markdown reference is built with Go `text/template` over the parsed spec (endpoints grouped by tag/path, parameters, request/response schemas), mirroring the existing deterministic-template pattern in `internal/releasedocs/template` and the changelog/releasenotes generators. No LLM, no network.

### Config / Toggle
- **D-07:** Single `apiDocs` config block with one `enabled` + one `when:` that gate all three outputs as a family (not three separate toggles). `enabled: false` skips all three. One `Enabled(cfg, bump)` gate for the whole generator, consistent with how `blog`/`changelog` gate a single family.
- **D-08:** Default `when: always` — API docs regenerate on every release (patch included), because the API surface can change in any release. (Contrast with `blog`'s `minor_or_above` default.) `specPath` defaults to `""` (→ conventional fallbacks per D-02).

### Detection / Graceful Degradation (SC-3)
- **D-09:** Supported spec versions: **Swagger 2.0 + OpenAPI 3.0.x + OpenAPI 3.1.x**. This favors a validator spanning all three (e.g. `pb33f/libopenapi`) over a 3.x-only library — to be confirmed in research.
- **D-10:** **Skip-with-logged-reason on any failure**, all-or-nothing for the apidocs family: no spec found at the resolved path(s), parse failure (malformed YAML/JSON), OpenAPI schema validation failure, OR unsupported version → skip the entire apidocs artifact, log a clear reason (e.g. `apidocs: openapi.yaml failed validation: <err>`), and **continue the rest of the release-docs run** (sibling artifacts like changelog/blog must never fail because apidocs skipped). Never publish invalid/garbage docs.

### Claude's Discretion
- Whether the published `openapi.yaml` artifact is the **raw fetched bytes** (most faithful + deterministic) or a re-serialized/normalized spec. Lean toward raw passthrough for the spec artifact; use the validator/parser only for validation + Markdown extraction. Confirm in research.
- How the three outputs map onto the `Artifact` / `ArtifactKind` model (one Kind emitting multiple files, vs. multiple Kinds) — see the cross-cutting constraint below; this is a planning/HOW decision.
- Exact Markdown layout, Redoc bundle version, and golden-file fixtures.
- Whether to introduce a new `KindOpenAPI` / `KindAPIDocs` constant set vs. reuse a family kind.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` — Phase 3 section: goal + 3 success criteria (supported-framework derivation, deterministic idempotent pages publishing, graceful degradation for unsupported repos).
- `.planning/REQUIREMENTS.md` — `REQ-release-artifact-generation` (api-docs + openapi delivery), `REQ-per-artifact-toggles` (enabled + `when:` per artifact), `REQ-configurable-templates` (preset + override template layering), `REQ-publish-destinations` (pages).

### Code analogs to mirror (release-docs subsystem)
- `internal/releasedocs/releasedocs.go` — `Generator` interface (`Kind`/`Enabled`/`Generate`), `Artifact`, `ReleaseContext`, `FileFetcher` (the single-file source-access capability this phase depends on), `ArtifactKind`/`PublishTarget` constants.
- `internal/releasedocs/generators/blog/blog.go` — closest generator analog: `Enabled` default-coercion, nil-tolerant `Generate`, deterministic skeleton.
- `internal/releasedocs/generators/releasenotes/releasenotes.go` — generator using the `text/template` rendering path.
- `internal/releasedocs/template/` — deterministic preset-template pattern to mirror for the Markdown reference (and any preset/override layering per `REQ-configurable-templates`).
- `internal/releasedocs/publishers/pages/pages.go` — the pages publisher this phase publishes through; **note the hardcoded `.md` extension** (cross-cutting constraint below).
- `internal/releasedocs/defaults/defaults.go` — `DefaultGenerators()` / `DefaultPublishers()` wiring point (add apidocs here).
- `internal/config/config.go` — `ReleaseDocs` / `ReleaseArtifacts` config structs to extend with the `apiDocs` block.

### External (confirm in research)
- `pb33f/libopenapi` (Go) — candidate validator/parser spanning Swagger 2.0 + OpenAPI 3.0/3.1 (D-09). Research to confirm vs `getkin/kin-openapi` (3.x-only).
- Redoc standalone bundle — candidate HTML renderer to vendor/embed (D-05). Confirm license (MIT) and a pinned, vendorable build.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `FileFetcher.FetchFileFromRef(repo, ref, path)` — exactly the capability needed to fetch the committed spec at the release tag; type-assert it off `rc.Provider`, degrade gracefully if absent.
- Pages publisher (Phase 2) — already writes per-artifact files to `{dir}/releases/{toRef}/{kind}.md` idempotently; apidocs publishes through it.
- `internal/releasedocs/template` — deterministic Go-template engine + preset pattern for the Markdown reference.
- Generator family pattern (`Kind`/`Enabled`/`Generate`, nil-tolerant) — apidocs is a new member.

### Established Patterns
- Deterministic-first, LLM nil-tolerant (Phase 1): apidocs is **fully deterministic** — no LLM dependency at all (a step beyond "nil-tolerant").
- Optional capability interfaces, type-asserted with graceful degradation (Phase 1): apidocs degrades when `FileFetcher` is absent OR when no valid spec exists.
- Per-artifact `enabled` + `when:` gating via `Enabled(cfg, bump)` (Phase 1).

### Integration Points
- `internal/config/config.go`: add `apiDocs` block (`enabled`, `when`, `specPath`) to the release artifacts config.
- `internal/releasedocs/defaults/defaults.go`: register the apidocs generator in `DefaultGenerators()`.
- Pages publisher: see cross-cutting constraint.

### Cross-cutting constraint (MUST handle in planning)
- The Phase 2 pages publisher computes its path as `string(art.Kind)+".md"` — a **hardcoded `.md` extension**. apidocs emits `.yaml` (spec) and `.html` (Redoc) in addition to `.md`. The plan MUST extend the artifact model and/or the pages publisher so each artifact carries its own filename/extension, **without breaking** the existing changelog/releasenotes/blog `.md` outputs (idempotency + Phase 1/2 golden tests must still pass).

</code_context>

<specifics>
## Specific Ideas

- Three concrete published paths per release (illustrative): `releases/<tag>/openapi.yaml`, `releases/<tag>/api-reference.html`, `releases/<tag>/api-reference.md`.
- HTML renderer is specifically **Redoc**, vendored (not Swagger-UI, not a CDN script).
- Validator spans Swagger 2.0 + OpenAPI 3.0 + 3.1 (broad legacy coverage was explicitly chosen over a narrower 3.x-only surface).

</specifics>

<deferred>
## Deferred Ideas

- **Derive OpenAPI from source code** (swaggo-style annotation scraping; chi/gin/echo route-AST analysis). Explicitly out of scope for Phase 3 — would require a new VCS tree-listing/clone capability and per-framework analyzers. Revisit as a future phase if "spec-passthrough" proves insufficient for target users.
- **LLM-assisted spec derivation / enrichment** (e.g. LLM-authored endpoint descriptions). Not in this deterministic-only phase.
- **Swagger-UI as an alternative renderer** / interactive "try it" console. Redoc static reference is the Phase 3 choice; an interactive console could be a later enhancement.

</deferred>

---

*Phase: 03-api-docs-openapi*
*Context gathered: 2026-06-05*
