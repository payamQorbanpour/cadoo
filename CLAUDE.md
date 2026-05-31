# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common commands

```sh
make tools-install   # install sqlc, goose, golangci-lint
make dev-up          # boot postgres+pgvector + litellm + services via deploy/docker/docker-compose.yml
make migrate         # apply db/migrations against $DATABASE_URL (goose)
make migrate-down    # roll back one migration
make sqlc            # regenerate internal/db/sqlc_gen from db/queries
make test            # go test -race -count=1 ./...
make lint            # golangci-lint (v2 config in .golangci.yml)
make vet             # go vet ./...
make build           # build all five cmd/* binaries to ./bin
make ci              # what CI runs: vet + test + build
```

Run a single test: `go test -race -run TestRenderConsolidatedOrdersReviewFirst ./internal/orchestrator/...`. The lint config is golangci-lint **v2** — `make lint` installs `@latest` if missing, but CI pins `v2.12.2`; bump locally if the schemas diverge.

Migrations are forward-only by convention (`.cadoo.yaml.example` documents this). New SQL goes in `db/migrations/` (goose, numbered `NNNN_name.sql`); query files go in `db/queries/` and are codegen'd to `internal/db/sqlc_gen` via `make sqlc`. CI's `migrations` job runs `up → down → up` against pgvector/pg16, so every migration must round-trip cleanly.

Go toolchain: **1.26** (see `go.mod` and CI). `goimports` is configured with `local-prefixes: github.com/payamqorbanpour/cadoo`, so the third import group must be cadoo-internal.

## Architecture

Cadoo is an AI code reviewer that posts inline review comments and check-runs to GitHub/GHES/GitLab. Five binaries live under `cmd/`:

- `cadoo-webhook` — receives VCS webhooks, verifies signatures, enqueues `orchestrator.ToolJob`. With `DATABASE_URL` set it enqueues to River (Postgres) for `cadoo-worker` to consume; without it, it uses an in-process memory queue consumed in a sibling goroutine (single-binary dev mode).
- `cadoo-worker` — River consumer running review pipelines.
- `cadoo-api` — REST API + dashboard backend.
- `cadoo-cli` — local pre-commit review and the **CI-mode** entry point (`cadoo ci --pr <url>` / `--mr <url>`). CI-mode is stateless — no DB, no KB/learnings — but is idempotent across pushes: it reconstructs dedup state by reading its own prior comments back from the PR/MR (hidden `<!-- cadoo:fp … -->` marker + `vcs.PriorReviewReader`), so the overview is edited in place, duplicate inline findings are suppressed, and fixed threads are resolved.
- `cadoo-tunnel` — reverse-tunnel agent so SaaS can reach private GHES/GitLab without inbound firewall rules.

### The review pipeline

The center of gravity is `internal/orchestrator`. `Dispatcher.Run(ctx, ToolJob)` (in `reviewer.go`) is the single entry point both the webhook and worker drive:

1. Look up the right `vcs.Provider` from `VCSPool` and the right `tools.Tool` from `Registry`.
2. Fetch the PR via the provider.
3. Load `.cadoo.yaml` from the PR head (`FileFetcher` capability on the adapter) — never from `main`. Per-PR config wins.
4. Build a `contextengine.Compressed` packed-context view of the diff (PR-compression algorithm, ported from Qodo PR-Agent).
5. Optionally run sandboxed static analysis (`internal/analysis` + `analysis/sandbox`) and feed findings into the tool input.
6. Optionally query KB (pgvector, `internal/kb`) and learnings (`internal/learnings`) — both nil-tolerant.
7. Run the tool, then post its `tools.Result` through the provider: summary comment edited in place, inline comments deduped by fingerprint, optional `cadoo/check/<rule>` check-runs.

