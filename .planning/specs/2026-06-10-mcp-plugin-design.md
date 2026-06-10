# Cadoo MCP Server + Claude Code Plugin — Design Spec

**Date:** 2026-06-10
**Status:** Approved design, ready for implementation planning
**Topic:** Expose Cadoo's review tools to AI assistants (Claude Code, Claude Desktop, Cursor, any MCP client)

## 1. Summary

Make Cadoo usable directly from AI coding assistants by adding:

1. **`cmd/cadoo-mcp`** — a sixth binary: an MCP *server* (stdio transport first,
   streamable HTTP later) that exposes Cadoo's review tools as MCP tools.
2. **A Claude Code plugin** — a thin distribution layer: plugin manifest registering
   the MCP server plus slash commands (`/cadoo:review`, `/cadoo:describe`, …) that
   map onto the MCP tools.

The server reuses the existing internal packages (`internal/orchestrator`,
`internal/tools`, `internal/vcs`, `internal/contextengine`) the same way
`cadoo-cli` CI-mode does — the internal packages *are* the shared SDK. No new
review logic is written; this is a new entry point, not a new pipeline.

**Note:** the existing `internal/mcp` package is an MCP *client* (Cadoo consuming
external MCP servers, Phase 6 work). The server side is a separate concern and
lives in its own package; the client package is untouched.

### Goals

- Invoke Cadoo tools from a conversation in any MCP-compatible client.
- **Two workflows:** local review (diff/working tree, results returned inline,
  nothing posted) and live PR/MR review (given a URL, post results back to
  GitHub / GHES / GitLab, idempotently).
- **Two operating modes:** embedded (default — stateless, no DB/backend, same
  path as CI-mode) and connected (proxy through a running Cadoo deployment so
  KB + learnings apply).
- **Configurable tool surface:** users enable/disable tools; disabled tools are
  not advertised to the client. Default core set: `review`, `describe`,
  `improve`, `ask`.
- Auth via tokens: env vars (`GITHUB_TOKEN`, `GITLAB_TOKEN`), MCP server config,
  or per-invocation input. No new credential store.
- VCS scope: GitHub.com + GHES + GitLab (matches existing adapters).

### Non-goals

- **Real-time / continuous review while editing.** MCP is request/response;
  file-watching belongs to editor hooks (e.g. Claude Code PostToolUse hooks
  invoking the same MCP tools). Tracked as a follow-up, not in this spec.
- Native VS Code / JetBrains extensions (a future consumer of the same binary).
- New VCS providers.
- Extending or modifying `tools.Tool` / `tools.Input` / `tools.Result` — the MCP
  server adapts *to* those interfaces, not the reverse.
- Multi-tenant server hosting of `cadoo-mcp` itself (it runs per-developer;
  connected mode reaches the multi-tenant backend via `cadoo-api`).

## 2. Architecture

```
cmd/cadoo-mcp/            // main: flag parsing, transport selection, wiring
internal/mcpserver/
  server.go               // MCP server core: initialize, tools/list, tools/call
  transport_stdio.go      // newline-delimited JSON-RPC over stdin/stdout
  toolmap.go              // tools.Tool -> MCP tool descriptor (name, inputSchema)
  embedded.go             // embedded mode: build tools.Input locally, run tool
  connected.go            // connected mode: forward to cadoo-api REST
  localdiff.go            // working-tree / staged / ref-range diff -> tools.Input
  config.go               // server config: enabled tools, mode, tokens, endpoint
plugins/claude/           // Claude Code plugin (manifest, .mcp.json, commands/)
```

### 2.1 Server library choice

Use the official `github.com/modelcontextprotocol/go-sdk` for the server
protocol layer rather than hand-rolling JSON-RPC or extending the homegrown
`internal/mcp` client. If the SDK's dependency footprint or API stability is a
problem at implementation time, fall back to a minimal hand-rolled stdio server
(the protocol surface needed — `initialize`, `tools/list`, `tools/call` — is
small). This is the implementation plan's first decision point.

### 2.2 Tool surface

Each enabled Cadoo tool is advertised as an MCP tool named `cadoo_<tool>`
(`cadoo_review`, `cadoo_describe`, …). The input schema is shared across tools:

```jsonc
{
  "target": "pr | local",            // what to review
  "url": "https://…/pull/42",        // required when target=pr (PR or MR URL)
  "range": "HEAD~3..HEAD",           // optional when target=local; default: staged + unstaged changes
  "post": false,                     // target=pr only: post results back to the VCS (default false = return inline)
  "question": "…"                    // cadoo_ask only
}
```

- `target=local` — diff computed from the working tree (or `range`), packed via
  `contextengine`, tool runs, result rendered as markdown and returned in the
  MCP tool response. Nothing leaves the machine except the LLM call.
- `target=pr, post=false` — fetch the PR via the provider, run the tool, return
  the rendered result inline (dry-run review).
- `target=pr, post=true` — same, then post through the provider exactly like
  CI-mode: consolidated comment edited in place, inline findings deduped via
  `vcs.PriorReviewReader` + fingerprint markers, check-runs if the tool emits
  them. Never bypasses the wrapper-marker format from
  `internal/orchestrator/consolidate.go`.

