---
phase: 02-webhook-auto-trigger-state
reviewed: 2026-06-05T00:00:00Z
depth: standard
files_reviewed: 21
files_reviewed_list:
  - cmd/cadoo-webhook/main.go
  - cmd/cadoo-webhook/release.go
  - cmd/cadoo-webhook/release_test.go
  - cmd/cadoo-worker/main.go
  - db/migrations/0006_release_docs_state.sql
  - internal/config/config.go
  - internal/releasedocs/defaults/defaults.go
  - internal/releasedocs/dispatcher.go
  - internal/releasedocs/dispatcher_test.go
  - internal/releasedocs/generators/blog/blog.go
  - internal/releasedocs/generators/blog/blog_test.go
  - internal/releasedocs/publishers/pages/pages.go
  - internal/releasedocs/publishers/pages/pages_test.go
  - internal/releasedocs/publishers/releasebody/releasebody.go
  - internal/releasedocs/publishers/releasebody/releasebody_test.go
  - internal/releasedocs/releasedocs.go
  - internal/releasedocs/state/state.go
  - internal/releasedocs/state/state_test.go
  - internal/riverq/queue.go
  - internal/vcs/github/release.go
  - internal/vcs/gitlab/release.go
  - internal/vcs/vcs.go
findings:
  critical: 3
  warning: 5
  info: 3
  total: 11
status: fixed
---

# Phase 02: Code Review Report

**Reviewed:** 2026-06-05
**Depth:** standard
**Files Reviewed:** 21
**Status:** issues_found

## Summary

This phase implements the release-docs subsystem: webhook routing for GitHub/GitLab release and tag-push events, a `releasedocs.Dispatcher` pipeline (config load → context build → generators → publishers), a Postgres-backed `state.Store`, a `blog.Generator`, a `pages.Publisher`, and a `releasebody.Publisher`. The River queue is extended with `ReleaseArgs` / `releaseWorker`.

The overall structure is solid — the interface boundaries are clean, idempotency contracts are respected by the publishers, and the test suite is wide. Three critical defects require fixes before shipping: a misleading (and incorrect) path-traversal "protection" in the pages publisher, the `Org` field being silently dropped on every webhook-triggered release job, and a first-release failure where `BuildContext` calls `ListCommits("", toRef)` with an empty `fromRef` that the GitHub and GitLab APIs will reject.

---

## Critical Issues

### CR-01: `path.Join` does NOT neutralize path traversal in the pages publisher

**File:** `internal/releasedocs/publishers/pages/pages.go:78`

**Issue:** The package-level comment and the inline comment both claim that `path.Join` "neutralizes path-traversal attacks on adversarial tag names (T-02-07)". This is factually wrong. `path.Join` cleans the result but does **not** prevent escape from the base directory:

```
path.Join("docs", "releases", "../../../etc/passwd", "changelog.md")
// → "../etc/passwd/changelog.md"   (escapes "docs")
path.Join("docs", "releases", "../../malicious", "changelog.md")
// → "malicious/changelog.md"        (escapes "docs")
```

Any tag name containing `..` segments (e.g. `../../../etc/shadow`) produces a path that escapes the intended `{dir}/releases/` prefix. That path is passed directly to `bc.UpsertFile`, which calls a live VCS API with the cleaned-but-escaped path. Both GitHub's `CreateFile`/`UpdateFile` and GitLab's `CreateFile` accept arbitrary relative paths; the exact behavior depends on provider, but the assumption that the publisher is safe is provably false.

The tag name arrives from the VCS webhook (signature-verified) so exploitation requires a repository that has created a tag containing `..`. VCS providers typically allow `/` in tag names (e.g. `releases/v1.0.0`), and `..` traversal within those is at minimum unvalidated.

**Fix:** After computing `p`, verify the result stays inside the intended base and reject adversarial inputs:

```go
p := path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")

// Guard: path.Join cleans ".." but does not prevent escape from the base prefix.
// Reject any path that does not start with the expected dir prefix.
expectedPrefix := dir + "/"
if !strings.HasPrefix(p, expectedPrefix) {
    slog.Warn("pages: computed path escapes base dir; skipping artifact",
        "path", p, "dir", dir, "toRef", rc.ToRef, "kind", art.Kind)
    continue
}
```

Alternatively, sanitize `rc.ToRef` by replacing or rejecting any tag that contains `..` or a leading `/` before it reaches the publisher.

