# Phase 1: Generators + Publishers + CLI - Research

**Researched:** 2026-06-04
**Domain:** Go subsystem design — VCS provider capability interfaces, marker-based stateless idempotency, deterministic + LLM generators, `text/template`/`go:embed`, semver/conventional-commit parsing
**Confidence:** HIGH (all integration points read from real source; cited `file:line`)

## Summary

This phase builds `internal/releasedocs` as a **parallel** subsystem to `internal/orchestrator`. The
SPEC is approved and locked; the research value here is **grounding every SPEC reference in real Cadoo
code** so the planner writes tasks that reuse actual signatures and conventions rather than SPEC
pseudocode. Every architectural building block the SPEC asks for already has a concrete, working
precedent in the review pipeline: optional capability interfaces (`vcs.PriorReviewReader`), marker
wrapping/splicing (`orchestrator/consolidate.go`), stateless marker reconstruction (`cmd/cadoo-cli/ci.go`
+ `findings.NewFromPrior`), config-from-ref loading (`FileFetcher` on the adapters), the nil-tolerant LLM
gateway (`llm.Provider`), and a `Dispatcher.Run(ctx, Job)` + `Registry` of built-ins.

Three patterns the SPEC needs **do not yet exist anywhere in the repo** and must be introduced fresh:
(1) `text/template` and `go:embed` — grep finds zero usage; (2) a `testdata/` golden-file convention —
none exists; (3) semver and Conventional-Commit parsing — no library in `go.mod`. These are the only
genuinely new-pattern areas; everything else is "mirror an existing file."

**Primary recommendation:** For each new piece, copy the shape of its review-pipeline twin
(capability interface → mirror `PriorReviewReader`; dispatcher → mirror `orchestrator.Dispatcher.Run`;
registry → mirror `orchestrator.DefaultRegistry`; markers → reuse the `consolidate.go` splice approach;
stateless reconstruction → mirror `ci.go priorStore` + `findings.NewFromPrior`). Use `golang.org/x/mod/semver`
(already-vendored-adjacent official module) for bump computation and a hand-rolled Conventional-Commit
prefix parser (trivial, golden-file-testable, no dependency).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Resolve prior tag / list commits+PRs in range | VCS adapter (`ReleaseRangeReader`) | dispatcher (orchestration) | Range queries are provider API calls; same tier as `ListChangedFiles` |
| Get/update release body | VCS adapter (`ReleasePublisher`) | releasebody publisher | Release REST calls live in the adapter, exactly like `UpsertCheckRun` |
| Create branch + open/update PR for CHANGELOG | VCS adapter (`BranchCommitter`) | changelogpr publisher | File-commit + PR-open are provider calls; publisher orchestrates |
| Build grouped change model | `releasedocs/context.go` + generators (shared) | — | Pure transform over `[]vcs.Commit`/`[]vcs.MergedPR`; no I/O |
| Render changelog (deterministic) | `generators/changelog` | LLM (optional polish) | Must be reproducible with LLM off → owns its own rendering |
| Author release-notes narrative | `generators/releasenotes` + `llm.Provider` | deterministic skeleton | LLM authors prose on a deterministic highlight skeleton |
| Marker wrap / splice / reconstruct | publishers + a shared `releasedocs` marker helper | — | Mirror `consolidate.go spliceCadooBody`; do NOT reinvent format |
| Load `releaseDocs` config from tag tree | dispatcher via `FileFetcher` capability | `config` package | `FileFetcher.FetchFileFromRef(repo, ToRef, ".cadoo.yaml")` already exists |
| CLI entry, provider pool, stateless wiring | `cmd/cadoo-cli` (`release-docs` subcommand) | dispatcher | Mirror `ci.go` flag parse + `buildProvider` + memory pool |

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `gopkg.in/yaml.v3` | (in go.mod) | Parse `releaseDocs` block in `.cadoo.yaml` | Already the config parser (`internal/config/config.go:12`) — extend `Repo` struct, no new dep |
| `golang.org/x/mod/semver` | v0.36.0 | Compute `major\|minor\|patch\|none` bump between two `vX.Y.Z` tags | Official Go module (`golang.org/x/...`); `semver.Compare`, `semver.Major`, `semver.MajorMinor`. Pure, no transitive bloat |
| `github.com/google/go-github/v66/github` | v66.0.0 | GitHub release + commit-compare + contents/PR API for new capabilities | Already the GitHub adapter client (`internal/vcs/github/github.go:17`) |
| `gitlab.com/gitlab-org/api/client-go` (`glab`) | v0.115.0 | GitLab release + compare + commits API for new capabilities | Already the GitLab adapter client (`internal/vcs/gitlab/gitlab.go:14`). NOTE: import path is `gitlab.com/gitlab-org/api/client-go`, aliased `glab` — NOT `xanzy/go-gitlab` (the `go.mod` require line still reads `xanzy` but the code imports the new path) |
| `text/template` (stdlib) | Go 1.26 | Render preset + custom override templates | SPEC D-07 mandates Go `text/template`. **No existing usage in repo** — new pattern |
| `embed` (stdlib) | Go 1.26 | Embed preset template files into `internal/releasedocs/template` | **No existing `go:embed` usage in repo** — new pattern |
| `golang.org/x/sync/errgroup` | v0.20.0 | Parallel generator fan-out (optional, per D-05) | Already used in `cmd/cadoo-cli/ci.go:21` for parallel tool dispatch |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `log/slog` | stdlib | Log graceful-degradation reasons ("capability X absent, skipping publisher Y") | Mirror `slog.Debug`/`slog.Warn` usage in `reviewer.go` |
| `flag` | stdlib | `release-docs` subcommand flag set | Mirror `ci.go ciCmd` `flag.NewFlagSet` |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `golang.org/x/mod/semver` | `github.com/Masterminds/semver/v3` | Masterminds has a richer `Version` type + constraints, but it's a NEW third-party dep; `x/mod` is official, minimal, and Cadoo already pulls `golang.org/x/sync`/`golang.org/x/mod` adjacents. Prefer `x/mod`. |
| Hand-rolled Conventional-Commit prefix parser | a CC library (e.g. `leodido/go-conventionalcommits`) | The grammar Phase 1 needs (`feat:`/`fix:`/`perf:`/`feat!:`/`BREAKING CHANGE`) is ~30 lines of `strings.HasPrefix`/`strings.Cut`. A library is overkill and adds slopcheck surface. Hand-roll it — it's golden-file-testable. |

