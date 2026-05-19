# CONCERNS
_Last updated: 2026-05-19_

---

## Import Cycle Risks

### kb ↔ llm circular dependency (mitigated)

**Issue:** `internal/kb` needs LLM for query distillation before embedding. Importing `internal/llm` directly from `internal/kb` would create a cycle because `internal/llm` embeds knowledge-layer types.

**Mitigation:** `internal/kb/querydistill` is a separate package (`distill.go`) that holds the `Distiller` struct and depends on `internal/llm`. `internal/kb` itself never imports `internal/llm`.

**Risk:** If a developer adds an LLM-assisted feature inside `internal/kb` (e.g., result reranking, query expansion) and naively imports `internal/llm`, the cycle reappears and breaks the build. The isolation is architectural, not enforced by tooling.

**Files:** `internal/kb/querydistill/distill.go`, `internal/kb/`

**Rule to follow:** All LLM calls originating from the KB layer must live in `internal/kb/querydistill` or a new sibling subpackage. Never import `internal/llm` from `internal/kb` directly.

---

### VCS adapter isolation (enforced by convention only)

**Issue:** The orchestrator and tools depend on `vcs.Provider` interface. Importing `internal/vcs/github` or `internal/vcs/gitlab` from outside `internal/vcs/` would couple the rest of the codebase to a specific adapter.

**Current state:** Only `cmd/cadoo-webhook/main.go` and `cmd/cadoo-cli/ci.go` import the concrete adapters, which is correct. No internal package violates this.

**Risk:** A future tool or orchestrator feature that needs adapter-specific behavior (e.g., checking GHES-specific fields) may be tempted to import the concrete package. This would not fail compilation but creates hidden coupling.

**Files:** `internal/vcs/vcs.go`, `internal/vcs/github/github.go`, `internal/vcs/gitlab/gitlab.go`

---

## Known TODOs and Deferred Work

### Webhook does not load .cadoo.yaml from PR head (phase-2.x)

**Issue:** Both `handleGithubPR` and `handleGitlabMR` in `cmd/cadoo-webhook/main.go` (lines 275 and 358) call `config.Default()` at webhook-dispatch time instead of reading `.cadoo.yaml` from the PR head SHA. Per-repo config is only applied inside `Dispatcher.loadCfg` after the job is dequeued, which means the **auto-trigger table** (`cfg.Auto`) always uses the global default — never the per-repo overrides.

**Impact:** Repositories that configure custom `auto:` tool triggers or disable certain tools in `.cadoo.yaml` will not see those settings respected for automatic webhook-triggered dispatches. Only slash-command runs (which go directly to `Dispatcher.Run`) and CI-mode runs see per-repo config correctly.

**Files:** `cmd/cadoo-webhook/main.go:275`, `cmd/cadoo-webhook/main.go:358`

**Fix approach:** After fetching the event's head SHA, fetch `.cadoo.yaml` via the VCS adapter before building the `ToolJob` list. This requires a FileFetcher call in the webhook handler, before the queue round-trip.

---

### GitLab multi-tenant auth not implemented

**Issue:** `internal/vcs/gitlab/gitlab.go` package comment explicitly states "multi-tenant SaaS will swap in per-installation tokens later." The current adapter uses a single global token (`Config.Token`), which means a multi-tenant SaaS deployment cannot route different customers to different GitLab installations.

**Files:** `internal/vcs/gitlab/gitlab.go:3`, `internal/settings/settings.go` (`GitLabToken` is a single string)

**Impact:** Self-host and single-org SaaS deployments are unaffected. Multi-tenant SaaS deployments cannot support multiple distinct GitLab installations without code changes.

---

### GitHub App uses a single default InstallationID

**Issue:** `buildDispatcher` in `cmd/cadoo-webhook/main.go` (line 162) passes `s.GitHubDefaultInstallationID` as the static installation ID for the GitHub adapter. In a multi-tenant SaaS context, the installation ID must be looked up per repository (stored in the `installations` table in migration 0001). The current webhook wires a single adapter instance and never consults the DB for the correct installation.

**Files:** `cmd/cadoo-webhook/main.go:162`, `cmd/cadoo-worker/main.go:145`, `internal/settings/settings.go:31-33`

**Impact:** Multi-repository GitHub App deployments are silently limited to one installation. This works for self-hosted single-org setups but will mis-authenticate requests in true multi-tenant mode.

---

## Fragile Areas

### Marker and dedup system (`internal/vcs/marker.go`, `internal/findings/`)

The comment dedup pipeline consists of four interlocking layers:

