# Architecture Research

**Domain:** MCP Server + Claude Code Plugin — new entry point into existing Go AI code reviewer
**Researched:** 2026-06-10
**Confidence:** HIGH (based on direct code tracing + SDK docs verification)

---

## 1. CI-Mode Code Path Trace

This is the exact path `cadoo ci` takes to build `tools.Input` without going through the full `Dispatcher`. All citations are from actual source.

### Entry: `cmd/cadoo-cli/ci.go`

**`ciCmd(args)`** (line 123) does the following in order:

1. Parses `--pr` / `--mr` flag into a `ciTarget` via `parseTargetURL()` (line 52). Extracts: `vcs.Kind`, `BaseURL`, `APIBaseURL`, `ProjectPath`, `Number`.
2. Calls `buildProvider(target)` (line 263) — reads `GITHUB_TOKEN` or `GITLAB_TOKEN` from env, constructs `cadoogh.Adapter` or `cadoogl.Adapter` using their respective `Config{Token, BaseURL}` structs. Both adapters are imported directly into the binary (allowed only at `cmd/*` layer).
3. Reads `LLM_GATEWAY_URL` / `LLM_GATEWAY_API_KEY` from env; constructs `litellm.New(url, key)`.
4. Loads `.cadoo.yaml` from local disk via `config.LoadFile(filepath.Join(repoDir, ".cadoo.yaml"))`. This is the one deviation from the webhook path: CI-mode reads config from the locally-checked-out working tree, not from PR head SHA. (The webhook path calls `d.loadCfg()` which uses `FileFetcher.FetchFileFromRef` against the PR head.)
5. Constructs a stateless `orchestrator.Dispatcher` (line 176) with only: `LLM`, `VCSPool`, `Model`, `BaseCfg`, `Registry`, `ReportStatus`. `KB`, `Learnings`, `Audit`, `LinterRegistry`, `SandboxRunner` are all zero-valued (nil).
6. If the provider implements `vcs.PriorReviewReader`, calls `priorStore()` (line 249) → `r.ListCadooArtifacts(ctx, pr)` → `findings.NewFromPrior(key, snap)` to seed `d.Posted` with an in-memory Store. If the type assertion fails, `d.Posted` stays nil — dedup degrades gracefully to non-idempotent mode.
7. Validates tool names against `d.Registry.Names()` for fast-fail before any LLM calls.
8. Builds `orchestrator.ToolJob{Provider, Tool, RepoFullName, PRNumber, Trigger: "ci"}` per tool name.
9. Calls `d.Run(gctx, job)` — this is exactly the same entry point used by `cadoo-worker`.

### What `Dispatcher.Run()` builds (`internal/orchestrator/reviewer.go`)

Once `d.Run()` is called, the path is identical for CLI and webhook/worker. The `tools.Input` struct is assembled at lines 199-277:

```
provider.FetchPullRequest()      → tools.Input.PR
provider.ListChangedFiles()      → tools.Input.Files
contextengine.Compress(files)    → tools.Input.Packed
d.loadCfg()                      → tools.Input.Config
d.LLM                            → tools.Input.LLM
d.Model / cfg.Model              → tools.Input.Model
slop.Detect()                    → tools.Input.Slop
d.runLinters()                   → tools.Input.Analysis   (nil when SandboxRunner is nil)
tracker.FindLinked()             → tools.Input.Issues
d.Posted.ListPostedFindings()    → tools.Input.PriorFindings
d.Learnings.Active()             → tools.Input.Learnings  (nil when Learnings is nil)
d.KB.Search()                    → tools.Input.KBHits     (nil when KB is nil)
```

The `tools.Input.PR` field is a `*vcs.PullRequest` — a normalized struct defined in `internal/vcs/vcs.go:24`. Tools never touch the VCS adapter directly.

### Key observation for MCP embedded mode

The CI-mode dispatcher is already the "light path": no DB, no KB, no learnings. `cadoo-mcp` in embedded mode is structurally identical — same `Dispatcher` construction, same `d.Run()` call. The only new concern is the `tools.Input.PR` source: for `target=local`, there is no VCS PR to fetch, so the MCP layer must construct a synthetic `*vcs.PullRequest` and a synthetic `[]vcs.FileChange` from local git diff output, bypassing `provider.FetchPullRequest()` and `provider.ListChangedFiles()`.

---

## 2. Local Diff Path: What Doesn't Exist Yet

