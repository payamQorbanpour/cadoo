# Architecture
_Last updated: 2026-05-19_

## System Overview

Cadoo is an AI code reviewer that posts inline review comments, summary comments, and check-runs to GitHub (including GHES) and GitLab. It is feature-parity-targeted at Qodo Merge and CodeRabbit, built as multi-tenant SaaS + self-host on the same binary.

```text
┌──────────────────────────────────────────────────────────────────────┐
│              Inbound: cadoo-webhook  /  cadoo-cli ci                  │
│  POST /webhook/github   POST /webhook/gitlab   cadoo ci --pr <url>   │
└────────────────────────────┬─────────────────────────────────────────┘
                             │  orchestrator.ToolJob (JSON)
                    ┌────────▼───────────┐
                    │   Queue selection  │
                    │  (DATABASE_URL?)   │
         ┌──────────┴──────────────────┐ └──────────────────────────┐
         │  River (Postgres-backed)    │     In-memory (dev mode)   │
         │  internal/riverq            │     internal/jobs          │
         └──────────┬──────────────────┘ ─────────────┬─────────────┘
                    │                                  │
                    └──────────────┬───────────────────┘
                                   │
                    ┌──────────────▼────────────────────┐
                    │    orchestrator.Dispatcher.Run()   │
                    │    internal/orchestrator/          │
                    │    reviewer.go                     │
                    └─────┬──────┬──────┬───────────────┘
              ┌───────────┘      │      └─────────────────────────┐
              ▼                  ▼                                 ▼
   ┌──────────────────┐  ┌───────────────────┐     ┌─────────────────────────┐
   │  vcs.Provider    │  │  tools.Tool        │     │  knowledge layer        │
   │  (GitHub/GitLab) │  │  (review/describe/ │     │  kb, learnings, audit   │
   │  internal/vcs/   │  │  improve/…)        │     │  internal/kb,learnings  │
   └──────┬───────────┘  │  internal/tools/  │     └─────────────────────────┘
          │              └───────────────────┘
          ▼
  VCS comment / check-run
  (inline + consolidated summary)
```

## Five Binary Roles

| Binary | Entry point | Purpose |
|--------|-------------|---------|
| `cadoo-webhook` | `cmd/cadoo-webhook/main.go` | HTTP server receiving VCS webhooks; verifies signatures; enqueues `orchestrator.ToolJob` to River or in-memory queue |
| `cadoo-worker` | `cmd/cadoo-worker/main.go` | River consumer; dequeues and runs `orchestrator.Dispatcher.Run()` for each job |
| `cadoo-cli` | `cmd/cadoo-cli/main.go` + `ci.go` | Local CLI: `version`, `config validate`, and `cadoo ci --pr/--mr <url>` one-shot CI-mode review (stateless, no DB) |
| `cadoo-api` | `cmd/cadoo-api/main.go` | REST API + dashboard backend (OIDC-protected `/v1/*`, Prometheus `/metrics`, public `/healthz`) |
| `cadoo-tunnel` | `cmd/cadoo-tunnel/main.go` | Reverse-tunnel agent so SaaS reaches private GHES/GitLab without inbound firewall rules |

### Queue Duality

When `DATABASE_URL` is set, `cadoo-webhook` enqueues to River (Postgres) and `cadoo-worker` dequeues. Without `DATABASE_URL`, `cadoo-webhook` spins an in-process `jobs.NewMemory()` consumer in a sibling goroutine — single-binary dev mode. `cadoo-cli ci` bypasses the queue entirely and calls `Dispatcher.Run()` synchronously.

## Core Review Pipeline

`orchestrator.Dispatcher.Run(ctx, ToolJob)` in `internal/orchestrator/reviewer.go` is the single gate both webhook/worker and CI-mode drive.

### Step-by-step flow

