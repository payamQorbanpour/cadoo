# Requirements: Cadoo — MCP Server + Claude Code Plugin (v2.0)

**Defined:** 2026-06-10
**Core Value:** A developer working inside an AI assistant can invoke Cadoo's review tools from the conversation — review a local diff inline, or run a tool against a live PR/MR and post results back to GitHub/GHES/GitLab idempotently — without leaving the editor.

> Source: `.planning/specs/2026-06-10-mcp-plugin-design.md` (approved SPEC) + `.planning/research/` (4-dimension research, HIGH confidence). Requirements MCP-03/04/05 and PR-03 are research-mandated hard constraints, not nice-to-haves: clients cache schemas, disconnect at ~60s without progress, break on stdout pollution, and `post=true` is a confused-deputy vector without an allowlist.

## v1 Requirements

Requirements for the MCP Server + Claude Code Plugin milestone. Each maps to roadmap phases.

### MCP Server Core

- [ ] **MCP-01**: Developer can connect an MCP client (Claude Code, Cursor, Claude Desktop) to `cadoo-mcp` over stdio and list the advertised Cadoo tools.
  - Acceptance: `initialize` → `tools/list` → `tools/call` round-trips against a real client; built on `github.com/modelcontextprotocol/go-sdk` (fallback: minimal hand-rolled stdio server if the SDK proves unsuitable at plan time).

- [ ] **MCP-02**: User can configure which tools are advertised (enable/disable); default core set is `review`, `describe`, `improve`, `ask`.
  - Acceptance: disabled tools are absent from `tools/list`; calling one anyway returns a tool-not-found error. Default-4 keeps Cadoo under Cursor's ~40-tool ceiling.

- [ ] **MCP-03**: Tool input schemas are strict (enums, required fields, URL format) so clients cannot hallucinate arguments.
  - Acceptance: `target` is an enum (`pr`|`local`); `url` required iff `target=pr`; unknown fields rejected; schema derived from Go structs (SDK inference).

- [ ] **MCP-04**: Long-running tool calls emit MCP progress notifications so clients don't time out.
  - Acceptance: a review exceeding the client's default timeout (~60s) completes successfully in Claude Code because progress heartbeats are emitted at pipeline checkpoints; `deep_review` stays out of the default tool set.

- [ ] **MCP-05**: All logging goes to stderr; stdout carries only JSON-RPC frames.
  - Acceptance: no code path (including go-github/go-gitlab and any logger) writes non-protocol bytes to stdout; verified by a framing round-trip test with logging enabled.

- [ ] **MCP-06**: User can configure tokens/endpoints via config file + env vars with documented precedence; token values are never logged.
  - Acceptance: precedence is per-invocation args → env vars → config file; `GITHUB_TOKEN`/`GITLAB_TOKEN`/`LITELLM_API_KEY` honored; redaction verified in error paths.

### Local Review

- [ ] **LOCAL-01**: User can review working-tree/staged changes (`target=local`) and get findings returned inline.
  - Acceptance: `cadoo_review` with no `url` diffs the working tree, packs via `contextengine`, runs the tool, and returns rendered markdown in the MCP response.

- [ ] **LOCAL-02**: User can review an arbitrary ref range (`range: A..B`).
  - Acceptance: `range: "HEAD~3..HEAD"` reviews exactly those commits' diff; invalid refs return a clear error.

- [ ] **LOCAL-03**: Local reviews post nothing to any VCS — only the LLM call leaves the machine.
  - Acceptance: no `vcs.Provider` network calls in the `target=local` path; verified by a test with a panicking provider stub.

### Live PR/MR Review

- [ ] **PR-01**: User can run a tool against a PR/MR URL with results returned inline (dry-run is the default).
  - Acceptance: `target=pr, post=false` (default) fetches the PR, runs the tool, returns rendered results; nothing is posted.