The entire path from "git working tree / staged / ref-range diff" to `tools.Input` is absent. This is the main new code for Phase 1.

### What `contextengine.Compress` already handles

`internal/contextengine/compress.go` takes `[]vcs.FileChange` and options — it does not care how the files were obtained (VCS API or local git). `FileChange.Patch` is just a unified diff string. `FileChange.Path`, `Additions`, `Deletions`, `IsBinary`, `Status` are all derivable from `git diff` output. The compress function is already reusable.

### What needs to be created: `internal/mcpserver/localdiff.go`

This file must produce `[]vcs.FileChange` from three local sources:

| Target | Git command | Notes |
|--------|-------------|-------|
| Working tree (unstaged + staged) | `git diff HEAD` | Catches all local modifications |
| Staged only | `git diff --cached` | Only files in the index |
| Ref range | `git diff <base>..<head>` | e.g. `HEAD~3..HEAD` or `main..HEAD` |

The output of each git command is a unified diff. Each file section (`diff --git a/X b/X`) maps to one `vcs.FileChange`. The parser needs to:
- Extract `Path` from the `diff --git` header
- Determine `Status` from the presence of `new file mode` / `deleted file mode` / `rename from` lines
- Count `+` and `-` lines for `Additions` / `Deletions`
- Detect binary files from `Binary files ... differ`
- Set `Patch` to the full hunk content for that file

There is no existing Go package in `go.mod` for unified diff parsing. Options:
- Hand-roll a file-section splitter (the format is stable and well-documented). Approximately 80–120 lines of Go.
- Use `github.com/sourcegraph/go-diff` if a dependency is acceptable (maintained, used in production). Check compatibility before adding.

The synthetic `*vcs.PullRequest` for local mode needs only a minimal set of fields:
- `Provider`: can be left empty or set to a sentinel `"local"`
- `RepoFullName`: resolved from `git remote get-url origin` (best-effort; fallback to CWD directory name)
- `HeadSHA`: from `git rev-parse HEAD`
- `Title`: user-provided or defaulted to `"Local diff review"`
- `Body`: empty

The `tools.Input.Reader` field (used by `deep_review` for full-file fetch) requires a `tools.FileReader` implementation. For local mode, this is a simple `os.ReadFile` wrapper against the working tree path — new, ~10 lines.

---

## 3. MCP `tools/call` Handler to Tool Invocation

### Protocol layer

Using `github.com/modelcontextprotocol/go-sdk` v1.6.1 (stable as of May 22, 2026). The server API is:

```go
server := mcp.NewServer(&mcp.Implementation{Name: "cadoo", Version: version.Version}, nil)
mcp.AddTool(server, &mcp.Tool{Name: "cadoo_review", Description: "..."}, handler)
server.Run(ctx, &mcp.StdioTransport{})
```

The SDK handles `initialize`, `tools/list`, and `tools/call` dispatch internally. The handler signature is:

```go
func(ctx context.Context, req *mcp.CallToolRequest, input CadooToolInput) (*mcp.CallToolResult, any, error)
```

The SDK automatically validates `input` against the JSON Schema inferred from the `CadooToolInput` struct tags.

### Shared input schema (`internal/mcpserver/toolmap.go`)

A single `CadooToolInput` struct covers all tools (per spec §2.2):

```go
type CadooToolInput struct {
    Target   string `json:"target"   jsonschema:"pr | local"`
    URL      string `json:"url,omitempty"   jsonschema:"PR or MR URL; required when target=pr"`
    Range    string `json:"range,omitempty" jsonschema:"git ref range; optional when target=local"`
    Post     bool   `json:"post,omitempty"  jsonschema:"post results to VCS (target=pr only)"`
    Question string `json:"question,omitempty" jsonschema:"cadoo_ask only"`
}
```

### Handler dispatch in `internal/mcpserver/embedded.go`

Each registered Cadoo tool gets its own handler function, but they all share the same body shape:

```
1. Validate CadooToolInput (SDK does this automatically)
2. Route on input.Target:
   target=local  → localdiff.Build(ctx, cfg, input.Range) → []vcs.FileChange + synthetic PR
   target=pr     → buildProvider(input.URL, cfg) + priorStore(ctx, rr, ...) + fetch via Dispatcher
3. contextengine.Compress(files, opts)
4. Build tools.Input{PR, Files, Packed, Config, LLM, Model, ...}
5. Call tool.Run(ctx, in) → *tools.Result
6. If target=pr and input.Post=true: d.applyResult(ctx, provider, pr, toolName, res)
7. Render res to markdown → mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: md}}}
```

