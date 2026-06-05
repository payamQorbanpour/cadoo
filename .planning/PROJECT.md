# Cadoo

## What This Is

Cadoo is a multi-tenant AI code reviewer that posts inline review comments, summary comments, and check-runs to GitHub, GHES, and GitLab — feature-parity-targeted at Qodo Merge and CodeRabbit, shipped as both SaaS and self-host on one binary set (five `cmd/*` services, Postgres/pgvector, LiteLLM sidecar). The current milestone adds **Release Docs**: after a customer cuts a release, Cadoo automatically generates and publishes release artifacts (changelog, release notes, blog, and later API/OpenAPI docs) for *their* repository.

## Core Value

After a customer release, Cadoo auto-generates and publishes the configured release artifacts (changelog, release notes, blog, later API/OpenAPI docs) to the configured destinations (release body, `CHANGELOG.md` PR, docs branch/Pages) — idempotently across re-runs, with per-artifact toggles honored.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

(Release Docs is the active milestone — nothing validated yet. The existing review pipeline ships separately and is the platform this builds on.)

### Active

<!-- Current scope. Building toward these (Release Docs milestone). -->

- [ ] **RELDOCS-GEN**: Auto-generate release artifacts (changelog, release notes, blog, later API/OpenAPI) after a release
- [ ] **RELDOCS-TOGGLES**: Per-artifact `enabled` + `when:` conditions keyed off the computed semver bump
- [ ] **RELDOCS-TEMPLATES**: Two configurability layers — presets out of the box, custom override templates for full control
- [ ] **RELDOCS-IDEMPOTENT**: Idempotent across re-runs/resyncs (edit-in-place, no duplicates), DB-backed and stateless/marker modes
- [ ] **RELDOCS-TRIGGER**: Configurable trigger per repo — release event (default), tag push (optional), manual CLI/CI
- [ ] **RELDOCS-PUBLISH**: Publish to release body, `CHANGELOG.md` via PR, and docs branch / GitHub Pages

### Out of Scope

<!-- Explicit boundaries. Includes reasoning to prevent re-adding. -->

- Extending `tools.Tool` / `tools.Input` / `tools.Result` for release docs — those types are PR-diff/inline-comment shaped; Release Docs is a parallel subsystem (`internal/releasedocs`).
- New LLM provider code paths in Go — all multi-provider routing stays in the LiteLLM sidecar.
- API docs for *arbitrary* languages — phase 3 starts with a narrow, well-supported framework set.
- Blog publish destinations beyond pages (e.g. dev.to) — out of scope for this milestone.
- Single-tenant shortcuts — `org_id` is carried throughout; self-host is a degenerate single-org tenant.

## Context

**Technical environment (grounded in `.planning/codebase/`):** Go 1.26, five binaries under `cmd/` (`cadoo-webhook`, `cadoo-worker`, `cadoo-api`, `cadoo-cli`, `cadoo-tunnel`). PostgreSQL 16 + pgvector. River (Postgres) job queue with an in-memory fallback when `DATABASE_URL` is unset (dual-mode). LLM access goes through a LiteLLM sidecar over an OpenAI-compatible HTTP API. VCS adapters live behind the `vcs.Provider` interface (`internal/vcs/github`, `internal/vcs/gitlab`).

**Reused primitives:** Release Docs deliberately mirrors the review pipeline rather than extending it — reusing the orchestrator's `VCSPool`, the dual-mode queue, the LiteLLM gateway, and the marker-based idempotency pattern from CI-mode review (`PriorReviewReader`). The new subsystem is `internal/releasedocs`, parallel to `internal/orchestrator`.

**Conventions to honor:** `.cadoo.yaml` is loaded from the release tag's tree (consistent with the existing "config from head, never main" rule). Exported symbols need docstrings (`exported` revive rule). `goimports` local-prefix grouping (`github.com/payamqorbanpour/cadoo`). `make ci` (vet + test + build) must stay green; any new migration must round-trip `up → down → up`.

**Source spec:** `docs/superpowers/specs/2026-06-04-release-docs-design.md` (approved design). Synthesized intel at `.planning/intel/` (the SPEC's `constraints.md` is the authoritative technical contract).

## Constraints

- **Tech stack**: Go 1.26, Postgres 16 + pgvector, River queue, LiteLLM sidecar — no new external runtimes for this feature.
- **Architecture**: `internal/releasedocs` is parallel to the review pipeline, NOT layered on `tools.*` — those are diff/inline shaped.
- **VCS**: Add release/range/branch ops as optional `vcs` capability interfaces (`ReleaseRangeReader`, `ReleasePublisher`, `BranchCommitter`), type-asserted like `PriorReviewReader`. Missing capability degrades gracefully with a logged reason. Never import `internal/vcs/github|gitlab` outside `internal/vcs/`.
- **Determinism**: Changelog is deterministic-first (parse → group → render); `LLM` is nil-tolerant and only polishes wording, so LLM-off output is reproducible (golden-file testable).
- **Idempotency**: Marker + stored-state model — DB table `(provider, repo, to_tag, artifact_kind)` when `DATABASE_URL` is set; stateless marker reconstruction in CLI/CI mode.
- **Config**: `.cadoo.yaml releaseDocs` block loaded from the release tag's tree, never `main`.
- **Multi-tenancy**: `org_id` carried throughout; one code path for SaaS and self-host.
- **CI/Lint**: `make ci` green; migrations round-trip; exported docstrings; `goimports` grouping.

## Key Decisions

<!-- SPEC-origin design choices recorded as PROPOSED (no ADRs present — not locked). -->

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Parallel `internal/releasedocs` subsystem, not built on `tools.*` | `tools.*` is PR-diff/inline shaped; release artifacts are a different shape | — Pending (proposed) |
| Release/range/branch ops as optional `vcs` capability interfaces | Keeps core `vcs.Provider` PR-centric; mirrors `PriorReviewReader`; graceful degradation | — Pending (proposed) |
| Changelog deterministic-first; LLM only polishes | Reproducible (golden-file) output with LLM off | — Pending (proposed) |
| `releaseDocs` config loaded from the release tag's tree | Consistent with "config from head, never main" | — Pending (proposed) |
| Marker + stored-state idempotency (DB-backed + stateless CLI) | Mirrors review pipeline; CLI/CI runs without a DB | — Pending (proposed) |
| Three-phase delivery: CLI → webhook+state → API docs | Ship dogfoodable CLI value first; defer webhook/state and OpenAPI complexity | — Pending (proposed) |

---
*Last updated: 2026-06-05 — Phase 02 complete (webhook auto-trigger + DB state + blog generator + pages publisher)*
