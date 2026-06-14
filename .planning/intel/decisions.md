# Decisions (from ADRs)

One entry per architectural decision: title, source, status (locked/proposed), decision statement, scope.

> No ADR documents were present in this ingest set, so there are **no LOCKED decisions**. The entries
> below are design decisions stated by the ingested SPEC documents. They are recorded as **proposed**
> (SPEC-origin, not ADR-origin) — downstream consumers must NOT treat them as immutable locked decisions.
> They are surfaced here so the roadmapper can see the key architectural choices in one place; the
> authoritative form lives in `constraints.md`.

---

## DEC-parallel-not-extend-review — Build a parallel subsystem, not on top of the review pipeline

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- status: proposed (SPEC-origin, not locked)
- scope: internal/releasedocs vs internal/orchestrator

Decision: `internal/releasedocs` is a dedicated subsystem parallel to the review pipeline, mirroring its
architecture but NOT extending `tools.Tool`/`tools.Input`/`tools.Result` (those are PR-diff/inline shaped).

---

## DEC-optional-vcs-capabilities — Add release/range ops as optional capability interfaces

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- status: proposed (SPEC-origin, not locked)
- scope: vcs.Provider

Decision: Add `ReleaseRangeReader`, `ReleasePublisher`, `BranchCommitter` as type-asserted optional
capability interfaces (same pattern as `PriorReviewReader`) rather than bloating core `vcs.Provider`.
Missing capability degrades gracefully with a logged reason.

---

## DEC-deterministic-first-changelog — Changelog is deterministic-first, LLM only polishes

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- status: proposed (SPEC-origin, not locked)
- scope: generators/changelog

Decision: The changelog generator parses -> groups -> renders deterministically; LLM is optional polish
only, so a repo can run with LLM off and get reproducible (golden-file testable) output.

---

## DEC-config-from-tag-tree — Load releaseDocs config from the release tag's tree

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- status: proposed (SPEC-origin, not locked)
- scope: dispatcher config loading

Decision: `.cadoo.yaml` `releaseDocs` is loaded from `ToRef`'s tree (the release tag), not `main` —
consistent with the existing "config from head, never main" rule.

---

## DEC-marker-plus-state-idempotency — Reuse marker + stored-state idempotency model

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- status: proposed (SPEC-origin, not locked)
- scope: publishers + state store + CLI mode

Decision: Mirror the review pipeline — DB-backed state table `(provider, repo, to_tag, artifact_kind)`
when `DATABASE_URL` is set; stateless marker reconstruction in CLI/CI mode (same philosophy as
`PriorReviewReader`).

---

## DEC-phasing — Three-phase delivery order

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- status: proposed (SPEC-origin, not locked)
- scope: delivery sequencing

Decision:
- Phase 1: core types + ReleaseContext builder + grouped change model + changelog & release-notes
  generators + releasebody & changelogpr publishers + `config.ReleaseDocs` + preset templates +
  `cadoo release-docs` CLI (stateless, marker-based). Dogfood on Cadoo's own repo.
- Phase 2: release/tag webhook ingestion + ReleaseJob enqueue (River + memory) + worker consumer + DB
  state table/migration + pages publisher + blog generator.
- Phase 3: API docs / OpenAPI (narrow framework set first).

---
---

## CI-mode dedup convergence (ingest 2026-06-14)

> Source SPEC: `docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md`. Distinct
> subsystem from release-docs (CI-mode dedup, not `internal/releasedocs`). All proposed, none locked.

---

## DEC-cidedup-carry-structural-key — Carry StructuralKey end-to-end, never recompute from first line

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- status: proposed (SPEC-origin, not locked)
- scope: findings.PostedFinding / orchestrator.resolveStalePriors

Decision: Thread the real `StructuralKey` from the marker / DB column through `PostedFinding` and compare it
directly in `resolveStalePriors`. Stop reconstructing the key from the lossy first line of the comment body.
Applies to both the CI memory-store and DB-backed worker paths. (Part A.)

---

## DEC-cidedup-resolved-as-durable-memory — Treat resolved threads + anchor line as first-class suppression

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- status: proposed (SPEC-origin, not locked)
- scope: vcs.PriorInline / findings.findingRec / memoryStore.has

Decision: Capture each prior thread's anchor line and resolved flag into the seeded store, and widen
`memoryStore.has` so a resolved prior suppresses a reworded new finding (line-overlap OR a lower
`ResolvedSuppressThreshold` ≈ 0.3), scoped to `(tool, file)` so genuinely new findings elsewhere are not
hidden. (Part B.)

---

## DEC-cidedup-incremental-dual-context — Incremental review via reviewed-sha marker + dual context on tools.Input

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- status: proposed (SPEC-origin, not locked)
- scope: summary wrapper / vcs.Provider DiffBetween / tools.Input

Decision: Persist the last-reviewed SHA in the summary wrapper (`<!-- cadoo:reviewed-sha:<sha> -->`), add a
`DiffBetween` provider capability, and carry **both** full and incremental context on `tools.Input` so each
tool selects (preferred over a registry-wide inline/summary classification). Inline tools use the incremental
view; summary tools use the full view; fallbacks to full review on first run / non-ancestor SHA / force-push.
(Part C. Recommended over tool classification per the SPEC's open question 1.)

---

## DEC-cidedup-resolvestale-scoped-to-changeset — resolveStalePriors only resolves priors inside the change set

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- status: proposed (SPEC-origin, not locked)
- scope: orchestrator.resolveStalePriors × incremental change set

Decision: Under incremental review, `resolveStalePriors` may only resolve a prior whose anchor line falls
inside the current run's incremental change set; threads on untouched code persist (neither re-posted nor
resolved). On a full run the change set is the entire diff, preserving today's semantics. (Required to keep
Part C from re-introducing churn.)

---

## DEC-cidedup-no-db-migration — CI-mode fix is memory-store only; no new migration

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- status: proposed (SPEC-origin, not locked)
- scope: persistence / scope boundary

Decision: This is the CI-mode (memory store) path — no DB schema changes or migrations. The DB-backed worker
path is unaffected by Parts B/C but inherits Part A. Code-content-hash finding identity and LLM-temperature
tuning are explicitly out of scope (YAGNI) unless leakage persists after Part C.
