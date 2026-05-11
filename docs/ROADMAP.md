# Roadmap

| Phase | Scope | Estimate |
|---|---|---|
| 0 | Foundation: monorepo, CI, db, queue interface, LLM gateway, webhook skeleton | 1–2w |
| 1 | `/review` MVP on GitHub: app install, webhook, PR-compression, inline comments, `.cadoo.yaml` | 3–4w |
| 2 | Tool suite: `/describe` `/improve` `/ask` `/changelog` `/add_docs`; slash-command parser | 3–4w |
| 3 | GHES + GitLab adapters; `cadoo-tunnel` reverse-tunnel agent | 3w |
| 4 | Sandboxed static analysis (10–15 linters); agentic tool-loop; severity model; slop detection | 4–6w |
| 5 | KB + learnings (pgvector); multi-repo cross-repo breaking-change index | 3–4w |
| 6 | Jira/Linear, MCP client, Slack agent, VS Code extension | 3w |
| 7 | Autofix suggested commits; `/resolve_conflicts` `/add_tests` `/plan`; custom NL checks; pre-merge gates | 3–4w |
| 8 | SAML SSO, RBAC, audit log, Helm hardening (no-egress mode), seat/billing UI, metrics dashboard | 4–6w |

## Current status

Phase 8 + Phase 4.x + first deferrals batch complete. Idempotent comments (no more spam on resync), `/learn` + `/unlearn`, per-check check runs, smarter KB queries, and Helm hardening (PDB + NetworkPolicy + ServiceMonitor) all landed. Tool surface stands at **13**: `/review` `/describe` `/improve` `/ask` `/changelog` `/add_docs` `/deep_review` `/resolve_conflicts` `/add_tests` `/plan` `/check` `/learn` `/unlearn`.

## Phase 0 deliverables

- [x] Monorepo + Go module
- [x] Provider interfaces: `internal/llm`, `internal/vcs`
- [x] DB schema migration `0001_init.sql`
- [x] In-memory `internal/jobs` queue
- [x] Webhook + API + Worker + CLI binaries
- [x] docker-compose dev stack (postgres+pgvector + LiteLLM proxy)
- [x] CI workflow (vet, lint, test, build, migration up/down)

## Phase 1 deliverables

- [x] GitHub App auth (`bradleyfalzon/ghinstallation` + `google/go-github/v66`)
- [x] Real `vcs.Provider` for GitHub: fetch PR, list files, post summary, post review with inline comments, upsert check run
- [x] Webhook signature verification (HMAC-SHA256 over `X-Hub-Signature-256`)
- [x] Webhook event routing: `pull_request` (opened/synchronize/reopened) → ReviewJob; `issue_comment` `/review` slash command → ReviewJob
- [x] `internal/contextengine`: PR-compression — path-glob filter, binary skip, sort-small-first, per-file budget, truncate overflow
- [x] `internal/tools/review`: prompt template + JSON-structured response parser (resilient to fence-wrapped output)
- [x] `internal/orchestrator`: pipeline that fetches → packs → calls model → posts comments + check run; severity threshold + max-comments respected
- [x] `internal/riverq`: River-backed Queue (Postgres). Webhook = enqueue-only client; worker = consumer.
- [x] Queue selection in `cadoo-webhook` / `cadoo-worker`: River when `DATABASE_URL` set, in-memory otherwise.

### Phase 1 deferrals

- [x] Idempotent comment editing via `findings` fingerprint — done in deferrals batch (`internal/findings` + `Dispatcher.postSummary`/`postInline`)
- [ ] Multi-installation cache (carried into Phase 2.x)
- [x] Loading per-repo `.cadoo.yaml` from the PR head — done in Phase 2 (`orchestrator.loadCfg` via `FetchFileFromRef`)

## Phase 2 deliverables

