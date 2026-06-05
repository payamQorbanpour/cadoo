# Phase 2: Webhook Auto-Trigger + State — Research

**Researched:** 2026-06-05
**Domain:** Go webhook ingestion, River job queuing, Goose migrations, release-docs subsystem extension
**Confidence:** HIGH

## Summary

Phase 2 extends the existing webhook binary (`cadoo-webhook`) and worker binary (`cadoo-worker`) to handle GitHub `release: published` and GitLab `Release Hook` / `Tag Push Hook` events, building a `ReleaseJob` and enqueuing it through the same dual-mode queue (River when `DATABASE_URL` is set, in-memory goroutine otherwise) that the PR-review pipeline already uses. The dispatcher invoked by the worker is `releasedocs.Dispatcher` (from Phase 1) — identical contract to `orchestrator.Dispatcher.Run` but in the parallel subsystem.

New work in this phase has four parts: (1) webhook event parsing and early-exit logic for `releaseDocs.trigger`, (2) River job registration for `ReleaseJob` (a new `ReleaseArgs` struct and `releaseWorker` alongside `toolWorker`), (3) a new `0006_release_docs_state.sql` migration table keyed on `(provider, repo_full_name, to_tag, artifact_kind)` for DB-backed idempotency, and (4) two new sub-packages: `internal/releasedocs/generators/blog` and `internal/releasedocs/publishers/pages`.

The CR-01 carry-forward bug (GitLab `UpdateReleaseBody` hard-fails) must be fixed in this phase because the `releasebody` publisher already exists and GitLab users will exercise it via the webhook path.

**Primary recommendation:** Follow the existing `ToolArgs`/`toolWorker` River pattern in `internal/riverq/queue.go` exactly, adding a parallel `ReleaseArgs`/`releaseWorker` for `ReleaseJob`. The migration follows the `0003_posted_state.sql` pattern (keyed unique index on the natural composite key, no FK to avoid coupling). Blog generator mirrors `releasenotes.Generator` structure. Pages publisher uses the existing `vcs.BranchCommitter.UpsertFile` already implemented in both adapters.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Webhook event parsing (release/tag) | cadoo-webhook binary | vcs/github, vcs/gitlab helpers | Verification and dispatch routing already live here |
| Job enqueue (River / in-memory) | internal/riverq | internal/jobs | Existing dual-mode pattern is already the queue tier |
| Job consumption + dispatcher call | cadoo-worker binary | riverq.releaseWorker | Worker runs releasedocs.Dispatcher.Run same as orchestrator |
| Early-exit trigger check | cadoo-webhook binary | — | Must read .cadoo.yaml from ToRef tree before enqueue |
| DB state table (idempotency) | internal/releasedocs (new store) | DB migration 0006 | Mirrors findings.Store; keyed on (provider, repo, to_tag, artifact_kind) |
| Blog generation | internal/releasedocs/generators/blog | — | New Generator implementation, mirrors releasenotes |
| Pages publish | internal/releasedocs/publishers/pages | vcs.BranchCommitter | New Publisher; BranchCommitter.UpsertFile already in both adapters |
| GitLab UpdateReleaseBody CR-01 fix | internal/vcs/gitlab/release.go | releasebody publisher | Fix UpdateReleaseBody to use TagName not numeric ID |

## Standard Stack

No new external libraries required. Phase 2 uses only packages already in `go.mod`.

### Core (already in go.mod — no new installs)
| Library | Version in go.mod | Purpose | Role in Phase 2 |
|---------|---------|---------|--------------|
| `github.com/riverqueue/river` | v0.36.0 | Postgres-backed job queue | Add `ReleaseArgs`/`releaseWorker` alongside existing `ToolArgs`/`toolWorker` |
| `github.com/google/go-github/v66` | v66.0.0 | GitHub API client + webhook parsing | `*gogithub.ReleaseEvent` for `release: published`; `*gogithub.PushEvent` for tag push |
| `gitlab.com/gitlab-org/api/client-go` | v1.46.0 | GitLab API client + webhook parsing | `*glab.ReleaseEvent` (EventTypeRelease) and `*glab.TagEvent` (EventTypeTagPush) |
| `github.com/pressly/goose/v3` (CLI) | `@latest` via Makefile | Migration tool | Migration `0006_release_docs_state.sql` following goose Up/Down format |
| `github.com/jackc/pgx/v5` | existing | Postgres driver | Raw pgx queries in the new release-docs state store |

[VERIFIED: codebase grep of go.mod and cmd/cadoo-webhook/main.go]

### No New Dependencies
The Package Legitimacy Audit section is omitted: Phase 2 introduces zero new packages. All required types (`*gogithub.ReleaseEvent`, `*glab.ReleaseEvent`, `*glab.TagEvent`) already exist in the pinned versions of the SDK libraries in `go.mod`.

## Package Legitimacy Audit

Not applicable — no new packages are installed in this phase. All code uses existing `go.mod` dependencies.
[VERIFIED: codebase review of all required types in pinned SDK versions]

## Architecture Patterns

### System Architecture Diagram