1. **`vcs.InlineMarker` / `vcs.ParseInlineMarker`** (`internal/vcs/marker.go`) — encodes tool, structural key, severity, and normalized title into a hidden HTML comment appended to each inline posting.
2. **`findings.StampInline`** (`internal/findings/prior.go`) — applies the marker to the wire copy only, preserving the pristine body for key computation.
3. **`findings.StructuralKey` / `findings.Fingerprint`** (`internal/findings/findings.go`) — two hashes of different granularity. Fingerprint covers the full body (exact match only). StructuralKey covers tool+file+severity+normalizedTitle (survives LLM rephrasing).
4. **`findings.NewFromPrior`** (`internal/findings/prior.go`) — reconstructs an in-memory `Store` from `PriorReview` (read back from VCS) for CI-mode stateless dedup.

**Why fragile:**
- The `StructuralKey` deliberately omits line numbers so it matches after code shifts. This means a finding on line 10 and a different finding on line 200 with identical title/severity in the same file will be treated as duplicates.
- `normalizeTitle` walks up to 200 characters and strips markdown, bullets, bold, and `[TAG]` prefixes. Tools that produce bodies where the first 200 characters are a static header (e.g., `"**Suggestions:**\n..."`) will have non-distinguishable keys. The `improve` tool is specifically called out in a comment as a known case.
- The Jaccard threshold `SimilarTitleThreshold = 0.5` (`internal/findings/findings.go:48`) is a single global constant. A too-low value causes false dedup; too-high misses rephrasing. There is no per-tool tuning mechanism.
- CI-mode dedup relies on reading back the marker from VCS (which requires `PriorReviewReader`). If the marker is stripped by a VCS host's comment sanitization or markdown rendering, CI-mode will lose stateless dedup for that run and re-post all findings.
- Legacy markers (no `nt=` field) degrade the Jaccard dedup match quality for multi-line bodies, falling back to first-line normalization only (`prior.go:42-44`).

**Safe modification:** Any change to `InlineMarker` format (e.g., adding a new field) requires a version bump in the marker regex AND backward-compatible parsing of legacy markers. The `v=1` version tag exists for exactly this purpose. New marker fields must be additive and optional.

---

### Consolidated comment rendering (`internal/orchestrator/consolidate.go`)

The consolidated comment wraps all tool sections inside HTML marker comments (`<!-- cadoo:wrapper:begin -->`). The same marker constant (`vcs.SummaryWrapperBegin`) is read by:
- `internal/vcs/github/github.go:ListCadooArtifacts` (grepping issue comment bodies)
- `internal/vcs/gitlab/gitlab.go:ListCadooArtifacts` (grepping note bodies)
- `internal/orchestrator/consolidate.go` (writer)

**Why fragile:** If the `wrapperBegin` or `wrapperEnd` constants in `consolidate.go` diverge from `vcs.SummaryWrapperBegin`, the CI-mode read-back will fail to identify the summary comment, and CI-mode will create a new comment on every run instead of editing the existing one. The writer uses `vcs.SummaryWrapperBegin` as an alias (`wrapperBegin = vcs.SummaryWrapperBegin`), which is correct, but `wrapperEnd` is a separate local constant with no cross-package check.

**Safe modification:** Never change `SummaryWrapperBegin` without also updating all regex/string-search consumers in both VCS adapters and the `consolidate.go` writer.

---

### `isMissingFile` uses string heuristics for 404 detection

**Issue:** `Dispatcher.loadCfg` calls `isMissingFile(err)` to distinguish "file doesn't exist on this branch" from other errors. The check (`internal/orchestrator/reviewer.go:518-527`) uses `errors.Is(err, fs.ErrNotExist)` first, then falls back to `strings.Contains(msg, "404") || strings.Contains(msg, "not found")`. VCS SDKs wrap errors in their own types; the string heuristic is explicitly called out as a "soft heuristic."

**Risk:** A transient 404 from a GitHub/GitLab rate limit (wrong URL path construction) would silently be treated as "no .cadoo.yaml" and fall back to base config. A VCS error message format change could cause legitimate missing-file cases to not be recognized, leading to a logged warning on every dispatch even when the file is correctly absent.

**Files:** `internal/orchestrator/reviewer.go:518-527`

---

### GitHub PostInlineComments returns no per-comment IDs

