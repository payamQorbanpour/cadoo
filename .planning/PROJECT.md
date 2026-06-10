# Cadoo

## What This Is

Cadoo is a multi-tenant AI code reviewer that posts inline review comments, summary comments, and check-runs to GitHub, GHES, and GitLab — feature-parity-targeted at Qodo Merge and CodeRabbit, shipped as both SaaS and self-host on one binary set (five `cmd/*` services, Postgres/pgvector, LiteLLM sidecar). The current milestone adds an **MCP server + Claude Code plugin**: a sixth binary (`cadoo-mcp`) that exposes Cadoo's review tools to AI assistants (Claude Code, Claude Desktop, Cursor, any MCP client), for both local diff review and live PR/MR review.

## Core Value

A developer working inside an AI assistant can invoke Cadoo's review tools from the conversation — review a local diff inline, or run a tool against a live PR/MR and post results back to GitHub/GHES/GitLab idempotently — without leaving the editor.

## Current Milestone: v2.0 MCP Server + Claude Code Plugin

**Goal:** Expose Cadoo's review tools to AI assistants via a new `cadoo-mcp` MCP server binary and a Claude Code plugin.

**Target features:**
- `cmd/cadoo-mcp` — MCP server (stdio) exposing Cadoo tools as MCP tools, reusing the CI-mode stateless path
- Local review (`target=local`) — working-tree / staged / ref-range diffs, results returned inline
- Live PR/MR review (`target=pr`) — dry-run or idempotent post-back to GitHub/GHES/GitLab (same wrapper markers)
- Configurable tool surface — enable/disable; default core set: review, describe, improve, ask
- Connected mode — proxy through `cadoo-api` so KB + learnings apply (new sync tool endpoint)
- Claude Code plugin (`plugins/claude/`) — manifest, `.mcp.json`, slash commands

**Source spec:** `.planning/specs/2026-06-10-mcp-plugin-design.md` (approved).

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- **Release Docs milestone (Phases 1–3) complete** (2026-06-06): changelog + release notes + blog generators, release-body/CHANGELOG-PR/pages publishers, webhook auto-trigger + DB state, and code API docs / OpenAPI (committed-spec ingestion → offline Redoc HTML + deterministic Markdown, published to pages). Idempotent across re-runs; per-artifact toggles honored. Validated in Phase 3: API Docs / OpenAPI.

The existing review pipeline ships separately and is the platform this builds on.

### Active

<!-- Current scope. Building toward these (MCP Server + Claude Code Plugin milestone). REQ-IDs defined in REQUIREMENTS.md. -->

- [ ] MCP server binary (`cadoo-mcp`, stdio transport) advertising Cadoo tools as MCP tools
- [ ] Local diff review returned inline (working tree / staged / ref range)
- [ ] Live PR/MR review — dry-run and idempotent post-back (GitHub, GHES, GitLab)
- [ ] Configurable tool surface (enable/disable; safe default core set)
- [ ] Connected mode via `cadoo-api` (KB + learnings; `learn`/`unlearn` exposure)
- [ ] Claude Code plugin (manifest, `.mcp.json`, slash commands) + setup docs for Cursor/Claude Desktop

### Out of Scope

<!-- Explicit boundaries. Includes reasoning to prevent re-adding. -->

- Real-time / continuous review while editing — MCP is request/response; file-watching belongs to editor hooks. Follow-up, not this milestone.
- Native VS Code / JetBrains extensions — future consumers of the same `cadoo-mcp` binary.
- Modifying `tools.Tool` / `tools.Input` / `tools.Result` — the MCP server adapts *to* those interfaces, not the reverse.
- OAuth flows for VCS auth — PATs only this milestone (env vars / config / per-invocation).
- Multi-tenant hosting of `cadoo-mcp` itself — it runs per-developer; connected mode reaches the multi-tenant backend via `cadoo-api`.
- New LLM provider code paths in Go — all multi-provider routing stays in the LiteLLM sidecar.
- Single-tenant shortcuts — `org_id` is carried throughout; self-host is a degenerate single-org tenant.

## Context

**Technical environment (grounded in `.planning/codebase/`):** Go 1.26, five binaries under `cmd/` (`cadoo-webhook`, `cadoo-worker`, `cadoo-api`, `cadoo-cli`, `cadoo-tunnel`). PostgreSQL 16 + pgvector. River (Postgres) job queue with an in-memory fallback when `DATABASE_URL` is unset (dual-mode). LLM access goes through a LiteLLM sidecar over an OpenAI-compatible HTTP API. VCS adapters live behind the `vcs.Provider` interface (`internal/vcs/github`, `internal/vcs/gitlab`).

**Reused primitives (this milestone):** `cadoo-mcp` mirrors `cadoo-cli` CI-mode — stateless, no DB, dedup reconstructed via `PriorReviewReader` + `<!-- cadoo:fp … -->` markers, consolidated comment format from `internal/orchestrator/consolidate.go`, diffs packed via `internal/contextengine`, LLM through the LiteLLM gateway. The MCP *server* lives in a new `internal/mcpserver` package — the existing `internal/mcp` package is an MCP *client* (Cadoo consuming external servers) and stays untouched.

**Conventions to honor:** `.cadoo.yaml` is loaded from the release tag's tree (consistent with the existing "config from head, never main" rule). Exported symbols need docstrings (`exported` revive rule). `goimports` local-prefix grouping (`github.com/payamqorbanpour/cadoo`). `make ci` (vet + test + build) must stay green; any new migration must round-trip `up → down → up`.

**Source spec:** `.planning/specs/2026-06-10-mcp-plugin-design.md` (approved design for v2.0). Prior milestone spec: `docs/superpowers/specs/2026-06-04-release-docs-design.md`.

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
| Three-phase delivery: CLI → webhook+state → API docs | Ship dogfoodable CLI value first; defer webhook/state and OpenAPI complexity | ✓ Good (v1.0 shipped this way) |
| MCP server in new `internal/mcpserver`, separate from `internal/mcp` client | Server and client are different concerns; client (Phase 6 work) stays untouched | — Pending (proposed) |
| Use official `modelcontextprotocol/go-sdk` for protocol layer | Avoid hand-rolling JSON-RPC; fallback to minimal stdio server if SDK unsuitable | — Pending (proposed) |
| Embedded mode default, connected mode opt-in via `api-url` | Zero-infra setup for individual devs; backend only when KB/learnings wanted | — Pending (proposed) |
| Three-phase delivery: local review → live PR → connected mode | Dogfoodable on Cadoo's own repo from phase 1; each phase releasable | — Pending (proposed) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-06-10 — Milestone v2.0 (MCP Server + Claude Code Plugin) started.*