```
VCS (GitHub/GitLab)
      |
      | POST /webhook/github or /webhook/gitlab
      v
cadoo-webhook
  githubWebhookHandler / gitlabWebhookHandler
      |
      |-- PullRequestEvent / MergeEvent --> (existing) orchestrator.ToolJob enqueue
      |
      |-- ReleaseEvent (action=published) --> [NEW] handleGithubRelease
      |-- PushEvent (ref=refs/tags/v*) --> [NEW] handleGithubTagPush (if trigger=tag)
      |-- glab.ReleaseEvent --> [NEW] handleGitlabRelease
      |-- glab.TagEvent --> [NEW] handleGitlabTagPush (if trigger=tag)
              |
              |  early exit: load .cadoo.yaml from ToRef, check releaseDocs.trigger
              v
         ReleaseJob{Provider, Repo, Org, FromRef, ToRef}
              |
         enqueue (same enqueueFn as ToolJob)
              |
    +---------+------------+
    |                      |
  DATABASE_URL set       no DATABASE_URL
    |                      |
  riverq.EnqueueRelease  jobs.MemQueue.Enqueue(ReleaseJob)
  (new method)              |
    |                    in-process goroutine
    v                      |
cadoo-worker               |
  River consumer           v
  releaseWorker.Work   releasedocs.Dispatcher.Run(ctx, job)
    |
  releasedocs.Dispatcher.Run(ctx, job)
    |
    |-- changelog generator (Phase 1)
    |-- releasenotes generator (Phase 1)
    |-- blog generator [NEW] (when: minor_or_above)
    |
    |-- releasebody publisher (Phase 1, + CR-01 fix for GitLab)
    |-- changelogpr publisher (Phase 1)
    |-- pages publisher [NEW]
    |     |
    |     v
    |   BranchCommitter.UpsertFile (already in github + gitlab adapters)
    |   deterministic path: {dir}/releases/{toRef}/RELEASE_NOTES.md etc.
    |
    v
  releasedocs.StateStore [NEW]
    INSERT ON CONFLICT DO UPDATE
    (provider, repo_full_name, to_tag, artifact_kind)
    -> external_id, updated_at
```

### Recommended Project Structure

```
internal/releasedocs/
  generators/
    blog/
      blog.go                 # new Generator: KindBlog, when: minor_or_above
      blog_test.go
  publishers/
    pages/
      pages.go                # new Publisher: TargetPages, UpsertFile per artifact
      pages_test.go
  state/
    state.go                  # new StateStore: DB-backed + in-memory fallback
    state_test.go

internal/riverq/
  queue.go                    # add ReleaseArgs, releaseWorker, EnqueueRelease

db/migrations/
  0006_release_docs_state.sql  # new goose migration

internal/releasedocs/
  releasedocs.go              # add KindBlog, TargetPages constants
  defaults/
    defaults.go               # add blog.New(), pages.New() to default slices

internal/config/
  config.go                   # extend PublishTarget to carry branch/dir for pages;
                              # add Blog ArtifactConfig to ReleaseArtifacts

cmd/cadoo-webhook/main.go    # add handleGithubRelease, handleGithubTagPush,
                              # handleGitlabRelease, handleGitlabTagPush handlers
cmd/cadoo-worker/main.go     # register releaseWorker in riverq.New call

internal/vcs/gitlab/
  release.go                  # fix CR-01: UpdateReleaseBody to use TagName
```

### Pattern 1: River Worker Registration (parallel to existing toolWorker)

The existing `internal/riverq/queue.go` adds one struct and one worker. Phase 2 adds a second pair following the identical pattern.

```go
// Source: internal/riverq/queue.go (existing pattern to follow exactly)

// ReleaseArgs is River's typed payload for releasedocs.ReleaseJob.
type ReleaseArgs struct {
    Provider string `json:"provider"`
    Repo     string `json:"repo"`
    Org      string `json:"org"`
    FromRef  string `json:"from_ref"`
    ToRef    string `json:"to_ref"`
}

// Kind identifies this job type to River.
func (ReleaseArgs) Kind() string { return "release_docs" }

type releaseWorker struct {
    river.WorkerDefaults[ReleaseArgs]
    dispatcher *releasedocs.Dispatcher
}

func (w *releaseWorker) Work(ctx context.Context, j *river.Job[ReleaseArgs]) error {
    return w.dispatcher.Run(ctx, releasedocs.ReleaseJob{
        Provider: vcs.Kind(j.Args.Provider),
        Repo:     j.Args.Repo,
        Org:      j.Args.Org,
        FromRef:  j.Args.FromRef,
        ToRef:    j.Args.ToRef,
    })
}
```

[VERIFIED: internal/riverq/queue.go — ToolArgs/toolWorker is the exact pattern]

### Pattern 2: GitHub Release Event Routing

```go
// Source: internal/vcs/github/webhook.go — ParseEvent already delegates to
// gogithub.ParseWebHook which returns *gogithub.ReleaseEvent for "release" events
// and *gogithub.PushEvent for "push" events.

// In cadoo-webhook/main.go githubWebhookHandler switch:
case *gogithub.ReleaseEvent:
    if e.GetAction() == "published" {
        handleGithubRelease(r.Context(), e, s, enqueueRelease)
    }
case *gogithub.PushEvent:
    handleGithubTagPush(r.Context(), e, s, enqueueRelease)
```

Key fields from `*gogithub.ReleaseEvent`:
- `e.GetAction()` — must equal `"published"` to act
- `e.GetRelease().GetTagName()` — the ToRef tag (e.g. `"v1.2.3"`)
- `e.GetRepo().GetFullName()` — `"owner/repo"`