**Installation:**
```bash
go get golang.org/x/mod/semver@v0.36.0   # only new module; yaml.v3, go-github, glab, errgroup already present
go mod tidy
```

**Version verification:**
- `golang.org/x/mod` — `go list -m -versions golang.org/x/mod` returns up to v0.36.0 (verified this session). Official Go module; `[VERIFIED: go module proxy]`.
- `go-github/v66` v66.0.0, `glab` v0.115.0, `yaml.v3`, `errgroup` v0.20.0 — all already in `go.mod` (verified by grep). `[VERIFIED: go.mod]`.

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `golang.org/x/mod` | go proxy | >5 yrs | very high | golang.org/x/mod (official) | n/a (Go ecosystem) | Approved — official Go sub-repo |
| `gopkg.in/yaml.v3` | go proxy | mature | very high | github.com/go-yaml/yaml | n/a | Already in go.mod |
| `go-github/v66` | go proxy | mature | very high | github.com/google/go-github | n/a | Already in go.mod |
| `glab` (gitlab-org/api/client-go) | go proxy | mature | high | gitlab.com/gitlab-org/api/client-go | n/a | Already imported |

**Packages removed due to slopcheck [SLOP] verdict:** none — only one new module, an official `golang.org/x/...` sub-repo.
**Packages flagged as suspicious [SUS]:** none.

*slopcheck is Python/npm-oriented and does not apply to Go module paths. The single new dependency is an
official Go team module (`golang.org/x/mod`), which is not a hallucination/slopsquat vector. No human-verify
checkpoint required for it. [ASSUMED] only in the sense that the exact patch version should be confirmed by
`go get` at implementation time.*

## Architecture Patterns

### System Architecture Diagram

```
cadoo release-docs --pr-host … --repo … --from vX --to vY        (cmd/cadoo-cli, NEW subcommand)
        │
        ▼
  parseTargetURL/flags → buildProvider(target)  ──►  vcs.Provider (github | gitlab adapter)
        │                                                   │ (implements optional capabilities)
        ▼                                                   ▼
  releasedocs.Dispatcher.Run(ctx, ReleaseJob) ───── type-assert ReleaseRangeReader / ReleasePublisher / BranchCommitter
        │                                                   │  (absent → skip dependent step, log reason)
        ├─ resolve FromRef (ResolvePriorTag) if empty
        ├─ load .cadoo.yaml from ToRef tree  ◄── FileFetcher.FetchFileFromRef(repo, ToRef, ".cadoo.yaml")
        │      └─ config.Parse → config.ReleaseDocs ; enabled:false ⇒ no-op
        ├─ build ReleaseContext  ◄── ListCommits + ListMergedPRs (FromRef..ToRef) + semver Bump
        │      └─ grouped change model (conventional | labels), built ONCE
        ├─ for each Generator where Enabled(cfg,bump): Generate(ctx, rc) → Artifact   [parallelizable]
        │      ├─ changelog  (deterministic render; LLM optional polish)
        │      └─ release-notes (deterministic skeleton; LLM narrative, tone-aware; nil LLM ⇒ skeleton only)
        └─ route Artifacts → Publishers (each idempotent via marker reconstruction)
               ├─ releasebody  → GetRelease + splice markers + UpdateReleaseBody
               └─ changelogpr  → read prior PR by marker → UpsertBranchFiles + OpenOrUpdatePR
```

The reader can trace the dogfood path: CLI flags → provider → dispatcher → range read → context →
generators → publishers, with each external call landing on a type-asserted capability that degrades
gracefully when absent.

### Recommended Project Structure

