## Conflict Detection Report

Mode: merge. Ingest date: 2026-06-14.
New doc this ingest: `docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md` (SPEC, high
confidence, locked: false, precedence: default ADR > SPEC > PRD > DOC).
Checked against existing LOCKED decisions in PROJECT.md and every phase `*-CONTEXT.md` `<decisions>` block.

### BLOCKERS (0)

None.

No LOCKED decisions exist anywhere in the existing context. Every decision in PROJECT.md (Key Decisions
table) and in the phase CONTEXT `<decisions>` blocks (01/03/07 `D-NN`) is explicitly recorded as
**proposed / SPEC-origin, not locked** (PROJECT.md L92: "SPEC-origin design choices recorded as PROPOSED
(no ADRs present — not locked)"; `01-CONTEXT.md` L147: "proposed, not locked ADRs"). There is therefore no
locked decision for the new SPEC to contradict.

Scope check (no contradiction even at the proposed level): the new SPEC targets `cadoo-cli` CI-mode dedup
in `internal/orchestrator` (`reviewer.go:resolveStalePriors`), `internal/findings`
(`PostedFinding`/`findingRec`/`memoryStore`), and `internal/vcs` (`PriorInline`/`PriorReview`/`DiffBetween`).
The existing decisions concern the `internal/releasedocs` subsystem (changelog/release-notes/blog/api-docs/
diagrams generators + publishers). The two subsystems share no scope, no types, and no decisions.
  Found: docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md — scope: internal/orchestrator, internal/findings, internal/vcs (CI-mode dedup)
  Existing: .planning/PROJECT.md, .planning/phases/{01,03,07}-*/*-CONTEXT.md — scope: internal/releasedocs (release-docs generators/publishers)
  Result: no overlap, no contradiction.

### WARNINGS (0)

None.

No competing acceptance variants. This is a single-doc ingest from one SPEC source; each derived
requirement (REQ-cidedup-convergent-review, REQ-cidedup-no-self-resolution, REQ-cidedup-honor-resolves,
REQ-cidedup-incremental-review) has exactly one acceptance set. No second PRD/SPEC defines the same
requirement scope with divergent criteria.
  Found: docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md (sole source for all CI-dedup requirements)

### INFO (0)

None.

No auto-resolution was required:
- No higher-precedence ADR exists for the SPEC to be overridden by (precedence default ADR > SPEC > PRD >
  DOC; the set contains only SPECs).
- The new SPEC asserts no technical decision that contradicts any existing SPEC-origin decision — the
  subsystems are disjoint, so no precedence tiebreak was applied.
- Cross-ref / cycle detection: the new doc's `cross_refs` array is empty; the cross-doc graph remains
  acyclic. The other classification in the directory
  (`2026-06-04-release-docs-design.md`) references code modules only, not other ingested documents. No
  cycle, no traversal-depth issue.
  Source: .planning/intel/classifications/2026-06-14-cadoo-ci-dedup-convergence-design-d4e7a1b2.json (cross_refs: [])

---

STATUS: READY — safe to route. No blockers, no competing variants, no auto-resolved conflicts.

---
*Prior ingest (mode: new, 2026-06-04 release-docs SPEC): 0 blockers / 0 warnings / 0 info. Superseded by
this merge report.*
