# Roadmap: Cadoo

## Milestones

- ✅ **v1.0 Release Docs** — Phases 1-3 (shipped 2026-06-06)
- ✅ **v1.1 Release-Docs Engineering Diagrams** — Phase 7 (shipped 2026-06-13)
- 🚧 **v2.0 MCP Server + Claude Code Plugin** — Phases 4-6 (active)
- 📋 **v2.1 CI-Mode Dedup Convergence** — Phase 8 (queued)

## Phases

<details>
<summary>✅ v1.0 Release Docs (Phases 1-3) — SHIPPED 2026-06-06</summary>

### Phase 1: Generators + Publishers + CLI

**Goal**: A maintainer can run `cadoo release-docs --repo … --from vX --to vY` and have Cadoo generate a grouped changelog section and polished release notes for that range and publish them — to the release body and to a single `CHANGELOG.md` PR — idempotently, with no DB, dogfooded on Cadoo's own repo.
**Depends on**: Nothing (first phase)
**Requirements**: REQ-release-artifact-generation (changelog + release-notes), REQ-per-artifact-toggles, REQ-configurable-templates, REQ-release-docs-idempotency (stateless/marker mode), REQ-configurable-trigger (manual CLI/CI entry point), REQ-publish-destinations (releasebody + changelogpr)
**Plans**: 7 plans — all complete (2026-06-05)

### Phase 2: Webhook Auto-Trigger + State

**Goal**: A published release (or configured tag push) on a customer repo automatically triggers release-docs generation through the dual-mode queue and worker, with DB-backed state so re-syncs edit in place, plus the pages publisher and blog generator.
**Depends on**: Phase 1
**Requirements**: REQ-configurable-trigger (release/tag webhook ingestion), REQ-release-docs-idempotency (DB-backed state table + migration), REQ-publish-destinations (pages), REQ-release-artifact-generation (blog generator)
**Plans**: 6 plans — all complete (2026-06-05)

### Phase 3: API Docs / OpenAPI

**Goal**: For a supported framework, Cadoo derives an OpenAPI spec and a rendered API reference from the repo's code at release time and publishes them to pages.
**Depends on**: Phase 2
**Requirements**: REQ-release-artifact-generation (api-docs + openapi)
**Plans**: 5 plans — all complete (2026-06-06)

</details>

<details>
<summary>✅ v1.1 Release-Docs Engineering Diagrams (Phase 7) — SHIPPED 2026-06-13</summary>

### Phase 7: Release-Docs Engineering Diagrams

**Goal**: At release time, Cadoo generates user-selected software-engineering diagrams (sequence, dependency, state, flowchart, class) from the repository and publishes them as a release-docs artifact to pages — per-type choosable in `.cadoo.yaml`, idempotent across re-runs, deterministic-first, and degrading gracefully when a type can't be derived.
**Depends on**: Phase 3 (extends the shipped `internal/releasedocs` artifact + pages-publisher pipeline; independent of the v2.0 MCP phases 4–6)
**Requirements**: DIAG-01, DIAG-02, DIAG-03, DIAG-04, DIAG-05 (all Complete)
**Verification**: passed (9/9 must-haves). Code review: 0 critical, 4 warnings (advisory hardening follow-ups), 3 info.

Plans:
- [x] 07-01-PLAN.md — Contract/config layer: KindDiagrams const + DiagramsConfig (5 per-type path lists + family gate) + .cadoo.yaml.example doc
- [x] 07-02-PLAN.md — diagrams generator package: Mermaid keyword sniff + fixed fence wrapper + GenerateMulti (ordered-slice, graceful skip) + golden tests
- [x] 07-03-PLAN.md — DefaultGenerators registration + pages path/idempotency tests + dogfood Mermaid sources (SC-5) + human-verify checkpoint