Per CONS-releasedocs-subsystem-shape / D-02 (Phase-1 subset; `blog`, `apidocs`, `publishers/pages` deferred):
```
internal/releasedocs/
├── releasedocs.go              # ReleaseJob, ReleaseContext, Artifact, Generator, Publisher, SemverBump, ArtifactKind, PublishTarget
├── dispatcher.go               # Dispatcher.Run(ctx, ReleaseJob)
├── registry.go                 # DefaultRegistry() → built-in generators + publishers
├── context.go                  # ReleaseContext builder (range→commits/PRs, Bump) + grouped change model
├── marker.go                   # release-docs marker constants + splice/parse helpers (mirror consolidate.go)
├── generators/
│   ├── changelog/              # deterministic-first changelog generator
│   └── releasenotes/           # release-notes generator (LLM narrative on deterministic skeleton)
├── publishers/
│   ├── releasebody/            # marker-wrapped release-body upsert
│   └── changelogpr/            # single marker-keyed CHANGELOG.md PR
└── template/
    ├── template.go             # //go:embed preset *.tmpl ; loader + override-from-tag-tree
    └── presets/                # *.tmpl files (keep-a-changelog, release-notes per tone)
```
Capability interfaces (`ReleaseRangeReader`/`ReleasePublisher`/`BranchCommitter`) + the new `vcs.Commit`,
`vcs.MergedPR`, `vcs.Release`, `vcs.FileWrite` types go in **`internal/vcs/`** (alongside `vcs.go`/`marker.go`),
because the orchestrator rule is "the new types live in `internal/vcs`; only adapters import provider SDKs."

### Pattern 1: Optional capability interface, type-asserted at the call site

**What:** Declare a narrow interface in `internal/vcs`, implement it on the adapter `*Adapter`, type-assert
it in the dispatcher; absent ⇒ skip + log.
**When to use:** Every new release op (range read, release publish, branch commit).
**Existing template — `PriorReviewReader`:**
```go
// internal/vcs/vcs.go:133  — declaration
type PriorReviewReader interface {
    ListCadooArtifacts(ctx context.Context, pr *PullRequest) (PriorReview, error)
}
// internal/orchestrator/reviewer.go:61 — a SECOND optional capability (FileFetcher) same pattern
type FileFetcher interface {
    FetchFileFromRef(ctx context.Context, repo, ref, path string) ([]byte, error)
}
// internal/orchestrator/reviewer.go:208 + :508 — type-assert at the call site
if ff, ok := provider.(FileFetcher); ok && pr.HeadSHA != "" { … }
// cmd/cadoo-cli/ci.go:187 — type-assert PriorReviewReader, degrade if absent
if rr, ok := provider.(vcs.PriorReviewReader); ok { … }
```
The GitHub adapter implements `FetchFileFromRef` at `internal/vcs/github/github.go:397` and
`ListCadooArtifacts` at `:240`; GitLab at `internal/vcs/gitlab/gitlab.go:352` and `:233`. The new
capabilities hang off the same `*Adapter` receivers and add a `var _ vcs.ReleaseRangeReader = (*Adapter)(nil)`
assertion line (mirror `var _ vcs.Provider = (*Adapter)(nil)` at `github.go:526` / `gitlab.go:580`).

### Pattern 2: `Dispatcher.Run(ctx, Job)` + `Registry` of built-ins

**What:** A single `Run` entry point resolves provider from a `map[vcs.Kind]vcs.Provider` pool and runs
registered units.
**Template:**
```go
// internal/orchestrator/reviewer.go:82  — Dispatcher struct (VCSPool, LLM, Registry, BaseCfg fields)
// internal/orchestrator/reviewer.go:168 — provider, ok := d.VCSPool[job.Provider]
// internal/orchestrator/registry.go:22  — DefaultRegistry(): r.Register(<unit>{}) per built-in
```
`releasedocs.Dispatcher` mirrors this: `VCSPool map[vcs.Kind]vcs.Provider`, `LLM llm.Provider`,
`Generators`/`Publishers` (or a `Registry`), `Run(ctx, ReleaseJob)`. Reuse the SAME `VCSPool` the
orchestrator builds (D-05) — the CLI constructs a one-entry pool exactly like `ci.go:178`.

### Pattern 3: Marker splice / wrap (do NOT reinvent the format)

**What:** Wrap Cadoo content between HTML-comment sentinels, preserve user content outside, replace inner
region on re-run.
**Template — `spliceCadooBody`:**
```go
// internal/orchestrator/consolidate.go:111  — spliceCadooBody(original, section)
//   finds prSectionBegin/prSectionEnd; if present, replaces inner; else appends after user text.
// markers defined at consolidate.go:16-20 ; SummaryWrapperBegin at internal/vcs/marker.go:14
```
The releasebody publisher reuses this exact splice logic with `<!-- cadoo:release-notes:begin -->` /
`:end` (D-12). The changelogpr publisher uses a single hidden marker `<!-- cadoo:changelog:vX.Y.Z -->`
keyed on `ToRef` (D-13) — recognized by `strings.Contains`, exactly how `ListCadooArtifacts` greps for
`vcs.SummaryWrapperBegin` (`github.go:307`, `gitlab.go:287`).

### Pattern 4: Stateless marker reconstruction (no DB)