For `target=local`, step 6 is skipped entirely. For `target=pr, post=false`, step 6 is also skipped.

The call to `applyResult` is what triggers the consolidated comment + fingerprint + wrapper-marker path. This must go through `applyResult` (not `provider.PostSummaryComment` directly) to preserve idempotency — see anti-patterns in `internal/orchestrator/reviewer.go:302`.

### Result rendering for inline return

`tools.Result` has `Summary string` and `InlineComments []vcs.InlineComment`. For inline-return mode, both need to be rendered into a single markdown string. A `renderToMarkdown(toolName string, res *tools.Result) string` function in `internal/mcpserver/embedded.go` is the right home for this — roughly: emit `res.Summary` as-is, then render each `InlineComment` as a markdown code-comment block (file, line range, severity, body).

---

## 4. New System Structure

### Repo layout additions

```
cadoo/
├── cmd/
│   └── cadoo-mcp/         # NEW: sixth binary entry point
│       └── main.go        # flag parsing, config loading, transport selection, wiring
├── internal/
│   └── mcpserver/         # NEW: MCP server library (separate from internal/mcp client)
│       ├── server.go      # Server struct; NewServer() wiring; tool registration loop
│       ├── config.go      # ServerConfig: enabled tools, mode, tokens, LLM endpoint
│       ├── toolmap.go     # CadooToolInput struct; tool name → mcp.Tool descriptor
│       ├── embedded.go    # Embedded mode: build tools.Input, run tool, render result
│       ├── connected.go   # Connected mode: forward to cadoo-api REST endpoint
│       └── localdiff.go   # git diff → []vcs.FileChange + synthetic *vcs.PullRequest
├── plugins/
│   └── claude/            # NEW: Claude Code plugin
│       ├── .claude-plugin/
│       │   └── plugin.json     # plugin manifest
│       ├── .mcp.json           # registers cadoo-mcp as stdio server
│       └── commands/           # slash command definitions
│           ├── cadoo-review.md
│           ├── cadoo-describe.md
│           ├── cadoo-improve.md
│           └── cadoo-ask.md
```

### Unchanged packages (used as-is)

| Package | Role in cadoo-mcp | Notes |
|---------|-------------------|-------|
| `internal/orchestrator` | `Dispatcher` struct, `Run()`, `applyResult()` | No modifications |
| `internal/tools` | `Tool` interface, `Input`, `Result`, `Registry` | No modifications |
| `internal/vcs` | `Provider` interface, types, `PriorReviewReader` | No modifications |
| `internal/contextengine` | `Compress()` | No modifications |
| `internal/findings` | `Store`, `NewFromPrior()`, `StampInline()` | No modifications |
| `internal/config` | `Repo`, `LoadFile()`, `Default()` | No modifications |
| `internal/llm/litellm` | `New()` | No modifications |
| `internal/orchestrator/consolidate.go` | marker constants, `renderConsolidated()` | No modifications |

### Modified files

| File | Change |
|------|--------|
| `go.mod` / `go.sum` | Add `github.com/modelcontextprotocol/go-sdk v1.6.1` |
| `.goreleaser.yaml` | Add sixth build entry for `cadoo-mcp` (linux/darwin/windows, amd64/arm64) |
| `Makefile` | `make build` picks up new `cmd/cadoo-mcp` automatically if using `./cmd/*` glob; verify and add explicit target if needed |

---

## 5. Component Boundaries and Data Flow

### Embedded mode data flow (Phase 1 + Phase 2)

