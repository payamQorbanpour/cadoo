# Decisions (from ADRs)

One entry per architectural decision: title, source, status (locked/proposed), decision statement, scope.

> No ADR documents were present in this ingest set, so there are **no LOCKED decisions**. The entries
> below are design decisions stated by the single SPEC document. They are recorded as **proposed**
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