---

### CR-02: `Org` field is never set in webhook-triggered `ReleaseJob`s — multi-tenancy org always NULL

**File:** `cmd/cadoo-webhook/release.go:56,116,154,208`

**Issue:** All four `releasedocs.ReleaseJob` constructions in the webhook release handlers omit the `Org` field entirely:

```go
job := releasedocs.ReleaseJob{
    Provider: vcs.KindGitHub,
    Repo:     repo,
    ToRef:    toRef,
    // Org: ??? — never set
}
```

`job.Org` is zero-valued (`""`). This empty string propagates through `enqueueRelease` → `riverq.ReleaseArgs{Org: job.Org}` → `releaseWorker.Work` → `dispatcher.Run` → `d.Store.Record(ctx, job.Org, ...)`, writing `org_id = NULL` into `release_docs_state` for every webhook-triggered run. The `Lookup` query does not filter by `org_id`, so cross-tenant contamination via `Lookup` is prevented, but `NULL` org_id violates the multi-tenancy schema intent documented in the migration comment ("Cadoo org for multi-tenancy") and means the audit trail is incomplete.

More practically: if a SaaS deployment has two tenants whose repos share the same `(provider, repo_full_name, to_tag, artifact_kind)` composite key (unlikely but theoretically possible for GHES), the UNIQUE constraint would collide across tenants.

**Fix:** Resolve the installation's `org_id` from the webhook installation ID before constructing the job. The installation ID is available in the GitHub `ReleaseEvent` / `PushEvent` via `e.GetInstallation().GetID()` and in the GitLab event via the webhook configuration. At minimum, add a `TODO` and document that `Org` will be populated once installation→org resolution is wired (the same gap exists for `ToolJob.InstallID` on the PR path). For now, the field is effectively unfilled:

```go
job := releasedocs.ReleaseJob{
    Provider: vcs.KindGitHub,
    Repo:     repo,
    Org:      resolveOrg(e.GetInstallation().GetID()), // or "" with a TODO
    ToRef:    toRef,
}
```

---

### CR-03: `BuildContext` calls `ListCommits("", toRef)` on first release — both VCS APIs will error

**File:** `internal/releasedocs/context.go` (called from `internal/releasedocs/dispatcher.go:96`)

**Issue:** When `job.FromRef` is empty and `LatestTagBefore` returns `""` (no prior matching tag — the first-release scenario), the code proceeds with `fromRef = ""` and then calls:

```go
commits, err := rr.ListCommits(ctx, job.Repo, "", job.ToRef)
```

The GitHub adapter passes this as the `base` argument to `Repositories.CompareCommits("", "v1.0.0")`. The GitHub Repositories Compare API requires both refs to be valid git refs; an empty string is not valid and produces a `422 Unprocessable Entity`. The GitLab adapter passes it as `From: ptr("")` to `Repositories.Compare`, which similarly rejects an empty base.

The `slog.Warn` log message ("proceeding with empty fromRef (first-release)") implies this is a supported codepath, but `ListCommits` will always fail immediately after, propagating an error back to `dispatcher.Run` and failing the entire job.

**Fix:** When `fromRef` resolves to `""` after the `LatestTagBefore` attempt, skip the commit/PR listing and construct an empty `ReleaseContext`:

```go
if fromRef == "" {
    // First release: no prior tag exists. Return a context with empty
    // commits/PRs so generators produce a minimal changelog.
    return ReleaseContext{
        Repo:    job.Repo,
        Org:     job.Org,
        FromRef: "",
        ToRef:   job.ToRef,
        Bump:    BumpMajor, // first release is always a major bump
        Config:  cfg,
        Provider: provider,
        LLM:     llmProvider,
        Model:   model,
    }, nil
}
```

---

## Warnings

### WR-01: GitLab `UpsertFile` hardcodes `start_branch: "main"` — fails for repos with a different default branch

**File:** `internal/vcs/gitlab/release.go:230`

**Issue:** When creating a new file on a branch that does not yet exist, the GitLab `UpsertFile` method always passes `StartBranch: ptr("main")`. GitLab uses `start_branch` as the source commit when creating a new branch. If the repository's default branch is not `main` (e.g., `master`, `develop`, `trunk`), the newly created branch would be based on `main` — which may not exist — causing the API to return a `404` or `400` error, or silently basing the docs branch on the wrong commit.