**Issue:** GitHub's review-create API returns a review object, not per-comment IDs. `PostInlineComments` in `internal/vcs/github/github.go:216-233` therefore returns `PostedInlineRef` slices with empty `ExternalID` fields. Thread resolution (`ResolveThread`) uses GraphQL node IDs recovered via `ListCadooArtifacts` — which only works if the CI-mode read-back path successfully parsed the marker from the thread's first comment.

**Impact:** If any inline comment is posted without a marker (e.g., a tool bypasses `StampInline`, or the comment is from a pre-marker Cadoo version), its thread can never be resolved. The `resolveStalePriors` function silently skips priors with empty `ExternalCommentID` (`reviewer.go:475`).

**Files:** `internal/vcs/github/github.go:216-233`, `internal/orchestrator/reviewer.go:466-493`

---

### GitLab unanchored notes cannot be resolved

**Issue:** When a GitLab inline comment target line is outside the diff, `PostInlineComments` (`internal/vcs/gitlab/gitlab.go:164-170`) posts a top-level MR note instead. These unanchored notes have no resolvable discussion thread in GitLab, so their `ExternalID` is always empty. `resolveStalePriors` will never resolve them even when the underlying finding is fixed.

**Files:** `internal/vcs/gitlab/gitlab.go:164-170`, `internal/findings/prior.go:46-52`

---

## Security Considerations

### GitHub webhook signature verification

**Status: Correct.** `internal/vcs/github/webhook.go` uses `hmac.Equal` (constant-time comparison) and HMAC-SHA256 over the full request body. An empty `secret` returns an error immediately rather than silently allowing all traffic.

### GitLab webhook token verification

**Status: Correct but weaker.** `internal/vcs/gitlab/webhook.go` uses `subtle.ConstantTimeCompare` for the `X-Gitlab-Token` header. However, GitLab's design does not HMAC the payload — only a shared secret is compared. A network adversary who can observe the token header value once can replay requests.

**Risk:** No replay protection. Mitigated in practice by TLS, but weaker than GitHub's body-signed HMAC scheme.

**Files:** `internal/vcs/gitlab/webhook.go`

### Multi-tenancy: `posted_findings` and `posted_summaries` lack `org_id`

**Issue:** The idempotency tables (`posted_findings`, `posted_summaries`) are keyed by `(provider, repo_full_name, pr_number)` with no `org_id` column (see `db/migrations/0003_posted_state.sql`, `0004_summary_sections.sql`, `0005_finding_dedup.sql`). The schema comment in `0003` explicitly notes this: "we deliberately skip the FK to keep the dispatcher's hot path simple."

**Risk:** In a multi-tenant SaaS deployment where two different organizations happen to share a GitHub App installation (or a self-managed GitLab instance), a `(provider, repo_full_name, pr_number)` tuple could theoretically match across organizations. In practice this is only exploitable if organizations can control repo names; in a properly partitioned GitHub App deployment, each org has its own install so `(github, org/repo, 42)` is globally unique. But this is an implicit assumption that is not enforced in the schema.

**Files:** `db/migrations/0003_posted_state.sql`, `internal/findings/findings.go`

### LiteLLM API key in environment

**Issue:** The LLM gateway API key (`LLM_GATEWAY_API_KEY`) flows from `settings.FromEnv()` through the whole dispatcher. It is stored in the `Dispatcher.LLM` client as a field. No key rotation, scoping, or audit of LLM call attribution to a specific org/installation is currently implemented.

**Files:** `internal/settings/settings.go:77`, `internal/llm/litellm/client.go:31`

---

## Performance Concerns

### LLM calls: no per-request timeout beyond HTTP client default

**Issue:** `internal/llm/litellm/client.go` sets `Timeout: 120 * time.Second` on the HTTP client. This is the only timeout for an LLM completion. For slow models or large diffs (up to `MaxTokens=50_000` input tokens), a single tool call can hold a goroutine for up to 2 minutes. With River workers, this affects throughput per worker.

**Files:** `internal/llm/litellm/client.go:33`

### LLM retry does not handle HTTP 429 (rate limit)

**Issue:** `internal/llm/retry.go` retries on HTTP 502, 503, 504 — but not 429 (Too Many Requests). A rate limit from the LLM gateway or underlying provider silently surfaces as a tool error and is not retried with backoff.

**Files:** `internal/llm/retry.go:40-47`

**Fix approach:** Add `http.StatusTooManyRequests` (429) to the retry set in `DoJSON`, ideally with respect for the `Retry-After` header.

### GitLab inline comments: one API call per comment

