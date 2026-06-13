# Roadmap: Cadoo

## Milestones

- ✅ **v1.0 Release Docs** - Phases 1-3 (shipped 2026-06-06)
- 🚧 **v1.1 Release-Docs Engineering Diagrams** - Phase 7 (active)
- ⏸️ **v2.0 MCP Server + Claude Code Plugin** - Phases 4-6 (defined, deferred behind v1.1 — 0 plans started)

## Phases

<details>
<summary>✅ v1.0 Release Docs (Phases 1-3) — SHIPPED 2026-06-06</summary>

### Phase 1: Generators + Publishers + CLI

**Goal**: A maintainer can run `cadoo release-docs --repo … --from vX --to vY` and have Cadoo generate a grouped changelog section and polished release notes for that range and publish them — to the release body and to a single `CHANGELOG.md` PR — idempotently, with no DB, dogfooded on Cadoo's own repo.
**Depends on**: Nothing (first phase)
**Requirements**: REQ-release-artifact-generation (changelog + release-notes), REQ-per-artifact-toggles, REQ-configurable-templates, REQ-release-docs-idempotency (stateless/marker mode), REQ-configurable-trigger (manual CLI/CI entry point), REQ-publish-destinations (releasebody + changelogpr)
**Success Criteria** (what must be TRUE):
  1. Running `cadoo release-docs --repo … --from vX --to vY` produces a grouped changelog section (Features/Fixes/Breaking/…) and polished release notes for the commits/PRs in that range.
  2. The release body is updated inside Cadoo markers (user content preserved) and a single `CHANGELOG.md` PR is opened or updated for the release tag.
  3. Re-running the same command edits the release body in place and updates the same changelog PR/branch — no duplicates — with no database (stateless marker reconstruction).
  4. A disabled artifact (`enabled:false`) or one whose `when:` excludes the bump is never generated; the changelog runs with LLM off and produces reproducible output.
  5. An artifact's `template:` override (loaded from the tag tree) replaces the preset; with no override, embedded preset templates are used.
  6. The flow is dogfooded end-to-end on Cadoo's own repository.
**Plans**: 7 plans

Plans:
**Wave 1**
- [x] 01-01-PLAN.md — Core contract layer: vcs capability interfaces + types, releasedocs types/interfaces, marker constants, config.ReleaseDocs schema, shared fake provider

**Wave 2** *(blocked on Wave 1 completion)*
- [x] 01-02-PLAN.md — ReleaseContext builder + grouped change model (conventional/labels), semver bump, Enabled gate (TDD)
- [x] 01-03-PLAN.md — Template subsystem: go:embed presets + tag-tree override loader (text/template)
- [x] 01-04-PLAN.md — VCS adapter capabilities (GitHub + GitLab): range read, release publish, branch-commit

**Wave 3** *(blocked on Wave 2 completion)*
- [x] 01-05-PLAN.md — Generators: deterministic-first changelog (golden-file) + release-notes (skeleton + tone narrative) (TDD)

**Wave 4** *(blocked on Wave 3 completion)*
- [x] 01-06-PLAN.md — Publishers: releasebody marker upsert + changelogpr single marker-keyed PR (idempotent, TDD)

**Wave 5** *(blocked on Wave 4 completion)*
- [x] 01-07-PLAN.md — Dispatcher + registry + release-docs CLI subcommand + dogfood on Cadoo's repo

### Phase 2: Webhook Auto-Trigger + State

**Goal**: A published release (or configured tag push) on a customer repo automatically triggers release-docs generation through the dual-mode queue and worker, with DB-backed state so re-syncs edit in place, plus the pages publisher and blog generator.
**Depends on**: Phase 1
**Requirements**: REQ-configurable-trigger (release/tag webhook ingestion), REQ-release-docs-idempotency (DB-backed state table + migration), REQ-publish-destinations (pages), REQ-release-artifact-generation (blog generator)
**Success Criteria** (what must be TRUE):
  1. A GitHub `release: published` / GitLab release webhook (and, when configured, a `v*` tag push filtered by `tagPattern`) builds a `ReleaseJob` and enqueues it via River when `DATABASE_URL` is set or the in-memory queue otherwise; the worker consumes it and runs the dispatcher.
  2. If `releaseDocs.trigger` excludes the event kind, the webhook no-ops early.
  3. Published state is recorded in a new `(provider, repo, to_tag, artifact_kind)` table (migration round-trips `up → down → up`) so re-runs edit the release body and changelog PR in place when DB-backed.
  4. The pages publisher commits rendered artifacts to the configured `branch`/`dir` at deterministic paths (`docs/releases/vX.Y.Z/…`); re-runs overwrite the same paths.
  5. The blog generator produces a long-form announcement on minor/major releases (per its `when:` condition) and is routed to pages.
