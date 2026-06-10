# Feature Research

**Domain:** MCP Server + Claude Code Plugin for AI Code Review (cadoo-mcp milestone)
**Researched:** 2026-06-10
**Confidence:** HIGH (spec is approved; competitive patterns confirmed via direct source inspection)

---

## Context

This research covers the NEW feature surface only: exposing Cadoo's existing 13 review tools
via MCP to AI assistants (Claude Code, Cursor, Claude Desktop). The underlying review pipeline,
VCS adapters, dedup logic, KB, and learnings already exist. The question is: what UX conventions,
input shapes, result formats, tool surfaces, and invocation patterns must the MCP layer honor to
feel complete, and what should be deliberately avoided?

Competitors inspected: Claude Code's own code-review plugin (official), GitHub MCP server
(official), Orcus2021/code-review-mcp-server (community), praneybehl/code-review-mcp
(community), Qodo Gen MCP tooling (docs), CodeRabbit MCP integration (blog + docs).

---

## Feature Landscape

### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Local diff review returned inline | Every community code-review MCP (praneybehl, dshills, Orcus2021) supports staged/HEAD/range local review as the primary workflow. Users expect results in the conversation, not posted elsewhere. | LOW | Reuses `contextengine` + CI-mode path. `target=local` with staged/HEAD/range. |
| PR/MR review by URL (dry-run) | GitHub MCP server, CodeRabbit, and all community tools support fetching a PR by URL and returning analysis inline without posting. Dry-run is the safe default that users trust before committing to post. | MEDIUM | `target=pr, post=false`. Fetches via existing `vcs.Provider` adapters. |
| PR/MR review with post-back | The whole point of an agentic code reviewer is that results land on the PR for team visibility. Claude Code's own `/code-review --comment` flag and GitHub MCP `pull_request_review_write` confirm this is expected. | MEDIUM | `target=pr, post=true`. Reuses CI-mode idempotent posting via `PriorReviewReader` + wrapper markers. |
| MCP tool names follow `cadoo_<tool>` snake_case convention | Over 90% of MCP tools use snake_case. Claude agent SDK exposes them as `mcp__<server>__<tool>`. Namespacing by service (e.g., `cadoo_review`, `cadoo_ask`) is the dominant ecosystem pattern for agent tool selection. | LOW | Confirmed by MCP tool naming research and AWS design guidelines. |
| Shared input schema across tools | Community implementations that expose many review tools converge on a shared parameter envelope (target, url, range, post). Users should not learn a different schema per tool. | LOW | Already specified in approved design spec (§2.2). |
| Setup docs for Claude Code, Cursor, Claude Desktop | All three clients have distinct config file locations and formats (`.mcp.json` for Claude Code plugin, `~/.cursor/mcp.json` for Cursor, `~/Library/Application Support/Claude/claude_desktop_config.json` for Desktop). Missing docs for any = users fail setup. | LOW | Docs task, not code. No new surface. |
| Token passthrough via env vars | Unanimous pattern: `GITHUB_TOKEN`, `GITLAB_TOKEN` in the `env` block of MCP server config. Users expect to set env vars, not OAuth flows. GitHub MCP server, all community tools use this exact convention. | LOW | Already in approved spec. No new surface. |
| Error messages with setup hints | When token is missing, MCP clients surface raw errors. Users expect "set GITHUB_TOKEN env var" not a stack trace. Claude Code's own `/code-review` and the approved spec both call this out. | LOW | Error handler in `internal/mcpserver`. Short strings. |
| Claude Code plugin with slash commands | Claude Code users expect `/cadoo:review`, `/cadoo:describe`, etc. as first-class slash commands — not just raw MCP tool calls. The plugin system (`plugin.json` + `.mcp.json` + `commands/` or `skills/`) is the official distribution mechanism. | MEDIUM | Plugin manifest + skills per core tool. Commands invoke MCP tools with sensible defaults (target=local by default). |
| Configurable enabled tool set | Users want to control which tools are advertised. A minimal default (review, describe, improve, ask) avoids overwhelming Cursor's ~40-tool cap or polluting the agent's tool list with rarely-used tools. Cursor has a hard ceiling of ~40 active tools across all MCP servers. | LOW | Config-driven filtering at `tools/list` response time. Already in approved spec. |