**Issue:** Unlike the GitHub adapter (which batches all inline comments into a single PR review), the GitLab adapter (`internal/vcs/gitlab/gitlab.go:PostInlineComments`) creates one MR discussion per comment in a serial loop. For a review with 20+ inline findings, this means 20+ sequential API calls, each with a GitLab round-trip. Additionally, each call to `PostInlineComments` fetches the full MR twice: once for `diff_refs` and once for `diffs`.

**Files:** `internal/vcs/gitlab/gitlab.go:130-209`

**Impact:** Latency for GitLab reviews scales linearly with the number of inline findings. GitLab's API rate limits (600 req/min per token) will become a bottleneck at moderate comment volumes.

### Token estimation uses 4 chars/token (rough approximation)

**Issue:** `contextengine.EstimateTokens` (`internal/contextengine/compress.go:17`) uses `(len(s) + 3) / 4`. This is a byte-count heuristic, not a true tokenizer. Code with many short tokens (e.g., Python with many symbols) will be underestimated; code with long identifier names will be overestimated.

**Risk:** With large PRs, context budgets may be miscalculated, causing either unnecessary truncation (skipping files the model could fit) or slight budget overruns.

**Files:** `internal/contextengine/compress.go:17`

### In-memory findings store persists synchronously on every mutation

**Issue:** `memoryStore.persist()` (`internal/findings/findings.go:692-722`) writes the full JSON state to a temp file and renames it on every `record`, `putSummaryID`, and `putSection` call. For high-frequency dispatches in the no-DB mode, this creates a write-per-posting pattern on the filesystem.

**Files:** `internal/findings/findings.go:692-722`

---

## Operational Concerns

### CI-mode degradation path when `ListCadooArtifacts` fails

**Issue:** In `cmd/cadoo-cli/ci.go:priorStore`, if `ListCadooArtifacts` returns an error, the function returns `nil` and logs a warning. The dispatcher then runs with `d.Posted == nil`, disabling all idempotency. On subsequent pushes to the same PR, Cadoo will re-post every finding from scratch (the warning text says "comments may duplicate this run").

**Risk:** Transient API errors (rate limit, network blip) during the artifact read-back silently degrade CI-mode to non-idempotent mode for that run, resulting in duplicate inline comments visible to reviewers.

**Files:** `cmd/cadoo-cli/ci.go:240-249`

### In-process memory queue has no persistence or overflow handling

**Issue:** `internal/jobs/jobs.go` implements the in-memory queue used in single-binary dev mode. If the process crashes before consuming a job, the job is lost. There is no queue depth limit — a burst of webhooks before the worker goroutine catches up will grow memory unboundedly.

**Files:** `internal/jobs/jobs.go`

### Dispatcher dedup with concurrent dispatches on the same PR

**Issue:** `Dispatcher.postInline` uses a two-stage filter: first checking `d.Posted.HasFinding` (which locks the `memoryStore` or queries Postgres), then the intra-batch `seenKeys` map. However, if two dispatches for the same PR arrive concurrently (e.g., two rapid `synchronize` events before the first finishes), both will read the same empty prior state, produce overlapping delta sets, and both will post. The `UNIQUE` constraint in Postgres on `(provider, repo_full_name, pr_number, fingerprint)` prevents duplicate DB rows, but both posts still reach GitHub/GitLab before the DB insert happens.

**Files:** `internal/orchestrator/reviewer.go:386-458`, `internal/findings/findings.go:162-193`

**Mitigation:** Acceptable in practice because River serializes jobs by default queue. The scenario primarily applies to the in-memory dev mode queue which does not serialize same-PR jobs.

### `.cadoo.yaml` loaded from CI-mode repo checkout, not from PR head SHA

**Issue:** In `cmd/cadoo-cli/ci.go:166`, the per-repo config is loaded from `filepath.Join(*repoDir, ".cadoo.yaml")` — the checked-out working tree. In webhook/worker mode, config is loaded from the **PR head SHA** via `Dispatcher.loadCfg`. These two paths are different: CI-mode uses the local disk file (which may be the target branch's version, not the PR branch's), while webhook mode reads from VCS at the exact head commit.

**Impact:** In CI-mode, if the PR branch modifies `.cadoo.yaml`, the PR-branch version will be used only if the CI pipeline checks out the head branch (which is typically the case for GitHub Actions `pull_request` triggers). For GitLab MR pipelines, the checkout SHA matters. This is a subtle behavior difference that is not documented in `cmd/cadoo-cli/ci.go`.

**Files:** `cmd/cadoo-cli/ci.go:162-169`

---

## Test Coverage Gaps

### Tool implementations have no tests

The following tool packages have zero test files:

