---
gsd_state_version: 1.0
milestone: v1.1
milestone_name: Release-Docs Engineering Diagrams
status: ready_to_plan
last_updated: "2026-06-13"
last_activity: 2026-06-13
progress:
  total_phases: 1
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-10)

**Core value:** A maintainer can publish auto-generated software-engineering diagrams (sequence, dependency, state, flowchart, class) as part of release docs, choosing which diagram types to ship per repo.
**Current focus:** Phase 7 — Release-Docs Engineering Diagrams (milestone v1.1)

## Current Position

Phase: 7 (Release-Docs Engineering Diagrams) — milestone v1.1 (single phase)
Plan: — of — (not yet planned)
Status: Ready to discuss → plan (run `/gsd:discuss-phase 7` next)
Last activity: 2026-06-13 — Milestone v1.1 opened; Phase 7 scaffolded into roadmap/requirements; v2.0 (Phases 4-6) deferred, 0 plans started

Progress: [░░░░░░░░░░] 0%

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

*Updated after each plan completion*

## Accumulated Context

### Decisions

From PROJECT.md Key Decisions table (v2.0 design choices, PROPOSED — not yet locked):

- MCP server in new `internal/mcpserver`; `internal/mcp` client package untouched.
- Use `github.com/modelcontextprotocol/go-sdk`; fallback to minimal hand-rolled stdio server if SDK unsuitable.
- Embedded mode default; connected mode opt-in via `--api-url`.
- `cadoo-mcp` mirrors `cadoo-cli` CI-mode: stateless, no DB, dedup via `PriorReviewReader`.
- `post=true` protected by `allowed_repos` allowlist + URL host validation (confused-deputy defense).

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

## Session Continuity

Last session: 2026-06-13
Stopped at: Milestone v1.1 + Phase 7 scaffolded; ready for `/gsd:discuss-phase 7` (then `/gsd:plan-phase 7`). v2.0 deferred at `/gsd:plan-phase 4`.
Resume file: None
