# Context (from DOCs)

Running notes keyed by topic, appended verbatim with source attribution.

> No DOC-type documents were present in this ingest set. The notes below capture the SPEC's
> deferred/open items and grounding context that downstream planning should keep in view.

---

## Topic: Open items deferred to planning

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