**Plans**: 6 plans

Plans:
**Wave 1** *(parallel)*
- [x] 02-01-PLAN.md — CR-01 fix (TagReleasePublisher) + config schema (Blog, PagesPublishTarget) + KindBlog/TargetPages constants
- [x] 02-02-PLAN.md — Migration 0006 release_docs_state + nil-tolerant state.Store (DB-backed idempotency)

**Wave 2** *(parallel; blocked on 02-01)*
- [x] 02-03-PLAN.md — Blog generator (long-form, when: minor_or_above, nil-tolerant) (TDD)
- [x] 02-04-PLAN.md — Pages publisher (deterministic paths via BranchCommitter.UpsertFile, idempotent overwrite)

**Wave 3** *(blocked on Wave 2)*
- [x] 02-05-PLAN.md — Webhook release/tag ingestion (GitHub + GitLab) → ReleaseJob → dual-mode enqueue; riverq ReleaseArgs/EnqueueRelease

**Wave 4** *(blocked on 02-05)*
- [x] 02-06-PLAN.md — Worker consumer: releasedocs dispatcher with blog+pages defaults + DB-backed PostedStore; releaseWorker registration

### Phase 3: API Docs / OpenAPI

**Goal**: For a supported framework, Cadoo derives an OpenAPI spec and a rendered API reference from the repo's code at release time and publishes them to pages.
**Depends on**: Phase 2
**Requirements**: REQ-release-artifact-generation (api-docs + openapi)
**Success Criteria** (what must be TRUE):
  1. For a repo in the supported (narrow, well-supported) framework set, the apidocs generator produces an OpenAPI YAML spec and a rendered API reference from the code.
  2. The generated API docs and OpenAPI are published to pages at deterministic paths and are idempotent across re-runs.
  3. A repo outside the supported framework set degrades gracefully (apidocs skipped with a logged reason), without failing the rest of the release-docs run.
**Plans**: 5 plans

Plans:
**Wave 1** *(BLOCKING — deps + model/wiring foundation)*
- [x] 03-01-PLAN.md — Wave 0 foundation: go get libopenapi + libopenapi-validator, vendor Redoc bundle, Artifact.Filename + KindAPIDocs + MultiGenerator + dispatcher spread, pages-publisher Filename change, APIDocsConfig

**Wave 2** *(blocked on 03-01)*
- [x] 03-02-PLAN.md — Wave 0 test scaffold: fixtures (v2/v3/v31/invalid/remote-ref/oversized), fakeFetcher, table-driven stubs for D-01..D-10 + security, pages apidocs path/idempotency tests

**Wave 3** *(blocked on 03-02)*
- [x] 03-03-PLAN.md — Spec ingestion: discover.go (fallback + 404 tolerance) + parse.go (libopenapi, version detect, validation, $ref-SSRF + 5MB-OOM guards, Swagger 2.0 isolation)

**Wave 4** *(blocked on 03-03)*
- [x] 03-04-PLAN.md — Renderers: render_html.go (offline Redoc, no-CDN, deterministic sorted-key JSON) + render_markdown.go (text/template, sorted iteration, injection-escape) + preset + golden files

**Wave 5** *(blocked on 03-04)*
- [x] 03-05-PLAN.md — apidocs Generator (Kind/Enabled/GenerateMulti, raw passthrough, graceful skip) + DefaultGenerators registration + .cadoo.yaml.example + full-suite + offline-render checkpoint

</details>

### 🚧 v2.0 MCP Server + Claude Code Plugin (In Progress)

**Milestone Goal:** Expose Cadoo's review tools to AI assistants via a new `cadoo-mcp` MCP server binary and a Claude Code plugin, enabling local diff review and live PR/MR review from inside any MCP-compatible client.

