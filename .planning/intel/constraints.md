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

---
---

# CI-mode dedup convergence (ingest 2026-06-14)

> Source SPEC: `docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md`
> (type: SPEC, confidence: high, locked: false, precedence: default). Distinct subsystem from
> release-docs above — this targets `cadoo-cli` CI-mode dedup in `internal/orchestrator` /
> `internal/findings` / `internal/vcs`. No scope overlap with `internal/releasedocs`.

---

## CONS-cidedup-self-resolution-fix — Carry StructuralKey end-to-end (Part A)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- type: api-contract
- scope: findings.PostedFinding / findings.ListPostedFindings / findings.NewFromPrior / orchestrator.resolveStalePriors

Root cause: `resolveStalePriors` (`reviewer.go:491`) rebuilds each prior finding's `StructuralKey` from
`p.Title` (only the first line of the body, `findings.go:184`), while the current run's keys come from the
full body (`reviewer.go:481`). For any multi-line comment `normalizeTitle(firstLine) != normalizeTitle(fullBody)`,
so a still-valid finding looks stale and Cadoo resolves its own thread every run.

Fix (normative):
- Add `StructuralKey string` to `findings.PostedFinding`.
- Extend `ListPostedFindings` to select the existing `structural_key` DB column (CI read-back already has it
  in `pi.StructuralKey` from the `sk=` marker).
- `findings.NewFromPrior` populates `PostedFinding.StructuralKey` from `pi.StructuralKey`.
- In `resolveStalePriors`, compare `p.StructuralKey` directly against `currentKeys` — do NOT recompute from
  `p.Title`.

Applies to **both** backends: the DB-backed worker path benefits from Part A as well (the
`resolveStalePriors` fix and `StructuralKey` on `PostedFinding`).

---

## CONS-cidedup-thread-state-suppression — Thread state as durable memory (Part B)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- type: protocol
- scope: vcs.PriorInline / gitlab.ListCadooArtifacts / findings.NewFromPrior / findings.findingRec / memoryStore.has

Make resolution and location first-class dedup inputs so a resolved thread carries suppression weight.

- **Capture anchor line:** add `Line int` (and optionally `EndLine int`) to `vcs.PriorInline`; populate from
  `n.Position.NewLine` in `ListCadooArtifacts`. Plumb through `NewFromPrior` into the seeded `findingRec`
  (add a `Line` field).
- **Capture resolved flag in the store:** `NewFromPrior` reads `pi.Resolved` (already on `PriorInline`) into
  the seeded record. (Currently `gitlab.go:263` records `Resolved` but `prior.go:34-53` drops it.)
- **Sticky suppression for resolved findings:** extend `memoryStore.has` so a new comment is suppressed when,
  for the same `(tool, file)`:
  - it matches an *open* prior by the existing rule (exact `StructuralKey` OR Jaccard >= `SimilarTitleThreshold`), **or**
  - it matches a *resolved* prior by a widened rule: line-range overlap with the resolved thread's anchor,
    **or** Jaccard >= a lower `ResolvedSuppressThreshold` (e.g. 0.3).

**Guardrail (normative):** widened suppression is scoped to `(tool, file)` and (for the line rule) to
overlapping lines, so it cannot hide a genuinely new, different finding elsewhere in the same file.

---

## CONS-cidedup-incremental-review — Incremental review change set (Part C)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- type: protocol
- scope: summary wrapper / vcs.PriorReview / vcs.Provider DiffBetween / tools.Input context selection

Only re-review code changed since Cadoo last reviewed — a structural ceiling on thread growth.

- **Persist last-reviewed SHA:** embed `<!-- cadoo:reviewed-sha:<head-sha> -->` in the summary wrapper
  comment. Add `LastReviewedSHA string` to `vcs.PriorReview`; parse it in `ListCadooArtifacts` from the
  summary note; write it into the summary body when posting/editing the overview.
