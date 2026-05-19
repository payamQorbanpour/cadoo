# Codebase Structure
_Last updated: 2026-05-19_

## Directory Layout

```
cadoo/
├── cmd/                       # Five binary entry points
│   ├── cadoo-webhook/         # HTTP webhook server
│   ├── cadoo-worker/          # River queue consumer
│   ├── cadoo-cli/             # Local CLI + CI-mode
│   ├── cadoo-api/             # REST API + dashboard backend
│   └── cadoo-tunnel/          # Reverse-tunnel agent
├── internal/                  # All application packages (not importable externally)
│   ├── orchestrator/          # Central review pipeline (Dispatcher)
│   ├── tools/                 # Tool interface + 13 built-in subpackages
│   ├── vcs/                   # VCS Provider interface + adapters
│   ├── findings/              # Inline dedup + summary idempotency store
│   ├── llm/                   # LLM Provider interface + LiteLLM client + embeddings
│   ├── kb/                    # Knowledge base (pgvector semantic search)
│   ├── learnings/             # Per-repo accepted/rejected rules store
│   ├── contextengine/         # PR diff compression / token packing
│   ├── analysis/              # Static analysis: Linter interface + sandboxed wrappers
│   ├── config/                # .cadoo.yaml loader (per-repo config)
│   ├── settings/              # Process-level env config
│   ├── db/                    # Postgres connection pool wrapper
│   ├── riverq/                # River (Postgres-backed) job queue wrapper
│   ├── jobs/                  # In-memory job queue (dev mode)
│   ├── issuetrackers/         # Issue tracker interface + Jira + Linear adapters
│   ├── notifiers/             # Result notifier interface + Slack adapter
│   ├── audit/                 # Append-only audit log
│   ├── metrics/               # Prometheus instruments
│   ├── slop/                  # Pre-LLM PR quality classification
│   ├── reports/               # Scheduled digest reports
│   ├── agent/                 # LLM agent loop abstraction
│   ├── mcp/                   # MCP (Model Context Protocol) server
│   ├── billing/               # Billing stubs (empty at Phase 0)
│   ├── auth/                  # OIDC auth + middleware for cadoo-api
│   ├── httpx/                 # HTTP server helper (ListenAndServe with graceful shutdown)
│   └── version/               # Build version string
├── db/
│   └── migrations/            # Goose SQL migrations (forward-only by convention)
├── deploy/
│   ├── docker/                # docker-compose.yml + litellm-config.yaml
│   ├── github/                # GitHub Actions workflow (cadoo-review.yml)
│   ├── gitlab/                # GitLab CI template (.gitlab-ci.cadoo.yml)
│   ├── helm/cadoo/            # Helm chart with PodDisruptionBudget, NetworkPolicy, ServiceMonitor
│   └── terraform/             # Infrastructure as code
├── docs/                      # User-facing docs + assets
├── ide/vscode/                # VS Code extension source
├── .planning/                 # GSD planning docs (not committed to main artifact)
├── go.mod                     # Go 1.26, module github.com/payamqorbanpour/cadoo
├── go.sum
├── Makefile                   # dev targets: test, lint, build, migrate, sqlc, ci
├── sqlc.yaml                  # sqlc codegen config (output: internal/db/sqlc_gen)
├── .golangci.yml              # golangci-lint v2 config
├── .goreleaser.yaml           # GoReleaser multi-binary publish config
└── .cadoo.yaml.example        # Fully-annotated .cadoo.yaml reference
```

## Key File Locations

### Entry Points

| Binary | File | Key function |
|--------|------|-------------|
| `cadoo-webhook` | `cmd/cadoo-webhook/main.go` | `main()`, `buildDispatcher()`, `githubWebhookHandler()`, `gitlabWebhookHandler()` |
| `cadoo-worker` | `cmd/cadoo-worker/main.go` | `main()`, `runRiver()`, `buildDispatcher()` |
| `cadoo-cli` | `cmd/cadoo-cli/main.go` + `ci.go` | `ciCmd()`, `parseTargetURL()`, `buildProvider()`, `priorStore()` |
| `cadoo-api` | `cmd/cadoo-api/main.go` | HTTP router, OIDC middleware, metrics endpoint |
| `cadoo-tunnel` | `cmd/cadoo-tunnel/main.go` | Tunnel agent |

### Orchestrator (center of gravity)

| File | Purpose |
|------|---------|
| `internal/orchestrator/reviewer.go` | `Dispatcher` struct, `Run()`, `applyResult()`, `postSummary()`, `postInline()`, `resolveStalePriors()`, `loadCfg()`, `runLinters()` |
| `internal/orchestrator/consolidate.go` | `renderConsolidated()`, `spliceCadooBody()`, marker constants |
| `internal/orchestrator/registry.go` | `DefaultRegistry()` wiring 13 tools |
| `internal/orchestrator/lintreg.go` | `DefaultLintRegistry()` wiring 5 sandboxed linters |