**What:** Read Cadoo's own prior markers back from the provider to rebuild idempotency state.
**Template:**
```go
// cmd/cadoo-cli/ci.go:249  priorStore(): rr.ListCadooArtifacts(ctx, pr) → findings.NewFromPrior(key, snap)
// internal/findings/prior.go:24  NewFromPrior(): builds an in-memory store from the read-back snapshot
```
Phase-1 release-docs has no `findings.Store` equivalent; instead each publisher reconstructs its own state
inline: releasebody calls `GetRelease` and checks for the marker; changelogpr lists open PRs (or fetches by
deterministic branch `cadoo/changelog/vX.Y.Z`) and checks for `<!-- cadoo:changelog:vX.Y.Z -->`. The
"read-back, then decide create-vs-update" shape is identical to `postSummary` at `reviewer.go:341`
(SummaryID lookup → UpdateSummaryComment else PostSummaryComment).

### Pattern 5: nil-tolerant LLM

**What:** Generators accept `llm.Provider` that may be nil; deterministic path runs without it.
**Template:** `ReleaseContext.LLM llm.Provider` (D-04). The interface is one method
(`llm.Provider.Chat`, `internal/llm/provider.go:71`); construct with `litellm.New(url, key)`
(`litellm/client.go:29`), same as `ci.go:177`. changelog generator: if `rc.LLM == nil`, skip the polish
call and return the deterministic render verbatim (this is what makes golden-file tests possible).

### Anti-Patterns to Avoid

- **Extending `tools.Tool`/`tools.Input`/`tools.Result`** — explicitly forbidden (D-01, REQUIREMENTS Out-of-Scope). Those are PR-diff/inline-comment shaped. Build parallel types.
- **Importing a provider SDK (`go-github`/`glab`) outside `internal/vcs/`** — CLAUDE.md hard rule. The dispatcher/generators/publishers depend ONLY on `vcs.Provider` + the new capability interfaces.
- **Reinventing the marker format** — `consolidate.go` is the source of truth; reuse the splice approach and the `<!-- cadoo:… -->` convention.
- **Bypassing graceful degradation** — never assume a capability is present; always `if c, ok := provider.(vcs.ReleaseRangeReader); ok` and log a skip reason otherwise.
- **A second default LLM model path in Go** — routing stays in LiteLLM (D-17). Pass the model string through like `reviewer.go modelName`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Semver compare / bump classification | Custom `vX.Y.Z` string math | `golang.org/x/mod/semver` (`Compare`, `Major`, `MajorMinor`) | Edge cases: prerelease/build metadata, `v` prefix normalization, ordering. Official, tested. |
| Marker wrap/splice | New regex per publisher | The `spliceCadooBody` shape in `consolidate.go:111` | Already handles first-write-vs-replace, user-content preservation. |
| Stateless idempotency store | New in-memory dedup store | The read-back-then-decide shape (`ci.go priorStore` + `postSummary`) | The "read marker → update-else-create" loop is the proven idempotency primitive. |
| Range commit/PR listing | Raw HTTP to provider | go-github `Repositories.CompareCommits` / `PullRequests.List`; glab `Repositories.Compare` / `Commits.ListCommits` | Pagination, auth, GHES base-URL handling already solved by the adapter clients. |
| YAML schema parsing | Custom parser | Extend `config.Repo` with a `ReleaseDocs` field + `yaml:"…"` tags (`config.go`) | One parser, one `config.Parse` (`config.go:150`). |

**Key insight:** Almost nothing here is novel — the review pipeline already solved capability detection,
markers, stateless reconstruction, provider pooling, and nil-LLM tolerance. The ONLY net-new mechanisms are
`text/template`+`go:embed` (template rendering) and semver/CC parsing.

## Runtime State Inventory

> Greenfield subsystem (new package, no rename/migration). Most categories N/A.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — Phase 1 is stateless (no DB, no `posted_findings`/`posted_summaries` equivalent). Idempotency state lives entirely in provider-side markers (release body, changelog PR). | none |
| Live service config | The `<!-- cadoo:release-notes:begin -->`/`:end` marker and `cadoo/changelog/vX.Y.Z` branch become a de-facto on-provider contract once dogfooded on Cadoo's repo — changing the marker format later is a breaking change for already-published releases. | Lock the marker strings as constants in `releasedocs/marker.go` from day one. |
| OS-registered state | None. | none |
| Secrets/env vars | New `release-docs` subcommand reads the same env as `ci`: `GITHUB_TOKEN`/`GITLAB_TOKEN`, `LLM_GATEWAY_URL`/`LLM_GATEWAY_API_KEY`, `CADOO_DEFAULT_MODEL` (`ci.go:156-158`, `:266`, `:279`). No NEW secret names. | Reuse the same env contract; document in CLI help. |
| Build artifacts | `make build` builds 5 cmd binaries; no new binary (the subcommand lives inside the existing `cadoo-cli`). | none |

**Nothing found in category:** Stored data / OS-registered state / build artifacts — verified: Phase 1 is
stateless by D-14 and adds no new binary or migration.

## Common Pitfalls

### Pitfall 1: GitLab import path mismatch in `go.mod` vs code
**What goes wrong:** `go.mod` line 0 of the require block reads `github.com/xanzy/go-gitlab v0.115.0`, but the
actual adapter imports `gitlab.com/gitlab-org/api/client-go` aliased `glab` (`gitlab.go:14`). A planner
copying the SPEC's "xanzy" reference will write the wrong import.
**Why it happens:** xanzy/go-gitlab moved upstream; the module require may be a replace/alias or stale line.
**How to avoid:** New GitLab capability code MUST `import glab "gitlab.com/gitlab-org/api/client-go"` and use
`glab.*` types, matching `gitlab.go`. Run `make vet`/`make build` to catch a wrong path immediately.
**Warning signs:** `undefined: gitlab.X` or duplicate-module errors.

