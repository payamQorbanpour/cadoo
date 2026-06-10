# Stack Research

**Domain:** MCP Server (stdio transport) + Claude Code Plugin — additive milestone on existing Go 1.26 monorepo
**Researched:** 2026-06-10
**Confidence:** HIGH

## Context

This is a subsequent milestone, not a greenfield project. The existing stack (Go 1.26, PostgreSQL 16, River queue, LiteLLM sidecar, `internal/mcp` HTTP client) is fixed. This document covers only the **new additions** needed for `cmd/cadoo-mcp` and `plugins/claude/`. Do not re-evaluate the base stack.

---

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/modelcontextprotocol/go-sdk` | v1.6.1 | MCP server protocol layer (stdio transport, `tools/list`, `tools/call`, progress notifications) | Official SDK, Google-maintained, v1.0.0 compatibility guarantee since Sep 2025, Go 1.25+ required (matches Go 1.26 repo), `StdioTransport{}` is first-class, ships `InMemoryTransport` for tests, ~7 direct deps, Apache-2.0 |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/google/jsonschema-go` | v0.4.3 (pulled in by go-sdk) | JSON Schema inference from Go structs for MCP tool `inputSchema` | Already a transitive dep of go-sdk; use directly to generate `inputSchema` from the MCP tool's input struct rather than hand-writing JSON Schema |
| Standard library `os/exec` + `bufio` | stdlib | `git diff` and `git status` invocation for `target=local` diffs | No new dep; mirrors the same pattern cadoo-cli uses for working-tree diffs |
| `gopkg.in/yaml.v3` | already in go.mod | Parsing `~/.config/cadoo/mcp.yaml` server config | Already a direct dependency; no new dep needed |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `mcp.InMemoryTransport` (from go-sdk) | Unit-test stdio framing without real pipes | Use `mcp.NewInMemoryTransports()` — the SDK ships this; no test harness to build |
| `make build` (existing) | Adds `cmd/cadoo-mcp` to the six-binary GoReleaser build | Add `cadoo-mcp` entry to `Makefile` and `.goreleaser.yaml` identically to the five existing binaries |
| `claude plugin validate` (Claude Code CLI) | Validates `plugins/claude/.claude-plugin/plugin.json` schema | Run in CI after plugin manifest changes; `--strict` treats unrecognized field warnings as errors |

---

## Plugin Distribution

### Claude Code Plugin Structure

A Claude Code plugin is a directory with the following layout. Only `.claude-plugin/plugin.json` and `.mcp.json` are mandatory for the Cadoo use case:

```
plugins/claude/
  .claude-plugin/
    plugin.json          # Plugin manifest (only "name" is required)
  .mcp.json              # MCP server registration (stdio command)
  skills/
    review/
      SKILL.md           # /cadoo:review slash command
    describe/
      SKILL.md           # /cadoo:describe slash command
    improve/
      SKILL.md           # /cadoo:improve slash command
    ask/
      SKILL.md           # /cadoo:ask slash command
```

### plugin.json Schema (verified from official docs)

```json
{
  "name": "cadoo",
  "displayName": "Cadoo Code Review",
  "version": "2.0.0",
  "description": "AI code review tools from Cadoo — review, describe, improve, and ask about your code",
  "author": {
    "name": "Payam Qorbanpour",
    "url": "https://github.com/payamqorbanpour/cadoo"
  },
  "repository": "https://github.com/payamqorbanpour/cadoo",
  "license": "MIT",
  "keywords": ["code-review", "mcp", "ai"],
  "defaultEnabled": false,
  "userConfig": {
    "github_token": {
      "type": "string",
      "title": "GitHub Token",
      "description": "Personal access token for GitHub/GHES (GITHUB_TOKEN env var as fallback)",
      "sensitive": true
    },
    "gitlab_token": {
      "type": "string",
      "title": "GitLab Token",
      "description": "Personal access token for GitLab (GITLAB_TOKEN env var as fallback)",
      "sensitive": true
    },
    "litellm_endpoint": {
      "type": "string",
      "title": "LiteLLM Endpoint",
      "description": "LiteLLM proxy endpoint (default: http://localhost:4000)",
      "default": "http://localhost:4000"
    }
  }
}
```