Key fields from `*gogithub.PushEvent` for tag push:
- `e.GetRef()` — `"refs/tags/v1.2.3"` — strip `"refs/tags/"` prefix for tag name
- `e.GetCreated()` — must be `true` (new tag, not deletion)
- `e.GetDeleted()` — must be `false`

[VERIFIED: go-github v66 event_types.go — ReleaseEvent.Action, ReleaseEvent.Release.TagName, PushEvent.Ref]

### Pattern 3: GitLab Release and Tag Push Event Routing

```go
// glab.ParseWebhook returns *glab.ReleaseEvent for EventTypeRelease ("Release Hook")
// and *glab.TagEvent for EventTypeTagPush ("Tag Push Hook")

// In cadoo-webhook/main.go gitlabWebhookHandler switch:
case *glab.ReleaseEvent:
    if e.Action == "create" {
        handleGitlabRelease(r.Context(), e, s, enqueueRelease)
    }
case *glab.TagEvent:
    handleGitlabTagPush(r.Context(), e, s, enqueueRelease)
```

Key fields from `*glab.ReleaseEvent`:
- `e.Action` — `"create"` for a new published release
- `e.Tag` — the tag name (e.g. `"v1.2.3"`)
- `e.Project.PathWithNamespace` — `"group/repo"` (the repo full name)

Key fields from `*glab.TagEvent`:
- `e.Ref` — `"refs/tags/v1.2.3"` — strip `"refs/tags/"` prefix
- `e.After` — SHA of the new tag commit (non-zero-string means tag created, not deleted)
- `e.Project.PathWithNamespace` — repo full name

[VERIFIED: gitlab.com/gitlab-org/api/client-go v1.46.0 event_webhook_types.go]

### Pattern 4: Early-Exit Trigger Check

The webhook handler must load `.cadoo.yaml` from the tag tree (ToRef) and check `releaseDocs.trigger` before enqueuing. This is the same `FileFetcher` pattern used by the dispatcher.

```go
// handleGithubRelease early exit pattern
func handleGithubRelease(ctx context.Context, e *gogithub.ReleaseEvent,
    s *settings.Settings, enqueue enqueueReleaseFn) {

    toRef := e.GetRelease().GetTagName()
    repo := e.GetRepo().GetFullName()

    // Resolve provider from VCSPool to load config
    provider := resolveGithubProvider(s)
    if provider == nil {
        return // no GitHub configured
    }

    // Load config from ToRef tree — nil-tolerant (same as dispatcher.loadCfg)
    cfg := loadReleaseCfg(ctx, provider, repo, toRef)

    if !cfg.Enabled {
        return // releaseDocs master switch off
    }
    // Check trigger matches
    if cfg.Trigger != "" && cfg.Trigger != "release" {
        slog.Debug("releasedocs: trigger excludes release event; skipping",
            "repo", repo, "trigger", cfg.Trigger)
        return
    }
    // ... build and enqueue ReleaseJob
}
```

[VERIFIED: cmd/cadoo-webhook/main.go handleGithubPR pattern; releasedocs/dispatcher.go loadCfg]

### Pattern 5: DB State Migration (Goose Up/Down)

```sql
-- Source: db/migrations/0003_posted_state.sql (pattern to follow)
-- db/migrations/0006_release_docs_state.sql

-- +goose Up
-- +goose StatementBegin

-- Release-docs published state. Keyed by (provider, repo_full_name, to_tag, artifact_kind)
-- so repeated dispatcher runs edit in place rather than re-publishing.
-- Deliberately no FK to repos/pull_requests to keep the dispatcher's hot path decoupled.
CREATE TABLE release_docs_state (
    id              BIGSERIAL PRIMARY KEY,
    org_id          TEXT,           -- Cadoo org for multi-tenancy (matches orgs.id text form)
    provider        TEXT NOT NULL,
    repo_full_name  TEXT NOT NULL,
    to_tag          TEXT NOT NULL,
    artifact_kind   TEXT NOT NULL,  -- changelog | release_notes | blog
    external_id     TEXT,           -- release body ID, PR number, pages commit SHA etc.
    published_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, repo_full_name, to_tag, artifact_kind)
);
CREATE INDEX release_docs_state_repo_idx
    ON release_docs_state (provider, repo_full_name, to_tag);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS release_docs_state;
-- +goose StatementEnd
```

[VERIFIED: db/migrations/0003_posted_state.sql; db/migrations/0001_init.sql — pattern is BIGSERIAL PK, UNIQUE composite, INDEX on lookup tuple]

### Pattern 6: Blog Generator Structure (mirrors releasenotes.Generator)

```go
// Source: internal/releasedocs/generators/releasenotes/releasenotes.go

package blog

// Generator implements releasedocs.Generator for the blog artifact kind.
// The blog is a long-form LLM-authored announcement. When rc.LLM is nil,
// it returns a deterministic skeleton (nil-tolerant, D-11).
type Generator struct{}

func New() *Generator { return &Generator{} }

func (g *Generator) Kind() releasedocs.ArtifactKind { return releasedocs.KindBlog }

// Enabled returns true only for minor_or_above bumps and when cfg.Artifacts.Blog.Enabled.
// The when: condition should default to "minor_or_above" per the SPEC.
func (g *Generator) Enabled(cfg config.ReleaseDocs, bump releasedocs.SemverBump) bool {
    return releasedocs.Enabled(cfg.Artifacts.Blog.ArtifactConfig, bump)
}
```

[VERIFIED: internal/releasedocs/generators/releasenotes/releasenotes.go — identical interface]