1. **Provider lookup** — pick `vcs.Provider` from `Dispatcher.VCSPool[job.Provider]`
2. **Tool lookup** — pick `tools.Tool` from `Dispatcher.Registry.Get(job.Tool)`
3. **Fetch PR** — `provider.FetchPullRequest()` → normalized `*vcs.PullRequest`
4. **Load per-PR config** — `Dispatcher.loadCfg()` fetches `.cadoo.yaml` from the PR head SHA via `FileFetcher` capability; falls back to `Dispatcher.BaseCfg` if absent
5. **List changed files** — `provider.ListChangedFiles()` → `[]vcs.FileChange`
6. **Compress context** — `contextengine.Compress(files, opts)` → token-bounded `contextengine.Compressed`; budget: 50 000 total / 8 000 per-file tokens
7. **Slop pre-check** — `slop.Detect()` classifies PR quality cheaply before paying for LLM
8. **Static analysis (optional)** — `Dispatcher.runLinters()` materializes head as temp workspace, runs sandboxed linters in parallel via `analysis.Linter.Run()`, merges `[]analysis.Finding`
9. **Issue tracker lookup** — each `issuetrackers.Tracker.FindLinked()` resolves Jira/Linear keys from PR title/body
10. **Prior findings load** — `d.Posted.ListPostedFindings()` pre-populates `tools.Input.PriorFindings` so the LLM sees what's already posted
11. **Learnings load** — `d.Learnings.Active()` fetches top-10 per-repo rules above weight 0.6
12. **KB semantic search** — `d.KB.Search()` retrieves top-5 cosine-nearest chunks; optionally distilled via `KBDistiller.Distill()` (one cheap LLM call)
13. **Run tool** — `tool.Run(ctx, tools.Input)` → `*tools.Result` (summary, inline comments, check-runs)
14. **Apply result** — `Dispatcher.applyResult()`:
    - `postSummary` — edits consolidated wrapper comment in place (or creates it)
    - `postInline` — deduplicates by `StructuralKey` + Jaccard title similarity; stamps hidden marker; resolves stale prior threads
    - `UpsertCheckRun` (if `ReportStatus`)
    - `applyPRBody` (if `EditPRBody` non-nil — used by `/describe`)
15. **Notify** — `Dispatcher.Notifier.NotifyResult()` (e.g. Slack webhook, best-effort)
16. **Audit** — `d.Audit.Record()` deferred; Prometheus counters incremented

## Key Subsystems

### orchestrator (`internal/orchestrator/`)

- `reviewer.go` — `Dispatcher` struct and `Run()` method; the main pipeline
- `consolidate.go` — `renderConsolidated()`: builds the single wrapper comment from per-tool sections; defines HTML marker constants (`<!-- cadoo:wrapper:begin -->`, `<!-- cadoo:pr-body:begin -->`)
- `registry.go` — `DefaultRegistry()`: registers all 13 built-in tools
- `lintreg.go` — `DefaultLintRegistry()`: registers golangci, ruff, eslint, semgrep, shellcheck

### tools (`internal/tools/`)

- `tools.go` — `Tool` interface, `Input`, `Result`, `Registry`
- 13 subpackages: `review/`, `describe/`, `improve/`, `ask/`, `changelog/`, `adddocs/`, `addtests/`, `deepreview/`, `resolveconflicts/`, `plan/`, `check/`, `learn/`, `unlearn/`
- Each satisfies `tools.Tool` with `Name() string` and `Run(ctx, Input) (*Result, error)`

### vcs (`internal/vcs/`)

- `vcs.go` — `Provider` interface (8 methods), `PullRequest`, `FileChange`, `InlineComment`, `CheckRun`, `PriorReviewReader` optional capability
- `marker.go` — `InlineMarker()` / `ParseInlineMarker()`: the hidden `<!-- cadoo:fp v=1 … -->` dedup marker protocol
- `github/github.go` — `Adapter` for GitHub.com + GHES; auth: GitHub App installation (`bradleyfalzon/ghinstallation`) or bearer token
- `gitlab/gitlab.go` — `Adapter` for GitLab SaaS + self-managed; auth: PAT/project token via `xanzy/go-gitlab` (now `gitlab.com/gitlab-org/api/client-go`)

### findings (`internal/findings/`)

- `findings.go` — `Store` wrapping either Postgres (`posted_findings` + `posted_summaries` tables) or an in-memory map with optional JSON file persistence
- Key methods: `HasFinding()`, `RecordFinding()`, `ListPostedFindings()`, `SummaryID()`, `PutSummaryID()`, `PutSection()`, `AllSections()`
- `Fingerprint()` — SHA1 of full comment body (exact match)
- `StructuralKey()` — coarser key: `tool|file|linerange|severity` (near-dup match)
- `normalizeTitle()` + Jaccard similarity — text-level dedup for rephrased findings
- `prior.go` — `NewFromPrior(key, vcs.PriorReview)`: seeds an in-memory Store from PR read-back for CI-mode

### llm (`internal/llm/`)

- `provider.go` — `Provider` interface: `Chat(ctx, ChatRequest) (*ChatResponse, error)`
- `litellm/client.go` — HTTP client for LiteLLM proxy (OpenAI-compatible `/v1/chat/completions`); handles retries
- `embed/embed.go` — embeddings client (same LiteLLM proxy endpoint)

### kb (`internal/kb/`)

- `store.go` — `Store.IngestDocument()` (chunk → embed → pgvector upsert) and `Store.Search()` (cosine nearest-neighbor via `pgvector`)
- `querydistill/` — `Distiller.Distill()`: one LLM call to rewrite PR title+body into a focused retrieval query; avoids kb↔llm import cycle

### contextengine (`internal/contextengine/`)