### Interfaces

| Interface | Defined in | Implementations |
|-----------|-----------|-----------------|
| `vcs.Provider` | `internal/vcs/vcs.go:103` | `internal/vcs/github.Adapter`, `internal/vcs/gitlab.Adapter` |
| `vcs.PriorReviewReader` | `internal/vcs/vcs.go:133` | implemented on both VCS adapters |
| `tools.Tool` | `internal/tools/tools.go:104` | 13 subpackages under `internal/tools/` |
| `llm.Provider` | `internal/llm/provider.go:71` | `internal/llm/litellm.Client` |
| `analysis.Linter` | `internal/analysis/analysis.go:41` | `golangci.Linter`, `ruff.Linter`, `eslint.Linter`, `semgrep.Linter`, `shellcheck.Linter` |
| `analysis/sandbox.Runner` | `internal/analysis/sandbox/` | `sandbox.DockerRunner` |
| `jobs.Handler` | `internal/jobs/jobs.go:17` | `orchestrator.Dispatcher` (via `Handle()`), `jobs.HandlerFunc` |
| `jobs.Queue` | `internal/jobs/jobs.go:33` | `jobs.memQueue` |
| `issuetrackers.Tracker` | `internal/issuetrackers/issues.go` | `jira.Client`, `linear.Client` |
| `orchestrator.ResultNotifier` | `internal/orchestrator/reviewer.go:39` | `notifiers/slack.Notifier` |
| `orchestrator.FileFetcher` | `internal/orchestrator/reviewer.go:61` | both VCS adapters |

### Configuration

| File | What it configures |
|------|-------------------|
| `internal/settings/settings.go` | Process env vars (HTTP_ADDR, DATABASE_URL, LLM_GATEWAY_URL, GitHub/GitLab creds, etc.) |
| `internal/config/config.go` | Per-repo `.cadoo.yaml` schema (`Repo` struct), `Default()`, `LoadFile()`, `Parse()` |
| `.cadoo.yaml.example` | Fully-annotated reference for all supported config keys |
| `sqlc.yaml` | sqlc codegen: queries from `db/queries/`, output to `internal/db/sqlc_gen/` |
| `.golangci.yml` | golangci-lint v2; `package_comments` disabled, `exported` on, `unused-parameter` disabled |
| `.goreleaser.yaml` | Builds all five `cmd/*` binaries |

### Database Layer

| File / Directory | Purpose |
|-----------------|---------|
| `db/migrations/0001_init.sql` | Core tables: `orgs`, `users`, `org_members`, `installations`, `repos`, `pull_requests`, `pr_jobs`, `findings` (old schema), `llm_calls`, `audit_events` |
| `db/migrations/0002_kb_learnings.sql` | `kb_documents`, `kb_chunks` (pgvector 1536-dim), `learnings` |
| `db/migrations/0003_posted_state.sql` | `posted_findings`, `posted_summaries` — idempotency tables keyed by `(provider, repo_full_name, pr_number)` without org_id FK |
| `db/migrations/0004_summary_sections.sql` | Sections table for multi-tool consolidated comment |
| `db/migrations/0005_finding_dedup.sql` | Adds `structural_key` and `normalized_title` to `posted_findings`; index for near-dup lookup |
| `internal/db/db.go` | `db.Open()`: pgxpool with pgvector codec registration, max 20 conns |
| `internal/db/sqlc_gen/` | sqlc-generated query code (regenerated via `make sqlc`) — not yet populated at Phase 0 |

## Package Dependency Graph

Key import relationships (non-exhaustive, showing architectural layers):

```
cmd/* (main packages)
  └─ internal/settings          (env config)
  └─ internal/orchestrator      (Dispatcher)
  └─ internal/vcs/github        (VCS adapter)
  └─ internal/vcs/gitlab        (VCS adapter)
  └─ internal/llm/litellm       (LLM client)
  └─ internal/riverq            (Postgres queue)
  └─ internal/jobs              (in-memory queue)
  └─ internal/findings          (dedup store)
  └─ internal/kb                (knowledge base)
  └─ internal/learnings         (rules store)
  └─ internal/audit             (audit log)

internal/orchestrator
  └─ internal/vcs               (interface only — never vcs/github or vcs/gitlab)
  └─ internal/tools             (interface + Registry)
  └─ internal/findings
  └─ internal/contextengine
  └─ internal/analysis
  └─ internal/kb
  └─ internal/kb/querydistill
  └─ internal/learnings
  └─ internal/llm               (interface only)
  └─ internal/issuetrackers
  └─ internal/slop
  └─ internal/metrics
  └─ internal/audit
  └─ internal/config

internal/tools/*
  └─ internal/vcs               (types: PullRequest, FileChange, InlineComment)
  └─ internal/llm               (interface)
  └─ internal/config
  └─ internal/analysis
  └─ internal/kb
  └─ internal/learnings
  └─ internal/slop

internal/kb
  └─ internal/llm/embed         (embeddings — NOT internal/llm full package)

internal/kb/querydistill
  └─ internal/llm               (chat — avoids kb↔llm cycle)
  └─ internal/kb is NOT imported here

internal/vcs/github
  └─ internal/vcs               (types only)
internal/vcs/gitlab
  └─ internal/vcs               (types only)

internal/riverq
  └─ internal/orchestrator      (ToolJob, Dispatcher)
  └─ internal/vcs               (Kind)

internal/findings
  └─ internal/vcs               (InlineComment, Severity, PriorReview)
```