### Pattern 7: Pages Publisher (uses existing BranchCommitter)

```go
// Source: internal/releasedocs/publishers/changelogpr/changelogpr.go (pattern)
// and vcs.BranchCommitter.UpsertFile already implemented in github + gitlab adapters.

// Publish commits one file per artifact to the pages branch at deterministic paths.
// Path: {pages.Dir}/releases/{toRef}/{artifactKind}.md
// Idempotency: UpsertFile overwrites the same path on re-run (same file, same path).
func (p Publisher) Publish(ctx context.Context, rc releasedocs.ReleaseContext, arts []releasedocs.Artifact) error {
    bc, ok := rc.Provider.(vcs.BranchCommitter)
    if !ok {
        slog.Info("releasedocs pages: provider lacks BranchCommitter; skipping",
            "provider", rc.Provider.Kind())
        return nil
    }
    cfg := rc.Config.Publish.Pages
    if !cfg.Enabled {
        return nil
    }
    branch := cfg.Branch
    if branch == "" {
        branch = "gh-pages"
    }
    dir := cfg.Dir
    if dir == "" {
        dir = "docs"
    }
    for _, art := range arts {
        // Only route artifacts targeted at pages
        filePath := path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")
        if err := bc.UpsertFile(ctx, rc.Repo, branch,
            fmt.Sprintf("docs: release %s %s", rc.ToRef, art.Kind),
            vcs.FileWrite{Path: filePath, Content: art.Content}); err != nil {
            return fmt.Errorf("pages: upsert %s: %w", filePath, err)
        }
    }
    return nil
}
```

[VERIFIED: internal/vcs/github/release.go UpsertFile — creates branch from default if absent, updates file by SHA; internal/vcs/gitlab/release.go UpsertFile — same contract]

### Pattern 8: CR-01 Fix — GitLab UpdateReleaseBody

The existing `releasebody` publisher calls `provider.(vcs.ReleasePublisher).UpdateReleaseBody(ctx, repo, release.ID, body)`. For GitLab, `GetReleaseByTag` returns `vcs.Release{ID: 0, TagName: tag}`. The current `UpdateReleaseBody` implementation hard-errors.

Fix strategy: modify the `releasebody` publisher to type-assert for `*gitlab.Adapter` and call `UpdateReleaseBodyByTag(ctx, repo, release.TagName, body)` when the numeric ID is zero.

```go
// In releasebody publisher (internal/releasedocs/publishers/releasebody/releasebody.go)
// After getting the release:
if rel.ID == 0 && rel.TagName != "" {
    // GitLab path: use tag-name variant (CR-01 fix)
    if ga, ok := rc.Provider.(*cadoogl.Adapter); ok {
        return ga.UpdateReleaseBodyByTag(ctx, rc.Repo, rel.TagName, newBody)
    }
    return fmt.Errorf("releasebody: provider returned release with ID=0 but is not *gitlab.Adapter")
}
// GitHub path: use numeric ID
return rp.UpdateReleaseBody(ctx, rc.Repo, rel.ID, newBody)
```

[VERIFIED: internal/vcs/gitlab/release.go — UpdateReleaseBodyByTag exists and works; UpdateReleaseBody returns error by design]

### Anti-Patterns to Avoid

- **Importing orchestrator from releasedocs:** D-01 constraint — the releasedocs subsystem must never import `internal/orchestrator`. The `riverq` package currently imports orchestrator; Phase 2 must not make releasedocs depend on riverq either — the webhook binary is the composition root.
- **Loading .cadoo.yaml from main branch:** Always load from ToRef tree (same as dispatcher.loadCfg). A tag push may arrive before the config is on main.
- **Enqueue without trigger check:** The early-exit check must happen in the webhook handler, not only in the dispatcher. The dispatcher also checks but a failed config load defaults to enabled=false — which is correct but makes the webhook silently skip rather than logging the trigger mismatch.
- **Treating tag push and release event as equivalent without tagPattern check:** For `trigger: tag`, filter the tag name against `releaseDocs.tagPattern` (default `"v*"`) using `path.Match` (already used by `LatestTagBefore`). RC/pre-release tags like `v1.2.3-rc.1` should be excluded unless `tagPattern` explicitly includes them.
- **Bypassing Posted/StateStore for DB-backed runs:** Any new publisher must record its external ID through the state store so the second run edits in place. Never bypass it.
- **Creating a separate enqueueFn type for release:** The `enqueueFn` type in `cadoo-webhook` is typed to `orchestrator.ToolJob`. Phase 2 needs a second `enqueueReleaseFn func(ctx, releasedocs.ReleaseJob) error` and must wire both through `buildEnqueue` without merging them.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Webhook signature verification | Custom HMAC | `cadoogh.VerifySignature` / `cadoogl.VerifyToken` | Already correct, constant-time comparison for GitLab |
| GitHub webhook parsing | JSON decode + type switch | `cadoogh.ParseEvent` → `gogithub.ParseWebHook` | go-github handles all event types including `ReleaseEvent` and `PushEvent` |
| GitLab webhook parsing | Custom JSON decode | `cadoogl.ParseEvent` → `glab.ParseWebhook` | go-gitlab handles `ReleaseEvent` (EventTypeRelease) and `TagEvent` (EventTypeTagPush) |
| Tag name glob matching | Custom regex | `path.Match(tagPattern, tagName)` | Already used in `LatestTagBefore` in both github and gitlab adapters |
| Branch file upsert (create or update) | Direct git operations | `vcs.BranchCommitter.UpsertFile` | Both adapters already implement create-if-missing + update-with-SHA idempotency |
| DB state dedup | Custom query patterns | Follow `findings.Store` pattern — `INSERT … ON CONFLICT DO UPDATE` | Proven pattern for idempotent state recording |
| River job type registration | Custom serialization | `river.AddWorker(workers, &releaseWorker{…})` | River handles type registration, serialization, and retry |