- `internal/tools/adddocs/`
- `internal/tools/addtests/`
- `internal/tools/ask/`
- `internal/tools/changelog/`
- `internal/tools/check/`
- `internal/tools/deepreview/`
- `internal/tools/improve/`
- `internal/tools/learn/`
- `internal/tools/plan/`
- `internal/tools/unlearn/`

**Risk:** LLM prompt changes, JSON schema changes, or input marshaling bugs in any of these tools will not be caught before deployment. The only coverage they get is through `orchestrator/reviewer_test.go` (integration-style tests using a fake LLM) and manual QA.

**Priority:** High for `improve` and `review` (core user-facing tools), medium for the rest.

### `internal/kb/querydistill/` has no tests

`distill.go` (the LLM-assisted query rewriter) has no test file. Its failure mode (returning the original text on error) means bugs here silently degrade KB recall rather than failing loudly.

**Files:** `internal/kb/querydistill/distill.go`

### `internal/riverq/` has no tests

The River queue adapter (`queue.go`) wires `ToolArgs` to `orchestrator.ToolJob` with manual field mapping. A field name mismatch between `ToolArgs` and `ToolJob` would cause silent zero-values when deserializing queued jobs. No test validates this round-trip.

**Files:** `internal/riverq/queue.go`

### `internal/settings/` has no tests

`settings.FromEnv()` reads every environment variable the application depends on. Missing or malformed values (e.g., non-integer `GITHUB_APP_ID`) return errors or silent zero values. No test validates the full env-parsing surface.

**Files:** `internal/settings/settings.go`

### Integration tests require a live Postgres (no test-only schema setup)

Test files in `internal/findings/` and `internal/orchestrator/` use in-memory stores exclusively. The Postgres-backed `Store` methods (`HasFinding`, `RecordFinding`, `ListPostedFindings`, etc.) have no integration tests. SQL correctness (including the `structural_idx` added in migration 0005) is only tested by running `make ci` with a live database.

---

## Tech Debt

### `findings.go` is a 754-line mega-file

`internal/findings/findings.go` contains: the public `Store` API, the `memoryStore` implementation, the dedup hashing algorithms (`Fingerprint`, `StructuralKey`, `normalizeTitle`, `jaccard`), the JSON persistence codec, and the `WrapperToolKey` constant. Splitting into sub-files (e.g., `hash.go`, `memstore.go`, `pgstore.go`) would improve navigability and allow independent testing of the hashing layer.

**Files:** `internal/findings/findings.go`

### `context.Background()` in CI-mode has no timeout

`cmd/cadoo-cli/ci.go:182` creates `ctx := context.Background()` without a deadline. A hung LLM call (e.g., LiteLLM sidecar unresponsive) will block the CI job indefinitely, relying on the 120s HTTP client timeout in `litellm/client.go` as the only backstop.

**Files:** `cmd/cadoo-cli/ci.go:182`

**Fix approach:** Add a configurable CI-mode timeout (e.g., `--timeout 10m`) wrapping the tool dispatch loop.

### Token budget hardcodes in `Dispatcher` defaults

`Dispatcher.maxTokens()` and `Dispatcher.perFileTokens()` return 50,000 and 8,000 respectively when the struct fields are zero (`internal/orchestrator/reviewer.go:541-553`). These defaults are not exposed in `settings.go` or as env vars — the only override is via `CADOO_MAX_TOKENS` (read inside `tools/llmjson.go` for the completion budget) and the struct fields set by callers. There is no completion vs context token budget separation at the dispatcher level.

**Files:** `internal/orchestrator/reviewer.go:541-553`

### Linter parallelism in `runLinters` spawns unbounded goroutines

`Dispatcher.runLinters` (`internal/orchestrator/reviewer.go:571-623`) spawns one goroutine per `(extension, linter)` pair with no concurrency cap. For a repository with many file extensions and many linters registered for each, this could spawn dozens of Docker containers simultaneously.

**Files:** `internal/orchestrator/reviewer.go:604-618`

### `spliceCadooBody` in `consolidate.go` has an edge case on trailing content

`spliceCadooBody` (`internal/orchestrator/consolidate.go:111-124`) only handles `prSectionEnd` at the end of a managed region. If a PR body contains `prSectionBegin` but no `prSectionEnd` (e.g., manually edited), `endIdx` will be -1, the `startIdx >= 0 && endIdx > startIdx` branch is skipped, and the function appends a second `prSectionBegin` block. The PR body will then contain two Cadoo sections.

**Files:** `internal/orchestrator/consolidate.go:111-124`
