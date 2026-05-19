# Technology Stack
_Last updated: 2026-05-19_

## Language

**Primary:** Go 1.26 — all application code under `cmd/` and `internal/`
**No secondary languages** in the main module (linter sandboxes use Node.js, Python, and shell, but those run in disposable containers).

## Runtime

**Environment:** Go 1.26 (pinned in `go.mod`, `Dockerfile ARG GO_VERSION=1.26`, and CI `actions/setup-go@v5 go-version: "1.26"`)
**CGO:** disabled (`CGO_ENABLED=0`) for all production builds — produces fully static binaries

## Package Manager

- **Go modules** (`go.mod` / `go.sum`); lockfile: `go.sum` present and committed

## Binaries (cmd/)

Five binaries are built from `cmd/`:

| Binary | Entry point | Purpose |
|--------|-------------|---------|
| `cadoo-api` | `cmd/cadoo-api/main.go` | REST API + dashboard backend |
| `cadoo-webhook` | `cmd/cadoo-webhook/main.go` | VCS webhook receiver / job enqueuer |
| `cadoo-worker` | `cmd/cadoo-worker/main.go` | River job consumer, runs review pipelines |
| `cadoo-cli` | `cmd/cadoo-cli/main.go` | Local pre-commit and stateless CI-mode reviews |
| `cadoo-tunnel` | `cmd/cadoo-tunnel/main.go` | Reverse-tunnel agent for private GHES/GitLab |

## Frameworks & Libraries

### HTTP

- **`github.com/go-chi/chi/v5` v5.2.5** — HTTP router used by `cadoo-api` and `cadoo-webhook`

### Database

- **`github.com/jackc/pgx/v5` v5.9.2** — PostgreSQL driver (pgx v5 DSN pool)
- **`github.com/pgvector/pgvector-go` v0.3.0** — pgvector extension binding for KB semantic search
- **`github.com/riverqueue/river` v0.36.0** — Postgres-backed reliable job queue
- **`github.com/riverqueue/river/riverdriver/riverpgxv5` v0.36.0** — pgx v5 driver adapter for River
- **`github.com/jackc/puddle/v2` v2.2.2** — connection pooling (indirect, used by pgx)

### VCS Integration

- **`github.com/google/go-github/v66` v66.0.0** — GitHub REST API client (primary)
- **`github.com/bradleyfalzon/ghinstallation/v2` v2.18.0** — GitHub App installation auth (JWT + access-token refresh)
- **`github.com/xanzy/go-gitlab` v0.115.0** — GitLab API client
- **`gitlab.com/gitlab-org/api/client-go` v1.46.0** — GitLab API types (indirect via go-gitlab)

### Authentication

- **`github.com/coreos/go-oidc/v3` v3.18.0** — OIDC token verification for `cadoo-api` RBAC
- **`golang.org/x/oauth2` v0.36.0** — OAuth2 flows (used by GitHub adapter)
- **`github.com/golang-jwt/jwt/v4` v4.5.2** — JWT handling (indirect, used by ghinstallation)

### Observability

- **`github.com/prometheus/client_golang` v1.23.2** — Prometheus metrics exported from `cadoo-api`, `cadoo-webhook`, `cadoo-worker` via `/metrics` endpoint

### Config & Serialization

- **`gopkg.in/yaml.v3` v3.0.1** — YAML parsing for `.cadoo.yaml` repo config (loaded from PR head SHA)
- **`github.com/tidwall/gjson` v1.18.0** — JSON path queries (indirect)
- **`github.com/tidwall/sjson` v1.2.5** — JSON mutation (indirect)

### Utilities

- **`github.com/bmatcuk/doublestar/v4` v4.10.0** — glob matching (used for `.cadoo.yaml` file filters)
- **`golang.org/x/sync` v0.20.0** — `errgroup` and other sync primitives
- **`golang.org/x/time` v0.14.0** — rate limiting
- **`github.com/hashicorp/go-retryablehttp` v0.7.8** — retrying HTTP client (indirect, used by go-gitlab)

### Testing

- **`github.com/stretchr/testify` v1.11.1** — assertions and test suites
- **`go.uber.org/goleak` v1.3.0** — goroutine leak detector

## Build System

**Primary:** `make` (GNU Make) — `Makefile` at repo root
**Version injection:** `git describe --tags --always --dirty` stamped into `internal/version.Version` via `-ldflags`

Key targets:

| Target | Command |
|--------|---------|
| `make build` | Build all 5 binaries to `./bin/` |
| `make test` | `go test -race -count=1 ./...` |
| `make lint` | `golangci-lint run ./...` (auto-installs if missing) |
| `make vet` | `go vet ./...` |
| `make ci` | `vet + test + build` (what CI runs) |
| `make tools-install` | Install `sqlc`, `goose`, `golangci-lint` |
| `make sqlc` | Regenerate `internal/db/sqlc_gen` |
| `make migrate` | `goose … up` against `$DATABASE_URL` |
| `make migrate-down` | `goose … down` (one migration) |
| `make dev-up` | `docker compose up` full dev stack |

## Code Generation