- `compress.go` — `Compress(files, opts)`: filters by path globs, drops binaries, sorts small files first, allocates per-file token budget, truncates overflow; ported from Qodo PR-Agent

### analysis (`internal/analysis/`)

- `analysis.go` — `Linter` interface, `Registry`, `Finding`
- `sandbox/` — `Runner` interface; `DockerRunner` implementation
- `workspace/` — `Open()`: materializes PR head as temp directory from VCS archive
- `linters/eslint/`, `golangci/`, `ruff/`, `semgrep/`, `shellcheck/` — sandboxed linter wrappers

### config (`internal/config/`)

- `config.go` — `Repo` struct (full `.cadoo.yaml` schema); `LoadFile()` / `Parse()` / `Default()`; per-PR config loaded from head SHA, never from `main`

### settings (`internal/settings/`)

- `settings.go` — process-level env vars; `Settings.FromEnv()`, `HasGitHub()`, `HasGitLab()`

## Data Flow: Webhook to VCS Comment

```
VCS push/comment
     │
     ▼
POST /webhook/github (or /webhook/gitlab)
     │  signature verified
     ▼
githubWebhookHandler / gitlabWebhookHandler
     │  parse event → ToolJob
     ▼
enqueue(ctx, ToolJob)
     │
     ├─ DATABASE_URL set → riverq.Queue.EnqueueTool() → River table
     │                     cadoo-worker: riverq.toolWorker.Work() → Dispatcher.Run()
     │
     └─ no DATABASE_URL → jobs.memQueue.Enqueue() → goroutine → Dispatcher.Run()
                          (cadoo-webhook single-binary dev mode)
                          
Dispatcher.Run(ctx, ToolJob)
     │
     ├─ FetchPullRequest → PullRequest
     ├─ loadCfg → Repo (from PR head SHA .cadoo.yaml or defaults)
     ├─ ListChangedFiles → []FileChange
     ├─ contextengine.Compress → Compressed
     ├─ slop.Detect → Report
     ├─ runLinters (Docker sandbox) → []analysis.Finding
     ├─ tracker.FindLinked → []Issue
     ├─ Posted.ListPostedFindings → []PriorFinding
     ├─ Learnings.Active → []Rule
     ├─ KB.Search → []Hit
     ├─ tool.Run(Input) → *Result
     │
     └─ applyResult
          ├─ postSummary: Posted.PutSection → renderConsolidated
          │               → provider.UpdateSummaryComment (or PostSummaryComment)
          ├─ postInline: HasFinding (cross-dispatch dedup) + StructuralKey (intra-dispatch)
          │              → StampInline (hidden marker) → provider.PostInlineComments
          │              → RecordFinding in posted_findings
          │              → resolveStalePriors → provider.ResolveThread
          ├─ UpsertCheckRun (if ReportStatus)
          └─ applyPRBody (if EditPRBody set)
```

## Idempotency Mechanisms

Cadoo is fully idempotent across re-dispatches (e.g. every `synchronize` event and manual re-triggers). Three complementary layers:

### 1. Summary comment consolidation

`postSummary` in `reviewer.go` writes each tool's output as a named section into a single consolidated PR comment delimited by `<!-- cadoo:wrapper:begin -->` / `<!-- cadoo:wrapper:end -->` markers. The comment ID is persisted in `posted_summaries`. On re-dispatch, `provider.UpdateSummaryComment` edits the existing comment instead of creating a new one.

### 2. Inline comment fingerprinting

`Fingerprint(tool, comment)` is a SHA1 of all fields including body. Stored in `posted_findings`. On re-dispatch, `HasFinding` looks up the fingerprint; exact matches are skipped.

### 3. Near-duplicate detection

`StructuralKey(tool, c)` = `tool|file|lineStart-lineEnd|severity`. The `normalized_title` column (migration 0005) and Jaccard similarity (threshold 0.5) match rephrased versions of the same finding. Intra-dispatch dedup (same run) uses `seenKeys` map before any DB round-trip.

### 4. CI-mode stateless dedup (`PriorReviewReader`)

`cadoo ci` has no DB. If the VCS adapter implements `vcs.PriorReviewReader`, `ciCmd` calls `ListCadooArtifacts()` to read back Cadoo's prior comments (parsed via `ParseInlineMarker` for hidden markers and `SummaryWrapperBegin` for the overview). `findings.NewFromPrior()` seeds an in-memory `Store`, restoring dedup state across pushes with no persistent store.

### 5. Stale thread resolution

`resolveStalePriors` in `reviewer.go` walks prior findings for the current tool, checks if each one's structural key is absent from the current run's output, and calls `provider.ResolveThread()` to mark the discussion resolved.

## Multi-Tenancy Design