## Naming Conventions

**Files:**
- Go source: `lowercase_snake.go`
- Tests: `*_test.go` co-located with source
- SQL migrations: `NNNN_descriptive_name.sql` (goose-numbered, forward-only)

**Packages:**
- Match directory name; single-word where possible (`orchestrator`, `findings`, `learnings`)
- VCS adapters: `github`, `gitlab` aliased in `cmd/*` as `cadoogh`, `cadoogl` to avoid collision with `google/go-github` alias `gogithub`

**Types:**
- Interfaces: noun or noun phrase (`Provider`, `Linter`, `Tool`, `Runner`)
- Adapters implementing an interface: `Adapter` (both VCS adapters)
- Queue args: `ToolArgs` (River payload), `ToolJob` (internal orchestrator payload)

**Constants:**
- `Kind` values: `KindGitHub`, `KindGitHubEnterprise`, `KindGitLab`
- Severity: `SeverityBlock`, `SeverityWarn`, `SeverityNit`
- Marker strings: unexported in `orchestrator/consolidate.go`, `vcs/marker.go` exports `SummaryWrapperBegin`

## Where to Add New Code

### New review tool

1. Create `internal/tools/<toolname>/` package with a struct satisfying `tools.Tool`
2. Implement `Name() string` returning a stable snake_case identifier
3. Implement `Run(ctx context.Context, in tools.Input) (*tools.Result, error)`
4. Register in `internal/orchestrator/registry.go` — add `r.Register(<toolname>.Tool{})` to `DefaultRegistry()`
5. Add display label to `sectionTitle` map and icon to `sectionEmoji` map in `internal/orchestrator/consolidate.go`
6. Tests: `internal/tools/<toolname>/<toolname>_test.go`

### New VCS adapter

1. Create `internal/vcs/<provider>/` package
2. Implement all 8 methods of `vcs.Provider` interface (defined in `internal/vcs/vcs.go:103`)
3. Optionally implement `vcs.PriorReviewReader` for CI-mode stateless dedup
4. Optionally implement `orchestrator.FileFetcher` for `.cadoo.yaml` loading from PR head
5. Add `Kind` constant to `internal/vcs/vcs.go`
6. Wire in `cmd/cadoo-webhook/main.go` and `cmd/cadoo-worker/main.go` `buildDispatcher()` functions
7. Wire in `cmd/cadoo-cli/ci.go` `buildProvider()` function

### New linter

1. Create `internal/analysis/linters/<lintername>/` implementing `analysis.Linter`
2. Register in `internal/orchestrator/lintreg.go` `DefaultLintRegistry()`

### New issue tracker

1. Create `internal/issuetrackers/<tracker>/` implementing `issuetrackers.Tracker`
2. Wire in `cmd/cadoo-webhook/main.go` and `cmd/cadoo-worker/main.go` `buildTrackers()`

### New database migration

1. Add `db/migrations/NNNN_description.sql` with `-- +goose Up` and `-- +goose Down` sections
2. Run `make migrate` to apply; CI runs `up → down → up` round-trip — every migration must be reversible
3. If adding new queries, add to `db/queries/` and run `make sqlc` to regenerate `internal/db/sqlc_gen/`

### New settings / env var

1. Add field to `internal/settings/settings.go:Settings`
2. Read in `FromEnv()` with `envOr()` or `os.Getenv()`
3. Document in `.cadoo.yaml.example` if per-repo (config), or in `CLAUDE.md` if process-level

## Special Directories

**`.claude/worktrees/`:**
- Purpose: Git worktrees for parallel feature development
- Generated: No (manually created)
- Committed: No (in `.gitignore`)

**`internal/db/sqlc_gen/`:**
- Purpose: sqlc-generated Go query code
- Generated: Yes (`make sqlc`)
- Committed: Yes (so builds work without sqlc installed)

**`bin/`:**
- Purpose: Output directory for `make build` compiled binaries
- Generated: Yes
- Committed: No

**`.planning/`:**
- Purpose: GSD planning documents (phases, codebase maps)
- Generated: By GSD tooling
- Committed: Yes (tracks planning state in source control)

**`deploy/helm/cadoo/`:**
- Purpose: Production Helm chart; includes optional `PodDisruptionBudget`, `NetworkPolicy` (deny-all + allow-intra + allow-egress), `ServiceMonitor`; all gated on `hardening.*` values
- Committed: Yes
