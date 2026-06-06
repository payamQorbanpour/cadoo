# Constraints (from SPECs)

Extracted technical constraints, contracts, and design decisions from classified SPEC documents.
Each entry carries a `source:` for provenance.

---

## CONS-releasedocs-subsystem-shape — Dedicated parallel subsystem

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: protocol
- scope: internal/releasedocs subsystem

A new subsystem `internal/releasedocs` is built **parallel** to `internal/orchestrator` (the review
pipeline), NOT on top of it. It deliberately does NOT extend `tools.Tool` / `tools.Input` / `tools.Result`
(those are PR-diff / inline-comment shaped — wrong fit for release artifacts). It mirrors the review
pipeline's architecture (dual-mode queue, marker+state idempotency, provider pool, LLM gateway reuse)
rather than reusing its types.

Package layout (normative):

```
internal/releasedocs/
  releasedocs.go     // ReleaseJob, Artifact, Generator, Publisher interfaces + types
  dispatcher.go      // Dispatcher.Run(ctx, ReleaseJob) — single entry point
  registry.go        // wires built-in generators + publishers
  context.go         // builds ReleaseContext (range -> PRs/commits)
  generators/{changelog,releasenotes,blog,apidocs}/
  publishers/{releasebody,changelogpr,pages}/
  template/          // embedded presets + repo override loading
```

---

## CONS-releasedocs-core-interfaces — Core types & interfaces

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: api-contract
- scope: ReleaseJob / ReleaseContext / Artifact / Generator / Publisher

Four core abstractions plus a dispatcher. Generators are pure (context -> artifact, side-effect-free);
publishers own all writes.

```go
type ReleaseJob struct {
    OrgID, Provider, InstallID, Repo string
    FromRef, ToRef, ReleaseID, Trigger string // Trigger: release | tag | manual
}

type ReleaseContext struct {
    Repo, OrgID, FromTag, ToTag string
    Bump      SemverBump        // major | minor | patch | none
    Commits   []vcs.Commit
    MergedPRs []vcs.MergedPR
    Config    config.ReleaseDocs
    Provider  vcs.Provider
    LLM       llm.Provider      // nil-tolerant
}

type Artifact struct {
    Kind    ArtifactKind   // changelog | release-notes | blog | api-docs | openapi
    Title, Body string
    Targets []PublishTarget
    Meta    map[string]string
}

type Generator interface {
    Kind() ArtifactKind
    Enabled(cfg config.ReleaseDocs, bump SemverBump) bool
    Generate(ctx context.Context, rc *ReleaseContext) (*Artifact, error)
}

type Publisher interface {
    Target() PublishTarget
    Publish(ctx context.Context, rc *ReleaseContext, arts []Artifact) error
}
```

---

## CONS-releasedocs-dispatcher-flow — Dispatcher.Run flow

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: protocol
- scope: internal/releasedocs/dispatcher.go

`Dispatcher.Run(ctx, ReleaseJob)`:
1. Resolve `vcs.Provider` from the orchestrator's `VCSPool` (reused).
2. Resolve `FromRef` if empty: find prior tag matching `tagPattern` before `ToRef`.
3. Load `.cadoo.yaml` from **`ToRef`'s tree (the release tag), not `main`**. Parse `releaseDocs`;
   if `enabled:false`, no-op.
4. Build `ReleaseContext`: list commits + merged PRs in `FromRef..ToRef`, compute `Bump` from semver tags.
5. Run each registered generator where `Enabled(cfg, bump)` is true (parallelizable); collect Artifacts.
6. Route each artifact to its configured publishers; each publisher runs idempotently.
7. Record published state so re-runs edit in place.

---

## CONS-releasedocs-config-schema — `.cadoo.yaml` releaseDocs block

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: schema
- scope: config.ReleaseDocs in internal/config

New `config.ReleaseDocs`, loaded from the **release tag's tree** (consistent with the repo-wide
"config from head/tag, never main" rule). Two configurability layers (approved): presets
(`preset:`, `grouping.source`, `tone:`) for out-of-the-box behavior; custom Go `text/template` override
files via `template:` (loaded from the tag tree) overriding the preset entirely.

```yaml
releaseDocs:
  enabled: true
  trigger: release            # release | tag | manual (default: release)
  tagPattern: "v*"
  artifacts:
    changelog:    { enabled: true,  preset: keep-a-changelog, file: CHANGELOG.md, template: ... }
    releaseNotes: { enabled: true,  when: always, tone: concise, template: ... }
    blog:         { enabled: false, when: minor-major, template: ... }   # opt-in
    apiDocs:      { enabled: false, openapi: { source: detect, output: docs/openapi.yaml } } # phase 3
  grouping:
    source: conventional       # conventional | labels | llm
    sections: [Features, Fixes, Performance, Breaking, Docs, Other]
  publish:
    releaseBody: true
    changelogPR: true
    pages: { enabled: false, branch: gh-pages, dir: docs }
```

Per-artifact control is an explicit user requirement: every artifact has its own `enabled` plus a
`when:` condition keyed off the computed semver bump. `Generator.Enabled` implements it; the dispatcher
never runs a disabled generator.