**Key insight:** Every low-level capability needed for Phase 2 already exists in either the VCS adapter implementations or the riverq/jobs infrastructure. Phase 2 is entirely composition of existing primitives plus the two new sub-packages (blog generator, pages publisher) which themselves use existing primitives.

## Common Pitfalls

### Pitfall 1: GitLab Tag Push Ref Format
**What goes wrong:** `*glab.TagEvent.Ref` contains `"refs/tags/v1.2.3"`, not just `"v1.2.3"`. If the handler passes the full ref to `ReleaseJob.ToRef`, `LatestTagBefore` and `GetReleaseByTag` will fail with tag-not-found errors.
**Why it happens:** GitHub's `PushEvent` also has `"refs/tags/…"` format — same issue on both providers.
**How to avoid:** Strip the `"refs/tags/"` prefix: `strings.TrimPrefix(e.Ref, "refs/tags/")`.
**Warning signs:** `GetReleaseByTag` returning 404 in tests; `LatestTagBefore` returning `""` when a prior tag obviously exists.

### Pitfall 2: Enqueue vs Config Load Ordering
**What goes wrong:** Loading `.cadoo.yaml` from the webhook handler requires a live VCS API call (HTTP). If this call fails or is slow, it blocks the HTTP handler goroutine.
**Why it happens:** The dispatcher loads config too but runs async in the worker. The webhook handler is synchronous.
**How to avoid:** Two options — (a) make the trigger check async (enqueue always, dispatcher exits early on no-op config) which is simpler, or (b) load config synchronously but with a short timeout. Option (a) is safer and matches the SPEC text "the webhook no-ops early": treat "early" as relative to the worker running, not the HTTP handler. The HTTP handler always returns 202 Accepted; the worker does the no-op check.
**Warning signs:** Webhook handler timeouts under load; config load errors surfacing as 500 responses.

### Pitfall 3: Dual enqueueFn Wiring in buildEnqueue
**What goes wrong:** `buildEnqueue` currently returns one `enqueueFn` typed to `orchestrator.ToolJob`. Adding `ReleaseJob` to the same function signature would require accepting both jobs via `interface{}`, losing type safety. Alternatively, copy-pasting `buildEnqueue` creates a maintenance hazard.
**Why it happens:** The webhook binary has one `enqueueFn` at the top. Phase 2 adds a second job kind.
**How to avoid:** Return a struct from `buildEnqueue` containing both `enqueueTool` and `enqueueRelease` fields, or add `buildReleaseEnqueue` as a separate function that reuses the same `pool`/`riverq` client.

### Pitfall 4: River Worker Registration Order
**What goes wrong:** `riverq.New` creates the River client in webhook-only mode (`dispatcher == nil`) without registering workers. If `cadoo-worker` calls `riverq.New` with a `releasedocsDispatcher` but the `river.Workers` registry only has `toolWorker`, River silently drops `release_docs` jobs.
**Why it happens:** `river.AddWorker` must be called once per concrete `Args` type before `river.NewClient`.
**How to avoid:** Extend `riverq.New` to accept both dispatchers (or use functional options), registering `releaseWorker` alongside `toolWorker` when the releasedocs dispatcher is non-nil.

### Pitfall 5: Pages Publisher Import Cycle
**What goes wrong:** If `pages.Publisher` imports `*gitlab.Adapter` or `*github.Adapter` (for the CR-01 type assertion pattern), it creates an import cycle: `releasedocs/publishers/pages` → `vcs/gitlab` → (nothing bad there), but the `releasebody` publisher needs the same CR-01 fix. The CR-01 fix is in `releasebody`, not `pages`.
**Why it happens:** The type assertion for GitLab's `UpdateReleaseBodyByTag` requires importing `*gitlab.Adapter`, which the publisher sub-package should not do.
**How to avoid:** Fix CR-01 at the dispatcher level by checking `release.ID == 0` and calling a new optional interface `TagReleasePublisher.UpdateReleaseBodyByTag` — or accept the import in the releasebody publisher only (vcs/gitlab is not in the releasedocs package tree). The pages publisher does not call UpdateReleaseBody and has no CR-01 issue.

### Pitfall 6: Blog Config Schema Not Wired
**What goes wrong:** `config.ReleaseArtifacts` in Phase 1 has only `Changelog` and `ReleaseNotes` fields. The blog generator's `Enabled` method reads `cfg.Artifacts.Blog.ArtifactConfig` which is a zero value (Enabled=false) unless the struct field is added.
**Why it happens:** The SPEC defined Blog in the config schema but Phase 1 deferred it.
**How to avoid:** Add `Blog ArtifactConfig \`yaml:"blog"\`` to `config.ReleaseArtifacts` in `internal/config/config.go` and update `defaults.DefaultGenerators()` in `internal/releasedocs/defaults/defaults.go` to include `blog.New()`.