- [ ] **PR-02**: User can opt in to `post=true` — posting is idempotent with the webhook and CI paths (no duplicate comments, same wrapper markers).
  - Acceptance: a PR reviewed alternately via webhook, `cadoo ci`, and MCP produces one consolidated comment edited in place; inline findings dedup via `PriorReviewReader` + `<!-- cadoo:fp … -->` fingerprints.

- [ ] **PR-03**: `post=true` is gated by an `allowed_repos` allowlist and URL host validation.
  - Acceptance: posting to a repo not on the allowlist fails closed with a config hint; URL host must match the configured providers (confused-deputy defense).

- [ ] **PR-04**: GitHub.com, GHES, and GitLab PR/MR URLs all resolve to the correct provider.
  - Acceptance: `github.com/...`, configured GHES host, and `gitlab.com`/self-managed MR URLs each route to the right `vcs.Provider`; unknown hosts rejected.

### Connected Mode

- [ ] **CONN-01**: User can point `cadoo-mcp` at a `cadoo-api` deployment so reviews include KB + learnings.
  - Acceptance: with `api-url` set, tool calls are forwarded to the backend and results include KB/learnings context; without it, embedded mode runs (no silent fallback between modes).

- [ ] **CONN-02**: `learn`/`unlearn` are advertised only in connected mode.
  - Acceptance: embedded-mode `tools/list` never includes them; connected-mode does (when enabled by config).

- [ ] **CONN-03**: `cadoo-api` gains a synchronous tool-run endpoint with auth and rate limiting.
  - Acceptance: endpoint authenticates the caller, enforces rate limits, survives long reviews (streaming/chunked progress — strategy decided at phase planning), carries `org_id` throughout.

### Plugin & Distribution

- [ ] **PLUG-01**: Claude Code user can install the Cadoo plugin; `/cadoo:review` with no args reviews the working tree.
  - Acceptance: `plugins/claude/` ships `.claude-plugin/plugin.json`, `.mcp.json` (stdio server registration, env passthrough for tokens), and slash commands for the core tools.

- [ ] **PLUG-02**: Cursor and Claude Desktop setup is documented (manual MCP config).
  - Acceptance: docs verified end-to-end against a scratch GitHub repo and a GitLab project.

- [ ] **PLUG-03**: GoReleaser ships `cadoo-mcp` as the sixth binary.
  - Acceptance: `make build` produces `bin/cadoo-mcp`; release pipeline publishes it alongside the existing five.

## v2 Requirements

Deferred beyond this milestone. Tracked but not in the current roadmap.

- **Real-time/continuous review while editing** — editor-hook territory (e.g. Claude Code PostToolUse hooks invoking the same MCP tools); revisit after v2.0 ships.
- **HTTP/SSE transport for `cadoo-mcp`** — structural fix for stdio process proliferation under parallel agents; stdio + advisory lock suffices for v2.0.
- **OAuth flows for VCS auth** — PATs only this milestone.

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Native VS Code / JetBrains extensions | Future consumers of the same binary; not this milestone |
| Modifying `tools.Tool` / `tools.Input` / `tools.Result` | The MCP server adapts *to* those interfaces, not the reverse |
| Extending `internal/mcp` for the server | That package is an MCP *client* (Phase 6 work); server is `internal/mcpserver` |
| Advertising all 13 tools by default | Cursor's ~40-tool ceiling across servers; configurable instead |
| Multi-tenant hosting of `cadoo-mcp` itself | Runs per-developer; connected mode reaches the multi-tenant backend via `cadoo-api` |
| New LLM provider code paths in Go | Multi-provider routing stays in the LiteLLM sidecar |

## Traceability

Filled by roadmap creation. Each requirement is assigned to its first delivering phase.

| Requirement | Phase | Status |
|-------------|-------|--------|
| (pending roadmap) | | |

**Coverage:**
- v1 requirements: 19 total
- Mapped to phases: 0 (pending roadmap)
- Unmapped: 19

---
*Requirements defined: 2026-06-10*