```
MCP client (Claude Code / Claude Desktop / Cursor)
        │  JSON-RPC over stdio
        ▼
cmd/cadoo-mcp/main.go
        │  parses flags, loads ServerConfig, constructs mcpserver.Server
        ▼
internal/mcpserver/server.go
        │  mcp.AddTool() for each enabled tool
        │  server.Run(ctx, &mcp.StdioTransport{})
        ▼
internal/mcpserver/embedded.go  (on tools/call)
        │
        ├── target=local  →  localdiff.Build(ctx, range)
        │                        ↓ git diff subprocess
        │                    []vcs.FileChange + synthetic *vcs.PullRequest
        │
        └── target=pr     →  ci.go-style: buildProvider(url, cfg)
                                 ↓ cadoogh/cadoogl.New(token)
                             d.Posted = priorStore(ctx, rr, ...)
                                 ↓ vcs.PriorReviewReader
                             provider.FetchPullRequest() + ListChangedFiles()
        │
        ▼
contextengine.Compress(files, opts)
        │
        ▼
tools.Input{PR, Files, Packed, Config, LLM, Model, ...}
        │
        ▼
tool.Run(ctx, in)   (e.g. tools/review, tools/describe)
        │
        ▼
*tools.Result{Summary, InlineComments}
        │
        ├── post=false  →  renderToMarkdown(result)
        │                      → mcp.CallToolResult (inline in conversation)
        │
        └── post=true   →  d.applyResult(ctx, provider, pr, toolName, res)
                                → postSummary (consolidated comment)
                                → postInline (deduped inline comments)
                            renderToMarkdown(result)
                                → mcp.CallToolResult (also returned inline)
```

### Connected mode data flow (Phase 3)

```
MCP client
        │
        ▼
internal/mcpserver/connected.go
        │  HTTP POST to cadoo-api /v1/tools/{tool}/run
        │  (new synchronous endpoint, scoped to Phase 3)
        ▼
cadoo-api REST handler
        │  full Dispatcher.Run() with KB + Learnings
        ▼
JSON response → mcp.CallToolResult
```

### Import graph for `internal/mcpserver`

```
internal/mcpserver
  └─ github.com/modelcontextprotocol/go-sdk/mcp  (protocol layer)
  └─ internal/orchestrator                         (Dispatcher, ToolJob, applyResult)
  └─ internal/tools                                (Registry, Input, Result)
  └─ internal/vcs                                  (types only — Provider, PullRequest, FileChange)
  └─ internal/contextengine                        (Compress)
  └─ internal/findings                             (NewFromPrior, PRKey)
  └─ internal/config                               (Repo, LoadFile, Default)
  └─ internal/llm/litellm                          (New)
  └─ internal/version                              (Version)
```

`cmd/cadoo-mcp/main.go` additionally imports:
```
  └─ internal/vcs/github   (adapter construction — allowed only at cmd/* layer)
  └─ internal/vcs/gitlab   (adapter construction — allowed only at cmd/* layer)
  └─ internal/mcpserver    (server wiring)
```

The import constraint "never import `internal/vcs/github|gitlab` outside `internal/vcs/`" applies to `internal/*` packages. `cmd/cadoo-mcp` is a `cmd/*` binary and follows the same pattern as `cmd/cadoo-cli/ci.go` lines 28-29, which already imports both adapters.

---

## 6. Claude Code Plugin Directory

**Location: `plugins/claude/`** at the repository root.

Rationale: `ide/vscode/` already establishes the pattern of `<integration-type>/<vendor>/` at root level. The `plugins/` prefix is more descriptive than `ide/` for non-IDE integrations. Claude Code, Claude Desktop, and Cursor docs should all live under `plugins/claude/` with a top-level `docs/MCP_SETUP.md` for all three clients.

Structure:
```
plugins/claude/
├── .claude-plugin/
│   └── plugin.json          # Claude Code plugin manifest (name, version, mcp, commands)
├── .mcp.json                # registers cadoo-mcp: {command: "cadoo-mcp", args: [], env: {}}
└── commands/                # one .md per slash command
    ├── cadoo-review.md      # /cadoo:review → cadoo_review(target=local)
    ├── cadoo-describe.md    # /cadoo:describe → cadoo_describe(target=local)
    ├── cadoo-improve.md     # /cadoo:improve → cadoo_improve(target=local)
    └── cadoo-ask.md         # /cadoo:ask <question> → cadoo_ask(target=local, question=...)
```

The `.mcp.json` env passthrough must cover `GITHUB_TOKEN`, `GITLAB_TOKEN`, `LITELLM_API_KEY`, and optionally `CADOO_MCP_CONFIG` (path to `~/.config/cadoo/mcp.yaml`). The plugin manifest should not hardcode token values — reference env var names only.

---

## 7. Build Order Across Three Phases

### Phase 1: Embedded Local Review

**Goal:** Dogfoodable on Cadoo's own repo. `cadoo_review(target=local)` works in Claude Code.