### Differentiators (Competitive Advantage)

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Idempotent multi-path posting (webhook + CI + MCP = no duplicate comments) | No competitor handles this. GitHub MCP server posts fresh comments every time. Cadoo's `PriorReviewReader` + `<!-- cadoo:fp -->` fingerprint markers mean webhook, CI-mode, and MCP can all run against the same PR without duplicating findings. | LOW (reuse) | Zero new code — this is already the CI-mode contract. The differentiator is that it works out of the box. |
| Unified `ask` tool for interactive Q&A against a PR/diff | Competitors (GitHub MCP, CodeRabbit) do not expose a conversational Q&A tool against the current diff context. Cadoo's existing `ask` tool enables "why did this change?" or "is this safe to deploy?" queries against packed diff context. | LOW (reuse) | Depends on `cadoo_ask` being in the default tool set and having a `question` parameter. |
| Connected mode: KB + learnings in MCP results | When `api-url` is configured, MCP results include team-specific KB hits and prior learnings. No community MCP code reviewer has organizational memory. Only CodeRabbit's enterprise tier approaches this via external MCP server connections. | HIGH | Phase 3 only. Requires new sync endpoint on `cadoo-api`. Depends on existing `internal/kb` + `internal/learnings`. |
| `learn`/`unlearn` tools to update team memory from the conversation | Developers can say "this pattern is acceptable in our codebase" and the MCP server records it to KB/learnings via connected mode. No competitor has this workflow. | HIGH | Connected mode only (Phase 3). Depends on `cadoo-api` sync endpoint. |
| Progress notifications for long reviews | `deep_review` can take minutes. Using the go-sdk's `req.ReportProgress` to send heartbeat notifications prevents timeout disconnects and shows progress in the client. The MCP go-sdk v1.6.1 supports this via `ProgressToken`. | MEDIUM | Needed especially for `deep_review` and `add_tests`. Mitigates the main latency risk identified in the spec (§8). |
| Per-invocation `question` param on `cadoo_ask` | Unlike raw LLM tools, `cadoo_ask` combines the question with the full packed-diff context, giving answers grounded in the actual change rather than hallucinated opinions. | LOW (reuse) | Input schema already differentiates `cadoo_ask` with a `question` field. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Auto-post on every file save / continuous background review | Real-time feedback feels powerful. Competitors market "instant review." | MCP is request/response. File-watching belongs to editor hooks (Claude Code `FileChanged` hook or `PostToolUse` hook). Auto-posting without user intent creates noise on PRs and burns LLM budget on every keystroke. Already explicitly out of scope in the spec. | Ship a `PostToolUse` hook example in the plugin that users can opt into on `Write`/`Edit`. |
| OAuth flow for VCS auth | OAuth is more "enterprise-friendly" in the abstract. | Per-developer MCP runs are short-lived processes with no redirect URI. OAuth adds a browser handoff, token refresh complexity, and a credential store — all out of scope and unnecessary when PATs work identically. | Document PAT scopes required (`repo` for GitHub, `api` for GitLab) in setup docs. |
| Multi-tenant hosted `cadoo-mcp` binary | "Just give me a URL to connect to, no binary install." | Hosting cadoo-mcp per-user is a different deployment model (SaaS subscription, auth, isolation). Connected mode already reaches the multi-tenant backend via `cadoo-api`. | The MCP binary runs per-developer. Connected mode is the right answer for users who want zero-infra. |
| Streaming partial review results mid-call | Users want to see findings as they are discovered, not wait for the full review. | The MCP `tools/call` protocol returns a single response. True streaming requires the Tasks primitive (long-running operations spec) or SSE transport, both of which are not in Phase 1–2 scope and add significant complexity. The go-sdk v1.6.1 does not fully support this for the 2025-11-25 spec. | Use progress notifications (heartbeat, percentage) to indicate liveness without true streaming. Document that `deep_review` should be run with generous timeout config. |
| Advertising all 13 tools by default | Power users want everything available. | Cursor's hard ceiling is ~40 active tools across all MCP servers. Advertising all 13 cadoo tools plus GitHub MCP's tools plus filesystem tools exceeds the limit. Users who configure multiple MCP servers get silent tool loss. | Default to 4 core tools (review, describe, improve, ask). Users explicitly enable extras in config. |
| Per-call VCS token injection via tool arguments | Flexibility for multi-account setups. | Leaks tokens into conversation history and MCP call logs. The MCP spec does not have a secure parameter channel for secrets. | Tokens via env vars only. Per-call token injection is a security anti-pattern — the spec already rules it out as a fallback path (§3), not the primary path. |
| Separate tool per review target (`cadoo_review_local`, `cadoo_review_pr`) | Avoids a `target` parameter, feels more discoverable. | Doubles the tool count (13 tools → 26), fragment the tool list, and conflicts with the "default 4, configure more" model. | Single tool per review command with `target` enum. Tool descriptions explain both modes. |