The GitHub implementation correctly resolves the default branch dynamically:
```go
repoData, _, err := a.client.Repositories.Get(ctx, owner, name)
defBranch := repoData.GetDefaultBranch()
```

**Fix:** Resolve the default branch via `a.client.Projects.GetProject(repo, nil, ...)` before creating the file:

```go
proj, _, err := a.client.Projects.GetProject(repo, nil, glab.WithContext(ctx))
if err != nil {
    return fmt.Errorf("get project %s: %w", repo, err)
}
startBranch := "main"
if proj != nil && proj.DefaultBranch != "" {
    startBranch = proj.DefaultBranch
}
```

---

### WR-02: `handleGitlabTagPush` missing `refs/tags/` prefix guard (present in GitHub equivalent)

**File:** `cmd/cadoo-webhook/release.go:189`

**Issue:** The GitHub `handleGithubTagPush` function guards against non-tag refs at line 86:

```go
if !strings.HasPrefix(ref, "refs/tags/") {
    return
}
```

The GitLab `handleGitlabTagPush` function does not have this guard. `TrimPrefix` is called unconditionally:

```go
tagName := strings.TrimPrefix(e.Ref, "refs/tags/")
```

If `e.Ref` is `"refs/heads/main"` or any non-tag ref, `TrimPrefix` returns the string unchanged (e.g. `"refs/heads/main"`), and the job is enqueued with `ToRef: "refs/heads/main"` — which then fails at the dispatcher level rather than being silently discarded at the webhook edge.

While the GitLab `TagEvent` type is only sent for tag operations in practice, defensive coding requires the guard. The test suite includes `TestHandleGithubTagPush_NonTagRef` for GitHub but has no equivalent GitLab test.

**Fix:**
```go
// handleGitlabTagPush — add after the zeroSHA check:
if !strings.HasPrefix(e.Ref, "refs/tags/") {
    slog.Debug("releasedocs: gitlab tag push ref is not a tag ref; skipping",
        "ref", e.Ref)
    return
}
tagName := strings.TrimPrefix(e.Ref, "refs/tags/")
```

Add a corresponding test `TestHandleGitlabTagPush_NonTagRef`.

---

### WR-03: `blog.Generator.Generate` silently discards LLM errors without logging

**File:** `internal/releasedocs/generators/blog/blog.go:65-71`

**Issue:** When `narrateWithLLM` returns an error, the blog generator falls back to the skeleton without logging the failure:

```go
narrative, err := narrateWithLLM(ctx, rc.LLM, rc.Model, skeleton)
if err != nil {
    // Narrative failure is non-fatal: fall back to skeleton (D-10).
    return releasedocs.Artifact{
        Kind:    releasedocs.KindBlog,
        Content: []byte(skeleton),
    }, nil
}
```

The same pattern exists in `releasenotes.Generator`. Operators have no visibility into LLM failures. A misconfigured LiteLLM proxy or quota exhaustion would silently produce skeleton-only output for every release, with no signal in logs.

**Fix:** Add a `slog.Warn` before the fallback return:

```go
if err != nil {
    slog.Warn("blog: LLM narrative failed; falling back to skeleton",
        "repo", rc.Repo, "toRef", rc.ToRef, "err", err)
    return releasedocs.Artifact{
        Kind:    releasedocs.KindBlog,
        Content: []byte(skeleton),
    }, nil
}
```

Apply the same fix to `releasenotes.Generator`.

---

### WR-04: `release_docs_state` UNIQUE constraint does not include `org_id` — cross-tenant collision possible on SaaS

**File:** `db/migrations/0006_release_docs_state.sql:18`

**Issue:** The uniqueness constraint is:

```sql
UNIQUE (provider, repo_full_name, to_tag, artifact_kind)
```

`org_id` is excluded. On a SaaS deployment where two tenants both connect the same GitHub repository (via different GitHub App installations on forked or mirrored repos), the second tenant's publish would silently update the first tenant's record rather than creating a separate one. The `posted_findings` pattern this migration explicitly follows uses the same org-exclusive UNIQUE key (per the comment), but `posted_findings` is protected from this collision because each org has its own installation/credentials that produce distinct `(provider, repo, pr)` tuples. For release docs, `(provider, repo_full_name, to_tag)` is potentially shared across tenants.

**Fix:**

```sql
UNIQUE (org_id, provider, repo_full_name, to_tag, artifact_kind)
```

