# Release Docs — Design Spec

**Date:** 2026-06-04
**Status:** Approved design, ready for implementation planning
**Topic:** Post-release documentation generation & publishing

## 1. Summary

Add a **product feature** to Cadoo that, after a customer cuts a release, automatically
generates and publishes a set of release artifacts for *their* repository:

- **Changelog** — structured, grouped (Features/Fixes/Breaking/…) section appended to a rolling `CHANGELOG.md`.
- **Release notes** — polished human-readable narrative for the GitHub/GitLab Release body.
- **PR blog post** — long-form announcement highlighting the release's headline changes.
- **API docs + OpenAPI** — human-readable API reference and an OpenAPI spec derived from code (later phase).

It is multi-tenant (`org_id` carried throughout), self-host and SaaS share the same code path,
and it reuses the existing LLM gateway (LiteLLM), VCS adapters, and dual-mode job queue
(River + in-memory). It mirrors the architecture of the review pipeline rather than extending it.

### Goals

- Auto-generate the artifacts above on each release, with **per-artifact toggles and conditions**.
- **Configurable templates/conventions**: presets out of the box, custom override template files for full control.
- **Idempotent** across re-runs/resyncs (re-tagging, edited release): edit-in-place, no duplicates.
- Trigger configurable per repo: **published Release event by default**, optionally tag push, plus a manual CLI/CI entry point.
- Publish to: **Release body**, **`CHANGELOG.md` via PR**, **docs branch / GitHub Pages**.

### Non-goals

- No new LLM provider code paths (routing stays in LiteLLM).
- Not extending `tools.Tool` / `tools.Input` / `tools.Result` (those are PR-diff/inline-comment shaped — wrong fit).
- No single-tenant shortcuts.
- API docs for *arbitrary* languages is explicitly phase 3 and may start with a narrow language/framework set.

## 2. Architecture

A dedicated subsystem `internal/releasedocs`, parallel to `internal/orchestrator` (the review pipeline),
not built on top of it. Three core abstractions plus an orchestrator:

```
internal/releasedocs/
  releasedocs.go     // ReleaseJob, Artifact, Generator, Publisher interfaces + types
  dispatcher.go      // Dispatcher.Run(ctx, ReleaseJob) — the single entry point
  registry.go        // wires built-in generators + publishers
  context.go         // builds ReleaseContext (range → PRs/commits) from a provider
  generators/
    changelog/
    releasenotes/
    blog/
    apidocs/         // phase 3
  publishers/
    releasebody/
    changelogpr/
    pages/
  template/          // embedded presets + repo override loading
```

### 2.1 Core types & interfaces

```go
// ReleaseJob is what the webhook/worker/CLI enqueue or pass directly.
type ReleaseJob struct {
    OrgID     string
    Provider  vcs.Kind
    InstallID string
    Repo      string   // full name
    FromRef   string   // previous release tag ("" => auto-detect prior tag)
    ToRef     string   // the release tag being documented
    ReleaseID string   // provider release id, if triggered by a Release event ("" for tag/manual)
    Trigger   string   // release | tag | manual
}

// ReleaseContext is the packed input every generator receives. Built once.
type ReleaseContext struct {
    Repo       string
    OrgID      string
    FromTag    string
    ToTag      string
    Bump       SemverBump        // major | minor | patch | none
    Commits    []vcs.Commit
    MergedPRs  []vcs.MergedPR    // PRs merged in the range (number, title, labels, author, body)
    Config     config.ReleaseDocs
    Provider   vcs.Provider
    LLM        llm.Provider      // nil-tolerant; deterministic generators may not need it
}

// Artifact is a generated document plus routing metadata.
type Artifact struct {
    Kind    ArtifactKind   // changelog | release-notes | blog | api-docs | openapi
    Title   string
    Body    string         // rendered markdown/HTML/YAML
    Targets []PublishTarget // which publishers should handle it
    Meta    map[string]string
}

type Generator interface {
    Kind() ArtifactKind
    // Enabled decides from config + bump whether this generator runs at all.
    Enabled(cfg config.ReleaseDocs, bump SemverBump) bool
    Generate(ctx context.Context, rc *ReleaseContext) (*Artifact, error)
}

type Publisher interface {
    Target() PublishTarget
    Publish(ctx context.Context, rc *ReleaseContext, arts []Artifact) error
}
```

### 2.2 Dispatcher flow

`Dispatcher.Run(ctx, ReleaseJob)`:

