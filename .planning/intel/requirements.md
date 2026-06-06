# Requirements (from PRDs)

Extracted requirements with derived IDs and acceptance criteria. Each carries a `source:`.

> No PRD documents were present in this ingest set. The requirements below are derived from the single
> SPEC document (release-docs design) for downstream planning convenience. They are SPEC-origin
> requirements, not PRD-origin — the roadmapper should treat the SPEC (`constraints.md`) as the
> authoritative contract and these as a planning-friendly restatement of its explicit user requirements
> and goals. No competing acceptance variants exist (single source).

---

## REQ-release-artifact-generation

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- origin: SPEC (Goals §1)
- scope: changelog / release-notes / blog / api-docs generation

Description: After a customer cuts a release, Cadoo automatically generates a set of release artifacts
for their repository: changelog section, polished release notes, PR blog post, and (later) API docs + OpenAPI.

Acceptance criteria:
- Changelog: structured, grouped (Features/Fixes/Breaking/…) section appended to a rolling `CHANGELOG.md`.
- Release notes: polished human-readable narrative for the GitHub/GitLab Release body.
- Blog: long-form announcement highlighting headline changes.
- API docs + OpenAPI: derived from code (deferred to phase 3).

---

## REQ-per-artifact-toggles

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- origin: SPEC (explicit user requirement, §3)
- scope: config.ReleaseDocs + Generator.Enabled

Description: Per-artifact toggles and conditions. Every artifact has its own `enabled` plus a `when:`
condition keyed off the computed semver bump.

Acceptance criteria:
- Changelog can run every release while blog runs only on minor/major.
- The dispatcher never runs a disabled generator (`Generator.Enabled(cfg, bump)` gates execution).

---

## REQ-configurable-templates

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- origin: SPEC (approved requirement, §3)
- scope: template presets + override loading

Description: Two configurability layers — presets out of the box, and custom override template files for
full control.

Acceptance criteria:
- Presets (`preset:`, `grouping.source`, `tone:`) give good behavior with no template authoring.
- Any artifact may set `template:` pointing at a Go `text/template` file in the repo (loaded from the
  tag tree), overriding the preset entirely.
- Templates receive the `ReleaseContext` plus the grouped change model.
- Defaults to embedded preset templates in `internal/releasedocs/template`.

---

## REQ-release-docs-idempotency

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- origin: SPEC (explicit user requirement, §1/§5)
- scope: publishers + state store

Description: Idempotent across re-runs/resyncs (re-tagging, edited release): edit-in-place, no duplicates.

Acceptance criteria:
- Running the dispatcher twice over the same range edits the release body (not duplicated), keeps a single
  changelog PR, and uses stable pages paths.
- Works both DB-backed and via stateless marker reconstruction (CLI/CI mode).

---

## REQ-configurable-trigger

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- origin: SPEC (explicit user requirement, §7)
- scope: webhook / worker / CLI triggers

Description: Trigger configurable per repo — published Release event by default, optionally tag push, plus
a manual CLI/CI entry point.

Acceptance criteria:
- Default `release`: webhook handles GitHub `release: published` / GitLab release webhooks and enqueues a `ReleaseJob`.
- Optional `tag`: a `v*` tag push (filtered by `tagPattern`) is treated as a release.
- Manual: `cadoo release-docs --repo … --from vX --to vY` runs stateless via the same dispatcher.
- If `releaseDocs.trigger` excludes the event kind, the webhook no-ops early.

---

## REQ-publish-destinations

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- origin: SPEC (Goals §1, §5)
- scope: publishers/{releasebody,changelogpr,pages}

Description: Publish artifacts to the Release body, to `CHANGELOG.md` via PR, and to a docs branch /
GitHub Pages.

Acceptance criteria:
- releasebody: upserts/updates the Release body inside Cadoo markers, preserving user content.
- changelogpr: opens/updates a single PR prepending the new `CHANGELOG.md` section.
- pages: commits rendered artifacts to a configured branch/dir with deterministic paths.