---

## Feature Dependencies

```
cadoo_review (target=local)
    └──requires──> internal/contextengine (local diff packing)
    └──requires──> internal/tools (review tool)
    └──requires──> LiteLLM endpoint reachable

cadoo_review (target=pr, post=false)
    └──requires──> cadoo_review (target=local) ──> (shared infra)
    └──requires──> vcs.Provider (GitHub/GHES/GitLab token in env)

cadoo_review (target=pr, post=true)
    └──requires──> cadoo_review (target=pr, post=false)
    └──requires──> vcs.PriorReviewReader (dedup)
    └──requires──> internal/orchestrator/consolidate.go (wrapper markers)

cadoo_ask
    └──requires──> cadoo_review (target=local) ──> (same infra)
    └──adds──> question parameter (distinct from other tools)

cadoo_learn / cadoo_unlearn
    └──requires──> connected mode (api-url set)
    └──requires──> cadoo-api sync endpoint (NEW, Phase 3)
    └──requires──> internal/kb + internal/learnings (already exist)

connected mode
    └──requires──> cadoo-api sync endpoint (NEW, Phase 3)
    └──enhances──> all tools (adds KB hits + learnings to results)

Claude Code plugin
    └──requires──> cadoo-mcp binary installed and in PATH
    └──requires──> .mcp.json referencing binary with env passthrough
    └──enhances──> user experience (slash commands vs raw tool invocation)

progress notifications
    └──requires──> go-sdk v1.6.1 (ReportProgress support confirmed)
    └──enhances──> deep_review, add_tests (long-running tools)
    └──mitigates──> MCP client timeout disconnects
```

### Dependency Notes

- **Local review requires LiteLLM:** The embedded mode needs a reachable LiteLLM endpoint. Docs must clarify this is part of zero-infra setup — users need to run the sidecar or configure a public endpoint.
- **Post-back requires PR-mode first:** Implementing `post=true` before `post=false` is working would be a mistake. Dry-run validates the pipeline end-to-end (fetch → pack → review → render) before adding the posting side-effect.
- **Connected mode is independent of local/PR modes:** Whether a call is `target=local` or `target=pr` is orthogonal to embedded vs connected mode. Both modes support both targets; connected adds KB/learnings on top.
- **learn/unlearn conflict with embedded mode:** These tools require the KB backend. They should be withheld from `tools/list` entirely when `api-url` is not set, not just return an error at call time — the spec (§2.2) already states this.
- **Plugin slash commands are thin wrappers:** They instruct Claude to call the MCP tool with defaults. They do not add review logic. The MCP tools must work without the plugin for Cursor/Desktop users.

