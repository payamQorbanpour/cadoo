---
gsd_state_version: 1.0
milestone: v2.0
milestone_name: MCP Server + Claude Code Plugin
status: ready_to_plan
last_updated: "2026-06-10"
last_activity: 2026-06-10
progress:
  total_phases: 3
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-10)

**Core value:** A developer working inside an AI assistant can invoke Cadoo's review tools from the conversation — review a local diff inline, or run a tool against a live PR/MR and post results back idempotently — without leaving the editor.
**Current focus:** Phase 4 — Embedded Local Review + Plugin

## Current Position

Phase: 4 of 6 (Embedded Local Review + Plugin)
Plan: — of — (not yet planned)
Status: Ready to plan
Last activity: 2026-06-10 — Milestone v2.0 roadmap created; Phase 4 ready for planning

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

Last session: 2026-06-10
Stopped at: Roadmap created for v2.0; Phase 4 ready for `/gsd:plan-phase 4`
Resume file: None