Build order within Phase 1:
1. `internal/mcpserver/config.go` — `ServerConfig` struct; config file loading + env override; enabled-tool list. No external dependencies. Build and test first.
2. `internal/mcpserver/localdiff.go` — git diff parser → `[]vcs.FileChange` + synthetic `*vcs.PullRequest`. Isolated, testable with golden files. Depends only on `internal/vcs` types.
3. `internal/mcpserver/toolmap.go` — `CadooToolInput` struct; `buildToolDescriptor(cadooTool tools.Tool) *mcp.Tool` mapping. Depends on `go-sdk` and `internal/tools`.
4. `internal/mcpserver/embedded.go` — assembles `tools.Input` for `target=local`, calls `tool.Run`, renders result. Depends on all above + `internal/orchestrator`, `internal/contextengine`.
5. `internal/mcpserver/server.go` — top-level `NewServer()` that wires config → tool list → `mcp.AddTool()` registration. Depends on all above + `go-sdk`.
6. `cmd/cadoo-mcp/main.go` — flag parsing (`--config`, `--tools`, `--transport`), adapter construction, `mcpserver.NewServer().Run()`. Add to `Makefile` and `.goreleaser.yaml`.
7. `plugins/claude/` — plugin manifest, `.mcp.json`, four slash command files. No Go code.

**Rationale:** `config.go` first because all other files depend on `ServerConfig`. `localdiff.go` before `embedded.go` because the local path is what Phase 1 exercises. `toolmap.go` before `server.go` because server registration depends on the descriptor builder. `plugins/claude/` last because it depends on the binary being in `$PATH`.

**Go SDK dependency:** Add `github.com/modelcontextprotocol/go-sdk v1.6.1` to `go.mod` in step 3. This is stable and maintained (v1.x, full 2025-11-25 MCP spec, co-maintained by Google).

### Phase 2: Live PR/MR Review

**Goal:** `cadoo_review(target=pr, post=false/true)` works on GitHub + GitLab.

Build order within Phase 2 (builds on all Phase 1 code):
1. Extend `internal/mcpserver/embedded.go` with the `target=pr` branch: `buildProvider()` (mirrors `cmd/cadoo-cli/ci.go:263`), `priorStore()` call, `provider.FetchPullRequest()` + `ListChangedFiles()`, then existing `Compress` + `tool.Run` path.
2. Add `post=true` path: call `d.applyResult(ctx, provider, pr, toolName, res)` from the handler. The `Dispatcher` used here must be constructed with `Posted` set (via `priorStore`) to get the idempotency behavior.
3. Update the four slash commands in `plugins/claude/commands/` with `target=pr, url=<arg>` variants.
4. Integration test: fake `vcs.Provider` + fake LLM, assert that a `cadoo_review(target=pr, post=true)` call produces the same wrapper-marker format as the equivalent `cadoo ci` run.

**Dependency on Phase 1:** The `embedded.go` file is already the right home — just extend the routing branch. No new files needed.

### Phase 3: Connected Mode

**Goal:** `--api-url` switches from local LLM to `cadoo-api` backend; KB + learnings appear in results.

Build order within Phase 3:
1. New `cadoo-api` endpoint: `POST /v1/tools/{tool}/run` — synchronous, auth-gated, runs `Dispatcher.Run()` with full KB/learnings, returns the rendered result as JSON. Auth via `cadoo-api`'s existing OIDC middleware.
2. `internal/mcpserver/connected.go` — HTTP client for the new API endpoint; replaces the embedded `tool.Run` call with a REST call. Mode selected by `cfg.Connected.APIURL != ""`.
3. `internal/mcpserver/server.go` — add `learn` / `unlearn` tools to registration only when in connected mode (they require `LearningsStore` which requires the backend).
4. Update `plugins/claude/commands/` with connected-mode slash commands if the experience warrants it.

**Dependency on Phases 1+2:** Connected mode is additive — it replaces the tool execution backend, not the MCP protocol layer or the input construction.

---

## 8. Integration Points

### Existing code touched by this milestone

| Integration point | Where | What changes |
|-------------------|-------|--------------|
| `orchestrator.Dispatcher` construction | `internal/mcpserver/embedded.go` | New construction site (mirrors `cmd/cadoo-cli/ci.go:176`) |
| `vcs.PriorReviewReader` type assertion | `internal/mcpserver/embedded.go` | Reuses existing pattern from `cmd/cadoo-cli/ci.go:187` |
| `findings.NewFromPrior()` | `internal/mcpserver/embedded.go` | Reused unchanged |
| `contextengine.Compress()` | `internal/mcpserver/embedded.go` | Reused unchanged; receives locally-constructed `[]vcs.FileChange` in local mode |
| `orchestrator.applyResult()` | `internal/mcpserver/embedded.go` | Called for `target=pr, post=true`; must go through this, never bypass |
| `orchestrator.DefaultRegistry()` | `internal/mcpserver/server.go` | Reused; filtered by enabled tool list from config |
| `.goreleaser.yaml` | repo root | Add sixth binary build + archive entry |

