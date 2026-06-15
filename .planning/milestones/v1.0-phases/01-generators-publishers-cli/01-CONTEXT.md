# Phase 1: Generators + Publishers + CLI - Context

**Gathered:** 2026-06-04
**Status:** Ready for planning
**Source:** Approved SPEC (`docs/superpowers/specs/2026-06-04-release-docs-design.md`) — used in place of discuss-phase per user direction.

<domain>
## Phase Boundary

**This phase delivers** the stateless, CLI-driven core of the Release Docs subsystem: a maintainer
runs `cadoo release-docs --repo … --from vX --to vY` and Cadoo generates a grouped changelog section
and polished release notes for that range, then publishes them to the release body (inside Cadoo
markers) and to a single `CHANGELOG.md` PR — idempotently, with no database, dogfooded on Cadoo's
own repo.

**In scope (Phase 1):**
- `internal/releasedocs` core types & interfaces: `ReleaseJob`, `ReleaseContext`, `Artifact`,
  `Generator`, `Publisher`, `SemverBump`, `ArtifactKind`, `PublishTarget`.
- `Dispatcher.Run(ctx, ReleaseJob)` single entry point + `registry.go` wiring.
- `context.go` — `ReleaseContext` builder (range → commits + merged PRs, semver bump computation).
- Grouped change model (built once) with `conventional` and `labels` grouping sources.
- Generators: **changelog** (deterministic-first, LLM-optional polish) and **release-notes**
  (LLM-authored narrative on a deterministic highlight skeleton, `tone`-aware).
- Publishers: **releasebody** (marker-wrapped upsert) and **changelogpr** (single PR, marker-keyed).
- `config.ReleaseDocs` in `internal/config`, loaded from the **release tag's tree** (`ToRef`), not `main`.
- Embedded preset templates + repo override template loading (`internal/releasedocs/template`).
- Optional VCS capability interfaces (`ReleaseRangeReader`, `ReleasePublisher`, `BranchCommitter`)
  type-asserted by the dispatcher, implemented for GitHub (go-github v66) and GitLab (go-gitlab),
  with graceful degradation when a capability is absent.
- `cadoo release-docs` CLI command (stateless; marker-based idempotency reconstruction).
- Dogfood end-to-end on Cadoo's own repository.

**Explicitly OUT of scope (deferred to later phases):**
- Webhook / worker auto-trigger, `ReleaseJob` enqueue (River + memory) — **Phase 2**.
- DB state table + `NNNN_release_docs.sql` migration — **Phase 2** (Phase 1 is stateless/marker-only).
- `pages` publisher, `blog` generator — **Phase 2**.
- `llm` grouping source — deferred decision (see Deferred Ideas); Phase 1 ships `conventional` + `labels`.
- API docs / OpenAPI generator — **Phase 3**.

</domain>

<decisions>
## Implementation Decisions

### Subsystem shape
- **D-01**: Build `internal/releasedocs` as a dedicated subsystem **parallel** to
  `internal/orchestrator`, NOT on top of it. Do NOT extend `tools.Tool` / `tools.Input` /
  `tools.Result` (PR-diff/inline-comment shaped — wrong fit). Mirror the review pipeline's
  architecture (provider pool, marker idempotency, LLM gateway reuse), not its types.
  (SPEC §2, CONS-releasedocs-subsystem-shape, DEC-parallel-not-extend-review)
- **D-02**: Normative package layout — `releasedocs.go` (types/interfaces), `dispatcher.go`,
  `registry.go`, `context.go`, `generators/{changelog,releasenotes}/`,
  `publishers/{releasebody,changelogpr}/`, `template/`. (Phase-1 subset of SPEC §2 layout;
  `blog`, `apidocs`, `publishers/pages` are created in later phases.)

### Core types & interfaces
- **D-03**: Four core abstractions plus a dispatcher: `Generator` (pure: context → artifact,
  side-effect-free) and `Publisher` (owns all writes). Interface signatures per
  CONS-releasedocs-core-interfaces — `Generator.Kind()/Enabled(cfg,bump)/Generate(ctx,rc)`,
  `Publisher.Target()/Publish(ctx,rc,arts)`.
- **D-04**: `ReleaseContext` is the packed input built once and passed to every generator: repo,
  org, from/to tags, `Bump`, `[]vcs.Commit`, `[]vcs.MergedPR`, `config.ReleaseDocs`, `vcs.Provider`,
  and a **nil-tolerant** `llm.Provider`.