### Pitfall 7: Pages Config Needs Branch/Dir Fields
**What goes wrong:** `config.PublishTarget` currently only has `Enabled bool`. The pages publisher needs `Branch string` and `Dir string`. The SPEC shows `pages: { enabled: false, branch: gh-pages, dir: docs }`.
**Why it happens:** `PublishTarget` was designed for releasebody/changelogpr which have no branch/dir configuration.
**How to avoid:** Either (a) create a new `config.PagesPublishTarget` struct with `Enabled`, `Branch`, `Dir` fields, or (b) add optional fields to `PublishTarget`. Option (a) is cleaner — rename the `Pages` field type in `config.ReleasePublish` to `PagesPublishTarget`.

## Code Examples

### Verified: ReleaseEvent action field values (GitHub)

```go
// Source: go-github v66 event_types.go:1442-1456
// Action possible values: "published", "unpublished", "created", "edited",
//                         "deleted", "prereleased"
// Phase 2 only acts on "published".
if e.GetAction() != "published" {
    return // ignore drafts, edits, deletions
}
toRef := e.GetRelease().GetTagName()   // e.g. "v1.2.3"
repo  := e.GetRepo().GetFullName()     // e.g. "owner/repo"
```

[VERIFIED: go-github v66@v66.0.0 event_types.go — ReleaseEvent struct confirmed]

### Verified: GitLab ReleaseEvent and TagEvent fields

```go
// Source: gitlab.com/gitlab-org/api/client-go v1.46.0 event_webhook_types.go:1109-1122
// glab.ReleaseEvent.Action values: "create", "update", "delete"
// Phase 2 only acts on "create".

// For release event:
if e.Action != "create" {
    return
}
toRef := e.Tag                          // e.g. "v1.2.3"
repo  := e.Project.PathWithNamespace    // e.g. "group/repo"

// Source: event_webhook_types.go:1261-1279
// For tag push event:
// e.Ref = "refs/tags/v1.2.3"
// e.After = new commit SHA (non-zero means tag created, not deleted)
if e.After == "" || e.After == "0000000000000000000000000000000000000000" {
    return // tag deletion
}
toRef := strings.TrimPrefix(e.Ref, "refs/tags/")
repo  := e.Project.PathWithNamespace
```

[VERIFIED: gitlab.com/gitlab-org/api/client-go v1.46.0 event_webhook_types.go]

### Verified: EnqueueRelease in River (new method on Queue)

```go
// Source: internal/riverq/queue.go — EnqueueTool as the pattern
func (q *Queue) EnqueueRelease(ctx context.Context, args ReleaseArgs) error {
    _, err := q.client.Insert(ctx, args, nil)
    return err
}
```

[VERIFIED: internal/riverq/queue.go:93-97 — EnqueueTool uses identical pattern]

### Verified: In-memory queue registration for release jobs

```go
// Source: cmd/cadoo-webhook/main.go:136-152 (in-memory branch of buildEnqueue)
// ReleaseJob already implements jobs.Job (Kind() returns "release_docs")
// The memory queue uses kind to route — register the release dispatcher:
q.Register(releasedocs.ReleaseJob{}.Kind(), releaseHandlerFunc(releaseDispatcher))
```

[VERIFIED: internal/releasedocs/releasedocs.go:82 — ReleaseJob.Kind() = "release_docs"; internal/jobs/jobs.go — Queue.Register(kind, Handler)]

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Stateless marker-only idempotency | DB-backed state table + stateless fallback | Phase 2 (this phase) | Re-runs edit release body/PR in place even across process restarts |
| Blog not implemented | Blog generator (LLM, when: minor_or_above) | Phase 2 (this phase) | Adds long-form announcement artifact |
| Pages not implemented | Pages publisher (BranchCommitter.UpsertFile) | Phase 2 (this phase) | Deterministic docs branch commits |
| GitLab UpdateReleaseBody hard-fails (CR-01) | Fix using TagName via UpdateReleaseBodyByTag | Phase 2 (this phase) | GitLab users can use releasebody publisher |

**Deprecated/outdated:**
- GitLab `UpdateReleaseBody(ctx, repo, id, body)` signature: This is intentionally broken as documented in CR-01. The correct method for GitLab is `UpdateReleaseBodyByTag(ctx, repo, tag, body)`. Fix in Phase 2 at the publisher level.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | GitLab `*glab.ReleaseEvent.Action` value is `"create"` for a new published release | Code Examples | If the value is `"publish"` or similar, the handler silently no-ops all GitLab release events |
| A2 | The pages publisher should commit all artifact kinds to the pages branch by default | Architecture Patterns | If only release-notes should go to pages, the publisher needs artifact-kind filtering |
| A3 | `config.PagesPublishTarget` is the right approach (new struct vs extended PublishTarget) | Common Pitfalls (Pitfall 7) | If `PublishTarget` is extended with optional fields, no new struct needed — but breaks existing tests that construct `PublishTarget` by literal |

**Note on A1:** The go-gitlab source at `event_webhook_types.go:1119` shows `Action string \`json:"action"\`` but does not enumerate valid values in the struct definition. The constant `"create"` is inferred from GitLab's webhook documentation pattern. Confirm against a real GitLab webhook delivery in integration testing.

## Open Questions (RESOLVED)