Key decisions:
- `defaultEnabled: false` — the plugin connects to external services and requires token config; users must opt in explicitly (requires Claude Code v2.1.154+).
- `userConfig` with `sensitive: true` stores tokens in the system keychain, not `settings.json`.
- The `${user_config.KEY}` substitution in `.mcp.json` passes config values directly to the `cadoo-mcp` process env.

### .mcp.json Schema (verified from official docs)

```json
{
  "mcpServers": {
    "cadoo": {
      "command": "${CLAUDE_PLUGIN_ROOT}/cadoo-mcp",
      "args": [],
      "env": {
        "GITHUB_TOKEN": "${user_config.github_token}",
        "GITLAB_TOKEN": "${user_config.gitlab_token}",
        "LITELLM_ENDPOINT": "${user_config.litellm_endpoint}"
      }
    }
  }
}
```

`${CLAUDE_PLUGIN_ROOT}` resolves to the plugin's installation directory where GoReleaser places the `cadoo-mcp` binary. The plugin directory must include the pre-built binary. This is the standard pattern used by `github/github-mcp-server` and similar Go-based Claude Code plugins.

### Plugin Installation (three paths)

1. **Via skills-directory (recommended for dev/self-host):** Place `plugins/claude/` under `<project>/.claude/skills/cadoo/` or `~/.claude/skills/cadoo/`. Loaded automatically; no marketplace needed.

2. **Via marketplace (recommended for distribution):** Create `.claude-plugin/marketplace.json` at the repo root listing the plugin; users run `/plugin marketplace add github:payamqorbanpour/cadoo` then `/plugin install cadoo@cadoo`.

3. **Manual / Cursor / Claude Desktop:** Users add to their MCP config JSON directly — the docs cover this as a fallback path (no plugin system required, just a `cadoo-mcp` binary in PATH).

---

## Installation

```bash
# In go.mod — single new direct dependency
go get github.com/modelcontextprotocol/go-sdk@v1.6.1

# New binary (add to Makefile alongside existing five)
go build -o ./bin/cadoo-mcp ./cmd/cadoo-mcp
```

The `go-sdk` pulls in these new transitive deps (all small, no C, no cgo):
- `github.com/golang-jwt/jwt/v5 v5.3.1` — already in go.mod as `v4.5.2`; go modules resolves the v5 separately (no conflict)
- `github.com/google/jsonschema-go v0.4.3` — new, pure Go
- `github.com/segmentio/encoding v0.5.4` — new, pure Go, high-performance JSON encoder
- `github.com/yosida95/uritemplate/v3 v3.0.2` — new, pure Go, URI template
- `golang.org/x/oauth2` and `golang.org/x/time` — already in go.mod
- `golang.org/x/tools v0.42.0` — already in go.mod at older version; will be upgraded

No CGo, no new system dependencies, no new runtime services. All pure Go. `make ci` stays green.

---

## Alternatives Considered

| Recommended | Alternative | Why Not |
|-------------|-------------|---------|
| `github.com/modelcontextprotocol/go-sdk` v1.6.1 | `github.com/mark3labs/mcp-go` v0.54.1 | Still at v0.x (pre-stable, no compatibility guarantee), MIT license (fine), 1,630 importers vs go-sdk's Google/Anthropic backing. go-sdk has formal v1.0 stability guarantee, ships `InMemoryTransport` for tests, and is spec-complete at 2025-11-25. mark3labs/mcp-go is a reasonable fallback if go-sdk integration causes unforeseen issues at implementation time. |
| `github.com/modelcontextprotocol/go-sdk` v1.6.1 | Hand-rolled stdio JSON-RPC | The protocol surface needed (`initialize`, `tools/list`, `tools/call`, progress) is small, but hand-rolling means owning spec compliance, error code mapping, cancellation, and future spec evolution. go-sdk eliminates this. Only revert to hand-rolled if the go-sdk's dependency footprint becomes a blocker for a specific release target (e.g., a minimal static binary for air-gapped customers). |
| `plugins/claude/` skills-dir layout | No plugin; docs only | The plugin provides discoverability via `/plugin install` and namespace-isolated slash commands (`/cadoo:review`). Worth the small overhead. |
| `userConfig` with `sensitive: true` | Env-var-only auth | `userConfig` stores tokens in the system keychain via the plugin system; env vars remain as a fallback (the MCP server reads them in both cases). `sensitive: true` prevents tokens appearing in `settings.json`. |