- [x] `internal/tools`: shared `Input`/`Result`/`Tool`/`Registry` + `CallJSON`/`CallText`/`ExtractJSON` helpers + `BuildDiffPrompt`
- [x] `/review` refactored to satisfy `tools.Tool`
- [x] `/describe` — proposes title + body (posts as summary; `EditPRBody` field reserved for Phase 2.x)
- [x] `/improve` — inline GitHub `suggestion` blocks
- [x] `/ask` — Q&A using slash-command remainder as the question
- [x] `/changelog` — Keep-a-Changelog grouped entries
- [x] `/add_docs` — inline `suggestion` blocks for undocumented public symbols
- [x] `orchestrator.Dispatcher` (replaces `Reviewer`): registry-driven routing, generic `ToolJob`, `applyResult` posts Summary / InlineComments / CheckRun
- [x] `riverq.ToolArgs` (replaces `ReviewArgs`): `Tool` + `Args` fields; one River worker dispatches to all tools
- [x] Webhook routing: slash commands → `ToolJob{Tool: cmd, Args: rest}`; `pull_request` opened/synchronize/reopened walks `cfg.Auto` and enqueues every matching tool
- [x] `config.Parse([]byte)`; `github.Adapter.FetchFileFromRef` for per-PR `.cadoo.yaml`
- [x] Tests: registry, slash parser, trigger matrix, dispatcher routing, fenced-JSON extraction

### Phase 2 deferrals (carried into Phase 2.x or later)

- [ ] Idempotent comment editing — needs `findings` table writes + `UpdatePullRequestComment` for review threads
- [ ] Multi-installation cache — adapter pool keyed by installation ID, populated from a DB-backed installations table
- [ ] `EditPRBody` action wiring — `vcs.Provider.EditPullRequest(title, body)` method + GitHub/GitLab adapter implementations
- [ ] `pull_request_review_comment` / GitLab inline-note events for inline `/ask` (asking on a specific line)

## Phase 3 deliverables

- [x] `internal/vcs/gitlab` — real adapter (xanzy/go-gitlab v0.115). FetchPullRequest (= MR), ListChangedFiles, PostSummaryComment + Update, PostInlineComments via Discussions API + position objects (base_sha/start_sha/head_sha, new_path/new_line), UpsertCheckRun (= commit status), FetchFileFromRef
- [x] `internal/vcs/gitlab/webhook.go` — VerifyToken (X-Gitlab-Token constant-time compare), ParseEvent
- [x] GHES base-URL plumbing: `GITHUB_BASE_URL` + `GITHUB_UPLOAD_URL` env passed through `cadoogh.Config`
- [x] `orchestrator.Dispatcher.VCSPool map[vcs.Kind]vcs.Provider` — replaced single `VCS` field; `ToolJob.Provider` selects which adapter runs
- [x] `riverq.ToolArgs` — added `Provider string` field; River worker round-trips via `vcs.Kind`
- [x] Webhook routes: `/webhook/github` (GitHub.com + GHES) and `/webhook/gitlab`. Each verifies its own auth scheme, parses provider-typed events, walks `cfg.Auto`, and enqueues `ToolJob{Provider, Tool, …}` per match. Slash commands routed for both providers
- [x] `cmd/cadoo-tunnel` — agent half: outbound HTTPS long-poll to `/v1/tunnel/poll?tenant=…` on the Cadoo SaaS, exponential backoff on transport errors, forwards each `delivery{Path, Headers, Body}` to the local `cadoo-webhook` verbatim (signatures still verify end-to-end). SaaS-side fan-out endpoint scheduled for Phase 3.x.
- [x] `settings`: `HasGitHub()` / `HasGitLab()` predicates; webhook + worker construct each adapter conditionally, both bins fail clean if zero configured

### Phase 3 deferrals (carried into Phase 3.x or later)