1. Resolve `vcs.Provider` from a `VCSPool` (reuse the orchestrator's pool).
2. Resolve `FromRef` if empty: find the prior tag matching `tagPattern` before `ToRef`.
3. Load `.cadoo.yaml` from **`ToRef`'s tree** (release tag), not `main`. Parse `releaseDocs`. If `enabled:false`, no-op.
4. Build `ReleaseContext`: list commits + merged PRs between `FromRef..ToRef`, compute `Bump` from the two semver tags.
5. For each registered generator where `Enabled(cfg, bump)` is true, run `Generate` (parallelizable). Collect `Artifact`s.
6. Route each artifact to its configured publishers; each publisher runs idempotently.
7. Record published state (see §5) so re-runs edit in place.

Generators are independent and side-effect-free (pure: context → artifact). Publishers own all writes.

## 3. Config (`.cadoo.yaml` → `releaseDocs`)

Loaded from the release tag's tree. Added as `config.ReleaseDocs` in `internal/config`.

```yaml
releaseDocs:
  enabled: true
  trigger: release            # release | tag | manual  (default: release)
  tagPattern: "v*"            # used when trigger includes tag pushes / prior-tag detection

  artifacts:
    changelog:
      enabled: true
      preset: keep-a-changelog # keep-a-changelog | conventional | custom
      file: CHANGELOG.md
      template: .cadoo/templates/changelog.md.tmpl   # optional override
    releaseNotes:
      enabled: true
      when: always             # always | minor-major | major
      tone: concise            # concise | detailed | marketing
      template: .cadoo/templates/release-notes.md.tmpl
    blog:
      enabled: false           # opt-in
      when: minor-major
      template: .cadoo/templates/blog.md.tmpl
    apiDocs:
      enabled: false           # phase 3
      openapi: { source: detect, output: docs/openapi.yaml }

  grouping:
    source: conventional       # conventional | labels | llm
    sections: [Features, Fixes, Performance, Breaking, Docs, Other]

  publish:
    releaseBody: true
    changelogPR: true
    pages: { enabled: false, branch: gh-pages, dir: docs }
```

**Per-artifact control** (explicit user requirement): every artifact has its own `enabled` plus a
`when:` condition keyed off the computed semver bump, so e.g. changelog runs every release but blog
only on minor/major. `Generator.Enabled` implements this; the dispatcher never runs a disabled generator.

**Two configurability layers** (approved):
- **Presets** — `preset:`, `grouping.source`, `tone:` give good out-of-the-box behavior with no template authoring.
- **Custom templates** — any artifact may set `template:` pointing at a Go `text/template` file in the repo
  (loaded from the tag tree). Overrides the preset entirely. Templates receive the `ReleaseContext` plus the
  grouped change model. Defaults to embedded preset templates in `internal/releasedocs/template`.

## 4. Generators (built-ins)

All change-derived generators share one **grouped change model** built once from `ReleaseContext`
according to `grouping.source`:
- `conventional` — parse Conventional Commit prefixes (`feat:`, `fix:`, `perf:`, `feat!:`/`BREAKING CHANGE`).
- `labels` — group merged PRs by label (`type:feature`, `bug`, …) with a configurable label→section map.
- `llm` — classify each PR/commit via the LLM into the configured `sections`.

| Generator | Determinism | LLM use | Output |
| --- | --- | --- | --- |
| **changelog** | Deterministic skeleton from grouped model | Optional polish of entry wording | `CHANGELOG.md` section (markdown) |
| **release-notes** | Skeleton from grouped model | LLM writes the narrative summary + highlights, `tone`-aware | Release-body markdown |
| **blog** | — | LLM-authored long-form from highlights | Blog markdown |
| **api-docs / openapi** (phase 3) | Deterministic extraction | Optional prose | OpenAPI YAML + rendered reference |

The changelog generator is deliberately **deterministic-first** (parse → group → render) with LLM only
polishing wording, so a repo can run it with `tone`/LLM off and get reproducible output. Release-notes and
blog are LLM-authored on top of the deterministic highlight list.

## 5. Publishers & idempotency

Everything is idempotent across resyncs (re-tag, edited release, re-run), mirroring the review pipeline's
marker + stored-state approach.

- **releasebody** — `UpsertRelease`/`UpdateReleaseBody`. Wraps Cadoo content in markers
  (`<!-- cadoo:release-notes:begin -->` … `:end`), preserving any user-written body outside the block.
  Edits in place on re-run.
- **changelogpr** — opens or updates a single PR that prepends the new section to `CHANGELOG.md`.
  Idempotent by a hidden marker keyed on `ToRef` (`<!-- cadoo:changelog:vX.Y.Z -->`): re-runs update the
  same PR/branch instead of stacking new ones. Branch name deterministic, e.g. `cadoo/changelog/vX.Y.Z`.
- **pages** — commits rendered artifacts (blog, API docs, OpenAPI) to the configured `branch`/`dir`.
  Deterministic file paths (e.g. `docs/releases/vX.Y.Z/…`); re-runs overwrite the same paths.

**State.** Reuse the existing pattern: a small store (new table, migration `NNNN_release_docs.sql`,
keyed `(provider, repo, to_tag, artifact_kind)`) records the published release-body id, changelog PR
number, and pages commit, so the dispatcher can edit-in-place when DB-backed. In **stateless CLI/CI mode**,
reconstruct state by reading Cadoo's own markers back from the release body / open PRs (same philosophy as
`PriorReviewReader` in CI-mode review).

