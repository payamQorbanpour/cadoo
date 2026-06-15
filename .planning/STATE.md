---
gsd_state_version: 1.0
milestone: v1.1
milestone_name: Release-Docs Engineering Diagrams
status: executing
stopped_at: Completed 07-03-PLAN.md (Phase 7 complete)
last_updated: "2026-06-15T10:51:08.892Z"
last_activity: 2026-06-15 -- Phase 08 execution started
progress:
  total_phases: 4
  completed_phases: 1
  total_plans: 3
  completed_plans: 3
  percent: 25
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-10)

**Core value:** A maintainer can publish auto-generated software-engineering diagrams (sequence, dependency, state, flowchart, class) as part of release docs, choosing which diagram types to ship per repo.
**Current focus:** Phase 08 — ci-mode-dedup-convergence

## Current Position

Phase: 08 (ci-mode-dedup-convergence) — EXECUTING
Plan: 1 of 4
Status: Executing Phase 08
Last activity: 2026-06-15 -- Phase 08 execution started

## Performance Metrics

**Velocity (v1.0 baseline):**

- Total plans completed (v1.0): 18
- Average duration: ~43 min/plan
- Total execution time: ~5 hours (wave-based)

**By Phase (v2.0):**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 04 — embedded-local-review-plugin | TBD | - | - |
| 05 — live-pr-mr-review | TBD | - | - |
| 06 — connected-mode | TBD | - | - |
| 07 | 3 | - | - |

*Updated after each plan completion*
| Phase 07 P01 | 6 min | 3 tasks | 3 files |
| Phase 07 P02 | 12 min | 3 tasks | 11 files |
| Phase 07 P03 | ~30 min | 4 tasks | 4 files |

## Accumulated Context

### Decisions

From PROJECT.md Key Decisions table (v2.0 design choices, PROPOSED — not yet locked):

- MCP server in new `internal/mcpserver`; `internal/mcp` client package untouched.
- Use `github.com/modelcontextprotocol/go-sdk`; fallback to minimal hand-rolled stdio server if SDK unsuitable.
- Embedded mode default; connected mode opt-in via `--api-url`.
- `cadoo-mcp` mirrors `cadoo-cli` CI-mode: stateless, no DB, dedup via `PriorReviewReader`.
- `post=true` protected by `allowed_repos` allowlist + URL host validation (confused-deputy defense).
- [Phase 07]: Diagram types are FIXED (sequence, dependency, state, flowchart, class) — D-04
- [Phase 07]: Diagrams gated by one inline ArtifactConfig family gate, no per-type toggles — D-07
- [Phase ?]: [Phase 07]: Dependency Mermaid keyword set adopted as {flowchart, graph, erDiagram} (RESEARCH Q3)
- [Phase ?]: [Phase 07]: diagrams.Generator emits one Artifact per valid Mermaid source; ordered-slice type iteration; family-level (nil,nil) skip on absent FileFetcher
- [Phase 07]: diagrams generator registered in DefaultGenerators() (enabled:false default); pages publisher routes KindDiagrams sub-paths idempotently with NO publisher code change — proven by test, not new branches

### Pending Todos

None yet.

### Blockers/Concerns

- **Go SDK maturity:** `github.com/modelcontextprotocol/go-sdk` is the first decision point at Phase 4 plan time — fallback specified (minimal hand-rolled stdio server).
- **Synchronous review latency (Phase 6):** `cadoo-api` sync endpoint for connected mode needs auth + rate limiting strategy; streaming/chunked approach TBD at Phase 6 planning.

## Deferred Items

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| v2 req | HTTP/SSE transport for `cadoo-mcp` | Deferred to post-v2.0 | Requirements |
| v2 req | OAuth flows for VCS auth | Deferred to post-v2.0 | Requirements |
| seed | SEED-001: GitHub Pages on every document publishing | dormant | v1.1 close (2026-06-13) |
| uat_gap | 01-HUMAN-UAT (v1.0 Phase 01) | approved, 0 open scenarios | v1.1 close (2026-06-13) |
| uat_gap | 02-HUMAN-UAT (v1.0 Phase 02) | partial, 0 open scenarios | v1.1 close (2026-06-13) |
| verification_gap | 01-VERIFICATION (v1.0 Phase 01) | human_needed (no open scenarios) | v1.1 close (2026-06-13) |
| verification_gap | 02-VERIFICATION (v1.0 Phase 02) | human_needed (no open scenarios) | v1.1 close (2026-06-13) |

## Session Continuity

Last session: 2026-06-13T18:29:40.758Z
Stopped at: Completed 07-03-PLAN.md (Phase 7 complete)
Resume file: None

## Operator Next Steps

- Start the next milestone with /gsd-new-milestone