> **Design resolved in `/gsd:discuss-phase 7` (see 07-CONTEXT.md, D-01..D-10):** render committed Mermaid sources (no derivation, no LLM); per-type explicit config paths (no tree listing); fixed ` ```mermaid ` fence wrapper (no rendering runtime); pages-only deterministic idempotent paths `releases/<tag>/diagrams/<type>/<name>.md`; per-source graceful skip via `(nil,nil)`.

</details>

### 🚧 v2.0 MCP Server + Claude Code Plugin (Active)

**Milestone Goal:** Expose Cadoo's review tools to AI assistants via a new `cadoo-mcp` MCP server binary and a Claude Code plugin, enabling local diff review and live PR/MR review from inside any MCP-compatible client.

- [ ] **Phase 4: Embedded Local Review + Plugin** — `cadoo-mcp` binary with stdio transport, local diff review, foundational contracts (stdout discipline, strict schemas, progress, credentials), and Claude Code plugin
- [ ] **Phase 5: Live PR/MR Review** — `target=pr` dry-run and idempotent post-back on GitHub/GHES/GitLab, security gates, multi-client setup docs
- [ ] **Phase 6: Connected Mode** — `cadoo-api` sync endpoint, KB + learnings in results, `learn`/`unlearn` tool exposure

## Phase Details

### Phase 4: Embedded Local Review + Plugin

**Goal**: A developer can connect Claude Code (or any MCP client) to `cadoo-mcp` over stdio, review their working-tree diff locally with results returned inline, and install the Claude Code plugin with one command — while the binary is shipped by GoReleaser as the sixth Cadoo binary.
**Depends on**: Phase 3 (v1.0 shipped; MCP builds on the existing review pipeline)
**Requirements**: MCP-01, MCP-02, MCP-03, MCP-04, MCP-05, MCP-06, LOCAL-01, LOCAL-02, LOCAL-03, PLUG-01, PLUG-03
**Success Criteria** (what must be TRUE):
  1. A developer can point Claude Code (or Cursor, or Claude Desktop) at `cadoo-mcp` via stdio, run `initialize` → `tools/list` → `tools/call`, and receive a review of their working-tree or staged diff inline in the conversation.
  2. The default tool set (`review`, `describe`, `improve`, `ask`) is advertised; a tool disabled in config is absent from `tools/list` and returns a tool-not-found error if called anyway.
  3. Tool input schemas are strict: `target` is an enum (`pr`|`local`), `url` is required only when `target=pr`, and unknown fields are rejected — clients cannot hallucinate arguments.
  4. A review longer than 60 seconds completes successfully because progress heartbeats are emitted at pipeline checkpoints; no timeout-induced client disconnection occurs.
  5. Stdout carries only JSON-RPC frames — no log line, no go-github/go-gitlab output, no middleware noise — verified by a framing round-trip test with logging enabled.
  6. Tokens (`GITHUB_TOKEN`, `GITLAB_TOKEN`, `LITELLM_API_KEY`) are resolved from per-invocation args → env vars → config file; token values never appear in logs or error messages.
  7. `make build` produces `bin/cadoo-mcp`; GoReleaser publishes it alongside the existing five binaries.
  8. Claude Code users can install the plugin from `plugins/claude/`, run `/cadoo:review` with no args, and have it review the current working tree.
**Plans**: TBD
**UI hint**: no

### Phase 5: Live PR/MR Review

**Goal**: A developer can paste a GitHub, GHES, or GitLab PR/MR URL into the conversation, get a dry-run review inline by default, and optionally post results back to the PR idempotently — with no duplicate comments across webhook, CI, and MCP paths — while Cursor and Claude Desktop users have verified setup docs.
**Depends on**: Phase 4
**Requirements**: PR-01, PR-02, PR-03, PR-04, PLUG-02
**Success Criteria** (what must be TRUE):
  1. A developer can call `cadoo_review(target=pr, url=<PR-URL>)` with `post=false` (default) and receive the review inline; nothing is posted to the PR.
  2. With `post=true`, the PR receives a single consolidated comment edited in place and deduplicated inline findings — indistinguishable from a webhook or `cadoo ci` run (same `<!-- cadoo:fp … -->` markers and wrapper format).
  3. `post=true` fails closed when the target repo is not on the `allowed_repos` allowlist, and the URL host must match a configured VCS provider — confused-deputy attacks are prevented.
  4. A `github.com/…`, configured-GHES-host, and `gitlab.com`/self-managed MR URL each route to the correct `vcs.Provider`; an unknown host returns a clear rejection.
  5. Cursor and Claude Desktop setup is documented and verified end-to-end against a scratch GitHub repo and a GitLab project.
**Plans**: TBD
**UI hint**: no

### Phase 6: Connected Mode

**Goal**: A developer with access to a running `cadoo-api` deployment can point `cadoo-mcp` at it so that reviews include KB hits and learnings, and can expose `learn`/`unlearn` tools to teach Cadoo from the conversation.
**Depends on**: Phase 5
**Requirements**: CONN-01, CONN-02, CONN-03
**Success Criteria** (what must be TRUE):
  1. With `api-url` set, tool calls are forwarded to the backend and results visibly include KB/learnings context; without it, embedded mode runs — there is no silent fallback between modes.
  2. `learn` and `unlearn` appear in `tools/list` only in connected mode (when enabled by config); they are never advertised in embedded mode.
  3. `cadoo-api` exposes a synchronous tool-run endpoint that authenticates the caller, enforces rate limits, carries `org_id` throughout, and handles long reviews without timing out (streaming/chunked strategy decided at plan time).
**Plans**: TBD
**UI hint**: no

### 📋 v2.1 CI-Mode Dedup Convergence (Queued)

**Milestone Goal:** Make `cadoo ci` review converge on re-reviewed PRs/MRs instead of accumulating duplicate threads unboundedly. Disjoint from v2.0 (touches `internal/orchestrator`, `internal/findings`, `internal/vcs`; no MCP scope). Source SPEC: `docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md`.

### Phase 8: CI-Mode Dedup Convergence

**Goal**: On a re-reviewed PR/MR, `cadoo ci` reaches a fixed point — resolving threads and pushing converges instead of accumulating duplicate threads — via end-to-end `StructuralKey` (no self-resolution), resolved-thread sticky suppression, and incremental review bounded by a persisted last-reviewed SHA.
**Depends on**: Nothing (modifies existing CI-mode dedup in `internal/orchestrator`/`internal/findings`/`internal/vcs`; independent of v2.0 MCP phases 4–6)
**Requirements**: REQ-cidedup-convergent-review, REQ-cidedup-no-self-resolution (Part A), REQ-cidedup-honor-resolves (Part B), REQ-cidedup-incremental-review (Part C)
**Success Criteria** (what must be TRUE):
  1. A `cadoo ci` re-run against an unchanged head posts zero new threads and resolves zero existing ones (fixed-point); once code stops changing, total thread count is monotonic non-increasing across resyncs.
  2. `resolveStalePriors` no longer resolves still-valid multi-line threads — the carried `StructuralKey` is compared directly (no first-line recompute), and the fix applies to both the CI memory-store and DB-backed worker paths.
  3. A thread resolved by the user (or by Cadoo) stays gone: a reworded duplicate in the same `(tool, file)` with line-overlap or Jaccard ≥ `ResolvedSuppressThreshold` is suppressed, while an unrelated new finding elsewhere in the file is still posted.
  4. Inline-emitting tools review only the `lastReviewedSHA..head` change set (persisted via `<!-- cadoo:reviewed-sha:<sha> -->`); first-run / non-ancestor SHA falls back to full review; `resolveStalePriors` only resolves priors whose anchor line is inside the incremental change set.
**Plans**: 4 plans (4 waves)
  - [x] 08-01-PLAN.md — Part A: carry StructuralKey end-to-end; resolveStalePriors direct compare (no self-resolution; fixes CI + DB paths)
  - [x] 08-02-PLAN.md — Part B: capture anchor line + resolved flag; widen memoryStore.has for sticky suppression of resolved threads
  - [ ] 08-03-PLAN.md — Part C infra: reviewed-sha marker (40-hex validated), DiffBetweener in both adapters, tools.Input dual-context
  - [ ] 08-04-PLAN.md — Part C orchestration + convergence fixed-point test: incremental dispatch, changeSet-scoped resolveStalePriors
**UI hint**: no

## Progress

**Execution Order:**
Shipped: v1.0 (Phases 1-3), v1.1 (Phase 7). Active milestone v2.0 runs in numeric order: 4 → 5 → 6. Queued: v2.1 (Phase 8) — independent of v2.0; promote when ready.

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Generators + Publishers + CLI | v1.0 | 7/7 | Complete | 2026-06-05 |
| 2. Webhook Auto-Trigger + State | v1.0 | 6/6 | Complete | 2026-06-05 |
| 3. API Docs / OpenAPI | v1.0 | 5/5 | Complete | 2026-06-06 |
| 7. Release-Docs Engineering Diagrams | v1.1 | 3/3 | Complete | 2026-06-13 |
| 4. Embedded Local Review + Plugin | v2.0 | 0/TBD | Not started | - |
| 5. Live PR/MR Review | v2.0 | 0/TBD | Not started | - |
| 6. Connected Mode | v2.0 | 0/TBD | Not started | - |
| 8. CI-Mode Dedup Convergence | v2.1 | 2/4 | In Progress|  |