## 6. VCS provider extension

`vcs.Provider` is PR-centric and lacks release/tag/range/commit operations. Add **optional capability
interfaces** (type-asserted by the dispatcher, same pattern as `PriorReviewReader`) rather than bloating
the core `Provider`:

```go
// ReleaseRangeReader — read the change set for a release range.
type ReleaseRangeReader interface {
    ResolvePriorTag(ctx context.Context, repo, toTag, pattern string) (string, error)
    ListCommits(ctx context.Context, repo, fromRef, toRef string) ([]Commit, error)
    ListMergedPRs(ctx context.Context, repo, fromRef, toRef string) ([]MergedPR, error)
}

// ReleasePublisher — write release-body + read prior Cadoo markers on a release.
type ReleasePublisher interface {
    GetRelease(ctx context.Context, repo, tagOrID string) (*Release, error)
    UpdateReleaseBody(ctx context.Context, repo, releaseID, body string) error
}

// BranchCommitter — commit files to a branch (changelog PR, pages).
type BranchCommitter interface {
    UpsertBranchFiles(ctx context.Context, repo, branch, base string, files []FileWrite, msg string) (commitSHA string, err error)
    OpenOrUpdatePR(ctx context.Context, repo, head, base, title, body string) (number int64, err error)
}
```

Implemented in `internal/vcs/github` (go-github v66) and `internal/vcs/gitlab` (go-gitlab). A provider that
doesn't implement a capability degrades gracefully (that publisher/generator is skipped with a logged reason).

## 7. Trigger & ingestion

- **Default `release`** — `cadoo-webhook` handles GitHub `release: published` / GitLab release webhooks,
  verifies signature, builds a `ReleaseJob`, and enqueues it (River when `DATABASE_URL` set; in-memory
  sibling goroutine otherwise) for `cadoo-worker`. Same dual-mode plumbing as `orchestrator.ToolJob`.
- **`tag`** — when configured, a `v*` tag push (or release of a tag) is treated as a release; `tagPattern` filters noise (RC tags).
- **manual / CLI** — `cadoo release-docs --pr-host … --repo … --from vX --to vY` (and a `--mr`-style form),
  stateless, runs the same dispatcher with a memory provider pool. This is the **phase-1 entry point** and the dogfooding path on Cadoo's own repo.

The webhook reads the trigger config from the tag tree's `.cadoo.yaml`; if `releaseDocs.trigger` doesn't
include the event kind, it no-ops early.

## 8. Phasing (delivery order)

**Phase 1 — Generators + publishers + CLI (no webhook).**
`internal/releasedocs` core types, `ReleaseContext` builder, grouped change model, changelog + release-notes
generators, releasebody + changelogpr publishers, `config.ReleaseDocs`, preset templates, `cadoo release-docs`
CLI command (stateless, marker-based idempotency). Dogfood on Cadoo's own repo.

**Phase 2 — Webhook auto-trigger + state.**
Release/tag webhook ingestion, `ReleaseJob` enqueue (River + memory), worker consumer, DB state table +
migration, pages publisher, blog generator.

**Phase 3 — API docs / OpenAPI.**
Code-derived extraction (start with a narrow, well-supported framework set), `apidocs` generator, pages output.

## 9. Testing

- Unit: grouped change model (conventional/labels/llm parsing), each generator against fixture
  `ReleaseContext`s, template override loading, `Enabled`/`when:` matrix per bump.
- Idempotency: run dispatcher twice over the same range → release body edited not duplicated, single
  changelog PR, stable pages paths. Cover both DB-backed and stateless/marker reconstruction.
- Golden-file tests for preset template output (deterministic changelog with LLM off).
- Provider capability tests with fakes; graceful-degradation when a capability is absent.
- `make ci` (vet + test + build) must stay green; new migration must round-trip `up → down → up` (CI `migrations` job).
- Lint: exported symbols need docstrings (`exported` revive rule on); `goimports` local-prefix grouping.

## 10. Open items deferred to planning

- Exact OpenAPI extraction strategy and initial supported language/framework (phase 3).
- Whether `llm` grouping is worth shipping in phase 1 or deferred (conventional/labels first).
- Blog publish destination beyond pages (e.g. dev.to) — out of scope for now.
