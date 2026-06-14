# Context (from DOCs)

Running notes keyed by topic, appended verbatim with source attribution.

> No DOC-type documents were present in this ingest set. The notes below capture each SPEC's
> deferred/open items and grounding context that downstream planning should keep in view.

---

## Topic: Open items deferred to planning (release-docs)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md (§10)

- Exact OpenAPI extraction strategy and initial supported language/framework (phase 3).
- Whether `llm` grouping is worth shipping in phase 1 or deferred (conventional/labels first).
- Blog publish destination beyond pages (e.g. dev.to) — out of scope for now.

---

## Topic: Codebase grounding (no PROJECT/ROADMAP/REQUIREMENTS yet)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/.planning/codebase/ (prior codebase intel)

Net-new bootstrap. Prior codebase intel exists at `.planning/codebase/` (STACK.md, ARCHITECTURE.md,
STRUCTURE.md, CONVENTIONS.md, INTEGRATIONS.md, TESTING.md, CONCERNS.md) and should be read by the
roadmapper for grounding. The release-docs SPEC explicitly reuses existing primitives: the orchestrator's
`VCSPool`, the dual-mode job queue (River + in-memory), the LiteLLM gateway, and the marker-based
idempotency pattern from CI-mode review (`PriorReviewReader`).

---

## Topic: Open implementation questions (CI-mode dedup convergence) — ingest 2026-06-14

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md (§ "Open implementation questions")

For the implementation plan to resolve (the SPEC is "Approved design, pending implementation plan"):

1. Tool classification vs dual-context on `tools.Input` for Part C — SPEC **recommends dual-context**
   (full + incremental) so tools opt in without a central registry edit.
2. Exact `ResolvedSuppressThreshold` value and line-overlap tolerance — start at **0.3** / exact-line-range
   overlap; make **both constants** for tuning.
3. `DiffBetween` reachability check — how to cheaply detect a non-ancestor `LastReviewedSHA` per provider
   before falling back to a full review.

---

## Topic: Codebase grounding (CI-mode dedup convergence)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md (Root cause / Data-marker sections)

The fix is grounded in concrete existing code locations the roadmapper/planner should read first:
- `internal/orchestrator/reviewer.go` — `resolveStalePriors` (~L491), current-run key build (~L481).
- `internal/findings/findings.go` — `StructuralKey` build (~L108), `firstLine`/`Title` (~L184),
  matching rule (exact key OR Jaccard ≥ 0.5, ~L152-155).
- `internal/findings/prior.go` — `NewFromPrior` (~L34-53) currently drops the `Resolved` flag and anchor line.
- `internal/vcs/gitlab/gitlab.go` — `ListCadooArtifacts` read-back, records `Resolved` (~L263), positions
  carry `n.Position.NewLine`.
- `vcs.PriorReviewReader` / `vcs.PriorInline` / `vcs.PriorReview` — the CI-mode read-back contract Parts B/C extend.

Reported failure mode is **GitLab CI-mode** (stateless, no DB). GitHub inherits Parts A/C generically via the
shared `DiffBetween` capability. The DB-backed worker path inherits Part A only.