### New boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `cmd/cadoo-mcp` ↔ `internal/mcpserver` | Direct Go import | Same pattern as all other `cmd/*` → `internal/*` |
| `internal/mcpserver` ↔ `internal/orchestrator` | Direct Go import | `mcpserver` depends on `orchestrator`; no reverse dependency |
| `internal/mcpserver` ↔ `go-sdk` | Direct Go import | SDK handles JSON-RPC; `mcpserver` handles tool logic |
| `internal/mcpserver` ↔ `internal/mcp` | No dependency | `internal/mcp` is the outbound client; `mcpserver` is the inbound server — separate concerns, no shared code |
| `plugins/claude/` ↔ `cmd/cadoo-mcp` binary | `$PATH` or absolute path in `.mcp.json` | Plugin references binary by name; build + install must precede plugin registration |

---

## 9. Anti-Patterns

### Bypassing `applyResult` for VCS posting

**What happens:** Calling `provider.PostSummaryComment` or `provider.PostInlineComments` directly from the MCP handler.
**Why wrong:** Skips fingerprint recording, dedup lookup, marker stamping, stale-thread resolution, and consolidated comment management. Every re-invocation from the MCP client creates duplicate comments on the PR.
**Do this instead:** Return `*tools.Result` from `tool.Run()` and pass it to `d.applyResult()` exactly as `reviewer.go:284` does.

### Extending `internal/mcp` (the client) for server functionality

**What happens:** Adding server-side types or transport to `internal/mcp`.
**Why wrong:** `internal/mcp` is the MCP *client* package (Cadoo consuming external servers, Phase 6 work). Mixing client and server code creates confusion and coupling.
**Do this instead:** All server code lives in `internal/mcpserver`. The two packages share zero code.

### Modifying `tools.Tool`, `tools.Input`, or `tools.Result`

**What happens:** Adding MCP-specific fields or behavior to the shared tool interfaces.
**Why wrong:** These interfaces are the shared SDK. Modifications break all 13 existing tools and all future callers. The spec explicitly calls this out as a non-goal.
**Do this instead:** `internal/mcpserver/embedded.go` adapts the MCP input schema *to* `tools.Input`. Adaptation code lives in `mcpserver`, never in `tools`.

### Local diff without a synthetic `vcs.PullRequest`

**What happens:** Passing `nil` as `tools.Input.PR` for local-mode reviews.
**Why wrong:** Tools dereference `in.PR` for title, body, author, repo context in prompts. A nil pointer panics.
**Do this instead:** `localdiff.Build()` always returns a populated synthetic `*vcs.PullRequest` with at minimum `RepoFullName`, `HeadSHA`, `Title`, and `Provider="local"`.

---

## Sources

- `cmd/cadoo-cli/ci.go` — CI-mode code path (lines 123-243 traced directly)
- `internal/orchestrator/reviewer.go` — `Dispatcher.Run()` and `tools.Input` assembly (lines 145-293 traced directly)
- `internal/tools/tools.go` — `Tool` interface, `Input`, `Result` struct
- `internal/vcs/vcs.go` — `Provider` interface, `PullRequest`, `FileChange`, `PriorReviewReader`
- `internal/contextengine/compress.go` — `Compress()` signature and behavior
- `internal/findings/prior.go` — `NewFromPrior()` implementation
- `internal/mcp/mcp.go` — confirms this is a client package (HTTP client only, no server)
- `go.mod` — confirms `github.com/modelcontextprotocol/go-sdk` is not yet in the module
- `.goreleaser.yaml` — current five-binary build configuration
- [MCP Go SDK docs](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp) — `NewServer`, `AddTool`, `StdioTransport`, `Run` API; v1.6.1 current (May 2026)
- [MCP Go SDK quick start](https://go.sdk.modelcontextprotocol.io/quick_start/) — server + stdio transport example

---
*Architecture research for: Cadoo MCP Server + Claude Code Plugin milestone*
*Researched: 2026-06-10*