### Pitfall 2: Config loaded from `main` instead of the tag tree
**What goes wrong:** Loading `.cadoo.yaml` from the default branch silently uses stale config.
**Why it happens:** `config.LoadFile` reads the local working tree (`config.go:138`); the orchestrator's
`loadCfg` (`reviewer.go:507`) is the correct pattern — it fetches from a specific ref via `FileFetcher`.
**How to avoid:** The dispatcher MUST call `ff.FetchFileFromRef(ctx, repo, job.ToRef, ".cadoo.yaml")` then
`config.Parse(raw)` (D-06). Use `ToRef`, never `main`/`HEAD`. Mirror `loadCfg` exactly, including the
`isMissingFile` 404-tolerant fallback (`reviewer.go:527`).
**Warning signs:** dogfood run picks up config edits on `main` that weren't in the tagged tree.

### Pitfall 3: Non-deterministic changelog breaks golden-file tests
**What goes wrong:** Map-iteration ordering or LLM polish leaking into the deterministic path makes
golden output flaky.
**Why it happens:** Go map iteration is randomized; LLM responses are non-deterministic.
**How to avoid:** Sort sections in a fixed order (the SPEC's `grouping.sections` list is the canonical order;
mirror the `sort.SliceStable` discipline in `consolidate.go:70`). The deterministic render must be a pure
function of the grouped model; LLM polish is a SEPARATE, skippable step (D-10). Golden tests run with
`rc.LLM == nil`.
**Warning signs:** `make test` (`-race -count=1`) golden test passes locally, fails on re-run.

### Pitfall 4: `exported` revive rule — missing docstrings fail lint
**What goes wrong:** Every exported type/method (and there are many new ones) needs a docstring or
`make lint` (golangci-lint v2, `exported` on) fails CI.
**Why it happens:** `.golangci.yml` enables `exported`; `package_comments` is off and `unused-parameter`
is off (CLAUDE.md), but `exported` is ON.
**How to avoid:** Docstring every exported symbol (`ReleaseJob`, `Generator`, each method). `goimports`
local-prefix is `github.com/payamqorbanpour/cadoo` — the cadoo-internal import group must be third.
**Warning signs:** `exported: exported type X should have comment or be unexported`.

### Pitfall 5: GitHub review API returns no per-comment ID — but release ops differ
**What goes wrong:** Assuming the GitHub release/PR APIs behave like the review API (which returns empty
`ExternalID`, `github.go:228`).
**Why it happens:** `CreateReview` is a special case. Release/contents/PR-create calls DO return usable IDs
(release ID, PR number).
**How to avoid:** `ReleasePublisher.GetRelease` returns a `*vcs.Release` carrying the release ID;
`BranchCommitter.OpenOrUpdatePR` returns the PR number (`int64`) — capture and use them. No GraphQL needed
for release ops (unlike thread resolution).

## Code Examples

Verified call sites from the real adapters (the planner maps these to the new capability methods):

### Config-from-ref load (dispatcher step 3)
```go
// Source: internal/orchestrator/reviewer.go:507-525 (loadCfg)
ff, ok := provider.(FileFetcher)            // FileFetcher already implemented on both adapters
raw, err := ff.FetchFileFromRef(ctx, repo, toRef, ".cadoo.yaml")  // toRef = release tag, not main
cfg, err := config.Parse(raw)               // extend config.Repo with ReleaseDocs field
```

### GitHub adapter: where new release methods hang off
```go
// Source: internal/vcs/github/github.go — a.client is *gogithub.Client (built in New(), :51)
// Range read:   a.client.Repositories.CompareCommits(ctx, owner, name, fromRef, toRef, opts)
//               a.client.PullRequests.List(ctx, owner, name, &PullRequestListOptions{State:"closed", Base:…})
// Release:      a.client.Repositories.GetReleaseByTag / GetLatestRelease ; EditRelease (body upsert)
// Branch+PR:    a.client.Git.* (refs/blobs/trees/commits) OR Repositories.CreateFile/UpdateFile ;
//               a.client.PullRequests.Create / .Edit
// (mirror splitRepo at github.go:467 and var _ vcs.Provider assertion at :526)
```

### GitLab adapter: equivalent calls
```go
// Source: internal/vcs/gitlab/gitlab.go — a.client is *glab.Client (built in New(), :32)
// Range read:   a.client.Repositories.Compare(repo, &CompareOptions{From, To}, glab.WithContext(ctx))
//               a.client.Commits.ListCommits ; a.client.MergeRequests.ListProjectMergeRequests (state=merged)
// Release:      a.client.Releases.GetRelease / .UpdateRelease (description = body)
// Branch+PR:    a.client.RepositoryFiles.CreateFile/UpdateFile ; a.client.MergeRequests.CreateMergeRequest
// All calls take glab.WithContext(ctx) as the trailing arg (house style, see every method in gitlab.go)
```