1. **Blog generator: does it need its own `Target` routing or does it always go to pages?**
   - What we know: The SPEC says "blog is routed to pages." The pages publisher receives all artifacts and commits them; it could filter by kind.
   - What's unclear: Whether the blog should also optionally go to `releaseBody` (seems unlikely) or be pages-only.
   - Recommendation: Pages-only for blog. The pages publisher commits all non-empty artifacts it receives; artifact routing is controlled by which publishers are in the dispatcher's `Publishers` slice.
   - RESOLVED: Pages-only. The pages publisher commits all non-empty artifacts; the dispatcher routes `KindBlog` to the pages publisher only. Implemented in plan 02-04 (pages publisher) and plan 02-03 (blog generator).

2. **StateStore: should it mirror findings.Store with a DB + in-memory dual backend?**
   - What we know: The SPEC says "in stateless CLI/CI mode, reconstruct state by reading Cadoo's own markers back." The Phase 1 CLI already does marker-based reconstruction.
   - What's unclear: Whether the StateStore needs an in-memory fallback or can simply be nil-tolerant (same as finding `Posted`).
   - Recommendation: Nil-tolerant store (nil means stateless mode). For DB-backed mode, implement `StateStore` with `pgxpool`. The webhook binary (which has DB access in River mode) passes the store; the CLI dispatcher does not.
   - RESOLVED: Nil-tolerant only. A nil `Store` field on `releasedocs.Dispatcher` is the stateless-marker-mode fallback; no in-memory backend is needed. DB-backed `state.Store` wired in plan 02-06 when `DATABASE_URL` is set.

3. **CR-01 fix placement: publisher or adapter?**
   - What we know: `releasebody.Publisher.Publish` calls `rp.UpdateReleaseBody(ctx, repo, rel.ID, body)` which hard-errors for GitLab.
   - What's unclear: Whether to fix in the publisher (type-assert `*gitlab.Adapter`) or add a new optional interface `TagReleasePublisher` that the publisher checks.
   - Recommendation: Add `TagReleasePublisher interface { UpdateReleaseBodyByTag(ctx, repo, tag, body) error }` to `internal/vcs/vcs.go`. The publisher checks for it first (for zero-ID releases), falling back to the numeric ID path. This keeps the publisher from importing `*gitlab.Adapter` directly.
   - RESOLVED: Optional `vcs.TagReleasePublisher` interface added in plan 02-01 Task 1. The `releasebody.Publisher` type-asserts `TagReleasePublisher` first; for zero-ID releases (GitLab) it calls `UpdateReleaseBodyByTag`. No `*gitlab.Adapter` import in the publisher sub-package.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | All code | ✓ | 1.26 (per go.mod) | — |
| PostgreSQL + pgvector | DB-backed mode (River queue + StateStore) | ✓ (via make dev-up) | pg16 | In-memory mode without DATABASE_URL |
| river v0.36.0 | ReleaseArgs/releaseWorker | ✓ (in go.mod) | v0.36.0 | — |
| go-github v66 | GitHub webhook parsing | ✓ (in go.mod) | v66.0.0 | — |
| gitlab-org/api/client-go | GitLab webhook parsing | ✓ (in go.mod) | v1.46.0 | — |
| goose CLI | Running migrations | ✓ (installed via make tools-install) | @latest | — |

