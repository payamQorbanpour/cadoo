# Requirements: Cadoo — Release Docs

**Defined:** 2026-06-04
**Core Value:** After a customer release, Cadoo auto-generates and publishes the configured release artifacts (changelog, release notes, blog, later API/OpenAPI docs) to the configured destinations — idempotently across re-runs, with per-artifact toggles honored.

> Source: `docs/superpowers/specs/2026-06-04-release-docs-design.md` (approved SPEC). These 6 requirements are SPEC-origin (no PRDs present); the SPEC's `.planning/intel/constraints.md` is the authoritative technical contract. Several requirements are delivered **incrementally** across phases — the Traceability table assigns each to its **first delivering phase**; later phases extend them (noted inline).

## v1 Requirements

Requirements for the Release Docs milestone. Each maps to roadmap phases.

### Release Docs

- [ ] **REQ-release-artifact-generation**: After a release, Cadoo generates release artifacts — changelog section, polished release notes, PR blog post, and (later) API docs + OpenAPI — from the commits/PRs in the release range.
  - Acceptance: changelog = grouped (Features/Fixes/Breaking/…) section appended to a rolling `CHANGELOG.md`; release notes = polished narrative for the Release body; blog = long-form announcement; API docs + OpenAPI derived from code (phase 3).
  - Delivery: changelog + release-notes in Phase 1; blog in Phase 2; api-docs/openapi in Phase 3.

- [ ] **REQ-per-artifact-toggles**: Per-artifact toggles and conditions — every artifact has its own `enabled` plus a `when:` condition keyed off the computed semver bump.
  - Acceptance: changelog can run every release while blog runs only on minor/major; `Generator.Enabled(cfg, bump)` gates execution; the dispatcher never runs a disabled generator.

- [ ] **REQ-configurable-templates**: Two configurability layers — presets out of the box, custom override template files for full control.
  - Acceptance: presets (`preset:`, `grouping.source`, `tone:`) work with no template authoring; any artifact may set `template:` (Go `text/template`, loaded from the tag tree) overriding the preset; templates receive the `ReleaseContext` plus the grouped change model; defaults to embedded preset templates in `internal/releasedocs/template`.

- [x] **REQ-release-docs-idempotency**: Idempotent across re-runs/resyncs (re-tagging, edited release) — edit-in-place, no duplicates.
  - Acceptance: running the dispatcher twice over the same range edits the release body (not duplicated), keeps a single changelog PR, and uses stable pages paths; works both DB-backed and via stateless marker reconstruction (CLI/CI mode).
  - Delivery: stateless/marker mode in Phase 1; DB-backed state table + migration in Phase 2.

- [ ] **REQ-configurable-trigger**: Trigger configurable per repo — published Release event by default, optionally tag push, plus a manual CLI/CI entry point.
  - Acceptance: default `release` (webhook on GitHub `release: published` / GitLab release → enqueues `ReleaseJob`); optional `tag` (`v*` push filtered by `tagPattern`); manual `cadoo release-docs --repo … --from vX --to vY` runs stateless via the same dispatcher; if `releaseDocs.trigger` excludes the event kind, the webhook no-ops early.
  - Delivery: manual CLI/CI entry point in Phase 1; release/tag webhook ingestion in Phase 2.

- [ ] **REQ-publish-destinations**: Publish artifacts to the Release body, to `CHANGELOG.md` via PR, and to a docs branch / GitHub Pages.
  - Acceptance: `releasebody` upserts/updates the Release body inside Cadoo markers (preserving user content); `changelogpr` opens/updates a single PR prepending the new `CHANGELOG.md` section; `pages` commits rendered artifacts to a configured branch/dir with deterministic paths.
  - Delivery: `releasebody` + `changelogpr` in Phase 1; `pages` in Phase 2.

## v2 Requirements

Deferred beyond this milestone. Tracked but not in the current roadmap.

(None defined yet — open items in `.planning/intel/context.md`, e.g. `llm` grouping in phase 1 vs deferred, will be resolved during phase planning.)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Extending `tools.Tool` / `tools.Input` / `tools.Result` | Those types are PR-diff/inline-comment shaped; Release Docs is a parallel subsystem |
| New LLM provider code paths in Go | Multi-provider routing stays in the LiteLLM sidecar |
| API docs for arbitrary languages | Phase 3 starts with a narrow, well-supported framework set |
| Blog publish destinations beyond pages (e.g. dev.to) | Out of scope for this milestone |
| Single-tenant shortcuts | `org_id` carried throughout; self-host is a degenerate single-org tenant |

## Traceability

Each requirement is assigned to its **first delivering phase**. Requirements marked "(extends)" gain additional artifacts/modes in later phases (see phase requirement lists in ROADMAP.md).

| Requirement | Phase | Status |
|-------------|-------|--------|
| REQ-release-artifact-generation | Phase 1 | ✓ Partial — changelog + release-notes delivered; blog (Phase 2), api-docs (Phase 3) |
| REQ-per-artifact-toggles | Phase 1 | ✓ Delivered |
| REQ-configurable-templates | Phase 1 | ✓ Delivered |
| REQ-release-docs-idempotency | Phase 1 | ✓ Partial — stateless/marker mode delivered; DB-backed state (Phase 2) |
| REQ-configurable-trigger | Phase 1 | ✓ Partial — CLI/CI entry point delivered; release/tag webhook (Phase 2) |
| REQ-publish-destinations | Phase 1 | ✓ Partial — releasebody + changelogpr delivered; pages (Phase 2) |

**Coverage:**
- v1 requirements: 6 total
- Mapped to phases: 6
- Unmapped: 0 ✓

**Incremental delivery (later-phase extensions):**
- Phase 2 extends: REQ-release-artifact-generation (blog), REQ-release-docs-idempotency (DB-backed state), REQ-configurable-trigger (release/tag webhook), REQ-publish-destinations (pages).
- Phase 3 extends: REQ-release-artifact-generation (api-docs/openapi).

---
*Requirements defined: 2026-06-04*
*Last updated: 2026-06-05 — Phase 1 complete, traceability updated*