`learn`/`unlearn` are only advertised in connected mode (they require the KB).
Tools that make no sense without a PR context (`resolve_conflicts`) reject
`target=local` with a clear error.

### 2.3 Operating modes

**Embedded (default).** No DB, no backend. Mirrors `cadoo ci`:
`.cadoo.yaml` loaded from PR head (live) or working tree (local), dedup state
reconstructed from prior comments via `PriorReviewReader`, LLM reached through
the configured LiteLLM endpoint. KB/learnings inputs are nil (the pipeline is
already nil-tolerant).

**Connected.** `--api-url` + `--api-token` point at a `cadoo-api` deployment.
Tool calls are forwarded as REST requests; the backend runs the full pipeline
including KB and learnings, and the MCP server relays the result. Requires a
(possibly new) `cadoo-api` endpoint that runs a tool synchronously and returns
the rendered result — that endpoint is part of this feature's scope.

Mode is chosen by config, not per-call: if `api-url` is set, connected; else
embedded.

### 2.4 Claude Code plugin

`plugins/claude/` contains:

- `.claude-plugin/plugin.json` — plugin manifest.
- `.mcp.json` — registers `cadoo-mcp` as a stdio server (command + env passthrough
  for tokens).
- `commands/` — slash commands, one per core tool, that instruct Claude to call
  the corresponding MCP tool with sensible defaults (e.g. `/cadoo:review` with
  no args → `target=local` on the current working tree).

Cursor and Claude Desktop need no plugin — users add the server to their MCP
config directly; docs cover all three setups.

## 3. Configuration & auth

Precedence (highest wins): per-invocation tool arguments → environment
variables → server config file.

```yaml
# ~/.config/cadoo/mcp.yaml (or --config flag)
tools:
  enabled: [review, describe, improve, ask]   # only these are advertised
llm:
  endpoint: http://localhost:4000             # LiteLLM
  api_key_env: LITELLM_API_KEY
vcs:
  github_token_env: GITHUB_TOKEN
  gitlab_token_env: GITLAB_TOKEN
  ghes_url: ""                                # optional GHES base URL
connected:
  api_url: ""                                 # set => connected mode
  api_token_env: CADOO_API_TOKEN
```

Per-repo `.cadoo.yaml` continues to govern review behavior (loaded from PR head
for live reviews, from the working tree for local reviews). Tokens are PATs —
no OAuth flow in this iteration. The server never logs token values.

## 4. Idempotency

Live PR posting reuses the CI-mode dedup contract wholesale: hidden
`<!-- cadoo:fp … -->` fingerprints, `PriorReviewReader` reconstruction, overview
edited in place, fixed threads resolved. A PR reviewed alternately by webhook,
CI-mode, and MCP must not produce duplicate comments — all three speak the same
marker format. Local reviews are stateless by definition.

## 5. Error handling

- Missing/invalid token: tool call returns an MCP error result with a setup hint
  (which env var to set), never a stack trace.
- LiteLLM unreachable: explicit "LLM endpoint unreachable at <url>" error.
- Connected mode backend down: error suggests embedded mode as fallback; no
  silent mode switching.
- Tool disabled by config but called anyway (client cache): JSON-RPC method-not-
  found-equivalent tool error.
- Context overflow on huge diffs: same compression/truncation behavior as the
  existing pipeline (contextengine handles it); the result notes truncation.

## 6. Testing

- `internal/mcpserver` unit tests: toolmap schema generation, config precedence,
  enabled-tool filtering, stdio framing round-trip (in-memory pipes).
- Embedded-mode integration test: fake `vcs.Provider` + fake LLM, assert a
  `cadoo_review(target=pr, post=true)` call produces the same posted artifacts
  as the equivalent CI-mode run (golden comparison against the wrapper format).
- Protocol conformance: initialize → tools/list → tools/call happy path against
  a real client harness (the Go SDK ships one) in `make test`.
- Manual acceptance: Claude Code + Cursor setup docs verified end-to-end against
  a scratch GitHub repo and a GitLab project.

## 7. Phasing

1. **Phase 1 — embedded local review:** `cmd/cadoo-mcp` + stdio transport +
   `target=local` for the core tool set + Claude Code plugin. Dogfoodable
   immediately on Cadoo's own repo.
2. **Phase 2 — live PR/MR:** `target=pr` with `post=false/true`, idempotent
   posting on GitHub + GHES + GitLab.
3. **Phase 3 — connected mode:** synchronous tool endpoint on `cadoo-api`,
   `learn`/`unlearn` exposure, KB/learnings in results.

Each phase is releasable on its own; GoReleaser gains the sixth binary in
Phase 1.

## 8. Risks & open questions

- **Go SDK maturity** (§2.1) — decision point at plan time; fallback specified.
- **Synchronous review latency:** a deep review can take minutes; MCP clients
  have per-call timeouts. Mitigation: progress notifications if the SDK supports
  them, otherwise document timeout configuration and keep `deep_review` out of
  the default tool set.
- **`cadoo-api` sync endpoint** (connected mode) is new backend surface — needs
  auth + rate limiting; scoped to Phase 3 so it doesn't block the MVP.