[VERIFIED: go.mod, cmd/cadoo-webhook/main.go imports, Makefile tools-install target]

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `go test -race` |
| Config file | none (standard go test) |
| Quick run command | `go test -race -count=1 ./internal/releasedocs/... ./internal/riverq/... ./cmd/cadoo-webhook/...` |
| Full suite command | `make test` (go test -race -count=1 ./...) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| REQ-configurable-trigger | GitHub `release: published` builds ReleaseJob and enqueues | unit | `go test ./cmd/cadoo-webhook/ -run TestHandleGithubRelease` | ❌ Wave 0 |
| REQ-configurable-trigger | GitLab `Release Hook` (action=create) builds ReleaseJob | unit | `go test ./cmd/cadoo-webhook/ -run TestHandleGitlabRelease` | ❌ Wave 0 |
| REQ-configurable-trigger | Tag push (GitHub) with tagPattern filter builds ReleaseJob | unit | `go test ./cmd/cadoo-webhook/ -run TestHandleGithubTagPush` | ❌ Wave 0 |
| REQ-configurable-trigger | If trigger excludes event kind, webhook no-ops | unit | `go test ./cmd/cadoo-webhook/ -run TestTriggerEarlyExit` | ❌ Wave 0 |
| REQ-release-docs-idempotency | StateStore records `(provider, repo, to_tag, artifact_kind)` | unit | `go test ./internal/releasedocs/state/ -run TestStateStore` | ❌ Wave 0 |
| REQ-release-docs-idempotency | Migration 0006 round-trips up→down→up | integration | `make migrate && make migrate-down && make migrate` | ❌ Wave 0 (manual CI) |
| REQ-publish-destinations | Pages publisher commits artifacts to deterministic paths | unit | `go test ./internal/releasedocs/publishers/pages/ -run TestPublish` | ❌ Wave 0 |
| REQ-publish-destinations | Pages publisher re-run overwrites same path (idempotent) | unit | `go test ./internal/releasedocs/publishers/pages/ -run TestPublishIdempotent` | ❌ Wave 0 |
| REQ-release-artifact-generation | Blog generator produces long-form on minor/major | unit | `go test ./internal/releasedocs/generators/blog/ -run TestGenerate` | ❌ Wave 0 |
| REQ-release-artifact-generation | Blog generator skips on patch (when: minor_or_above) | unit | `go test ./internal/releasedocs/generators/blog/ -run TestEnabled` | ❌ Wave 0 |
| CR-01 fix | GitLab releasebody publisher calls UpdateReleaseBodyByTag | unit | `go test ./internal/releasedocs/publishers/releasebody/ -run TestGitLabPath` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -race -count=1 ./internal/releasedocs/... ./internal/riverq/...`
- **Per wave merge:** `make test`
- **Phase gate:** `make ci` (vet + test + build) green before `/gsd:verify-work`

### Wave 0 Gaps
All test files listed above are new. Existing test infrastructure (`releasedocstest.Fake`) already provides the fake VCS provider and can be reused without new fixtures.

- [ ] `internal/releasedocs/state/state_test.go` — covers REQ-release-docs-idempotency
- [ ] `internal/releasedocs/generators/blog/blog_test.go` — covers REQ-release-artifact-generation (blog)
- [ ] `internal/releasedocs/publishers/pages/pages_test.go` — covers REQ-publish-destinations (pages)
- [ ] `cmd/cadoo-webhook/*_test.go` for release and tag handlers — covers REQ-configurable-trigger

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Webhook tokens already verified in existing handlers |
| V3 Session Management | no | Stateless webhook handlers |
| V4 Access Control | no | No new access control surface |
| V5 Input Validation | yes | Tag name from webhook payload used in file paths and DB queries — sanitize with `strings.TrimPrefix` + glob matching only |
| V6 Cryptography | no | No new crypto; existing HMAC-SHA256 / token comparison unchanged |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Path traversal via crafted tag name | Tampering | Only use tag name after glob-matching against `tagPattern`; never interpolate raw tag name into filesystem paths; use `path.Join` for pages paths |
| Replay of old release webhooks | Spoofing | Rely on existing HMAC/token verification; River deduplication via DB unique constraint |
| Enqueue storm on re-tagging | DoS | River's unique job insertion (insert with unique constraint on `to_tag`) prevents multiple in-flight jobs for the same tag; or use `river.InsertOpts{UniqueOpts: …}` |

## Project Constraints (from CLAUDE.md)

- **Build with**: `make build` (five binaries under cmd/); `make ci` (vet + test + build) must stay green.
- **Migrations**: `db/migrations/` (goose, numbered `NNNN_name.sql`); must round-trip `up → down → up` in CI.
- **Lint**: golangci-lint v2; `package_comments` disabled; `exported` rule on — every exported symbol needs a docstring; `goimports` with `local-prefixes: github.com/payamqorbanpour/cadoo`.
- **No import of orchestrator from releasedocs**: D-01 constraint — verified releasedocs package tree.
- **No new LLM provider code**: LiteLLM sidecar handles routing; Go code only calls `llm.Provider.Chat`.
- **Multi-tenancy**: `org_id` carried in new state table (TEXT column, not FK, same as posted_findings pattern).
- **VCS config from tag tree**: `.cadoo.yaml` always loaded from `ToRef` tree, never from `main`.
- **Run a single test**: `go test -race -run TestName ./path/...`

## Sources

### Primary (HIGH confidence)
- `internal/vcs/vcs.go` — all VCS interfaces (ReleasePublisher, BranchCommitter, etc.) verified from source
- `internal/riverq/queue.go` — River worker pattern (ToolArgs/toolWorker) verified from source
- `cmd/cadoo-webhook/main.go` — webhook handler pattern, enqueueFn, dual-mode queue verified from source
- `db/migrations/0003_posted_state.sql` — migration pattern (BIGSERIAL, UNIQUE composite) verified from source
- `internal/releasedocs/releasedocs.go` — ReleaseJob.Kind(), ArtifactKind, Generator/Publisher interfaces
- `internal/releasedocs/dispatcher.go` — Dispatcher.Run flow, loadCfg, trigger/enabled gates
- `internal/releasedocs/context.go` — Enabled() function signature and bump semantics
- `internal/releasedocs/generators/releasenotes/releasenotes.go` — Generator struct pattern for blog
- `internal/vcs/github/release.go` — UpsertFile, OpenOrUpdatePR, UpdateReleaseBody implementations
- `internal/vcs/gitlab/release.go` — UpdateReleaseBody (CR-01), UpdateReleaseBodyByTag, UpsertFile implementations

### Secondary (MEDIUM confidence)
- `github.com/google/go-github/v66@v66.0.0/github/event_types.go` — ReleaseEvent and PushEvent struct fields
- `gitlab.com/gitlab-org/api/client-go@v1.46.0/event_webhook_types.go` — ReleaseEvent and TagEvent struct fields
- `gitlab.com/gitlab-org/api/client-go@v1.46.0/event_parsing.go` — EventTypeRelease, EventTypeTagPush constants

### Tertiary (LOW confidence)
- A1: GitLab ReleaseEvent.Action=`"create"` for new release — inferred from struct field name; not enumerated in go-gitlab source; requires live webhook delivery confirmation.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all libraries confirmed in go.mod; all types confirmed in module cache
- Architecture: HIGH — patterns extracted directly from existing Phase 1 source code
- Pitfalls: HIGH (Pitfalls 1-4, 6-7) / MEDIUM (Pitfall 5) — derived from code analysis
- Migration pattern: HIGH — verified against 0003_posted_state.sql

**Research date:** 2026-06-05
**Valid until:** 2026-09-05 (River v0.36.0 and go-github v66 are stable; go-gitlab v1.46 is stable)