- [ ] **Phase 4: Embedded Local Review + Plugin** - `cadoo-mcp` binary with stdio transport, local diff review, foundational contracts (stdout discipline, strict schemas, progress, credentials), and Claude Code plugin
- [ ] **Phase 5: Live PR/MR Review** - `target=pr` dry-run and idempotent post-back on GitHub/GHES/GitLab, security gates, multi-client setup docs
- [ ] **Phase 6: Connected Mode** - `cadoo-api` sync endpoint, KB + learnings in results, `learn`/`unlearn` tool exposure

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

### Phase 7: Release-Docs Engineering Diagrams

**Milestone**: v1.1 Release-Docs Engineering Diagrams
**Goal**: At release time, Cadoo generates user-selected software-engineering diagrams (sequence, dependency, state, flowchart, class) from the repository and publishes them as a release-docs artifact to pages — per-type choosable in `.cadoo.yaml`, idempotent across re-runs, deterministic-first, and degrading gracefully when a type can't be derived.
**Depends on**: Phase 3 (extends the shipped `internal/releasedocs` artifact + pages-publisher pipeline; independent of the v2.0 MCP phases 4–6)
**Requirements**: DIAG-01, DIAG-02, DIAG-03, DIAG-04, DIAG-05
**Success Criteria** (what must be TRUE):
  1. A user can enable a `diagrams` release-docs artifact in `.cadoo.yaml` and choose which diagram types are produced (sequence, dependency, state, flowchart, class); a type not selected is never generated.
  2. For each selected type, Cadoo produces a diagram derived from the repository at release time and publishes it to pages at deterministic paths; re-running the release overwrites the same paths (idempotent), consistent with the api-docs publisher (Phase 3).
  3. A diagram type that cannot be derived for the repo is skipped with a logged reason, without failing the changelog/release-notes/blog/api-docs artifacts in the same release-docs run.
  4. Diagram generation is deterministic-first and reproducible with the LLM disabled (golden-file testable), consistent with the changelog/release-notes generators; any LLM use is nil-tolerant and only refines.
  5. The flow is dogfooded end-to-end on Cadoo's own repository.
**Plans**: 3 plans

Plans:
**Wave 1**
- [x] 07-01-PLAN.md — Contract/config layer: KindDiagrams const + DiagramsConfig (5 per-type path lists + family gate) + .cadoo.yaml.example doc

**Wave 2** *(blocked on 07-01)*
- [x] 07-02-PLAN.md — diagrams generator package: Mermaid keyword sniff + fixed fence wrapper + GenerateMulti (ordered-slice, graceful skip) + golden tests

**Wave 3** *(blocked on 07-02)*
- [ ] 07-03-PLAN.md — DefaultGenerators registration + pages path/idempotency tests + dogfood Mermaid sources (SC-5) + human-verify checkpoint

> **Design resolved in `/gsd:discuss-phase 7` (see 07-CONTEXT.md, D-01..D-10):** render committed Mermaid sources (no derivation, no LLM); per-type explicit config paths (no tree listing); fixed ` ```mermaid ` fence wrapper (no rendering runtime); pages-only deterministic idempotent paths `releases/<tag>/diagrams/<type>/<name>.md`; per-source graceful skip via `(nil,nil)`.

## Progress

**Execution Order:**
Active milestone v1.1 runs first: Phase 7. Deferred milestone v2.0 follows in numeric order: 4 → 5 → 6.

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Generators + Publishers + CLI | v1.0 | 7/7 | Complete | 2026-06-05 |
| 2. Webhook Auto-Trigger + State | v1.0 | 6/6 | Complete | 2026-06-05 |
| 3. API Docs / OpenAPI | v1.0 | 5/5 | Complete | 2026-06-06 |
| 7. Release-Docs Engineering Diagrams | v1.1 | 2/3 | In Progress|  |
| 4. Embedded Local Review + Plugin | v2.0 | 0/TBD | Deferred | - |
| 5. Live PR/MR Review | v2.0 | 0/TBD | Deferred | - |
| 6. Connected Mode | v2.0 | 0/TBD | Deferred | - |
