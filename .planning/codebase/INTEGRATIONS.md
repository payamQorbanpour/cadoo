# External Integrations
_Last updated: 2026-05-19_

## VCS Providers

### GitHub (GitHub.com and GitHub Enterprise Server)

- **Package:** `github.com/google/go-github/v66` v66.0.0 (REST API client)
- **Auth package:** `github.com/bradleyfalzon/ghinstallation/v2` v2.18.0 (GitHub App JWT + installation access-token refresh)
- **Adapter:** `internal/vcs/github/github.go` — implements `vcs.Provider`
- **Webhook parsing:** `internal/vcs/github/webhook.go` — `VerifySignature` (HMAC-SHA256 over `X-Hub-Signature-256`) + `ParseEvent` (delegates to `go-github`'s `ParseWebHook`)
- **GraphQL seam:** `internal/vcs/github/graphql.go` — resolves thread IDs not available from the REST review-create response
- **Auth modes:**
  - GitHub App (webhook/worker): `AppID` + `InstallationID` + `PrivateKeyPEM` via `ghinstallation`
  - Bearer token (CI-mode/Actions): `GITHUB_TOKEN` or PAT passed as `Token` field in `Config`
- **GHES support:** `Config.BaseURL` (e.g. `https://ghe.example.com/api/v3`) and optional `Config.UploadURL`
- **Env vars required:** `GITHUB_APP_ID`, `GITHUB_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY` (App mode) **or** `GITHUB_TOKEN` (CI-mode). `GITHUB_WEBHOOK_SECRET` for signature verification. `GITHUB_BASE_URL` for GHES.
- **Provider kind constants:** `vcs.KindGitHub`, `vcs.KindGitHubEnterprise`

### GitLab (GitLab.com and self-managed)

- **Package:** `github.com/xanzy/go-gitlab` v0.115.0 (wraps `gitlab.com/gitlab-org/api/client-go` v1.46.0)
- **Adapter:** `internal/vcs/gitlab/gitlab.go` — implements `vcs.Provider`
- **Webhook parsing:** `internal/vcs/gitlab/webhook.go` — verifies `X-Gitlab-Token` header
- **Auth mode:** Personal or project access token (`Config.Token`)
- **Inline comments:** posted through the Discussions API with `base_sha`/`start_sha`/`head_sha` positions
- **Env vars required:** `GITLAB_TOKEN`, `GITLAB_WEBHOOK_SECRET`. `GITLAB_BASE_URL` for self-managed.
- **Provider kind constant:** `vcs.KindGitLab`

### Provider Interface

All adapters implement `vcs.Provider` (`internal/vcs/vcs.go`). Stateless CI-mode adapters also implement `vcs.PriorReviewReader` (`ListCadooArtifacts`) so the CLI can reconstruct dedup state without a database by reading its own prior comments back from the PR/MR.

---

## LLM Gateway

### LiteLLM Proxy

- **Image:** `ghcr.io/berriai/litellm:main-stable` (port 4000)
- **Protocol:** OpenAI-compatible HTTP (`/v1/chat/completions`, `/v1/embeddings`)
- **Go client (chat):** `internal/llm/litellm/client.go` — `litellm.Client` implements `llm.Provider`
- **Go client (embeddings):** `internal/llm/embed/embed.go` — `embed.Client` implements `embed.Embedder`
- **Retry layer:** `internal/llm/retry.go` — wraps `llm.Provider` with configurable exponential back-off
- **Config files:**
  - Dev: `deploy/docker/litellm-config.yaml`
  - Helm: `deploy/helm/cadoo/templates/litellm-config.yaml`
- **Env vars required:** `LLM_GATEWAY_URL` (e.g. `http://litellm:4000`), `LLM_GATEWAY_API_KEY` (optional for dev, required for CI-mode users). Provider API keys (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY`) are consumed by LiteLLM, not by the Go service directly.
- **Default model:** `claude-sonnet-4-6` (configurable via `CADOO_DEFAULT_MODEL` / Helm `llm.defaultModel`)
- **Design constraint:** No provider-specific code paths in Go — all multi-provider routing is delegated to LiteLLM.

### Embedding Model

- **Default model:** `text-embedding-3-small` (1536-dim output)
- **Used by:** `internal/kb` (semantic KB search) and `internal/learnings` (rule deduplication)
- **Endpoint:** same LiteLLM proxy at `/v1/embeddings`

---

## Data Storage

### PostgreSQL 16 + pgvector

- **Client:** `github.com/jackc/pgx/v5` v5.9.2 (pgxpool)
- **Vector extension:** `github.com/pgvector/pgvector-go` v0.3.0 — 1536-dim embeddings in `kb_chunks.embedding`
- **Extensions:** `pgcrypto` (UUID generation), `vector` (pgvector)
- **Connection env var:** `DATABASE_URL` (standard libpq DSN)
- **Generated query code:** `internal/db/sqlc_gen/` (package `sqlcgen`), regenerated via `make sqlc`
- **Key tables:** `orgs`, `users`, `org_members`, `installations`, `repos`, `pull_requests`, `pr_jobs`, `findings`, `llm_calls`, `audit_events`, `posted_findings`, `posted_summaries`, `kb_chunks`, `learnings`, `cross_pr_findings`
- **Migration tool:** `goose` (SQL files `db/migrations/0001_init.sql` → `0005_finding_dedup.sql`); forward-only by convention, CI validates up→down→up round-trip.

### In-process Memory Queue (dev mode)

- **Package:** `internal/jobs`
- **Activation:** used when `DATABASE_URL` is not set; both webhook and dispatcher goroutine run in the same process.

---

## Job Queue

### River (Postgres-backed)

- **Package:** `github.com/riverqueue/river` v0.36.0 + `riverdriver/riverpgxv5` v0.36.0
- **Integration point:** `internal/riverq/queue.go` — `ToolArgs` job type, `toolWorker` processes it via `orchestrator.Dispatcher`
- **Activation:** only when `DATABASE_URL` is set; `cadoo-webhook` enqueues, `cadoo-worker` consumes.

---

## Authentication

### OIDC (API layer)

- **Package:** `github.com/coreos/go-oidc/v3` v3.18.0
- **Implementation:** `internal/auth/auth.go` + `internal/auth/middleware.go`
- **Flow:** bearer token in `Authorization` header → OIDC `IDTokenVerifier` → `auth.Claims` (subject, email, org, roles)
- **RBAC tiers:** `owner`, `admin`, `member`, `viewer` (tier-ordered; `owner` implies all lower roles)
- **IdP:** any OIDC-fronted IdP (Dex recommended for SAML SSO delegation)
- **Env vars required:** `OIDC_ISSUER`, `OIDC_CLIENT_ID`

### GitHub App (webhook auth)

- See [VCS Providers → GitHub](#github-githubcom-and-github-enterprise-server) above.

---

## Webhook Processing

### GitHub Webhooks

- **Receiver:** `cmd/cadoo-webhook/main.go` → `internal/vcs/github/webhook.go`
- **Signature:** HMAC-SHA256, `X-Hub-Signature-256: sha256=<hex>`, secret from `GITHUB_WEBHOOK_SECRET`
- **Events handled:** `pull_request` (opened, synchronize, reopened, labeled)
- **Enqueuing:** verified payload → `orchestrator.ToolJob` → River or in-memory queue

### GitLab Webhooks

- **Receiver:** same `cadoo-webhook` binary, different route
- **Signature:** `X-Gitlab-Token` header, secret from `GITLAB_WEBHOOK_SECRET`
- **Events handled:** Merge Request Hook (open, update)
- **Adapter:** `internal/vcs/gitlab/webhook.go`

---

## Issue Trackers

### Jira

- **Package:** standard `net/http` (no external SDK; raw REST calls)
- **Adapter:** `internal/issuetrackers/jira/jira.go` — implements `issuetrackers.Tracker`
- **Auth:** HTTP Basic (`JIRA_EMAIL` + `JIRA_TOKEN`)
- **Env vars required:** `JIRA_BASE_URL`, `JIRA_EMAIL`, `JIRA_TOKEN`

### Linear

- **Package:** standard `net/http` (GraphQL over HTTP)
- **Adapter:** `internal/issuetrackers/linear/linear.go` — implements `issuetrackers.Tracker`
- **Auth:** `Authorization: <LINEAR_API_KEY>` header
- **Env vars required:** `LINEAR_API_KEY`

### Key Extraction

`internal/issuetrackers/issues.go` — `ExtractKeys` regex (`\b([A-Z][A-Z0-9]+-\d+)\b`) matches Jira/Linear-style keys from PR title + body.

---

## Notifications

### Slack

- **Package:** standard `net/http` (Incoming Webhook, no Slack SDK)
- **Adapter:** `internal/notifiers/slack/slack.go`
- **Auth:** incoming webhook URL (secret URL, no token header)
- **Env var required:** `SLACK_WEBHOOK_URL`
- **Triggered:** after each review pipeline completes (Phase 6 feature)

---

## Static Analysis (Sandboxed Linters)

Linters run in isolated Docker containers via `internal/analysis/sandbox/sandbox.go` (`DockerRunner` shells out to `docker` CLI).

| Linter | Language | Adapter |
|--------|----------|---------|
| golangci-lint | Go | `internal/analysis/linters/golangci/golangci.go` |
| ruff | Python | `internal/analysis/linters/ruff/ruff.go` |
| eslint | JavaScript/TypeScript | `internal/analysis/linters/eslint/eslint.go` |
| semgrep | Multi-language | `internal/analysis/linters/semgrep/semgrep.go` |
| shellcheck | Shell | `internal/analysis/linters/shellcheck/shellcheck.go` |

All linters are bundled in the polyglot sandbox image `deploy/docker/Dockerfile.sandbox-polyglot` (`cadoo/sandbox-polyglot:latest`, base `alpine:3.19`). Network is disabled inside the container (`--network=none`).

---

## Observability

### Prometheus

- **Package:** `github.com/prometheus/client_golang` v1.23.2
- **Metrics defined:** `internal/metrics/metrics.go` — `cadoo_dispatch_total`, `cadoo_dispatch_duration_seconds`, `cadoo_llm_call_total`, `cadoo_llm_tokens_total`
- **Scrape endpoint:** `/metrics` served by `cadoo-api`
- **Kubernetes integration:** optional Prometheus `ServiceMonitor` in `deploy/helm/cadoo/templates/servicemonitor.yaml` (gated by `hardening.serviceMonitor.enabled`)

---

## CI/CD

### GitHub Actions (project's own CI)

- **Workflows:** `.github/workflows/ci.yml` (vet + build + test + lint + migrations), `.github/workflows/docker-publish.yml` (publish container images), `.github/workflows/release.yml` (GoReleaser)
- **Go version:** 1.26 (via `actions/setup-go@v5`)
- **Linter action:** `golangci/golangci-lint-action@v9` pinned to `v2.12.2`

### Cadoo CI-mode (for end-user repos)

- **GitHub Actions runner template:** `deploy/github/cadoo-review.yml` — runs `ghcr.io/payamqorbanpour/cadoo-cli:latest` container
- **GitLab CI template:** `deploy/gitlab/.gitlab-ci.cadoo.yml`
- **Trigger:** `pull_request` events (opened, synchronize, reopened, ready_for_review)

---

## Container Registries

- **Application images:** `ghcr.io/payamqorbanpour/cadoo` (tag per binary via `--build-arg CMD`)
- **CLI image:** `ghcr.io/payamqorbanpour/cadoo-cli:latest`
- **LiteLLM proxy:** `ghcr.io/berriai/litellm:main-stable`
- **Postgres+pgvector:** `pgvector/pgvector:pg16`

---

## Environment Variable Reference

| Variable | Service | Purpose |
|----------|---------|---------|
| `DATABASE_URL` | webhook, worker, api | pgx DSN; absence enables in-process dev mode |
| `LLM_GATEWAY_URL` | worker, api, cli | LiteLLM (or compatible) base URL, e.g. `http://litellm:4000` |
| `LLM_GATEWAY_API_KEY` | worker, api, cli | API key for LLM gateway |
| `CADOO_DEFAULT_MODEL` | worker, api, cli | Default model name (e.g. `claude-sonnet-4-6`) |
| `GITHUB_APP_ID` | webhook | GitHub App ID |
| `GITHUB_INSTALLATION_ID` | webhook | GitHub App installation ID |
| `GITHUB_APP_PRIVATE_KEY` | webhook | PEM-encoded GitHub App private key |
| `GITHUB_WEBHOOK_SECRET` | webhook | HMAC secret for `X-Hub-Signature-256` |
| `GITHUB_TOKEN` | cli (CI-mode) | Actions-injected bearer token or PAT |
| `GITHUB_BASE_URL` | webhook, cli | GHES API base URL (omit for github.com) |
| `GITLAB_TOKEN` | webhook | GitLab personal/project access token |
| `GITLAB_WEBHOOK_SECRET` | webhook | `X-Gitlab-Token` secret |
| `GITLAB_BASE_URL` | webhook | Self-managed GitLab base URL |
| `OIDC_ISSUER` | api | OIDC provider issuer URL |
| `OIDC_CLIENT_ID` | api | OIDC client ID |
| `JIRA_BASE_URL` | worker | Jira instance base URL |
| `JIRA_EMAIL` | worker | Jira basic-auth email |
| `JIRA_TOKEN` | worker | Jira API token |
| `LINEAR_API_KEY` | worker | Linear API key |
| `SLACK_WEBHOOK_URL` | worker | Slack incoming webhook URL |
| `ANTHROPIC_API_KEY` | litellm sidecar | Anthropic provider key |
| `OPENAI_API_KEY` | litellm sidecar | OpenAI provider key |
| `GEMINI_API_KEY` | litellm sidecar | Gemini provider key |
| `HTTP_ADDR` | api, webhook | Bind address (e.g. `:8080`) |

Secrets in Kubernetes are stored in a `Secret` named `cadoo-secrets` (key `secretsRef.name` in Helm values).