### Dispatcher flow
- **D-05**: `Dispatcher.Run(ctx, ReleaseJob)` flow: resolve provider from the reused orchestrator
  `VCSPool` → resolve `FromRef` (prior tag matching `tagPattern` before `ToRef`) when empty →
  load config from `ToRef`'s tree (no-op if `enabled:false`) → build `ReleaseContext` → run each
  `Enabled` generator (parallelizable) → route artifacts to configured publishers (each idempotent)
  → reconstruct/record published state. (CONS-releasedocs-dispatcher-flow)

### Config
- **D-06**: Add `config.ReleaseDocs` to `internal/config`, loaded from the **release tag's tree**
  (`ToRef`), consistent with the existing "config from head/tag, never main" rule.
  (DEC-config-from-tag-tree, CONS-releasedocs-config-schema)
- **D-07**: Two configurability layers — **presets** (`preset:`, `grouping.source`, `tone:`) for
  out-of-the-box behavior; **custom templates** (`template:` → Go `text/template` file loaded from
  the tag tree) overriding the preset entirely; defaults to embedded preset templates in
  `internal/releasedocs/template`. Templates receive the `ReleaseContext` + grouped change model.
  (REQ-configurable-templates)
- **D-08**: Per-artifact control is mandatory — every artifact has its own `enabled` plus a `when:`
  condition keyed off the computed semver bump. `Generator.Enabled(cfg, bump)` implements the gate;
  the dispatcher **never** runs a disabled generator. (REQ-per-artifact-toggles)

### Generators
- **D-09**: One shared **grouped change model**, built once from `ReleaseContext`, with grouping
  sources `conventional` (parse Conventional Commit prefixes `feat:`/`fix:`/`perf:`/`feat!:`/
  `BREAKING CHANGE`) and `labels` (group merged PRs by label via configurable label→section map).
  `llm` grouping is deferred (see Deferred Ideas). (CONS-releasedocs-generators)
- **D-10**: **changelog** generator is deterministic-first (parse → group → render); LLM is
  **optional polish only**, so a repo can run with LLM off and get reproducible, golden-file-testable
  output. Emits a `CHANGELOG.md` section (markdown). (DEC-deterministic-first-changelog)
- **D-11**: **release-notes** generator builds a deterministic highlight skeleton from the grouped
  model, then has the LLM write a `tone`-aware (`concise|detailed|marketing`) narrative + highlights.
  Output is release-body markdown.

### Publishers & idempotency (stateless / marker mode)
- **D-12**: **releasebody** publisher wraps Cadoo content in markers
  (`<!-- cadoo:release-notes:begin -->` … `:end`), preserving any user-written body outside the
  block, and upserts/updates the release body in place on re-run.
- **D-13**: **changelogpr** publisher opens or updates a **single** PR that prepends the new section
  to `CHANGELOG.md`, idempotent via a hidden marker keyed on `ToRef`
  (`<!-- cadoo:changelog:vX.Y.Z -->`), with a deterministic branch name `cadoo/changelog/vX.Y.Z`.
- **D-14**: Phase 1 is **stateless** — reconstruct published state by reading Cadoo's own markers
  back from the release body / open PRs (same philosophy as `PriorReviewReader` in CI-mode review).
  No DB, no state table, no migration in this phase. (REQ-release-docs-idempotency, Phase-1 delivery)

### VCS provider extension
- **D-15**: Keep `vcs.Provider` PR-centric. Add **optional capability interfaces**
  (`ReleaseRangeReader`, `ReleasePublisher`, `BranchCommitter`) **type-asserted by the dispatcher**
  (same pattern as `PriorReviewReader`) rather than bloating core `Provider`. A provider lacking a
  capability degrades gracefully — the dependent publisher/generator is skipped with a logged reason.
  Implement for GitHub (go-github v66) and GitLab (go-gitlab). (DEC-optional-vcs-capabilities)

### Trigger / entry point
- **D-16**: Phase 1 entry point is the **manual CLI** only: `cadoo release-docs --repo … --from vX
  --to vY` (plus a `--mr`-style form), stateless, running the same dispatcher with a memory provider
  pool. This is also the dogfooding path on Cadoo's own repo. No webhook/worker in this phase.
  (REQ-configurable-trigger, Phase-1 delivery)