---

## MVP Definition

### Launch With (Phase 1 — embedded local review)

- [x] `cmd/cadoo-mcp` binary with stdio transport
- [x] `cadoo_review`, `cadoo_describe`, `cadoo_improve`, `cadoo_ask` tools advertised
- [x] `target=local` with staged/unstaged/range diffs
- [x] Results returned inline as markdown in the MCP tool response
- [x] Token passthrough via env vars (`GITHUB_TOKEN`, `GITLAB_TOKEN`)
- [x] Claude Code plugin: `plugin.json`, `.mcp.json`, skills for core 4 tools
- [x] Error messages with setup hints (missing token, unreachable LLM)
- [x] Setup docs for Claude Code, Cursor, Claude Desktop

### Add After Validation (Phase 2 — live PR/MR)

- [ ] `target=pr, post=false` (dry-run PR review by URL)
- [ ] `target=pr, post=true` (idempotent post-back, dedup via PriorReviewReader)
- [ ] GitHub.com + GHES + GitLab support (existing adapters, no new code)
- [ ] Progress notifications for long-running tools (go-sdk `ReportProgress`)
- [ ] Remaining tools beyond core 4 available via config

### Future Consideration (Phase 3 — connected mode)

- [ ] `cadoo-api` sync tool endpoint (new backend surface, needs auth + rate limiting)
- [ ] Connected mode (`--api-url`) forwarding to cadoo-api
- [ ] `cadoo_learn` / `cadoo_unlearn` tools (connected mode only)
- [ ] KB hits + learnings surfaced in all tool results via connected mode

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| `cadoo_review` target=local | HIGH | LOW (CI-mode reuse) | P1 |
| Claude Code plugin + slash commands | HIGH | LOW (thin wrapper) | P1 |
| Setup docs (3 clients) | HIGH | LOW (docs only) | P1 |
| Error messages with setup hints | HIGH | LOW | P1 |
| `cadoo_ask` target=local | HIGH | LOW (existing tool) | P1 |
| `cadoo_describe` + `cadoo_improve` target=local | MEDIUM | LOW | P1 |
| `target=pr, post=false` (dry-run) | HIGH | MEDIUM | P2 |
| `target=pr, post=true` (idempotent post) | HIGH | MEDIUM (dedup reuse) | P2 |
| Progress notifications | MEDIUM | MEDIUM | P2 |
| Configurable tool surface (enable/disable) | MEDIUM | LOW | P1 |
| Connected mode (cadoo-api) | HIGH | HIGH (new endpoint) | P3 |
| `cadoo_learn` / `cadoo_unlearn` | MEDIUM | HIGH (connected dep) | P3 |

---

## Competitor Feature Analysis

| Feature | GitHub MCP Server | Claude Code code-review plugin | Community code-review MCP (praneybehl, Orcus2021) | Cadoo MCP (this milestone) |
|---------|-------------------|-------------------------------|--------------------------------------------------|---------------------------|
| Local diff review | No | Yes (slash command) | Yes (primary workflow) | Yes (target=local, Phase 1) |
| PR review by URL | Yes (fetch diff, get files) | Yes (reads PR context) | Partial (Orcus2021 via GitHub URL) | Yes (target=pr, Phase 2) |
| Post review comments to PR | Yes (pull_request_review_write) | Yes (--comment flag) | Yes (Orcus2021) | Yes (post=true, Phase 2) |
| Idempotent re-posting | No (new comment each time) | No | No | Yes (PriorReviewReader + fp markers) |
| Multiple specialized review tools | No (1 review tool) | No (1 review command) | No (1-7 tools, no specialization) | Yes (13 tools: review, ask, describe, improve, ...) |
| Interactive Q&A against diff | No | No | No | Yes (cadoo_ask with question param) |
| Organizational memory / KB | No | No | No | Yes (connected mode, Phase 3) |
| Progress notifications | No | No | No | Yes (go-sdk ReportProgress, Phase 2) |
| GitLab support | No | No | No | Yes (existing adapter, Phase 2) |
| Configurable tool set | No | No | No | Yes (enabled list in config) |
| Claude Code plugin | N/A (VCS tool, not review) | Built-in | No | Yes (plugin.json + skills) |
| Cursor / Desktop setup docs | Yes | N/A | Partial | Yes |