Note: this also requires `org_id` to be `NOT NULL` or a sentinel value, which ties back to CR-02. Until `Org` is properly populated, adding `org_id` to the UNIQUE key with a `NULL` default will cause `NULL != NULL` semantics and the ON CONFLICT clause will never fire (PostgreSQL treats NULLs as distinct in UNIQUE indexes). Address CR-02 first, then migrate the constraint.

---

### WR-05: Duplicate `buildDispatcher` and `buildTrackers` implementations between `cadoo-webhook` and `cadoo-worker`

**File:** `cmd/cadoo-webhook/main.go:183-265`, `cmd/cadoo-worker/main.go:156-291`

**Issue:** Both binaries contain an identical `buildDispatcher` function (aside from a single local variable name difference: `pool2` vs `vcspool`) and an identical `buildTrackers` function. Any future change to how the `orchestrator.Dispatcher` is assembled (new field, new dependency) must be applied in two places. Drift has already happened: `cadoo-worker` uses the variable name `vcspool` (more descriptive) while `cadoo-webhook` uses `pool2` (less descriptive).

**Fix:** Extract into a shared package (e.g. `internal/dispatcherbuild`) or a shared library function. The `cadoo-webhook` path doesn't need `buildReleaseDispatcher` (that's worker-only), but the `buildDispatcher` / `buildTrackers` pair is identical and should be DRY'd.

---

## Info

### IN-01: `path.Match` is used for tag-pattern filtering but silently accepts patterns with `**`

**File:** `cmd/cadoo-webhook/release.go:105,197`, `internal/vcs/github/release.go:136`, `internal/vcs/gitlab/release.go:157`

**Issue:** `path.Match` from the Go standard library does not support `**` (double-star) globbing. A user who configures `tagPattern: "v**"` in `.cadoo.yaml` expecting recursive matching gets `path.ErrBadPattern` and the tag is silently skipped. The error is logged as a warning at the VCS adapter level, but not in the webhook handlers (webhook handlers return early with a `slog.Warn`). The documentation in `config.go` uses examples like `"v*"` without noting the limitation.

**Fix:** Document the `path.Match` limitation in the `TagPattern` field comment in `config.go`. Optionally validate `tagPattern` values at config-parse time and return a descriptive error.

---

### IN-02: `GitLab.UpdateReleaseBody` satisfies `vcs.ReleasePublisher` but always returns an error

**File:** `internal/vcs/gitlab/release.go:196-199`

**Issue:** The GitLab adapter satisfies `vcs.ReleasePublisher` (enforced by a compile-time assertion) but its `UpdateReleaseBody` implementation always returns an error directing callers to use `UpdateReleaseBodyByTag`. This is an unusual and potentially dangerous interface contract: the interface method is callable but always fails. If any future caller invokes `UpdateReleaseBody` on a GitLab adapter directly (e.g., bypassing the `rel.ID == 0` CR-01 check), they receive a runtime error with no compile-time indication.

**Fix:** Consider removing the `vcs.ReleasePublisher` compile-time assertion for `*gitlab.Adapter` and keeping only `vcs.TagReleasePublisher`. Update the `releasebody.Publisher` to use `vcs.TagReleasePublisher` as the primary type-assertion for GitLab. This makes the capability interface accurately reflect what the adapter actually supports.

---

### IN-03: Magic constant `"release_docs"` duplicated between `releasedocs.ReleaseJob.Kind()` and `riverq.ReleaseArgs.Kind()`

**File:** `internal/releasedocs/releasedocs.go:89`, `internal/riverq/queue.go:72`

**Issue:** Both `ReleaseJob.Kind()` and `ReleaseArgs.Kind()` return `"release_docs"`. These must stay in sync for River job routing to work. They are currently in sync, but there is no compile-time or test assertion enforcing this. If one is changed (e.g., a refactor renames the job type) the other will silently diverge, causing River to stop routing release jobs.

**Fix:** Add a test:
```go
// In internal/riverq/queue_test.go or similar:
func TestReleaseJobKindConsistency(t *testing.T) {
    if releasedocs.ReleaseJob{}.Kind() != riverq.ReleaseArgs{}.Kind() {
        t.Errorf("ReleaseJob.Kind() %q != ReleaseArgs.Kind() %q — river routing will break",
            releasedocs.ReleaseJob{}.Kind(), riverq.ReleaseArgs{}.Kind())
    }
}
```

---

_Reviewed: 2026-06-05_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