### Non-functional (cross-cutting)
- **D-17**: Multi-tenant — `org_id` carried throughout; self-host and SaaS share one code path; no
  single-tenant shortcuts. No new LLM provider code paths in Go (routing stays in LiteLLM). `make ci`
  (vet + test + build) must stay green; exported symbols need docstrings (`exported` revive rule);
  `goimports` local-prefix grouping (`github.com/payamqorbanpour/cadoo`).
  (CONS-releasedocs-nonfunctional)

### Claude's Discretion
- Exact Go struct field tags, file splitting within each subpackage, and helper organization.
- Concrete preset template wording (subject to golden-file tests being deterministic).
- How the CLI flag set maps to `ReleaseJob` fields (e.g. `--pr-host`/`--repo`/`--from`/`--to`/`--mr`),
  provided it satisfies the success criteria and reuses existing CLI plumbing in `cmd/cadoo-cli`.
- Internal naming of the grouped change model types and the label→section default map contents.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Approved design (authoritative)
- `docs/superpowers/specs/2026-06-04-release-docs-design.md` — the full approved SPEC (architecture,
  types, config schema, generators, publishers, idempotency, VCS capabilities, phasing).
- `.planning/intel/constraints.md` — authoritative technical contract distilled from the SPEC
  (CONS-* entries; normative package layout, interfaces, config schema, idempotency, VCS capabilities).
- `.planning/intel/decisions.md` — SPEC-origin design decisions (DEC-*; proposed, not locked ADRs).
- `.planning/REQUIREMENTS.md` — the 6 Phase-1 REQ-IDs and their acceptance criteria.

### Existing patterns to reuse (ground the SPEC's references in real code)
- `internal/orchestrator/reviewer.go` — `Dispatcher.Run` shape, `VCSPool` usage, config-from-head loading.
- `internal/orchestrator/registry.go` — registry wiring pattern for built-ins (mirror for generators/publishers).
- `internal/orchestrator/consolidate.go` — canonical wrapper/marker format (`<!-- cadoo:… -->`); do not reinvent.
- `internal/vcs/vcs.go` — provider-agnostic interface + existing optional-capability pattern (`PriorReviewReader`).
- `internal/vcs/github/` and `internal/vcs/gitlab/` — adapter implementations to extend with new capabilities.
- `internal/config/` — `.cadoo.yaml` parsing + config-from-head loading to extend with `releaseDocs`.
- `internal/llm/litellm` — chat client (nil-tolerant usage for changelog LLM-off path).
- `cmd/cadoo-cli/main.go` — CI-mode/stateless CLI entry pattern + `PriorReviewReader`-style marker reconstruction.

</canonical_refs>

<specifics>
## Specific Ideas

- Config schema (`releaseDocs` block) is specified in SPEC §3 / CONS-releasedocs-config-schema —
  follow it exactly for Phase-1 keys (`enabled`, `trigger`, `tagPattern`, `artifacts.changelog`,
  `artifacts.releaseNotes`, `grouping`, `publish.releaseBody`, `publish.changelogPR`). Phase-2/3 keys
  (`blog`, `apiDocs`, `publish.pages`) may be present in the struct but their generators/publishers
  are not wired this phase.
- Idempotency markers: `<!-- cadoo:release-notes:begin -->`/`:end` (release body),
  `<!-- cadoo:changelog:vX.Y.Z -->` (changelog PR), branch `cadoo/changelog/vX.Y.Z`.
- Testing expectations (SPEC §9): unit tests for grouped change model (conventional/labels),
  each generator against fixture `ReleaseContext`s, template override loading, `Enabled`/`when:`
  matrix per bump; idempotency test (run dispatcher twice → release body edited not duplicated,
  single changelog PR) via stateless marker reconstruction; golden-file tests for preset changelog
  output with LLM off; provider-capability fakes + graceful-degradation tests.

</specifics>

<deferred>
## Deferred Ideas

- **`llm` grouping source** — SPEC §10 open item ("worth shipping in phase 1 or deferred"). Decision:
  defer; Phase 1 ships `conventional` + `labels` only. The config enum may still accept `llm` but it
  is not implemented this phase.
- **DB-backed state table + migration** — Phase 2 (Phase 1 is stateless/marker-only).
- **Webhook/worker auto-trigger, `pages` publisher, `blog` generator** — Phase 2.
- **API docs / OpenAPI** — Phase 3 (narrow framework set first).
- **Blog publish destinations beyond pages (e.g. dev.to)** — out of scope for the milestone.

</deferred>

---

*Phase: 01-generators-publishers-cli*
*Context gathered: 2026-06-04 from approved SPEC (in place of discuss-phase)*