---

## Input Shape Conventions (confirmed from ecosystem research)

The approved spec's shared input schema aligns perfectly with ecosystem conventions:

```jsonc
{
  "target": "pr | local",           // present in all community tools
  "url": "https://…/pull/42",       // standard for PR-mode tools
  "range": "HEAD~3..HEAD",          // standard for local tools (staged/HEAD/branch_diff)
  "post": false,                    // dry-run default is the ecosystem norm
  "question": "…"                   // cadoo_ask only; keeps schema uniform
}
```

`range` defaults to staged + unstaged if omitted — matches community tools that default to
"current working state." Requiring explicit `range` would be an anti-pattern.

## Result Format Convention (confirmed)

All inspected MCP code review tools return markdown as `text` content in the MCP tool response.
No tool returns structured JSON findings. Structured JSON would prevent the response from being
read directly in the conversation. Markdown is the table-stakes format.

The Cadoo `tools.Result` already renders to markdown via `internal/orchestrator/consolidate.go`.
The MCP layer just needs to pass that rendered string as the tool response content.

---

## Sources

- [Claude Code Plugins Reference](https://code.claude.com/docs/en/plugins-reference) — plugin.json schema, MCP server config in plugins, slash command conventions. HIGH confidence.
- [Claude Code code-review plugin README](https://github.com/anthropics/claude-code/blob/main/plugins/code-review/README.md) — /code-review vs /code-review --comment pattern. HIGH confidence.
- [GitHub MCP Server](https://github.com/github/github-mcp-server) — pull_request_review_write, get_diff, get_review_comments tool surface. HIGH confidence.
- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) — v1.6.1, maintained with Google, progress notifications via ReportProgress, stdio transport. HIGH confidence.
- [Orcus2021/code-review-mcp-server](https://github.com/Orcus2021/code-review-mcp-server) — 7-tool surface (CodeReview, GetLocalGitDiff, CodeReviewWithGithubUrl, AddPRSummaryComment, AddPRLineComment, GetPRTemplate, CreatePR). MEDIUM confidence (community).
- [praneybehl/code-review-mcp](https://github.com/praneybehl/code-review-mcp) — single `perform_code_review` tool, target enum (staged/HEAD/branch_diff). MEDIUM confidence (community).
- [CodeRabbit MCP blog](https://www.coderabbit.ai/blog/coderabbits-mcp-server-integration-code-reviews-that-see-the-whole-picture) — CodeRabbit acts as MCP client, not server; ingests external MCP context. MEDIUM confidence.
- [MCP tool naming conventions (HasMCP glossary)](https://hasmcp.com/glossary/tool-naming-conventions) — snake_case dominant (90%+), prefix_action pattern. MEDIUM confidence.
- [Cursor MCP docs](https://cursor.com/docs/cli/mcp) — .cursor/mcp.json structure, ~40 tool ceiling, env vars in config. HIGH confidence.
- [Claude Code issue #58687 — MCP timeout with progress](https://github.com/anthropics/claude-code/issues/58687) — confirmed timeout behavior and heartbeat pattern. MEDIUM confidence.
- [MCP go-sdk progress notification example](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp) — req.ReportProgress API confirmed. HIGH confidence.

---

*Feature research for: cadoo-mcp MCP Server + Claude Code Plugin milestone*
*Researched: 2026-06-10*
