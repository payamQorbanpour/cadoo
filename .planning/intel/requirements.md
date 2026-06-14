# Requirements (from PRDs)

Extracted requirements with derived IDs and acceptance criteria. Each carries a `source:`.

> No PRD documents were present in this ingest set. The requirements below are derived from the SPEC
> documents for downstream planning convenience. They are SPEC-origin requirements, not PRD-origin — the
> roadmapper should treat the SPECs (`constraints.md`) as the authoritative contract and these as a
> planning-friendly restatement of explicit user requirements and goals. No competing acceptance variants
> exist (each requirement derives from a single source).

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

---
---

# CI-mode dedup convergence (ingest 2026-06-14)

> Derived from `docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md` (SPEC, high
> confidence, not locked). Distinct subsystem from release-docs above — `cadoo-cli` CI-mode dedup, not
> `internal/releasedocs`. Single source; no competing acceptance variants.

---

## REQ-cidedup-convergent-review

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- origin: SPEC (Problem / convergence goal)
- scope: cadoo-cli CI-mode stateless dedup path

Description: On a re-reviewed PR/MR, CI-mode review must reach a fixed point instead of ratcheting thread
count upward (observed 39 → 45 → …). Resolving open threads and pushing must converge — no fresh batch of
near-duplicate threads reappearing.

Acceptance criteria:
- Once code stops changing, total thread count is **monotonic non-increasing** across resync runs.
- A re-run against an **unchanged head** posts **zero** new threads and resolves **zero** existing ones
  (fixed-point test).
- A push touching N lines posts at most findings scoped to that change; pre-existing threads on untouched
  code persist (coverage from earlier full runs is not lost).

---

## REQ-cidedup-no-self-resolution

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- origin: SPEC (Part A)
- scope: orchestrator.resolveStalePriors / findings.PostedFinding.StructuralKey

Description: Cadoo must stop resolving its own still-valid multi-line threads. The real `StructuralKey` is
threaded end-to-end instead of being lossily reconstructed from the comment's first line.

Acceptance criteria:
- `resolveStalePriors` compares the carried `StructuralKey` against current keys (no first-line recompute).
- Regression test: a multi-line `improve`-style body whose finding is still present is **not** resolved.
- Fix applies to both the CI memory-store path and the DB-backed worker path.

---

## REQ-cidedup-honor-resolves

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- origin: SPEC (Part B)
- scope: memoryStore.has / vcs.PriorInline / findings.findingRec

Description: A resolved thread (by user or by Cadoo) must carry durable suppression weight so a reworded
version of the same finding does not return.

Acceptance criteria:
- Resolved prior + reworded new finding in the same `(tool, file)` with line-overlap or Jaccard ≥
  `ResolvedSuppressThreshold` (≈0.3) → **suppressed**.
- Resolved prior + unrelated new finding elsewhere in the same file → **NOT suppressed** (guardrail holds).
- Anchor line is captured from `n.Position.NewLine` and the resolved flag is carried into the seeded store record.

---

## REQ-cidedup-incremental-review

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- origin: SPEC (Part C)
- scope: summary wrapper reviewed-sha / vcs.Provider DiffBetween / tools.Input

Description: Only re-review code changed since Cadoo last reviewed — a structural ceiling on thread growth.
The last-reviewed SHA is persisted in the summary wrapper; inline tools see only the incremental change set.

Acceptance criteria:
- Summary wrapper embeds `<!-- cadoo:reviewed-sha:<sha> -->`; `LastReviewedSHA` is parsed back on read.
- When `LastReviewedSHA` is an ancestor of head, inline-emitting tools receive only the `lastReviewedSHA..head`
  change set; summary tools keep the full PR view.
- Fallbacks honored: first run / non-ancestor SHA (force-push, rebase) → full review; empty incremental
  diff → no inline tools run, summary refreshed only.
- `resolveStalePriors` only resolves priors whose anchor line falls inside the incremental change set.