- **`sqlc` (latest via `go install`)** — generates `internal/db/sqlc_gen` from `db/queries/*.sql` + schema `db/migrations/`. Config: `sqlc.yaml` (pgx/v5 driver, JSON tags, pointer-for-null, emit-interface).
- **`goose` (latest via `go install`)** — migration runner, numbered SQL files in `db/migrations/`, forward-only by convention.

## Linting & Formatting

**Linter:** `golangci-lint` v2 (CI pins `v2.12.2` via `golangci/golangci-lint-action@v9`)
Config: `.golangci.yml`

Enabled linters: `bodyclose`, `errcheck`, `govet`, `ineffassign`, `misspell`, `revive`, `rowserrcheck`, `sqlclosecheck`, `staticcheck`, `unused`

Enabled formatters: `gofmt`, `goimports`
`goimports` local-prefixes: `github.com/payamqorbanpour/cadoo` (cadoo-internal imports form the third group)

Notable revive rules disabled intentionally: `package-comments`, `unused-parameter`

## Database

**Engine:** PostgreSQL 16 with pgvector extension
**Image (dev/CI):** `pgvector/pgvector:pg16`
**Extensions required:** `pgcrypto` (UUIDs), `vector` (KB embeddings at 1536 dims)
**Migrations tool:** `goose` (SQL files `db/migrations/0001_init.sql` … `0005_finding_dedup.sql`)
**Query layer:** `sqlc`-generated code in `internal/db/sqlc_gen` (`package sqlcgen`, pgx/v5 sql package)

## LLM Gateway

**Proxy:** LiteLLM (`ghcr.io/berriai/litellm:main-stable`) running as a sidecar on port 4000
**Protocol:** OpenAI-compatible HTTP API (`/v1/chat/completions`, `/v1/embeddings`)
**Config file:** `deploy/docker/litellm-config.yaml` (also templated for Helm: `deploy/helm/cadoo/templates/litellm-config.yaml`)

Configured models (dev defaults):
- `claude-opus-4-7` → `anthropic/claude-opus-4-7`
- `claude-sonnet-4-6` → `anthropic/claude-sonnet-4-6` (default model)
- `claude-haiku-4-5` → `anthropic/claude-haiku-4-5-20251001`
- `gpt-4o` → `openai/gpt-4o`
- `gemini-1.5-pro` → `gemini/gemini-1.5-pro`

Embedding model default: `text-embedding-3-small` (1536-dim vectors, see `internal/llm/embed/embed.go`)

## Job Queue

**Mode A (with `DATABASE_URL`):** `riverqueue/river` v0.36.0 on Postgres — `cadoo-webhook` enqueues, `cadoo-worker` consumes. Job type: `ToolArgs` in `internal/riverq/queue.go`.
**Mode B (no `DATABASE_URL`):** In-process memory queue (`internal/jobs`) — both webhook receiver and dispatcher goroutine run in the same process (dev mode).

## Static Analysis Sandbox

Sandboxed linters run in Docker containers via `internal/analysis/sandbox/sandbox.go` (shells out to `docker` CLI, `DockerRunner`). The polyglot sandbox image `deploy/docker/Dockerfile.sandbox-polyglot` (base: `alpine:3.19`) bundles:

| Tool | Version |
|------|---------|
| golangci-lint | v1.62.2 |
| ruff | 0.7.4 |
| eslint | 9 (global npm) |
| typescript | 5 (global npm) |
| semgrep | latest pip |
| shellcheck | apk |

Go-side linter adapters: `internal/analysis/linters/{golangci,eslint,ruff,semgrep,shellcheck}/`

## Containerization

**Application image:** `deploy/docker/Dockerfile` — multi-stage (golang:1.26-alpine build → gcr.io/distroless/static-debian12:nonroot runtime). CGO disabled, trimpath. Selects binary via `--build-arg CMD=<name>`.
**CLI image:** `deploy/docker/Dockerfile.cli` — published as `ghcr.io/payamqorbanpour/cadoo-cli:latest` (used in GitHub Actions and GitLab CI runners).
**Dev stack:** `deploy/docker/docker-compose.yml` — postgres + litellm + cadoo-api + cadoo-webhook + cadoo-worker.

## Release

**Tool:** GoReleaser v2 (`.goreleaser.yaml`)
- Builds for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`; `cadoo-cli` also builds `windows/amd64`, `windows/arm64`
- Publishes archives to GitHub Releases
- Docker images published to `ghcr.io/payamqorbanpour/cadoo` (see `.github/workflows/docker-publish.yml`)

## Kubernetes / Helm

**Chart:** `deploy/helm/cadoo/` — Helm chart deploying api, webhook, worker, postgres (optional), and litellm (optional)
Optional hardening features (all off by default): `PodDisruptionBudget`, `NetworkPolicy` (deny-all + allow-intra + allow-egress), Prometheus `ServiceMonitor`

## CI

**Platform:** GitHub Actions (`.github/workflows/ci.yml`)
Jobs: `build` (vet + build + test with `-race`), `lint` (golangci-lint v2.12.2), `migrations` (up→down→up against pgvector/pg16 service container)
Go version in CI: `1.26`