---

## What NOT to Add

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `mark3labs/mcp-go` alongside `go-sdk` | Two MCP libraries for the same binary creates confusion and dep bloat | Pick go-sdk; use mark3labs/mcp-go only as a full replacement if go-sdk is unsuitable |
| Extending `internal/mcp` client package | The existing `internal/mcp` is a client (HTTP JSON-RPC). The new server code belongs in `internal/mcpserver`. Mixing server/client concerns in one package will cause import cycle pressure. | New `internal/mcpserver` package as specified in the design spec |
| `github.com/spf13/cobra` or `github.com/urfave/cli` for `cmd/cadoo-mcp` | All five existing binaries use `flag` stdlib + hand-written usage text. Adding a CLI framework for the sixth binary breaks the convention. | `flag` stdlib, same pattern as `cmd/cadoo-cli` |
| OAuth flow libraries (e.g., `golang.org/x/oauth2` client flows) | Out of scope this milestone; PAT-only auth explicitly specified | Existing `golang.org/x/oauth2` is already in go.mod as a transitive dep; do not wire OAuth flows |
| Streamable HTTP transport for Phase 1 | The spec says stdio first, streamable HTTP later. go-sdk ships `StreamableServerTransport` but it should not be wired in Phase 1. | Add in a later phase; the binary flag `--transport` can gate it |

---

## Version Compatibility

| Package | Constraint | Notes |
|---------|-----------|-------|
| `github.com/modelcontextprotocol/go-sdk` v1.6.1 | Requires Go 1.25+ | Go 1.26 in this repo satisfies this |
| `github.com/google/jsonschema-go` v0.4.3 | Pulled by go-sdk; also in mark3labs/mcp-go at v0.4.2 | No conflict; go modules selects the higher version |
| `golang.org/x/tools` | go-sdk requires v0.42.0; current go.mod has an older version | `go mod tidy` will upgrade; no API breakage expected (x/tools follows backward compat) |
| `github.com/golang-jwt/jwt/v5` | go-sdk adds v5; existing go.mod has `github.com/golang-jwt/jwt/v4` | Different major versions coexist cleanly in Go modules; no conflict |
| Claude Code plugin system | Requires Claude Code v2.1.154+ for `defaultEnabled: false` | Document minimum Claude Code version in setup guide; earlier versions ignore `defaultEnabled` and enable the plugin on install (safe fallback) |

---

## Sources

- [Official go-sdk GitHub](https://github.com/modelcontextprotocol/go-sdk) — v1.6.1, stability guarantee, Go 1.25 min, dep list from go.mod (HIGH confidence)
- [go-sdk pkg.go.dev](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp) — API surface: `StdioTransport`, `AddTool`, `InMemoryTransport`, generics API (HIGH confidence)
- [go-sdk v1.0.0 release notes](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.0.0) — formal compatibility guarantee stated (HIGH confidence)
- [mark3labs/mcp-go GitHub](https://github.com/mark3labs/mcp-go) — v0.54.1, pre-stable, dep footprint (HIGH confidence)
- [Claude Code plugins reference](https://code.claude.com/docs/en/plugins-reference) — `plugin.json` schema, `.mcp.json` format, `${CLAUDE_PLUGIN_ROOT}`, `userConfig`, `defaultEnabled`, skills layout (HIGH confidence — official Anthropic docs)
- [Claude Code plugin marketplaces](https://code.claude.com/docs/en/plugin-marketplaces) — `marketplace.json` schema, distribution model (HIGH confidence)
- [go-sdk go.mod](https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/main/go.mod) — exact direct deps and Go version requirement (HIGH confidence)
- [mark3labs/mcp-go go.mod](https://raw.githubusercontent.com/mark3labs/mcp-go/main/go.mod) — exact direct deps and Go version requirement (HIGH confidence)

---
*Stack research for: MCP server + Claude Code plugin additive milestone on Cadoo Go monorepo*
*Researched: 2026-06-10*
