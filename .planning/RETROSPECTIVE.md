# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.1 — Release-Docs Engineering Diagrams

**Shipped:** 2026-06-13
**Phases:** 1 (Phase 7) | **Plans:** 3 | **Tasks:** 10

### What Was Built
- `internal/releasedocs/generators/diagrams/` — a deterministic, LLM-free `MultiGenerator` that fetches user-configured committed Mermaid sources at the release tag, sniffs per-type keywords, wraps in a fixed ` ```mermaid ` fence, and emits one markdown page per source at `diagrams/<type>/<base>.md`.
- Config contract: `releasedocs.KindDiagrams` + `config.DiagramsConfig` (five per-type path lists + inline `ArtifactConfig` family gate), wired into `ReleaseArtifacts` and documented in `.cadoo.yaml.example`.
- Registered in `DefaultGenerators()`; proven to route + overwrite idempotently through the **unchanged** pages publisher; dogfooded with two real Mermaid sources confirmed rendering on github.com (SC-5).

### What Worked
- **Clone-an-analog planning.** Phase 7 was scoped as a near-mechanical mirror of the Phase 3 apidocs generator (`MultiGenerator` + `Filename` sub-paths + graceful skip). This made the generator simpler (no `text/template`, no `embed`) and the plans crisp.
- **Linear single-plan waves executed sequentially on the main tree.** With a strict 07-01 → 07-02 → 07-03 dependency chain and one plan per wave, worktree isolation offered no parallelism — sequential-on-main avoided the merge/cleanup risk surface entirely.
- **Discuss-phase locked the hard design call early.** "Render committed Mermaid sources, no derivation, no LLM" (D-01..D-10) removed the biggest ambiguity before any code was written.
- **Honest checkpoint handling.** The non-autonomous dogfood task stopped at a human-verify gate (github.com rendering) instead of fabricating approval.

### What Was Inefficient
- **Stale IDE diagnostics caused a false alarm.** After wave 2, the IDE flagged `sniffMermaid`/`wrapMermaidFence`/`diagramName` as "unused" — a snapshot from the intermediate commit before `diagrams.go` landed. Required a re-verify (build/test/lint) to dismiss. Lesson: trust a fresh `make ci` over mid-execution diagnostics.
- **Code-review edge cases shipped unhardened.** Review found 4 verified WARNINGs (`diagramName` degenerate inputs, unclosed-frontmatter drop, no size bound, missing edge tests). They don't break the verified paths but are real — deferred as follow-ups rather than fixed inline.

### Patterns Established
- **`MultiGenerator` + `Filename` sub-path** is now the established shape for any release-docs artifact family that emits multiple files (apidocs, diagrams).
- **Publisher stays closed to new artifact kinds** — routing is driven by `Artifact.Filename` + a prefix guard, proven by test rather than new publisher branches.

### Key Lessons
1. For a strictly linear, single-plan-per-wave phase, sequential-on-main execution is simpler and safer than worktree isolation — reserve worktrees for genuine intra-wave parallelism.
2. Re-verify against a clean build when mid-execution diagnostics conflict with an executor's "lint clean" claim; intermediate-commit snapshots lie.
3. Lock the highest-ambiguity design decision in discuss-phase; it cascades into far simpler plans and code.

### Cost Observations
- Model mix: predominantly Opus 4.8 (orchestrator + executors + reviewer + verifier).
- Notable: 3 plans executed in ~48 min of executor time; full post-execution gate suite (regression tests, code review, verification) added one reviewer + one verifier agent.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Key Change |
|-----------|--------|-------|------------|
| v1.0 Release Docs | 3 | 18 | Initial release-docs subsystem; wave-based parallel execution |
| v1.1 Release-Docs Engineering Diagrams | 1 | 3 | Single-phase milestone; sequential-on-main for linear waves; clone-an-analog planning |

### Cumulative Quality

| Milestone | Verification | Code Review | Notes |
|-----------|--------------|-------------|-------|
| v1.1 | Passed 9/9 must-haves | 0 critical, 4 warning, 3 info | Golden-file byte-stable; LLM-free generator |

### Top Lessons (Verified Across Milestones)

1. Mirroring an existing, shipped analog (apidocs → diagrams) produces the cleanest plans and the least new surface area.
2. Deterministic, golden-file-tested generators with nil-tolerant LLM hooks keep release-docs output reproducible and reviewable.