- Every database table in migration 0001 carries `org_id UUID` (FK to `orgs`).
- `orgs` has `plan TEXT` (free/pro/enterprise), `slug`, `name`.
- `installations` links `org_id` to VCS provider + external install ID + `auth_secret_ref`.
- `repos`, `pull_requests`, `pr_jobs`, `findings` chain to `org_id` via `repos.org_id`.
- Self-host is a degenerate single-org tenant: same schema, same code path.
- `posted_findings` / `posted_summaries` (migration 0003) are keyed by `(provider, repo_full_name, pr_number)` without an `org_id` FK to keep the dispatcher's hot path DB-free when `pool` is nil.

## CI-Mode Stateless Operation

`cadoo ci --pr <url>` (GitHub) / `cadoo ci --mr <url>` (GitLab):

1. Parses the PR/MR URL via `parseTargetURL()` to extract provider kind, base URL, project path, and number.
2. Reads `GITLAB_TOKEN` or `GITHUB_TOKEN` from env; builds the VCS adapter.
3. Reads `LLM_GATEWAY_URL` / `LLM_GATEWAY_API_KEY` from env.
4. Optionally loads `.cadoo.yaml` from the checked-out repo root (`--repo` flag or `CI_PROJECT_DIR` / `GITHUB_WORKSPACE`).
5. Constructs a stateless `Dispatcher` — no `KB`, no `Learnings`, no `Audit`, no DB pool.
6. If provider implements `PriorReviewReader`: calls `ListCadooArtifacts()` and seeds `d.Posted` via `findings.NewFromPrior()`.
7. Runs tools sequentially from `--tools` CSV (default: `describe,review,improve`).
8. Exits 0 if all succeed; exits 1 on first error (but continues remaining tools).

## Error Handling Strategy

- **Dispatch errors** (pre-tool): `failCheck()` posts a `cadoo/review` check-run with `failure` status when `ReportStatus` is true; returns error to queue.
- **Post errors** (post-tool): logged at ERROR level; pipeline continues. Partial output is still posted.
- **Linter errors**: logged at DEBUG; linter is skipped, not fatal.
- **KB/Learnings errors**: logged at DEBUG; nil result, pipeline continues.
- **Audit errors**: swallowed (append-only audit log is best-effort).

## Cross-Cutting Concerns

**Logging:** `log/slog` (structured JSON) throughout. No custom wrapper.

**Metrics:** Prometheus via `internal/metrics/`. `cadoo_dispatch_total`, `cadoo_dispatch_duration_seconds`, `cadoo_llm_call_total`, `cadoo_llm_tokens_total`. Served by `cadoo-api` but registered on the default registry by all binaries.

**Validation:** `.cadoo.yaml` validated via `config.Parse()` / YAML unmarshal with field-level struct tags. No JSON Schema.

**Authentication:** GitHub App installation (`bradleyfalzon/ghinstallation`); GitLab PAT/project token. `cadoo-api` uses OIDC (`coreos/go-oidc`) via `internal/auth/`.

## Architectural Constraints

- **No VCS imports outside `internal/vcs/`** — orchestrator and tools depend only on `vcs.Provider`; never import `internal/vcs/github` or `internal/vcs/gitlab` from tools or orchestrator.
- **No kb↔llm cycle** — `internal/kb` calls `internal/llm/embed` (embeddings); query distillation goes through `internal/kb/querydistill` which imports `internal/llm`. Direct KB-calls-LLM path is forbidden.
- **`.cadoo.yaml` from PR head only** — never from `main`. `loadCfg()` in `reviewer.go` always uses `pr.HeadSHA`.
- **`findings.Store` nil-safe** — all public methods handle `s == nil` as no-op so the dispatcher hot path needs no nil checks.
- **Thread safety** — `Dispatcher` is concurrent-safe (all mutable state lives in stores/pools). `runLinters` uses a mutex + WaitGroup for parallel linter execution.
- **River concurrency** — `MaxWorkers: 4` on the default queue (`internal/riverq/queue.go`).

## Anti-Patterns

### Bypassing `Posted` when posting comments

**What happens:** Calling `provider.PostInlineComments` or `provider.PostSummaryComment` directly without going through `postInline` / `postSummary`.
**Why it's wrong:** Bypasses fingerprint recording, dedup lookup, marker stamping, stale-thread resolution, and consolidated comment management. Every re-push creates duplicate comments.
**Do this instead:** Return `tools.Result` from the tool and let `applyResult` in `reviewer.go` call `postInline` / `postSummary`.

### Per-tool summary comments

**What happens:** Posting one raw comment per tool instead of contributing a section to the consolidated comment.
**Why it's wrong:** Floods the PR with one comment per tool per push. The consolidated wrapper (`<!-- cadoo:wrapper:begin -->`) keeps everything in one editable comment.
**Do this instead:** Return `Result.Summary` from the tool; `postSummary` handles section placement and edit-in-place via `findings.Store.PutSection`.