Everything is **idempotent across resyncs**. `internal/findings.Store` (table `posted_findings` / `posted_summaries`, migration 0003) fingerprints inline comments and tracks the summary comment ID per `(provider, repo, pr)`. When `Dispatcher.Posted` is non-nil, `postInline` skips already-posted fingerprints and `postSummary` edits the existing comment instead of creating a new one. Migration 0005 added the cross-PR finding-dedup table. Keep this in mind when adding new tools that post comments — never bypass `Posted`.

### Tools

Each review command lives in its own subpackage under `internal/tools/` and satisfies the `tools.Tool` interface declared in `internal/tools/tools.go`. The shared `tools.Input` already carries everything a tool typically needs (PR, files, packed context, repo config, LLM provider, prior findings, KB hits, learnings, issue tracker resolutions, slop report). `tools.Result` lets the tool emit a summary section, inline comments, and one or more check-runs. `orchestrator/registry.go` wires the 13 built-ins into a `DefaultRegistry`.

Wrapper markers (HTML comments like `<!-- cadoo:pr-body:begin -->`, `<!-- cadoo:wrapper:begin -->`) are how the consolidated comment and PR-body section stay machine-greppable across runs. `internal/orchestrator/consolidate.go` is the source of truth — don't reinvent the wrapper format elsewhere.

### VCS adapters

`internal/vcs/vcs.go` defines the provider-agnostic interface. Implementations live in `internal/vcs/github` (powers both GitHub.com and GHES; built on `bradleyfalzon/ghinstallation` + `google/go-github/v66`) and `internal/vcs/gitlab` (built on `xanzy/go-gitlab` v0.115; inline notes go through the discussions API with `base_sha`/`start_sha`/`head_sha` positions). The orchestrator and tools depend **only** on the `vcs.Provider` interface — never import a provider package from outside `internal/vcs/`.

### LLM

`internal/llm` is the gateway. The Go code talks to a LiteLLM proxy (sidecar container, deployed via `deploy/docker/litellm-config.yaml`) over LiteLLM's OpenAI-compatible HTTP API. Multi-provider routing (OpenAI / Anthropic / Bedrock / Azure / Ollama) is LiteLLM's responsibility — don't add provider-specific code paths in Go. `internal/llm/embed` is for embeddings (KB), `internal/llm/litellm` is the chat client.

### Multi-tenancy

Cadoo is multi-tenant from the schema up — every table carries `org_id`. Self-host and SaaS use the same code path. Don't add single-tenant shortcuts even in self-host docs.

## Repo-specific gotchas

- `package_comments` is **disabled** in revive (`.golangci.yml`), but `exported` is on — every exported symbol still needs a docstring.
- `unused-parameter` revive rule is **disabled**, so unused function parameters won't fail lint (intentional for interface implementations).
- `internal/kb` and `internal/llm` would form a cycle if KB called LLM directly for query distillation — that's why `internal/kb/querydistill` exists as a separate package. Don't re-introduce the cycle.
- `.cadoo.yaml` is loaded from the **PR head SHA**, not from `main`. Tests that touch config loading should respect this.
- `cadoo/review` is the check-run name added to branch protection. `internal/tools` may emit additional per-rule check-runs as `cadoo/check/<rule>` via `Result.CheckRuns`.
- The Helm chart (`deploy/helm/cadoo`) ships optional `PodDisruptionBudget`, `NetworkPolicy` (deny-all + allow-intra + allow-egress), and a Prometheus `ServiceMonitor`, all gated behind `hardening.*` values. A no-egress mode is supported for air-gapped customers.

## Reference docs

- Sample output and feature matrix: `README.md`.
- Pre-merge gate setup: `docs/PRE_MERGE_GATES.md`.
- Cutting a release (tag + push → GoReleaser + image publish): `docs/RELEASING.md`.
- Example per-repo config (every supported key): `.cadoo.yaml.example`.
- CI-mode runners: `deploy/github/cadoo-review.yml` and `deploy/gitlab/.gitlab-ci.cadoo.yml`.
