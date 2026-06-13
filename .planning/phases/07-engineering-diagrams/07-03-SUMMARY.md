---
phase: 07-engineering-diagrams
plan: 03
subsystem: releasedocs
tags: [diagrams, mermaid, pages-publisher, dispatcher, dogfood, idempotency]

# Dependency graph
requires:
  - phase: 07-engineering-diagrams (plan 02)
    provides: diagrams.Generator package (Mermaid sniff + fixed-fence wrapper + GenerateMulti)
  - phase: 03-apidocs
    provides: Artifact.Filename + KindDiagrams routing + pages publisher path/UpsertFile idempotency pattern
provides:
  - diagrams generator wired into DefaultGenerators() so the dispatcher runs it on every release-docs run (gated enabled:false by default)
  - pages publisher proven to route KindDiagrams sub-path filenames to deterministic, idempotent paths (no publisher code change)
  - two committed dogfood Mermaid sources describing Cadoo's real architecture (release-pipeline flowchart + cadoo-binaries classDiagram)
affects: [release-docs operators enabling the diagrams artifact, future diagram-type additions]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Generator registration via one-line append to DefaultGenerators() slice (mirrors apidocs.New())"
    - "Publisher routes arbitrary Artifact.Filename sub-paths unchanged; type-specific routing proven by test, not by new code"

key-files:
  created:
    - internal/releasedocs/publishers/pages/pages_diagrams_test.go
    - docs/diagrams/release-pipeline.mmd
    - docs/diagrams/cadoo-binaries.mmd
  modified:
    - internal/releasedocs/defaults/defaults.go

key-decisions:
  - "No pages.go change needed — the publisher already routes arbitrary Filename sub-paths idempotently; KindDiagrams routing is proven by test against the unchanged publisher"
  - "Dogfood sources are real Cadoo architecture (CLAUDE.md-grounded): a flowchart of the release-docs pipeline and a classDiagram of the five cmd/* binaries around the orchestrator Dispatcher"

patterns-established:
  - "Pattern: register a generator by appending generator.New() to DefaultGenerators(); the enabled:false default keeps existing runs unaffected until a user opts in"
  - "Pattern: prove deterministic+idempotent pages routing for a new Kind via a path-assertion test on the recording fake (TestPublish_*_Paths + TestIdempotent_*), not by adding publisher branches"

requirements-completed: [DIAG-03, DIAG-02]

# Metrics
duration: ~30min
completed: 2026-06-13
---

# Phase 7 Plan 03: DefaultGenerators Registration + Pages Idempotency + Dogfood Summary

**Diagrams generator wired into the release-docs dispatcher, pages routing proven deterministic/idempotent for KindDiagrams against the unchanged publisher, and dogfooded end-to-end on Cadoo's own repo with two committed Mermaid sources that render on github.com.**

## Performance

- **Duration:** ~30 min
- **Tasks:** 4 (3 automated + 1 human-verify checkpoint)
- **Files modified:** 4 (1 modified, 3 created)

## Accomplishments

- Registered `diagrams.New()` in `DefaultGenerators()` so the dispatcher invokes the diagrams generator on every release-docs run (DIAG-02), gated `enabled:false` by default (D-07) so existing runs are unaffected until a user opts in.
- Added `pages_diagrams_test.go` proving the pages publisher routes `KindDiagrams` sub-path filenames to deterministic paths `docs/releases/<toRef>/diagrams/<type>/<name>.md` and overwrites them in place on re-run (DIAG-03, D-09) — with **no change** to `pages.go`.
- Committed two repo-accurate dogfood Mermaid sources under `docs/diagrams/`: a `flowchart` of the release-docs pipeline and a `classDiagram` of the five `cmd/*` binaries around the orchestrator Dispatcher (SC-5).
- Human-verify checkpoint passed: the operator ran the release-docs CLI, confirmed both dogfood diagram pages render on github.com, and confirmed re-runs overwrite the same paths in place (idempotent).

## Task Commits

Each task was committed atomically:

1. **Task 1: Register diagrams.New() in DefaultGenerators()** - `9381be6` (feat)
2. **Task 2: pages_diagrams_test.go (path routing + idempotency, DIAG-03)** - `cd7696d` (test)
3. **Task 3: Dogfood Mermaid sources (release-pipeline.mmd, cadoo-binaries.mmd, SC-5)** - `42bb824` (docs)
4. **Task 4: human-verify checkpoint** - no commit (user ran the release-docs CLI; user_response = "approved": both pages render on github.com and the re-run is idempotent)

**Plan metadata:** this commit (docs: complete plan)

## Files Created/Modified

- `internal/releasedocs/defaults/defaults.go` - Added the `generators/diagrams` import and appended `diagrams.New()` to the `DefaultGenerators()` slice; updated the doc comment to list the diagrams generator (item 5) as a MultiGenerator alongside apidocs.
- `internal/releasedocs/publishers/pages/pages_diagrams_test.go` - `TestPublish_Diagrams_Paths` (asserts `docs/releases/v1.2.0/diagrams/sequence/login.md` and `.../class/domain.md`) and `TestIdempotent_Diagrams` (asserts second-run captured paths equal first-run), against the recording fake; `pages.go` untouched.
- `docs/diagrams/release-pipeline.mmd` - `flowchart TD` of `.cadoo.yaml` → Dispatcher → ordered generators (changelog/release-notes/blog/apidocs/diagrams) → ordered publishers (releasebody/changelogpr/pages) → deterministic idempotent page path.
- `docs/diagrams/cadoo-binaries.mmd` - `classDiagram` of the five `cmd/*` binaries (webhook/worker/api/cli/tunnel) and their relation to the orchestrator `Dispatcher`.

## Decisions Made

- No `pages.go` change — the publisher already computes `path.Join(dir,"releases",toRef,filename)`, prefix-guards against escaping `{dir}/`, and overwrites via `UpsertFile`. KindDiagrams sub-path routing is therefore a behavior to *prove by test*, not new code (satisfies threat T-07-07 by construction + the idempotency control).
- Dogfood sources describe Cadoo's actual architecture (grounded in CLAUDE.md), not placeholder content, so the dogfood is a faithful end-to-end exercise of the feature.

## Deviations from Plan

None - plan executed exactly as written.

## Authentication Gates

None.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required. (Operators who want diagram pages set `releaseDocs.artifacts.diagrams.enabled: true` and per-type source paths in their own `.cadoo.yaml`; the repo's `.cadoo.yaml` is intentionally not modified.)

## Next Phase Readiness

- Phase 7 (v1.1 Release-Docs Engineering Diagrams) is complete: all three plans shipped, all five phase requirements (DIAG-01..DIAG-05) satisfied, and SC-5 dogfood verified on github.com.
- No blockers. Deferred milestone v2.0 (Phases 4–6, MCP Server + Plugin) is next in execution order.

## Self-Check: PASSED

- All 4 plan files present on disk (defaults.go, pages_diagrams_test.go, release-pipeline.mmd, cadoo-binaries.mmd) plus SUMMARY.md.
- All 3 task commits present in git history (9381be6, cd7696d, 42bb824).
- `make ci` (vet + test + build) green — exit 0, no failures; `internal/releasedocs/...` and `generators/diagrams` packages pass.

---
*Phase: 07-engineering-diagrams*
*Completed: 2026-06-13*