---

## CONS-releasedocs-generators — Built-in generators & grouped change model

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: protocol
- scope: generators/{changelog,releasenotes,blog,apidocs}

All change-derived generators share one **grouped change model** built once from `ReleaseContext` per
`grouping.source`: `conventional` (parse Conventional Commit prefixes), `labels` (group merged PRs by
label via configurable label->section map), `llm` (classify each PR/commit into configured sections).

- changelog: deterministic-first (parse -> group -> render); LLM only polishes wording so output is
  reproducible with LLM off. Emits a `CHANGELOG.md` section.
- release-notes: deterministic skeleton from grouped model; LLM writes `tone`-aware narrative + highlights.
- blog: LLM-authored long-form from highlights.
- api-docs / openapi (phase 3): deterministic extraction + optional prose; OpenAPI YAML + rendered reference.

---

## CONS-releasedocs-idempotency — Publishers & idempotency

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: protocol
- scope: publishers/{releasebody,changelogpr,pages} + state store

Everything idempotent across resyncs (re-tag, edited release, re-run), mirroring the review pipeline's
marker + stored-state approach.

- releasebody: `UpsertRelease`/`UpdateReleaseBody`; wraps Cadoo content in markers
  (`<!-- cadoo:release-notes:begin -->` … `:end`), preserving user-written body outside the block.
- changelogpr: opens/updates a single PR prepending the new `CHANGELOG.md` section; idempotent by hidden
  marker keyed on `ToRef` (`<!-- cadoo:changelog:vX.Y.Z -->`); deterministic branch `cadoo/changelog/vX.Y.Z`.
- pages: commits rendered artifacts to configured `branch`/`dir` with deterministic paths
  (`docs/releases/vX.Y.Z/…`); re-runs overwrite same paths.

State: new table, migration `NNNN_release_docs.sql`, keyed `(provider, repo, to_tag, artifact_kind)`,
recording published release-body id, changelog PR number, pages commit. In stateless CLI/CI mode,
reconstruct state by reading Cadoo's own markers back (same philosophy as `PriorReviewReader`).

---

## CONS-releasedocs-vcs-capabilities — Optional VCS capability interfaces

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: api-contract
- scope: vcs.Provider capability interfaces (github + gitlab)

`vcs.Provider` stays PR-centric. Add **optional capability interfaces** (type-asserted by the dispatcher,
same pattern as `PriorReviewReader`) rather than bloating core `Provider`. A provider lacking a capability
degrades gracefully (publisher/generator skipped with a logged reason). Implemented in
`internal/vcs/github` (go-github v66) and `internal/vcs/gitlab` (go-gitlab).

```go
type ReleaseRangeReader interface {
    ResolvePriorTag(ctx, repo, toTag, pattern string) (string, error)
    ListCommits(ctx, repo, fromRef, toRef string) ([]Commit, error)
    ListMergedPRs(ctx, repo, fromRef, toRef string) ([]MergedPR, error)
}
type ReleasePublisher interface {
    GetRelease(ctx, repo, tagOrID string) (*Release, error)
    UpdateReleaseBody(ctx, repo, releaseID, body string) error
}
type BranchCommitter interface {
    UpsertBranchFiles(ctx, repo, branch, base string, files []FileWrite, msg string) (string, error)
    OpenOrUpdatePR(ctx, repo, head, base, title, body string) (int64, error)
}
```

---

## CONS-releasedocs-triggers — Trigger & ingestion

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: protocol
- scope: webhook / worker / CLI release-docs entry points

- Default `release`: `cadoo-webhook` handles GitHub `release: published` / GitLab release webhooks,
  verifies signature, builds `ReleaseJob`, enqueues (River when `DATABASE_URL` set, in-memory sibling
  goroutine otherwise) for `cadoo-worker`. Same dual-mode plumbing as `orchestrator.ToolJob`.
- `tag`: a `v*` tag push treated as a release when configured; `tagPattern` filters RC noise.
- manual/CLI: `cadoo release-docs --pr-host … --repo … --from vX --to vY` (+ `--mr` form), stateless,
  memory provider pool, same dispatcher. This is the **phase-1 entry point** and the dogfooding path.

Webhook reads trigger config from the tag tree's `.cadoo.yaml`; if `releaseDocs.trigger` excludes the
event kind, no-op early.

---

## CONS-releasedocs-nonfunctional — Non-functional constraints & non-goals

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-04-release-docs-design.md
- type: nfr
- scope: cross-cutting

- Multi-tenant: `org_id` carried throughout; self-host and SaaS share one code path; no single-tenant shortcuts.
- No new LLM provider code paths (routing stays in LiteLLM).
- `LLM` is nil-tolerant; deterministic generators (changelog with LLM off) must produce reproducible output.
- Must keep `make ci` (vet + test + build) green; new migration must round-trip `up -> down -> up`
  (CI `migrations` job).
- Lint: exported symbols need docstrings (`exported` revive rule on); `goimports` local-prefix grouping.

Non-goals: extending `tools.*`; arbitrary-language API docs (phase 3, narrow framework set first);
blog publish destinations beyond pages (e.g. dev.to) out of scope.
