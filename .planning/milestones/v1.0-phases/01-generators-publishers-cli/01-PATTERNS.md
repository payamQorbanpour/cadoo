# Phase 1: Generators + Publishers + CLI - Pattern Map

**Mapped:** 2026-06-05
**Files analyzed:** 22 new/modified files
**Analogs found:** 20 / 22 (2 net-new patterns with no codebase analog)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/releasedocs/releasedocs.go` | model (types/interfaces) | transform | `internal/tools/tools.go` | role-match |
| `internal/releasedocs/dispatcher.go` | service (orchestration) | request-response | `internal/orchestrator/reviewer.go` | exact |
| `internal/releasedocs/registry.go` | config (wiring) | transform | `internal/orchestrator/registry.go` | exact |
| `internal/releasedocs/context.go` | service (builder + grouped model) | transform | `internal/orchestrator/reviewer.go` (Run context build §183-277) | role-match |
| `internal/releasedocs/marker.go` | utility | transform | `internal/orchestrator/consolidate.go` + `internal/vcs/marker.go` | exact |
| `internal/releasedocs/generators/changelog/changelog.go` | generator (deterministic) | transform | `internal/orchestrator/consolidate.go` (render) + nil-LLM from `tools` | partial (deterministic render is new) |
| `internal/releasedocs/generators/releasenotes/releasenotes.go` | generator (LLM narrative) | transform | `internal/tools/changelog/changelog.go` (LLM-call shape only) | role-match |
| `internal/releasedocs/publishers/releasebody/releasebody.go` | publisher | request-response | `internal/orchestrator/reviewer.go:applyPRBody` + `consolidate.go:spliceCadooBody` | exact |
| `internal/releasedocs/publishers/changelogpr/changelogpr.go` | publisher | request-response | `internal/orchestrator/reviewer.go:postSummary` (read-back→update-else-create) | role-match |
| `internal/releasedocs/template/template.go` | utility (embed + loader) | file-I/O | **none** (no `go:embed`/`text/template` in repo) | no analog |
| `internal/releasedocs/template/presets/*.tmpl` | config (asset) | file-I/O | **none** | no analog |
| `internal/vcs/vcs.go` (modify: add capability ifaces + types) | model (interface) | — | `internal/vcs/vcs.go:PriorReviewReader` (§128-135) | exact |
| `internal/vcs/github/release.go` (new methods on `*Adapter`) | middleware (adapter) | request-response | `internal/vcs/github/github.go:FetchFileFromRef` (§393-415) | exact |
| `internal/vcs/gitlab/release.go` (new methods on `*Adapter`) | middleware (adapter) | request-response | `internal/vcs/gitlab/gitlab.go:FetchFileFromRef` (§350-359) | exact |
| `internal/config/config.go` (modify: add `ReleaseDocs`) | config | transform | `internal/config/config.go:Repo` (§16-46) | exact |
| `cmd/cadoo-cli/releasedocs.go` | route (CLI subcommand) | request-response | `cmd/cadoo-cli/ci.go` | exact |
| `cmd/cadoo-cli/main.go` (modify: add switch case) | route | request-response | `cmd/cadoo-cli/main.go:33` switch | exact |
| `internal/releasedocs/*_test.go` (multiple) | test | — | `internal/vcs/github/github_test.go` (httptest convention) | role-match |
| `internal/releasedocs/generators/changelog/testdata/*.golden` | test (fixture) | file-I/O | **none** (no golden convention in repo) | no analog |

## Pattern Assignments

### `internal/releasedocs/releasedocs.go` (types/interfaces)

**Analog:** `internal/tools/tools.go` (interface + `Input`/`Result` + `Registry` shape; do NOT reuse the types themselves — D-01 forbids extending `tools.*`).

**Interface declaration pattern** (`tools.go:103-133`):
```go
// Tool is one Cadoo command.
type Tool interface {
	Name() string
	Run(ctx context.Context, in Input) (*Result, error)
}

// Registry holds the tools the orchestrator can dispatch to.
type Registry struct{ m map[string]Tool }
func NewRegistry() *Registry { return &Registry{m: map[string]Tool{}} }
func (r *Registry) Register(t Tool) { r.m[t.Name()] = t }
func (r *Registry) Get(name string) (Tool, bool) { t, ok := r.m[name]; return t, ok }
```
Mirror this for `Generator` / `Publisher`. Per D-03 the signatures are `Generator.Kind()/Enabled(cfg,bump)/Generate(ctx,rc)` and `Publisher.Target()/Publish(ctx,rc,arts)`. Per D-04 `ReleaseContext` carries `repo, org, from/to tags, Bump, []vcs.Commit, []vcs.MergedPR, config.ReleaseDocs, vcs.Provider, llm.Provider (nil-tolerant)`.

**Docstring discipline** — every exported symbol needs a comment (`exported` revive rule, Pitfall 4). Note `tools.go:104` style: short, imperative.

---

### `internal/releasedocs/dispatcher.go` (service, request-response)

**Analog:** `internal/orchestrator/reviewer.go`

**Dispatcher struct + nil-tolerant fields** (`reviewer.go:80-133`):
```go
type Dispatcher struct {
	LLM      llm.Provider
	VCSPool  map[vcs.Kind]vcs.Provider
	Model    string
	BaseCfg  config.Repo
	Registry *tools.Registry
	// ... optional fields commented as nil-tolerant
}
```
`releasedocs.Dispatcher` mirrors this: `VCSPool map[vcs.Kind]vcs.Provider`, `LLM llm.Provider`, `Generators`/`Publishers` (or a `Registry`). Reuse the SAME `VCSPool` type (D-05).

**Provider resolution from pool** (`reviewer.go:168-176`):
```go
provider, ok := d.VCSPool[job.Provider]
if !ok {
	return fmt.Errorf("no adapter for provider %q (configured: %v)", job.Provider, configuredKinds(d.VCSPool))
}
```

**Run entry point + default-fill** (`reviewer.go:144-152`):
```go
func (d *Dispatcher) Run(ctx context.Context, job ToolJob) (retErr error) {
	if job.Tool == "" { job.Tool = "review" }
	if job.Provider == "" { job.Provider = vcs.KindGitHub }
	...
}
```
`ReleaseJob.Kind()` mirrors `ToolJob.Kind()` (`reviewer.go:77-78`) for the later River path (Phase 2), but Phase 1 only calls `Run` directly.

**Config-from-ref load (use ToRef, never main — D-06, Pitfall 2)** — copy `loadCfg` exactly (`reviewer.go:505-525`), changing the ref from `pr.HeadSHA` to `job.ToRef`:
```go
ff, ok := provider.(FileFetcher)        // FileFetcher already on both adapters
if !ok || pr.HeadSHA == "" { return d.BaseCfg }
raw, err := ff.FetchFileFromRef(ctx, pr.RepoFullName, pr.HeadSHA, ".cadoo.yaml")
if err != nil {
	if !isMissingFile(err) { slog.Debug("load .cadoo.yaml failed; using base config", "err", err) }
	return d.BaseCfg
}
cfg, err := config.Parse(raw)
```
Also copy `isMissingFile` (`reviewer.go:527-536`) — the 404-tolerant fallback. NOTE: `FileFetcher` is declared in `orchestrator` (`reviewer.go:59-63`); the releasedocs dispatcher should declare its own identical interface or move it to `vcs` (the new capability ifaces live in `vcs` per the architecture rule).

**Type-assert optional capability, degrade if absent** (`reviewer.go:208`, mirror for each new capability):
```go
if ff, ok := provider.(FileFetcher); ok && pr.HeadSHA != "" { ... }
```
Apply to `ReleaseRangeReader` / `ReleasePublisher` / `BranchCommitter`: `if c, ok := provider.(vcs.ReleaseRangeReader); ok { ... } else { slog.Warn("capability absent; skipping", ...) }` (D-15, anti-pattern: never assume present).

**Per-generator gate (never run a disabled generator — D-08)** — the dispatcher loop calls `gen.Enabled(cfg, bump)` before `Generate`, mirroring how `ci.go` validates tools up-front.

---

### `internal/releasedocs/registry.go` (config wiring)

**Analog:** `internal/orchestrator/registry.go` (exact)

**Full pattern** (`registry.go:20-38`):
```go
// DefaultRegistry returns a Registry with every Cadoo built-in tool
// registered. Cmd binaries call this at startup; tests can build their own.
func DefaultRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(review.Tool{})
	r.Register(describe.Tool{})
	...
	return r
}
```
For releasedocs: `DefaultRegistry()` (or two: `DefaultGenerators()`/`DefaultPublishers()`) registers `changelog.Generator{}`, `releasenotes.Generator{}`, `releasebody.Publisher{}`, `changelogpr.Publisher{}`. Import each subpackage exactly as `registry.go:5-17` does.

---

### `internal/releasedocs/context.go` (service, builder + grouped change model)

**Analog:** `internal/orchestrator/reviewer.go` context-build region (`Run` §183-277) — the "build one packed input passed to every unit" shape.

**Build-once-pass-everywhere pattern** (`reviewer.go:199-207`):
```go
in := tools.Input{ PR: pr, Files: files, Packed: packed, Config: cfg, LLM: d.LLM, Model: model, ... }
```
`ReleaseContext` is the analog: built once, passed to every generator (D-04). Grouped change model is built once here too (D-09) — pure transform over `[]vcs.Commit`/`[]vcs.MergedPR`, no I/O.

**Deterministic ordering (Pitfall 3)** — when building the grouped model, sort sections in a fixed order. Mirror the `sort.SliceStable` discipline at `consolidate.go:70-80`:
```go
sort.SliceStable(sections, func(i, j int) bool { ... })
```
Use `grouping.sections` config order as canonical; never rely on Go map iteration order (golden tests would flake).

**Semver bump** — `golang.org/x/mod/semver` (NEW dep, `go get golang.org/x/mod/semver@v0.36.0`): `semver.Compare`, `semver.Major`, `semver.MajorMinor`. No codebase analog (no semver lib today). Do NOT hand-roll `vX.Y.Z` math (Don't Hand-Roll table).

**Conventional-Commit parser** — hand-rolled `strings.HasPrefix`/`strings.Cut` for `feat:`/`fix:`/`perf:`/`feat!:`/`BREAKING CHANGE` (A5; ~30 lines). No analog; golden-file-testable.

---

### `internal/releasedocs/marker.go` (utility)

**Analog:** `internal/orchestrator/consolidate.go` (splice) + `internal/vcs/marker.go` (constants/grep). Do NOT reinvent the format (anti-pattern, Runtime State Inventory: lock marker strings from day one).

**Marker constants pattern** (`consolidate.go:16-21`, `vcs/marker.go:14`):
```go
const SummaryWrapperBegin = "<!-- cadoo:wrapper:begin -->"
const (
	wrapperBegin   = vcs.SummaryWrapperBegin
	prSectionBegin = "<!-- cadoo:pr-body:begin -->"
	prSectionEnd   = "<!-- cadoo:pr-body:end -->"
)
```
Declare release-docs constants the same way (D-12/D-13): `<!-- cadoo:release-notes:begin -->` / `:end`, and a per-version `<!-- cadoo:changelog:vX.Y.Z -->` plus branch `cadoo/changelog/vX.Y.Z`.

**Splice/wrap helper (replace-inner-else-append)** — copy `spliceCadooBody` (`consolidate.go:111-125`):
```go
func spliceCadooBody(original, section string) string {
	section = strings.TrimSpace(section)
	startIdx := strings.Index(original, prSectionBegin)
	endIdx := strings.Index(original, prSectionEnd)
	if startIdx >= 0 && endIdx > startIdx {            // already managed: replace inner
		head := strings.TrimRight(original[:startIdx], " \n\t")
		tail := original[endIdx+len(prSectionEnd):]
		return joinBody(head, section, tail)
	}
	return joinBody(strings.TrimRight(original, " \n\t"), section, "")  // first write: append
}
```
Plus `joinBody` (`consolidate.go:149-165`) for user-content preservation. The release-notes marker version uses `release-notes:begin/:end`.

**Single-marker grep (changelog PR keyed on ToRef)** — mirror `strings.Contains(body, vcs.SummaryWrapperBegin)` (`github.go:307`) with `strings.Contains(prBody, "<!-- cadoo:changelog:"+toRef+" -->")`.

---

### `internal/releasedocs/generators/changelog/changelog.go` (generator, deterministic)

**Analog:** rendering structure from `internal/orchestrator/consolidate.go:renderConsolidated/renderSection` (§66-105); nil-LLM tolerance from the `tools.Input.LLM` convention. **Do NOT reuse `internal/tools/changelog`** — it is PR-scoped + LLM-only, wrong shape (D-01, State of the Art).

**Deterministic render (strings.Builder, fixed order)** (`consolidate.go:82-91`):
```go
var b strings.Builder
b.WriteString(wrapperBegin)
b.WriteString("\n## ... \n\n")
for _, s := range sections {            // sections already sorted upstream
	b.WriteString(renderSection(s))
	b.WriteString("\n")
}
```

**nil-LLM tolerance (the golden-test enabler — D-10, Pitfall 3)**:
```go
// if rc.LLM == nil: skip the polish call, return deterministic render verbatim.
```
The deterministic render must be a pure function of the grouped model; LLM polish is a separate, skippable step. Golden tests run with `rc.LLM == nil`.

---

### `internal/releasedocs/generators/releasenotes/releasenotes.go` (generator, LLM narrative)

**Analog:** `internal/llm/provider.go:Provider.Chat` (§71-73) for the call shape; `internal/tools/changelog/changelog.go` only for "build a system prompt + call Chat" structure (NOT its PR-diff input).

**LLM provider interface (one method, nil-tolerant — D-04, D-11)** (`provider.go:69-73`):
```go
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}
```
Build deterministic highlight skeleton first; if `rc.LLM != nil`, call `rc.LLM.Chat` for a `tone`-aware narrative (`concise|detailed|marketing`); if nil, return the skeleton only. Pass `rc.Model` through (no second default-model path in Go — D-17, anti-pattern).

---

### `internal/releasedocs/publishers/releasebody/releasebody.go` (publisher, request-response)

**Analog:** `internal/orchestrator/reviewer.go:applyPRBody` (§382-393) — the splice-then-update-if-changed idempotency shape.

**Read-current → splice → no-op-if-unchanged → write** (`reviewer.go:384-392`):
```go
newBody := spliceCadooBody(pr.Body, section)
if newBody == pr.Body { return nil }      // idempotent: nothing to do
return provider.EditPullRequestBody(ctx, pr, newBody)
```
For releasebody: type-assert `vcs.ReleasePublisher`, call `GetRelease` (read current body), splice with the `release-notes:begin/:end` markers (preserve user content outside — D-12), then `UpdateReleaseBody` only if changed. Stateless: state is reconstructed from the marker in the live release body (D-14).

---

### `internal/releasedocs/publishers/changelogpr/changelogpr.go` (publisher, request-response)

**Analog:** `internal/orchestrator/reviewer.go:postSummary` (§341-380) — the "read-back prior ID → update-else-create" idempotency primitive (Don't Hand-Roll). And `cmd/cadoo-cli/ci.go:priorStore` (§245-259) for stateless read-back-on-degrade.

**Read-back → update-else-create** (`reviewer.go:364-379`):
```go
existing, err := d.Posted.SummaryID(ctx, key, findings.WrapperToolKey)
if err == nil && existing != "" {
	if err := provider.UpdateSummaryComment(ctx, pr, existing, rendered); err == nil { return }
	// fall through to create-new on edit failure (may have been deleted)
}
id, err := provider.PostSummaryComment(ctx, pr, rendered)
```
For changelogpr: list open PRs (or fetch by deterministic branch `cadoo/changelog/vX.Y.Z`), grep for `<!-- cadoo:changelog:vX.Y.Z -->` (D-13). If found → update via `BranchCommitter`; else create branch + open PR. Single-PR invariant guarded by marker key + deterministic branch (Security: prevents PR spam).

**Stateless read-back, degrade on error** (`ci.go:249-258`):
```go
snap, err := r.ListCadooArtifacts(ctx, pr)
if err != nil { slog.Warn("...read-back failed; ...may duplicate this run", "err", err); return nil }
```

---

### `internal/releasedocs/template/template.go` (utility, embed + loader)

**Analog:** NONE — `text/template` and `go:embed` have zero usage in the repo (Standard Stack, Wave 0 Gaps). This is net-new infrastructure.

**Use RESEARCH.md guidance + stdlib docs.** D-07: embedded presets are the default; a repo `template:` key loads an override via `FileFetcher.FetchFileFromRef(ctx, repo, ToRef, <path>)` (same call used for config). Use `text/template` (NOT `html/template` — output is markdown; Security V5). NO custom `FuncMap` exposing `os`/`exec` (Security threat: template EoP). Templates receive `ReleaseContext` + grouped model as data.

**Embed pattern (new):**
```go
//go:embed presets/*.tmpl
var presetFS embed.FS
```

---

### `internal/vcs/vcs.go` (modify: add capability interfaces + types)

**Analog:** `internal/vcs/vcs.go:PriorReviewReader` (§128-135) — exact template for an optional capability.

**Optional capability interface declaration** (`vcs.go:128-135`):
```go
// PriorReviewReader is an OPTIONAL capability. ... The orchestrator
// type-asserts for it; providers that don't implement it fall back ...
type PriorReviewReader interface {
	ListCadooArtifacts(ctx context.Context, pr *PullRequest) (PriorReview, error)
}
```
Add three siblings (D-15): `ReleaseRangeReader`, `ReleasePublisher`, `BranchCommitter`, each documented "OPTIONAL capability ... type-asserted ... degrades gracefully".

**New normalized types** — mirror the `PullRequest`/`FileChange` field style (`vcs.go:23-49`). Open Question 2 recommends: `vcs.Commit{SHA, Message, Author, Date}`, `vcs.MergedPR{Number, Title, Body, Author, Labels[], MergedAt, MergeSHA}`, `vcs.Release{ID, TagName, Body, Draft, Prerelease}`, `vcs.FileWrite{Path, Content[], Mode}` (final field set is Claude's Discretion).

---

### `internal/vcs/github/release.go` (new methods on `*Adapter`)

**Analog:** `internal/vcs/github/github.go:FetchFileFromRef` (§393-415) — method on `*Adapter`, `splitRepo`, error-wrap style.

**Method shape + helpers** (`github.go:393-415`):
```go
func (a *Adapter) FetchFileFromRef(ctx context.Context, repo, ref, path string) ([]byte, error) {
	owner, name, err := splitRepo(repo)            // splitRepo at github.go:467
	if err != nil { return nil, err }
	file, _, _, err := a.client.Repositories.GetContents(ctx, owner, name, path,
		&gogithub.RepositoryContentGetOptions{Ref: ref})
	if err != nil { return nil, fmt.Errorf("get contents %s@%s:%s: %w", repo, ref, path, err) }
	...
}
```
`a.client` is `*gogithub.Client` (`github.go:41`, built in `New`). New methods use (signatures [ASSUMED] — confirm via `go doc github.com/google/go-github/v66/github`, A2): `Repositories.CompareCommits`, `PullRequests.List`, `Repositories.GetReleaseByTag`, `Repositories.EditRelease`, `Repositories.CreateFile`/`UpdateFile`, `PullRequests.Create`/`Edit`. All REST, no GraphQL (Pitfall 5, Open Question 3).

**Capability assertion line** (`github.go:526`):
```go
var _ vcs.Provider = (*Adapter)(nil)
```
Add: `var _ vcs.ReleaseRangeReader = (*Adapter)(nil)` etc. — compile-time proof the adapter satisfies each new capability.

**Marker grep for read-back** (`github.go:307`): `strings.Contains(c.Body, vcs.SummaryWrapperBegin)` — same shape for the changelog marker.

---

### `internal/vcs/gitlab/release.go` (new methods on `*Adapter`)

**Analog:** `internal/vcs/gitlab/gitlab.go:FetchFileFromRef` (§350-359) + `New` (§31-42).

**glab import path (Pitfall 1 — CRITICAL):** import `glab "gitlab.com/gitlab-org/api/client-go"` (`gitlab.go:14`), NOT `xanzy/go-gitlab` (the `go.mod` line is stale/aliased). All calls take `glab.WithContext(ctx)` as the trailing arg (house style).

**Method shape** (`gitlab.go:350-359`):
```go
func (a *Adapter) FetchFileFromRef(ctx context.Context, repo, ref, path string) ([]byte, error) {
	data, _, err := a.client.RepositoryFiles.GetRawFile(repo, path,
		&glab.GetRawFileOptions{Ref: ptr(ref)}, glab.WithContext(ctx))
	if err != nil { return nil, fmt.Errorf("get raw file %s@%s:%s: %w", repo, ref, path, err) }
	return data, nil
}
```
`a.client` is `*glab.Client` (`gitlab.go:28`). New methods (signatures [ASSUMED] — confirm via `go doc`, A3): `Repositories.Compare`, `Commits.ListCommits`, `MergeRequests.ListProjectMergeRequests`, `Releases.GetRelease`/`UpdateRelease`, `RepositoryFiles.CreateFile`/`UpdateFile`, `MergeRequests.CreateMergeRequest`. Add `var _ vcs.ReleaseRangeReader = (*Adapter)(nil)` (mirror `gitlab.go:580`).

---

### `internal/config/config.go` (modify: add `ReleaseDocs`)

**Analog:** `internal/config/config.go:Repo` (§16-46) — extend the struct; one parser (`Parse`, §150-159) already handles it.

**Add a field with yaml tag** (mirror `config.go:19-23`):
```go
type Repo struct {
	...
	ReleaseDocs ReleaseDocs `yaml:"releaseDocs"`
}
```
Then add the `ReleaseDocs` struct + nested types (`enabled`, `trigger`, `tagPattern`, `artifacts.changelog`, `artifacts.releaseNotes`, `grouping`, `publish.releaseBody`, `publish.changelogPR` — SPEC §3 / CONS-releasedocs-config-schema). Phase-2/3 keys (`blog`, `apiDocs`, `publish.pages`) may exist in the struct but stay unwired. Follow the per-field-docstring style of `CommentPolicy` (§55-73). No new parser — `config.Parse` covers it.

---

### `cmd/cadoo-cli/releasedocs.go` (CLI subcommand)

**Analog:** `cmd/cadoo-cli/ci.go` (exact — copy the whole flow).

**Flag set** (`ci.go:123-148`):
```go
fs := flag.NewFlagSet("release-docs", flag.ExitOnError)
fs.StringVar(&targetURL, "repo", "", "...")   // + --from / --to / --pr-host / --mr form
```
Flag→`ReleaseJob` mapping is Claude's Discretion (D-16). Reuse `parseTargetURL` (`ci.go:52`) for URL validation (Security V5).

**Build stateless dispatcher + one-entry pool** (`ci.go:176-191`):
```go
d := &orchestrator.Dispatcher{       // analog: releasedocs.Dispatcher
	LLM:     litellm.New(llmURL, llmKey),
	VCSPool: map[vcs.Kind]vcs.Provider{target.Provider: provider},
	Model:   model,
	...
}
```
Reuse `buildProvider` (`ci.go:263-295`) and the env contract verbatim (Runtime State: NO new secret names) — `GITHUB_TOKEN`/`GITLAB_TOKEN`, `LLM_GATEWAY_URL`/`LLM_GATEWAY_API_KEY`, `CADOO_DEFAULT_MODEL` (`ci.go:156-158`, `:266`, `:279`).

**litellm constructor** (`litellm/client.go:29`): `litellm.New(baseURL, apiKey) *Client`.

---

### `cmd/cadoo-cli/main.go` (modify: add switch case)

**Analog:** `cmd/cadoo-cli/main.go:33-49` (exact).

**Add a case** (`main.go:41-42`):
```go
case "ci":
	ciCmd(os.Args[2:])
// add:
case "release-docs":
	releaseDocsCmd(os.Args[2:])
```
Also add a line to `usage()` (`main.go:54-65`).

---

## Shared Patterns

### Optional capability interface (type-assert + graceful degradation)
**Source:** `internal/vcs/vcs.go:128-135` (declaration) + `internal/orchestrator/reviewer.go:208` / `cmd/cadoo-cli/ci.go:187` (assertion).
**Apply to:** dispatcher (all three new release capabilities), both adapters.
```go
type PriorReviewReader interface {
	ListCadooArtifacts(ctx context.Context, pr *PullRequest) (PriorReview, error)
}
// call site:
if rr, ok := provider.(vcs.PriorReviewReader); ok { ... } // else: log skip reason, degrade
```
Each adapter adds `var _ vcs.ReleaseRangeReader = (*Adapter)(nil)` (compile-time check, mirror `github.go:526` / `gitlab.go:580`).

### Marker splice / wrap (do NOT reinvent)
**Source:** `internal/orchestrator/consolidate.go:111-165` (`spliceCadooBody`/`joinBody`) + `internal/vcs/marker.go:14`.
**Apply to:** `releasedocs/marker.go`, releasebody publisher, changelogpr publisher.
Replace-inner-else-append; preserve user content outside markers; constants locked from day one (Runtime State Inventory).

### Stateless read-back-then-decide idempotency (no DB)
**Source:** `internal/orchestrator/reviewer.go:341-380` (`postSummary`) + `cmd/cadoo-cli/ci.go:245-259` (`priorStore`) + `internal/findings/prior.go:24` (`NewFromPrior`).
**Apply to:** both publishers.
"Read marker back from provider → update-else-create"; degrade (log, non-idempotent) when read fails. Phase-1 has NO `findings.Store` equivalent — each publisher reconstructs its own state inline (D-14).

### Config-from-ref load (use ToRef, never main)
**Source:** `internal/orchestrator/reviewer.go:505-536` (`loadCfg` + `isMissingFile`).
**Apply to:** dispatcher step 3.
`ff.FetchFileFromRef(ctx, repo, job.ToRef, ".cadoo.yaml")` → `config.Parse` → 404-tolerant fallback. `FileFetcher` is already implemented on both adapters (`github.go:397`, `gitlab.go:352`). Pitfall 2.

### nil-tolerant LLM
**Source:** `internal/llm/provider.go:71-73` (one-method interface) + `internal/llm/litellm/client.go:29` (constructor).
**Apply to:** both generators via `ReleaseContext.LLM`.
`if rc.LLM == nil` → deterministic path only (changelog: render verbatim; release-notes: skeleton only). Enables golden-file tests. No second default-model path in Go (D-17).

### Deterministic ordering for golden tests
**Source:** `internal/orchestrator/consolidate.go:70-80` (`sort.SliceStable`).
**Apply to:** grouped change model, changelog render.
Never rely on Go map iteration; sort by canonical `grouping.sections` order. Pitfall 3.

### Exported-symbol docstrings
**Source:** repo-wide convention (every exported symbol in the analogs has a `//` comment).
**Apply to:** every new exported type/method. `exported` revive rule is ON (Pitfall 4); `package_comments` and `unused-parameter` are OFF. `goimports` local-prefix `github.com/payamqorbanpour/cadoo` → cadoo-internal imports are the third group.

## No Analog Found

| File | Role | Data Flow | Reason | Guidance |
|------|------|-----------|--------|----------|
| `internal/releasedocs/template/template.go` | utility | file-I/O | No `text/template` or `go:embed` anywhere in the repo | Use stdlib + RESEARCH.md (D-07); `text/template` not `html/template`; no OS-exposing `FuncMap` |
| `internal/releasedocs/template/presets/*.tmpl` | config asset | file-I/O | No template assets exist | New; embed via `//go:embed presets/*.tmpl` |
| `internal/releasedocs/generators/changelog/testdata/*.golden` | test fixture | file-I/O | Repo has NO `testdata/`/golden-file convention today | This phase establishes it (Wave 0); golden render with `rc.LLM == nil` |
| Shared fake `vcs.Provider` (test helper) | test | — | No interface-fake helper exists; adapter tests use `httptest.NewServer` only | Build a releasedocs-local fake implementing `vcs.Provider` + the 3 new capabilities, with toggles to omit a capability (degradation tests) |

**Net-new dependency:** `golang.org/x/mod/semver@v0.36.0` (`go get` + `go mod tidy`). Official Go module; no slopcheck concern. Hand-rolled Conventional-Commit parser (no library).

## Metadata

**Analog search scope:** `internal/orchestrator/`, `internal/vcs/` (+ github/gitlab adapters), `internal/config/`, `internal/tools/`, `internal/llm/`, `internal/findings/`, `cmd/cadoo-cli/`
**Files scanned:** 13 analog files read in full or targeted ranges
**Pattern extraction date:** 2026-06-05