- **Compute incremental change set:** when `LastReviewedSHA` is present AND an ancestor of current head,
  fetch the `lastReviewedSHA..head` diff via a new provider capability `DiffBetween(ctx, pr, oldSHA, newSHA)`
  (GitLab compare API, GitHub compare API). Result = files + hunks/lines touched since last review.
- **Feed inline tools the incremental view:** inline-emitting tools (`review`, `improve`, security, …)
  receive a `tools.Input` filtered to the incremental change set. Summary tools (`describe`, `changelog`)
  keep the **full** PR view. Preferred mechanism: carry **both** a full and an incremental context on
  `tools.Input` and let each tool select (avoids a brittle registry-wide inline/summary classification).
- **Fallbacks (normative):** first run (no prior SHA) → full review. `LastReviewedSHA` not reachable from
  head (force-push / rebase) → full review. Empty incremental diff → no inline tools run, summary refreshed only.

---

## CONS-cidedup-resolvestale-incremental-rule — resolveStalePriors under incremental review

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- type: protocol
- scope: orchestrator.resolveStalePriors × incremental change set

Critical interaction: under incremental review, findings on unchanged code are intentionally not
regenerated this run, so naive `resolveStalePriors` would see them missing from `currentKeys` and resolve
them all — re-introducing churn.

**Rule (normative):** `resolveStalePriors` may only consider a prior "resolvable" when its anchor line falls
**inside the incremental change set** for this run. Threads anchored to untouched code are neither re-posted
nor resolved — they simply persist. (Requires Part B's captured anchor line.) On a full run (no prior SHA /
force-push fallback) the change set is the entire diff, so behavior matches today's full-review semantics.

---

## CONS-cidedup-data-marker-changes — Data / marker changes (summary)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- type: api-contract
- scope: vcs.PriorInline / vcs.PriorReview / findings.PostedFinding / findings.findingRec / summary wrapper / vcs.Provider

| Artifact | Change |
|---|---|
| `vcs.PriorInline` | add `Line int` (and `EndLine int`), populated from `n.Position` |
| `vcs.PriorReview` | add `LastReviewedSHA string` |
| `findings.PostedFinding` | add `StructuralKey string` |
| `findings.findingRec` | add `Line int`, `Resolved bool` |
| Summary wrapper body | embed `<!-- cadoo:reviewed-sha:<sha> -->` |
| Inline marker | **unchanged** — `sk=` / `nt=` already sufficient |
| `vcs.Provider` | add `DiffBetween(ctx, pr, oldSHA, newSHA)` capability (GitLab + GitHub) |

**No new DB migration:** this is the CI-mode (memory store) path. The DB-backed worker path is unaffected by
Parts B/C but benefits from Part A.

---

## CONS-cidedup-nonfunctional — Non-functional constraints & out-of-scope (YAGNI)

- source: /Users/payam/projects/github.com/payamqorbanpour/cadoo/docs/superpowers/specs/2026-06-14-cadoo-ci-dedup-convergence-design.md
- type: nfr
- scope: cross-cutting

- **Convergence invariant (testable):** once code stops changing, thread count must be **monotonic
  non-increasing**; a re-run against an unchanged head posts **zero** new threads and resolves **zero**
  existing ones (fixed-point test).
- **Shippable independently:** Parts A, B, C are built in order; each is independently shippable and verifiable.
  A and B stop the runaway loop; C is the structural ceiling.
- **Tunable constants:** `ResolvedSuppressThreshold` (start 0.3) and line-overlap tolerance (start
  exact-line-range overlap) must be constants for tuning.
- **GitLab CI-mode is the reported bug; GitHub inherits Parts A/C generically** — no GitHub/GHES behavioral
  parity testing beyond the shared `DiffBetween` capability.

Out of scope (YAGNI):
- DB schema changes / new migrations (CI-mode is memory-backed).
- Lowering LLM temperature / model-determinism tuning (addressed structurally by Part C).
- Code-content-hash finding identity (hashing the flagged source span instead of prose) — stronger but
  larger; revisit only if leakage persists after Part C.
- GitHub/GHES behavioral parity testing beyond the shared `DiffBetween` capability.