- [ ] SaaS-side `/v1/tunnel/poll` fan-out endpoint, agent token issuance, delivery store
- [ ] Reverse-tunnel auth + multi-tenant addressing (today's agent assumes one tenant per process)
- [ ] GitLab `pull_request_review_comment`-equivalent (inline note replies) events
- [ ] GitLab self-signed-cert support (TLS skip flag for on-prem with internal CA)
- [ ] Bitbucket Cloud / Bitbucket DC / Azure DevOps adapters (deferred per scope decision)
- [ ] Migrate from xanzy/go-gitlab (archived) to gitlab.com/gitlab-org/api/client-go

## Phase 4 deliverables

- [x] `internal/analysis` — Finding, Severity, Linter, Workspace, Registry
- [x] `internal/analysis/sandbox` — Spec, Result, Runner; DockerRunner shells out to `docker run` (NoNetwork, MemoryLimit, CPULimit, read-only mounts, ordered Mounts/Env keys); MockRunner for tests
- [x] `internal/analysis/linters/golangci` — `golangci-lint run --out-format json` parser
- [x] `internal/analysis/linters/ruff` — `ruff check --output-format json` parser
- [x] `internal/agent` — Loop with safety-bounded iteration; `read_file` and `grep` tools; FileReader interface
- [x] `internal/slop` — heuristic detector (empty body, generic title, large unexplained diff, etc.)
- [x] `tools.Input` extended: Reader, Analysis, Slop fields
- [x] `tools.BuildDiffPrompt` — includes slop signal + lint-narrowed findings when present
- [x] Dispatcher: computes `slop.Detect` on every run; wires `prFileReader` adapter when the VCS supports `FetchFileFromRef`
- [x] `internal/tools/deepreview` — `/deep_review` tool: agent loop + JSON-structured findings → inline review

### Phase 4 deferrals

- [x] Workspace setup: clone-at-head — done in Phase 4.x via tarball download (`internal/analysis/workspace`)
- [x] Linter dispatch from the orchestrator — done in Phase 4.x; `Dispatcher.runLinters` groups changed files by extension and fans out in parallel
- [x] Bundled `cadoo/sandbox-polyglot` Docker image — done in Phase 4.x; `deploy/docker/Dockerfile.sandbox-polyglot` bundles five linters
- [x] More linters: ESLint, Semgrep, ShellCheck — done in Phase 4.x (now five total: golangci-lint, ruff, eslint, semgrep, shellcheck)
- [ ] Yet more linters: Oxlint/Biome, Clippy, TruffleHog/OSV-Scanner, Clang-Tidy, hadolint, actionlint
- [ ] ast-grep tool for the agent loop (structural search beyond plain grep)
- [ ] Sandbox cost controls: warm-pool of containers per language; cache lint results keyed by file hash

## Phase 4.x deliverables

- [x] `internal/analysis/workspace` — `Workspace.Open` materializes a PR head tarball into a temp directory with path-traversal guards and single-root-flatten; `RepoArchiver` interface; safe `Close()` on every exit path; `extractTarGz` rejects entries that escape `dest`.
- [x] `RepoArchiver` on both VCS adapters: `github.Adapter.FetchArchive` follows GitHub's tarball link with `http.DefaultClient` (presigned URL); `gitlab.Adapter.FetchArchive` calls `Repositories.Archive` with format=tar.gz and wraps the byte slice as an `io.ReadCloser`.
- [x] Three new linter wrappers: `eslint` (`--format json`, severity 1/2 → warning/error), `semgrep` (`--json` with `p/default`, INFO/WARNING/ERROR mapping), `shellcheck` (`-f json`, level + SC code).
- [x] `Dispatcher.runLinters`: opens workspace; groups changed files by extension (skipping binary + removed); fans out matching linters in parallel via WaitGroup; merges findings under a mutex; returns nil cleanly when not configured. Runs unconditionally on every dispatch when configured (the existing prompt-side wiring renders findings only when present).
- [x] `orchestrator.DefaultLintRegistry(polyglotImage)` — convenience constructor that registers all five wrappers with a single shared image.
- [x] `deploy/docker/Dockerfile.sandbox-polyglot` — Alpine-based image bundling node, python, golangci-lint (static binary), ruff (static binary), eslint, semgrep, shellcheck.
- [x] `settings`: `CADOO_SANDBOX_IMAGE`, `CADOO_SANDBOX_DOCKER_BIN`. webhook + worker wire `LinterRegistry` + `DockerRunner` when `CADOO_SANDBOX_IMAGE` is set; sandbox stays disabled (no-op `runLinters`) otherwise.

## Phase 5 deliverables

- [x] Migration `0002_kb_learnings.sql` — `kb_documents`, `kb_chunks` (`vector(1536)` + IVFFlat cosine index), `learnings` (weighted with `accepted`/`rejected`/`weight` columns)
- [x] `internal/llm/embed` — Embedder interface + OpenAI-compatible client targeting `/v1/embeddings` (defaults to `text-embedding-3-small`, 1536 dim)
- [x] `internal/kb` — Chunk(body) char-window splitter with paragraph awareness + overlap; `Store.IngestDocument` (transactional upsert + chunk replacement) and `Store.Search` (cosine top-K)
- [x] `internal/learnings` — `Reaction` (Accept/Reject), `Rule`; `Store.Record` (upsert + weight recompute clamped 0.05..0.95) and `Store.Active(repoKey, limit, minWeight)`
- [x] `internal/db` — pgvector type registration on every fresh pgxpool connection (best-effort: tolerates databases without the extension)
- [x] `tools.Input` extended with `KBHits []kb.Hit` and `Learnings []learnings.Rule`
- [x] `BuildDiffPrompt` renders both sections when present (learnings as weighted bullets; KB hits with similarity score and truncated bodies)
- [x] `orchestrator.Dispatcher.KB` and `.Learnings` fields; per-dispatch lookup keyed by `provider:repo_full_name`
- [x] `cadoo-webhook` and `cadoo-worker` construct both stores when DATABASE_URL is set; silently skip the knowledge layer otherwise

### Phase 5 deferrals (carried into Phase 5.x or later)

- [x] Reaction capture — done in deferrals batch as explicit `/learn` + `/unlearn` slash commands (more reliable than webhook reaction events; users get explicit control)
- [ ] KB ingestion sources: repo `docs/`, ADRs, MCP servers (Confluence/Notion/Linear)
- [ ] Multi-repo cross-reference index for breaking-change detection across services in the same org
- [x] Smarter KB query construction — done in deferrals batch (`internal/kb/querydistill`)
- [ ] Hybrid search: pgvector cosine + BM25 rerank
- [ ] KB freshness: scheduled re-embedding when a doc changes

## Phase 6 deliverables

- [x] `internal/issuetrackers` — Issue + Tracker interface + shared `ExtractKeys` (matches `TEAM-NNN` style for both Jira and Linear)
- [x] `internal/issuetrackers/jira` — REST client (`/rest/api/3/issue/{key}`); supports basic auth (Cloud) and PAT bearer
- [x] `internal/issuetrackers/linear` — GraphQL client against `api.linear.app/graphql`
- [x] `internal/notifiers/slack` — incoming-webhook poster invoked after `applyResult` for every dispatch
- [x] `internal/mcp` — JSON-RPC 2.0 envelope + Streamable-HTTP `Call` / `ListTools` / `CallTool` (single round-trip per call)
- [x] `tools.Input.Issues []issuetrackers.Issue` + prompt rendering as "Linked tracker issues (validate the PR addresses these)"
- [x] `orchestrator.Dispatcher.Trackers` (slice consulted in order) + `.Notifier` (`ResultNotifier` interface)
- [x] `settings`: `JIRA_BASE_URL`/`JIRA_EMAIL`/`JIRA_TOKEN`, `LINEAR_API_KEY`, `SLACK_WEBHOOK_URL`
- [x] cmd wiring: `buildTrackers(s)` in both `cadoo-webhook` and `cadoo-worker`; Slack notifier attached when configured
- [x] `ide/vscode/` — extension scaffold (package.json + tsconfig + extension.ts) with two commands (`cadoo.review`, `cadoo.config.validate`) and a settings entry for `cadoo.apiUrl`

### Phase 6 deferrals (carried into Phase 6.x or later)

- [ ] MCP stdio transport (currently HTTP-only) and bidirectional notifications/resource subscription
- [ ] Wire MCP-server tools into the agent loop alongside built-in `read_file` / `grep`
- [ ] Slack slash commands + interactive shortcuts (today is one-way notifications)
- [ ] Inline `/ask` on Slack threads
- [ ] Real VS Code review behaviour: `cadoo-api` `/v1/review` endpoint that takes a unified diff; render findings as VS Code Diagnostics
- [ ] Linked-issue ingestion to KB (so PR descriptions automatically pull in past tickets they reference)
- [ ] GitHub Issues / GitLab Issues trackers (currently only external Jira / Linear)

## Phase 7 deliverables

- [x] `internal/tools/resolveconflicts` — `/resolve_conflicts`. Detects literal `<<<<<<<` markers in the diff; emits per-block resolution suggestions as inline ` ```suggestion ` blocks. Short-circuits with a friendly message when no markers are present.
- [x] `internal/tools/addtests` — `/add_tests`. Generates language-specific unit-test scaffolds (table-driven Go, pytest, jest/vitest, cargo-test) for changed functions; output rendered as code blocks in the summary comment.
- [x] `internal/tools/plan` — `/plan`. Takes a PRD from `/plan <text>` args (falls back to PR body); outputs numbered steps with file lists and explicit open-questions section.
- [x] `internal/tools/check` — `/check`. Iterates `.cadoo.yaml` `checks:` entries; one focused LLM call per rule scoped via path glob; aggregates findings into one PR review with `<rule_name>` attribution on each comment.
- [x] All four tools registered in `orchestrator.DefaultRegistry`.
- [x] `docs/PRE_MERGE_GATES.md` — branch-protection wiring against `cadoo/review` for both GitHub and GitLab; documents `request_changes_on` semantics.

### Phase 7 deferrals (carried into Phase 7.x or later)

- [ ] Apply suggested commits via the GitHub `pulls/{n}/comments/{id}/replies` "Apply suggestion" path (today comments hold ` ```suggestion ` blocks but Cadoo doesn't auto-apply them on a "👍" reaction)
- [x] Per-check check-run names — done in deferrals batch (`/check` now emits `cadoo/check/<rule>` check-runs; `tools.Result.CheckRuns` slice added)
- [ ] `/resolve_conflicts` against actual three-way merge state (today only handles markers already present in the diff; can't propose for a "merge will conflict" PR with no markers yet)
- [ ] `/add_tests` that posts directly into the right test file when one exists (today pastes into the summary)
- [ ] `/plan` follow-through: optionally dispatch each step as its own follow-up tool job

## Phase 8 deliverables

- [x] `internal/auth` — OIDC verifier (`coreos/go-oidc/v3`); Role tier (Owner/Admin/Member/Viewer) with `Allows(min)` rank check; chi middleware `Required` + `RequireRole`; `ClaimsFrom`/`WithClaims` context helpers
- [x] `internal/audit` — append-only `Logger` over the `audit_events` table; `Record` writes a structured event; `Query` returns recent events for org or system-wide; nil-safe so callers don't need to nil-check
- [x] `internal/metrics` — Prometheus instruments: `cadoo_dispatch_total{tool,provider,outcome}`, `cadoo_dispatch_duration_seconds{tool,provider}`, `cadoo_llm_call_total{model,outcome}`, `cadoo_llm_tokens_total{model}`. Recorded automatically on every `Dispatcher.Run`
- [x] `internal/reports` — `Reporter.Run` ticker loop aggregates audit events and posts a `Summarize`d per-action count table via the configured `ResultNotifier`; runs in a `cadoo-worker` goroutine when `REPORTS_INTERVAL` is set
- [x] `cadoo-api` rewritten with: `/healthz` and `/version` open; `/metrics` (Prometheus exposition); `/v1/*` gated by OIDC verifier when `OIDC_ISSUER`/`OIDC_CLIENT_ID` are set, returning 503 otherwise. Endpoints: `/v1/me`, `/v1/audit` (admin-only)
- [x] `Dispatcher` gains `.Audit *audit.Logger`; `.Run` is now `(retErr error)` so a deferred block records metrics + audit on every exit path
- [x] `settings`: `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `REPORTS_INTERVAL`
- [x] `deploy/helm/cadoo/` — production Helm chart: `Chart.yaml`, `values.yaml`, `_helpers.tpl` (envList/envFromList split keeps templates structurally valid), templates for postgres+pgvector StatefulSet, LiteLLM Deployment+Service+ConfigMap, cadoo-api/webhook/worker Deployments+Services, optional Ingress for the webhook. `helm lint` + `helm template` both pass.

### Phase 8 deferrals (carried into post-MVP)

- [ ] Frontend: seat assignment, billing UI, audit explorer (today exposed as REST only)
- [ ] SAML SSO via Dex bundled in the chart (today the OIDC verifier just talks to whatever IdP — Dex/Okta/Auth0/Google — but you have to deploy the IdP yourself)
- [ ] OIDC-token-driven multi-tenancy across the dispatcher (today `Run` always logs system-level audit; per-org claims aren't passed through)
- [ ] Per-org metric labels (today metrics are global)
- [ ] Audit log streaming export (S3 mirror, SIEM webhook)
- [x] Helm chart hardening: PodDisruptionBudgets, NetworkPolicy (deny-all + allow-intra + allow-egress-CIDRs), ServiceMonitor for kube-prometheus — done in deferrals batch (still TODO: PodSecurityStandards, HPA)
