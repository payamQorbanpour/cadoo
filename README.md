<p align="center">
  <img src="docs/assets/Logo.png" alt="Cadoo logo" width="360" />
</p>

# Cadoo

**AI code reviewer. Multi-VCS, multi-tenant, self-hostable.**

Cadoo posts inline review comments, generates PR descriptions, suggests improvements, and gates merges via a status check — across GitHub, GitHub Enterprise Server, and GitLab. Self-host on your own cluster or run against the SaaS.

---

## Features

- **Inline PR review** — model-driven findings posted as review comments, with severity-based filtering and a `cadoo/review` check run for branch protection.
- **13 review tools** — `/review` `/describe` `/improve` `/ask` `/changelog` `/add_docs` `/deep_review` `/resolve_conflicts` `/add_tests` `/plan` `/check` `/learn` `/unlearn`.
- **Multi-VCS** — GitHub.com, GitHub Enterprise Server, GitLab (self-managed + gitlab.com). Reverse-tunnel agent (`cadoo-tunnel`) lets SaaS reach private VCS without inbound firewall rules.
- **Multi-provider LLM** — talks to any [LiteLLM](https://github.com/BerriAI/litellm)-compatible endpoint, so you can route per-tenant to OpenAI, Anthropic, Bedrock, Azure, local Ollama, etc.
- **Idempotent comments** — fingerprinted findings so resyncs update existing threads instead of spamming new ones.
- **Per-repo config** — `.cadoo.yaml` loaded from the PR head; controls which tools auto-run on which events and paths.
- **Sandboxed static analysis** — runs 10–15 linters inside isolated containers, results fused into the model's context.
- **Knowledge base + learnings** — pgvector-backed; `/learn` and `/unlearn` teach Cadoo team-specific rules that persist across reviews.
- **Enterprise-ready** — OIDC + SAML SSO, RBAC, tamper-resistant audit log, Helm chart with PodDisruptionBudget, NetworkPolicy, and ServiceMonitor.

## Sample output

A typical PR triggers three things: an edited description, one consolidated review comment, and inline suggestions anchored to the diff. Everything is idempotent across resyncs.

### Edited PR description

The user's text stays on top; Cadoo's section is appended between idempotent markers, so re-running `/describe` replaces it cleanly instead of stacking new copies.

```markdown
This PR migrates the `/v2/users` endpoint to the new auth middleware.
Closes #842.

<!-- cadoo:pr-body:begin -->
## Cadoo description

**Title:** Migrate /v2/users to scoped-token middleware

Replaces the legacy bearer-token check with the new `scopedauth` middleware
on the /v2/users endpoint to align with the org-wide auth rotation.

**Type:** Enhancement, Tests

**Changes**

- Swap `authn.Bearer` for `scopedauth.Require("users:read")` on the route
- Drop the now-unused `legacyTokenFromCtx` helper
- Add table-driven tests covering 401 / 403 / scope-mismatch paths
- Update `docs/auth.md` with the new scope name

**Risks:** Callers still on legacy tokens will start receiving 403 — feature-flagged behind `auth.scoped_v2`.
<!-- cadoo:pr-body:end -->
```

### Consolidated review comment

One top-level comment per PR — never multiple. Each tool (`/review`, `/improve`, `/changelog`, …) appears as a collapsible section, and the comment is edited in place on every resync.

```markdown
## Cadoo

<details><summary>🔍 <strong>Review</strong></summary>

This PR cleanly swaps the auth middleware. The new scope check is well-tested;
the only concern is that the rollback path silently drops legacy tokens.

| | |
|---|---|
| ⏱ Effort | ●●●○○ |
| 📄 Files | 7 (~9k tokens) |
| 🔎 Findings | 2 posted (1 blocking) |

<details><summary>⚡ Focus areas</summary>

- **BLOCK** `internal/auth/scoped.go:84` — Token rotation drops in-flight requests
- **WARN** `internal/auth/scoped.go:142` — Scope check missing audit log entry

</details>

</details>

<details><summary>💡 <strong>Suggested improvements</strong></summary>

3 inline suggestion(s) posted:
- `internal/auth/scoped.go:42`
- `internal/auth/scoped.go:91`
- `docs/auth.md:18`

</details>
```

### Inline suggestion

Suggestions are anchored to the exact line. The action is a one-liner; the `suggestion` block lets reviewers apply the change with a single click.

````markdown
**Suggestions:**
- Use pinned digest instead of `latest`

```suggestion
image: ghcr.io/payamqorbanpour/cadoo-cli@sha256:abc123…
```
````

## Architecture

```
cmd/cadoo-api       Public REST API + dashboard backend
cmd/cadoo-webhook   VCS webhook receiver (GitHub / GHES / GitLab)
cmd/cadoo-worker    Job consumer running review pipelines
cmd/cadoo-cli       Local CLI for pre-commit review and admin
cmd/cadoo-tunnel    Reverse-tunnel agent for self-hosted VCS

internal/agent           Tool-calling agent loop
internal/analysis        Sandboxed static-analysis runners
internal/audit           Tamper-resistant audit log
internal/auth            OIDC + SAML + RBAC
internal/billing         Seat tracking + usage metering
internal/config          Config loader (.cadoo.yaml + env)
internal/contextengine   Diff fetch + PR-compression + repo index
internal/db              pgx pool + sqlc-generated queries
internal/findings        Fingerprinting for idempotent comments
internal/issuetrackers   Jira + Linear adapters
internal/jobs            In-memory queue (dev) — interface
internal/kb              Knowledge base (pgvector)
internal/learnings       /learn + /unlearn persistence
internal/llm             LLM gateway (LiteLLM-compatible)
internal/mcp             MCP client
internal/metrics         Prometheus instrumentation
internal/notifiers       Slack + email
internal/orchestrator    Tool dispatcher + pipeline
internal/reports         Run reports + dashboard data
internal/riverq          River-backed Postgres queue (prod)
internal/settings        Multi-tenant settings + secrets
internal/slop            LLM output slop/spam detection
internal/tools           /review /describe /improve /ask ...
internal/vcs             Provider adapters (github, gitlab, ...)
```

The webhook receiver verifies signatures, parses provider-typed events, and enqueues `ToolJob{Provider, Tool, …}` rows. The worker consumes them, runs the tool against the appropriate `vcs.Provider` adapter, and posts results (summary, inline comments, check run).

## Quick start (dev)

Requires Go 1.26+, Docker, and `make`.

```sh
make tools-install   # installs sqlc, goose, golangci-lint
make dev-up          # boots postgres+pgvector + litellm proxy + services
make migrate         # applies db/migrations via goose
make test            # go test -race ./...
make build           # builds all cmd/* binaries to ./bin
make dev-logs        # tail dev stack logs
make dev-down        # tear down dev stack
```

Run `make help` to see every target.

## Configuration

Per-repo behaviour is controlled by a `.cadoo.yaml` file at the repo root, loaded from the PR head SHA on each event.

```yaml
# .cadoo.yaml
auto:
  - on: [opened, synchronize, reopened]
    tools: [review, describe]
  - on: [opened]
    paths: ["**/*.go", "**/*.ts"]
    tools: [improve, add_docs]

review:
  severity_threshold: warn        # nit | warn | block
  request_changes_on: [block]     # severities that fail the check run
  max_comments: 20

comment_policy:
  silent_on_clean: true           # suppress inline comments on clean PRs
  stats_on_clean: true            # still post a one-line "no findings" summary (default)
  skip_if_only_nits: true         # drop the post when every finding is a nit
  min_findings_to_post: 1
```

Tenant-wide config (LLM endpoints, VCS credentials, SSO) lives in `internal/settings` and is managed via the API / dashboard.

## VCS support

| Provider | Webhooks | Inline review | Check run | Slash commands | Tunnel agent |
|---|---|---|---|---|---|
| GitHub.com         | ✅ | ✅ | ✅ check run         | ✅ | n/a |
| GitHub Enterprise  | ✅ | ✅ | ✅ check run         | ✅ | ✅ |
| GitLab (SaaS+self) | ✅ | ✅ | ✅ commit status     | ✅ | ✅ |

GitHub adapter is built on `bradleyfalzon/ghinstallation` + `google/go-github/v66`. GitLab adapter uses `xanzy/go-gitlab v0.115` (discussions API for inline notes with `base_sha`/`start_sha`/`head_sha` positions).

## Pre-merge gates

Cadoo posts a check run named `cadoo/review` on every `/review` dispatch. Add it to your branch protection rules to block merges on findings of severity `block` (default).

Details: [docs/PRE_MERGE_GATES.md](docs/PRE_MERGE_GATES.md).

## Self-hosting

Cadoo runs in two deployment shapes. Pick the one that fits.

### CI-mode (GitLab CI / GitHub Actions)

Run as a one-shot Docker image inside your CI pipeline. No server, no webhook, no DB. Cadoo posts as whatever identity owns the token.

**GitLab CI** — on `merge_request_event`. Mint a **Project Access Token** to post as the auto-created `@project_NNN_bot_HEX` identity.

```yaml
# .gitlab-ci.yml — minimal version
cadoo-review:
  stage: test
  image:
    name: ghcr.io/payamqorbanpour/cadoo-cli:latest
    entrypoint: [""]
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
  script:
    - cadoo ci --mr "${CI_MERGE_REQUEST_PROJECT_URL}/-/merge_requests/${CI_MERGE_REQUEST_IID}"
  allow_failure: true
```

Set `GITLAB_TOKEN`, `LLM_GATEWAY_URL`, and `LLM_GATEWAY_API_KEY` as masked CI/CD variables. Full example: [`deploy/gitlab/.gitlab-ci.cadoo.yml`](deploy/gitlab/.gitlab-ci.cadoo.yml).

**GitHub Actions** — on `pull_request`. Uses the runner-injected `GITHUB_TOKEN`, so Cadoo posts as `github-actions[bot]` with no separate App install.

```yaml
# .github/workflows/cadoo-review.yml — minimal version
name: cadoo-review
on:
  pull_request:
    types: [opened, synchronize, reopened, ready_for_review]
jobs:
  cadoo-review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
      checks: write
      issues: write
    container: ghcr.io/payamqorbanpour/cadoo-cli:latest
    steps:
      - name: Run cadoo
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          LLM_GATEWAY_URL: ${{ secrets.LLM_GATEWAY_URL }}
          LLM_GATEWAY_API_KEY: ${{ secrets.LLM_GATEWAY_API_KEY }}
        run: cadoo ci --pr "${{ github.event.pull_request.html_url }}"
```

Set `LLM_GATEWAY_URL` and `LLM_GATEWAY_API_KEY` as repo (or org) Actions secrets. Full example: [`deploy/github/cadoo-review.yml`](deploy/github/cadoo-review.yml). GHES is supported automatically — the PR URL's host drives provider selection.

Trade-off: CI-mode is stateless, so KB/learnings (`/learn` and `/unlearn`), audit log, and slash-commands on PR/MR notes are not available. Idempotent-comment editing across resyncs also needs the webhook+DB shape. For everything else, CI-mode is the lower-friction path.

### Webhook + Helm (full feature set)

Deploy the Helm chart in [`deploy/helm/cadoo`](deploy/helm/cadoo) against any Kubernetes cluster. Requirements:

- Postgres 15+ with the `pgvector` extension.
- A LiteLLM-compatible endpoint (run the bundled sidecar or point at your own).
- Either inbound webhooks from your VCS, or `cadoo-tunnel` for outbound-only deployments.

The chart includes PodDisruptionBudget, NetworkPolicy, and a Prometheus ServiceMonitor. A no-egress mode is available for air-gapped environments.

## Contributing

1. Fork and branch from `main`.
2. `make ci` must pass (`vet`, `test`, `build`).
3. Run `make lint` and address golangci-lint findings before opening a PR.
4. New SQL goes in `db/migrations/` (goose) with queries in `db/queries/` regenerated via `make sqlc`.

## License

MIT — see [LICENSE](LICENSE).

<p align="center">
  <img src="docs/assets/Banner.png" alt="Cadoo banner" width="100%" />
</p>