### Stateless reconstruction shape
```go
// Source: cmd/cadoo-cli/ci.go:187 + :249 — type-assert capability, read back, build state, degrade on error
if rr, ok := provider.(vcs.PriorReviewReader); ok {
    snap, err := rr.ListCadooArtifacts(ctx, pr)   // ← analog: GetRelease + grep marker / list changelog PR
    if err != nil { /* log; degrade to non-idempotent */ }
}
```

### CLI subcommand wiring
```go
// Source: cmd/cadoo-cli/main.go:33 (switch) + ci.go:123 (ciCmd) + ci.go:263 (buildProvider)
// Add: case "release-docs": releaseDocsCmd(os.Args[2:])
// releaseDocsCmd: flag.NewFlagSet("release-docs"); --repo/--from/--to/--pr-host (+ --mr form);
//   reuse parseTargetURL/buildProvider; build one-entry VCSPool like ci.go:178; new releasedocs.Dispatcher.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `xanzy/go-gitlab` import path | `gitlab.com/gitlab-org/api/client-go` (alias `glab`) | upstream moved the canonical repo | New GitLab code must use the new path (matches existing `gitlab.go:14`), NOT the `xanzy` line the SPEC/CLAUDE.md mention |
| Per-PR LLM changelog (`/changelog` tool) | Deterministic-first range changelog generator | This phase | The existing `internal/tools/changelog` is PR-scoped + LLM-only; the new generator is range-scoped, deterministic, golden-testable — do NOT reuse it, it's the wrong shape (D-01) |

**Deprecated/outdated:**
- The SPEC's and CLAUDE.md's `xanzy/go-gitlab v0.115` reference: the *version* is right, the *import path* in code is `gitlab.com/gitlab-org/api/client-go`. Follow the code.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `golang.org/x/mod/semver` is the right semver lib (vs Masterminds) | Standard Stack | Low — both work; x/mod avoids a new third-party dep. Planner may swap if constraints parsing needed (not in Phase 1). |
| A2 | go-github v66 exposes `Repositories.CompareCommits`, `GetReleaseByTag`, `EditRelease`, `CreateFile/UpdateFile`, `PullRequests.Create/Edit/List` | Code Examples | Medium — method names/signatures must be confirmed against v66 at implementation time (`go doc github.com/google/go-github/v66/github.RepositoriesService`). The *capability* exists; exact signatures are [ASSUMED]. |
| A3 | glab exposes `Repositories.Compare`, `Releases.GetRelease/UpdateRelease`, `RepositoryFiles.CreateFile/UpdateFile`, `MergeRequests.CreateMergeRequest/ListProjectMergeRequests` | Code Examples | Medium — confirm against v0.115 (`go doc gitlab.com/gitlab-org/api/client-go`). Capability exists; exact signatures [ASSUMED]. |
| A4 | The `xanzy` line in `go.mod` resolves to the `gitlab.com/gitlab-org/api/client-go` module at build time (replace/alias) | Pitfall 1 | Low — `make build` currently passes, so the existing import already resolves; new code using the same import will too. |
| A5 | Conventional-Commit parsing for Phase 1 is limited to `feat/fix/perf/feat!/BREAKING CHANGE` and is hand-rollable | Don't Hand-Roll | Low — SPEC D-09 scopes it to exactly these prefixes. |

## Open Questions

1. **`llm` grouping source in Phase 1?**
   - What we know: SPEC §10 open item; CONTEXT.md Deferred Ideas says **defer** — Phase 1 ships `conventional` + `labels` only; the config enum may accept `llm` but it's not implemented.
   - What's unclear: whether the config struct should validate/reject `llm` or silently fall back to `conventional`.
   - Recommendation: Accept `llm` in the enum, log a "not implemented in this phase, falling back to conventional" warning. Matches the graceful-degradation philosophy.

2. **Where do `vcs.MergedPR` / `vcs.Release` / `vcs.FileWrite` live, and what fields?**
   - What we know: They must live in `internal/vcs` (orchestrator rule), align with existing `vcs.PullRequest`/`vcs.FileChange` field naming (`internal/vcs/vcs.go:23,41`).
   - What's unclear: exact field set (the SPEC sketches the interfaces, not the structs).
   - Recommendation: `vcs.Commit{SHA, Message, Author, Date}`, `vcs.MergedPR{Number, Title, Body, Author, Labels[], MergedAt, MergeSHA}`, `vcs.Release{ID, TagName, Body, Draft, Prerelease}`, `vcs.FileWrite{Path, Content[], Mode}`. Planner finalizes (Claude's Discretion per CONTEXT.md).

3. **Does go-github need GraphQL for any release op (like thread resolution)?**
   - What we know: Thread resolution needed GraphQL (`github.go:355`); release/PR/contents are REST.
   - Recommendation: All Phase-1 release ops are REST — no GraphQL seam needed. Confirm `EditRelease` exists in v66 REST.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26 (`go.mod`, CLAUDE.md) | — |
| `golang.org/x/mod` | semver bump | ✗ (not yet in go.mod) | will `go get` v0.36.0 | hand-roll `vX.Y.Z` compare (discouraged) |
| go-github v66 | GitHub capabilities | ✓ | v66.0.0 (go.mod) | — |
| glab (gitlab-org/api/client-go) | GitLab capabilities | ✓ | v0.115.0 (go.mod) | — |
| `make` / golangci-lint v2 | `make ci` lint gate | ✓ (CI pins v2.12.2) | v2 | — |
| LiteLLM gateway (`LLM_GATEWAY_URL`) | release-notes LLM narrative | runtime env | — | nil LLM ⇒ deterministic skeleton only (D-04, D-10) |

**Missing dependencies with no fallback:** none (the only missing module is fetchable via `go get`).
**Missing dependencies with fallback:** LiteLLM gateway — absent ⇒ changelog still renders deterministically; release-notes degrades to the skeleton.

## Validation Architecture

> nyquist_validation treated as enabled (no `.planning/config.json` override observed disabling it).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven; `httptest.NewServer` for adapter tests — see `github_test.go`) |
| Config file | none (Go built-in) |
| Quick run command | `go test ./internal/releasedocs/...` |
| Full suite command | `make test` (= `go test -race -count=1 ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| REQ-release-artifact-generation | grouped change model parses conventional + labels correctly | unit | `go test ./internal/releasedocs/... -run TestGroupedModel` | ❌ Wave 0 |
| REQ-release-artifact-generation | changelog generator renders expected section from fixture ReleaseContext | golden | `go test ./internal/releasedocs/generators/changelog/... -run TestChangelogGolden` | ❌ Wave 0 |
| REQ-release-artifact-generation | release-notes builds skeleton with nil LLM | unit | `go test ./internal/releasedocs/generators/releasenotes/... -run TestSkeletonNoLLM` | ❌ Wave 0 |
| REQ-per-artifact-toggles | `Enabled(cfg,bump)` matrix (enabled flag × `when:` × computed bump) | unit (table) | `go test ./internal/releasedocs/... -run TestEnabledMatrix` | ❌ Wave 0 |
| REQ-configurable-templates | preset render vs custom `template:` override loaded from tag tree | unit | `go test ./internal/releasedocs/template/... -run TestTemplateOverride` | ❌ Wave 0 |
| REQ-configurable-templates | embedded presets load via `go:embed` | unit | `go test ./internal/releasedocs/template/... -run TestEmbeddedPresets` | ❌ Wave 0 |
| REQ-release-docs-idempotency | run dispatcher twice → release body edited not duplicated; single changelog PR (marker reconstruction) | integration (fake provider) | `go test ./internal/releasedocs/... -run TestIdempotentTwiceRun` | ❌ Wave 0 |
| REQ-configurable-trigger | `release-docs` CLI parses flags → ReleaseJob; URL forms (--repo/--from/--to, --mr) | unit | `go test ./cmd/cadoo-cli/... -run TestReleaseDocsFlags` | ❌ Wave 0 |
| REQ-publish-destinations | releasebody splice preserves user content outside markers | unit | `go test ./internal/releasedocs/publishers/releasebody/... -run TestSplicePreserves` | ❌ Wave 0 |
| REQ-publish-destinations | changelogpr opens-then-updates single PR via marker key | integration (fake) | `go test ./internal/releasedocs/publishers/changelogpr/... -run TestSinglePR` | ❌ Wave 0 |
| (cross-cutting) | provider missing a capability → publisher/generator skipped with logged reason | unit (fake w/o capability) | `go test ./internal/releasedocs/... -run TestGracefulDegradation` | ❌ Wave 0 |
| (cross-cutting) | semver bump computation `vX.Y.Z → bump` | unit (table) | `go test ./internal/releasedocs/... -run TestBump` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/releasedocs/...` (+ the touched adapter package)
- **Per wave merge:** `make test` (`-race -count=1 ./...`)
- **Phase gate:** `make ci` (vet + test + build) green, plus a successful **dogfood run** on Cadoo's own repo (`cadoo release-docs --repo payamqorbanpour/cadoo --from <prev tag> --to <tag>`), run twice to prove idempotency.

### Wave 0 Gaps
- [ ] `internal/releasedocs/context_test.go` — grouped change model (conventional/labels), bump matrix — covers REQ-release-artifact-generation
- [ ] `internal/releasedocs/generators/changelog/changelog_test.go` + `testdata/*.golden` — **establishes the repo's first golden-file convention** (none exists today)
- [ ] `internal/releasedocs/generators/releasenotes/releasenotes_test.go` — nil-LLM skeleton path
- [ ] `internal/releasedocs/template/template_test.go` + `presets/*.tmpl` — embed + override loading
- [ ] `internal/releasedocs/dispatcher_test.go` — fake `vcs.Provider` implementing/omitting capabilities; twice-run idempotency; graceful degradation
- [ ] `internal/releasedocs/publishers/{releasebody,changelogpr}/*_test.go` — splice + single-PR idempotency
- [ ] `internal/vcs/<github|gitlab>/*_test.go` additions — new capability methods via `httptest.NewServer` (mirror `github_test.go` GraphQL stub / glab REST stub pattern)
- [ ] `cmd/cadoo-cli/releasedocs_test.go` — flag→ReleaseJob mapping, URL parsing (reuse `parseTargetURL`)
- [ ] **Shared fake provider** — a `releasedocs`-local fake implementing `vcs.Provider` + the three new capabilities, with toggles to omit a capability (for degradation tests). No fake exists today; build one.

*Note: the repo has NO existing `testdata/`/golden convention and NO fake-provider helper — both are net-new
infrastructure this phase introduces. Adapter tests today use `httptest.NewServer` (not interface fakes), so
the new in-process fake provider is a fresh pattern justified by the dispatcher's pure-Go testability.*

## Security Domain

> `security_enforcement` not explicitly disabled in config; included for completeness. Phase 1 is a
> developer-facing CLI that writes to release bodies / opens PRs using a provided token.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Reuse existing token auth (`GITHUB_TOKEN`/`GITLAB_TOKEN` env, `ci.go:266/279`); App-install auth via `ghinstallation` (`github.go:61`). No new auth code. |
| V3 Session Management | no | Stateless CLI; no sessions. |
| V4 Access Control | no (deferred) | Multi-tenant `org_id` carried through `ReleaseJob` (D-17), but Phase-1 CLI is single-operator; no cross-tenant authz checks in this phase. |
| V5 Input Validation | yes | Validate `--repo`/`--from`/`--to`/URL forms (reuse `parseTargetURL` validation, `ci.go:52`); reject malformed tags before provider calls. Template override files are operator-authored (loaded from their own tag tree) — `text/template` (NOT `html/template`) is correct since output is markdown, but the template author is the repo owner, so no untrusted-template injection surface. |
| V6 Cryptography | no | No crypto introduced; webhook signature verification is Phase 2. |

### Known Threat Patterns for {Go CLI + VCS API + text/template}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Token leakage in logs | Information disclosure | Never log token env values; mirror existing adapters (tokens never logged). |
| Template executing arbitrary funcs | Tampering / EoP | Use `text/template` with NO custom `FuncMap` exposing OS/filesystem; data is the `ReleaseContext` only. Template author = repo owner (trusted), but still avoid `os`/`exec` funcs. |
| Writing to an unintended branch/PR | Tampering | Deterministic branch name `cadoo/changelog/vX.Y.Z`; marker-keyed single PR prevents PR spam; idempotency tests assert single-PR invariant. |
| Markdown/HTML injection into release body | Tampering | Body is markdown rendered by the provider; Cadoo content is wrapped in HTML-comment markers (same as `consolidate.go`), user content outside is preserved verbatim — no escaping needed beyond existing behavior. |

## Sources

### Primary (HIGH confidence)
- `internal/vcs/vcs.go` (Provider :103, PriorReviewReader :133) — capability-interface template
- `internal/vcs/marker.go` (:14 SummaryWrapperBegin, :32 InlineMarker, :44 ParseInlineMarker) — marker helpers
- `internal/orchestrator/reviewer.go` (:82 Dispatcher, :145 Run, :168 VCSPool resolve, :208/:508 FileFetcher assert, :341 postSummary idempotency, :507 loadCfg) — dispatcher + config-from-ref + idempotency
- `internal/orchestrator/registry.go` (:22 DefaultRegistry) — registry-of-built-ins
- `internal/orchestrator/consolidate.go` (:16 markers, :111 spliceCadooBody, :70 sort) — marker splice/format
- `internal/vcs/github/github.go` (:51 New/client, :240 ListCadooArtifacts, :397 FetchFileFromRef, :526 assertion) — GitHub adapter
- `internal/vcs/gitlab/gitlab.go` (:14 glab import, :32 New, :233 ListCadooArtifacts, :352 FetchFileFromRef, :580 assertion) — GitLab adapter
- `internal/config/config.go` (:16 Repo, :150 Parse) — config schema/parser
- `internal/llm/provider.go` (:71 Provider.Chat) + `internal/llm/litellm/client.go` (:29 New, :90 Chat) — LLM gateway
- `cmd/cadoo-cli/ci.go` (:123 ciCmd, :176 stateless Dispatcher, :187 PriorReviewReader assert, :249 priorStore, :263 buildProvider) + `cmd/cadoo-cli/main.go` (:33 subcommand switch) — CLI plumbing
- `internal/findings/prior.go` (:24 NewFromPrior) — stateless reconstruction
- `internal/vcs/github/github_test.go` — httptest stub test convention
- `go.mod` — dep versions (go 1.26, go-github v66, glab/xanzy v0.115, errgroup v0.20, yaml.v3)
- `.planning/intel/constraints.md`, `.planning/intel/decisions.md`, `01-CONTEXT.md`, `REQUIREMENTS.md` — SPEC contract

### Secondary (MEDIUM confidence)
- `go list -m -versions golang.org/x/mod` (this session) → v0.36.0 available — semver lib version
- go-github v66 / glab v0.115 method names (release/compare/contents/PR) — capability exists; exact signatures to confirm via `go doc` at implementation (A2/A3)

### Tertiary (LOW confidence)
- None — every architectural claim is grounded in read source.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all deps but one already in go.mod; the one new (`golang.org/x/mod`) is an official module verified via proxy.
- Architecture: HIGH — every pattern has a cited working precedent in the review pipeline.
- Pitfalls: HIGH — derived from actual code discrepancies (xanzy path, config-from-ref, exported-docstring lint).
- Exact provider method signatures (A2/A3): MEDIUM — confirm via `go doc` at implementation.

**Research date:** 2026-06-04
**Valid until:** 2026-07-04 (stable codebase; refresh if go-github/glab major bump or marker format changes)
