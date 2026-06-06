---
phase: 01-generators-publishers-cli
verified: 2026-06-05T20:00:00Z
status: human_needed
score: 5/6 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Dogfood end-to-end run on Cadoo's own repository"
    expected: "cadoo release-docs --repo payamqorbanpour/cadoo --from <prevTag> --to <tag> produces a grouped changelog PR and (if release exists) updates the release body inside markers; second identical run edits in place, no duplicates"
    why_human: "Requires a live GITHUB_TOKEN and real VCS side-effects; automated fake-provider tests cannot substitute for the stateless marker reconstruction path on live GitHub. Task 3 of Plan 07 was declared operator-approved-skip in the SUMMARY, which means SC-6 (dogfooded end-to-end) has not been observed."
  - test: "GitLab release-body publishing does not hard-fail"
    expected: "Running cadoo release-docs against a GitLab repo with publish.releaseBody.enabled:true should either succeed (update the release body) or skip with a logged warning, not exit 1"
    why_human: "CR-01 from the code review (01-REVIEW.md) documents that gitlab.UpdateReleaseBody unconditionally returns an error while still satisfying vcs.ReleasePublisher, causing hard failure at the publisher level. The fix requires an interface change or GitLab-specific path. No test exercises GitLab + releasebody; the defect is confirmed in code."
---

# Phase 01: Generators + Publishers + CLI — Verification Report

**Phase Goal:** Ship the Release Docs subsystem — generators, publishers, CLI command — so `cadoo release-docs` can produce and publish a changelog PR and release body for any tag range, with deterministic-first output and idempotent re-runs.
**Verified:** 2026-06-05T20:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| SC-1 | Running `cadoo release-docs --repo … --from vX --to vY` produces a grouped changelog section (Features/Fixes/Breaking/…) and polished release notes | ✓ VERIFIED | `cadoo release-docs -h` shows `--from`/`--to` flags; dispatcher wires generators (changelog + releasenotes); 26 generator tests pass; golden file `basic.golden` is 18 lines of grouped markdown |
| SC-2 | The release body is updated inside Cadoo markers (user content preserved) and a single `CHANGELOG.md` PR is opened or updated | ✓ VERIFIED | `releasebody.Publisher` splices via `SpliceReleaseBody` (no-op when unchanged); `changelogpr.Publisher` uses `ChangelogBranch(toRef)` deterministic branch; 5 publisher tests pass. CR-01 (GitLab hard-fail) is a WARNING but does not block GitHub/GHES use cases which are the primary target |
| SC-3 | Re-running edits the release body in place and updates the same changelog PR/branch — no duplicates — with no database (stateless marker reconstruction) | ✓ VERIFIED | `TestIdempotentTwiceRun` and `TestSinglePR` pass; `releasebody.Publish` no-op guard on unchanged body confirmed in code; idempotency owned by stateless marker splice + deterministic branch |
| SC-4 | A disabled artifact (`enabled:false`) or one whose `when:` excludes the bump is never generated; the changelog runs with LLM off and produces reproducible output | ✓ VERIFIED | `TestDisabledNotGenerated` passes; dispatcher's `gen.Enabled(cfg.ReleaseDocs, rc.Bump)` gate confirmed in code; golden-file test with nil LLM confirms byte-stable output |
| SC-5 | An artifact's `template:` override (loaded from the tag tree) replaces the preset; with no override, embedded preset templates are used | ✓ VERIFIED | `template.Resolve` fetches from `rc.ToRef` via `releasedocs.FileFetcher`; `TestTemplateOverride` 4 subtests pass; `//go:embed presets/*.tmpl` confirmed; no OS-exposing FuncMap |
| SC-6 | The flow is dogfooded end-to-end on Cadoo's own repository | ? UNCERTAIN (human needed) | Plan 07 Task 3 was a `checkpoint:human-verify` gate. SUMMARY declares "operator-approved skip." No live GitHub run was observed or recorded. |

**Score:** 5/6 truths verified (SC-6 requires human verification)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/releasedocs/releasedocs.go` | Core types + Generator/Publisher interfaces + FileFetcher | ✓ VERIFIED | Contains `type Generator interface`, `type FileFetcher interface`, `type Publisher interface`, `ReleaseContext`, `ReleaseJob`, enums |
| `internal/vcs/vcs.go` | ReleaseRangeReader/ReleasePublisher/BranchCommitter + Commit/MergedPR/Release/FileWrite | ✓ VERIFIED | All 3 capability interfaces present; all 4 normalized types present |
| `internal/config/config.go` | config.ReleaseDocs schema field on Repo | ✓ VERIFIED | `ReleaseDocs ReleaseDocs \`yaml:"releaseDocs"\`` confirmed; full Phase-1 schema present |
| `internal/releasedocs/marker.go` | Locked marker constants + splice/parse helpers | ✓ VERIFIED | `ReleaseNotesBegin/End` constants; `SpliceReleaseBody`; `HasChangelogMarker`; `ChangelogMarker`; `ChangelogBranch` |
| `internal/releasedocs/releasedocstest/fake.go` | Shared importable fake provider (exported Fake type, regular .go file) | ✓ VERIFIED | Package is `releasedocstest`, not `_test.go`; `type Fake struct` exported; functional options pattern |
| `internal/releasedocs/changemodel.go` | Grouped change model + conventional/labels grouping + bump computation | ✓ VERIFIED | `ComputeBump`, `BuildGroupedModel`, `classifyCommit`; semver.IsValid/Canonical used |
| `internal/releasedocs/context.go` | ReleaseContext builder from range read + Enabled gate helper | ✓ VERIFIED | `BuildContext` function; type-asserts `vcs.ReleaseRangeReader` |
| `internal/releasedocs/changemodel_test.go` | TestGroupedModel | ✓ VERIFIED | `TestGroupedModel` test function exists; 52 tests across packages pass |
| `internal/releasedocs/context_test.go` | TestBump + TestEnabledMatrix | ✓ VERIFIED | `TestBump`, `TestEnabledMatrix` test functions exist |
| `internal/releasedocs/template/template.go` | go:embed preset loader + override resolver + render | ✓ VERIFIED | `//go:embed presets/*.tmpl`; `Resolve`; `Render`; `LoadPreset` |
| `internal/releasedocs/template/presets/changelog.tmpl` | Default changelog preset (keep-a-changelog style) | ✓ VERIFIED | 9 lines; grouped sections template |
| `internal/releasedocs/template/template_test.go` | TestEmbeddedPresets + TestTemplateOverride | ✓ VERIFIED | Both test functions exist; 11 tests pass |
| `internal/vcs/github/release.go` | GitHub adapter release/range/branch-commit capability methods | ✓ VERIFIED | `var _ vcs.ReleaseRangeReader = (*Adapter)(nil)` and 2 more; 13 httptest tests |
| `internal/vcs/gitlab/release.go` | GitLab adapter release/range/branch-commit capability methods | ✓ VERIFIED | 3 compile-time assertions; glab correct import path; **CR-01 WARNING: UpdateReleaseBody always returns error** |
| `internal/vcs/github/release_test.go` | httptest-stubbed capability tests | ✓ VERIFIED | `net/http/httptest` imported; tests pass |
| `internal/vcs/gitlab/release_test.go` | httptest-stubbed capability tests | ✓ VERIFIED | `net/http/httptest` imported; tests pass |
| `internal/releasedocs/generators/changelog/changelog.go` | Deterministic-first changelog Generator | ✓ VERIFIED | `func (g *Generator) Generate` exists; `rc.LLM` nil guard (7 occurrences) |
| `internal/releasedocs/generators/changelog/testdata/basic.golden` | First golden-file fixture in the repo | ✓ VERIFIED | 18 lines; grouped markdown with Breaking Changes/Features/Bug Fixes/Performance sections |
| `internal/releasedocs/generators/releasenotes/releasenotes.go` | Release-notes Generator (skeleton + LLM narrative) | ✓ VERIFIED | `func (g *Generator) Generate` exists; `rc.LLM` nil guard |
| `internal/releasedocs/publishers/releasebody/releasebody.go` | Marker-wrapped release-body upsert Publisher | ✓ VERIFIED | `func (Publisher) Publish` exists; `SpliceReleaseBody` call; no-op guard when body unchanged |
| `internal/releasedocs/publishers/changelogpr/changelogpr.go` | Single marker-keyed CHANGELOG.md PR Publisher | ✓ VERIFIED | `func (Publisher) Publish` exists; `ChangelogBranch`/`ChangelogMarker` used |
| `internal/releasedocs/publishers/changelogpr/changelogpr_test.go` | TestSinglePR (open-then-update) | ✓ VERIFIED | `TestSinglePR` test function exists; 2 occurrences (function + subtest) |
| `internal/releasedocs/dispatcher.go` | Dispatcher.Run(ctx, ReleaseJob) single entry point | ✓ VERIFIED | `func (d *Dispatcher) Run` exists; FetchFileFromRef from ToRef confirmed |
| `internal/releasedocs/registry.go` | DefaultGenerators/DefaultPublishers wiring of built-ins | ⚠️ PARTIAL | File exists but is a documentation stub — actual wiring in `internal/releasedocs/defaults/defaults.go` due to import cycle. Plan's `contains: "changelog"` passes (comment reference). WR-03: unused `GeneratorRegistry`/`PublisherRegistry` map types with non-deterministic `Generators()`/`Publishers()` iteration order present in `releasedocs.go` |
| `internal/releasedocs/defaults/defaults.go` | Cycle-free wire package (not in plan but necessary) | ✓ VERIFIED | `DefaultGenerators()` returns changelog+releasenotes; `DefaultPublishers()` returns releasebody+changelogpr |
| `cmd/cadoo-cli/releasedocs.go` | release-docs CLI subcommand (stateless, one-entry pool) | ✓ VERIFIED | `flag.NewFlagSet("release-docs", ...)` with --from/--to/--repo flags; `releasedocs.Dispatcher` wired |
| `internal/releasedocs/dispatcher_test.go` | TestIdempotentTwiceRun + TestGracefulDegradation | ✓ VERIFIED | Both test functions exist; 5 dispatcher integration tests pass |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/releasedocs/releasedocs.go` | `internal/vcs Commit/MergedPR types` | ReleaseContext struct fields | ✓ WIRED | `Commits []vcs.Commit`, `MergedPRs []vcs.MergedPR` confirmed |
| `internal/config/config.go` | Repo struct | ReleaseDocs field with yaml tag | ✓ WIRED | `ReleaseDocs ReleaseDocs \`yaml:"releaseDocs"\`` confirmed |
| `internal/releasedocs/context.go` | `vcs.ReleaseRangeReader` | type-assert provider then list commits/PRs | ✓ WIRED | `rr, ok := provider.(vcs.ReleaseRangeReader)` confirmed |
| `internal/releasedocs/changemodel.go` | `golang.org/x/mod/semver` | semver.Compare/IsValid for bump | ✓ WIRED | `semver.IsValid`, `semver.Canonical` calls confirmed |
| `internal/releasedocs/template/template.go` | `presets/*.tmpl` | `//go:embed presets/*.tmpl` | ✓ WIRED | Embed directive confirmed; all 4 presets present |
| `internal/releasedocs/template/template.go` | `releasedocs.FileFetcher` (override from ToRef tree) | `FetchFileFromRef` for `template:` path | ✓ WIRED | `ff, ok := rc.Provider.(releasedocs.FileFetcher)` + FetchFileFromRef from `rc.ToRef` confirmed |
| `internal/vcs/github/release.go` | go-github v66 Repositories/PullRequests services | `a.client.Repositories.CompareCommits` / `EditRelease` / `PullRequests.Create` | ✓ WIRED | Calls confirmed in code |
| `internal/vcs/gitlab/release.go` | glab client-go services | correct import `glab "gitlab.com/gitlab-org/api/client-go"` | ✓ WIRED | Import confirmed |
| `internal/releasedocs/generators/changelog/changelog.go` | template + grouped model | `template.Resolve`/`Render` over grouped change model | ✓ WIRED | `rdtemplate.Resolve` and `rdtemplate.Render` calls confirmed |
| `internal/releasedocs/generators/releasenotes/releasenotes.go` | `llm.Provider` (nil-tolerant) | `rc.LLM == nil` guard then `Chat` | ✓ WIRED | 7 `rc.LLM` references confirmed in file |
| `internal/releasedocs/publishers/releasebody/releasebody.go` | `vcs.ReleasePublisher` + `marker.SpliceReleaseBody` | `GetRelease → splice → UpdateReleaseBody if changed` | ✓ WIRED | `rp, ok := rc.Provider.(vcs.ReleasePublisher)` + `SpliceReleaseBody` + no-op guard confirmed |
| `internal/releasedocs/publishers/changelogpr/changelogpr.go` | `vcs.BranchCommitter` + `marker.ChangelogMarker` | read-back marker → update-else-create single PR | ✓ WIRED | `bc, ok := rc.Provider.(vcs.BranchCommitter)` + `ChangelogBranch` + `ChangelogMarker` confirmed |
| `internal/releasedocs/dispatcher.go` | config from ToRef tree | `releasedocs.FileFetcher.FetchFileFromRef(repo, ToRef, .cadoo.yaml)` | ✓ WIRED | Line 118 confirmed: `ff.FetchFileFromRef(ctx, job.Repo, job.ToRef, ".cadoo.yaml")` |
| `cmd/cadoo-cli/releasedocs.go` | `releasedocs.Dispatcher` | one-entry VCSPool + buildProvider | ✓ WIRED | `VCSPool: map[vcs.Kind]vcs.Provider{target.Provider: provider}` confirmed |
| `cmd/cadoo-cli/main.go` | `releaseDocsCmd` | `case "release-docs"` in switch | ✓ WIRED | `case "release-docs": releaseDocsCmd(os.Args[2:])` at line 43 confirmed |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `changelog.Generator.Generate` | `rc.GroupedModel` | `BuildContext` → `vcs.ReleaseRangeReader.ListCommits/ListMergedPRs` → `BuildGroupedModel` | Yes — live VCS range read, deterministic transform | ✓ FLOWING |
| `releasenotes.Generator.Generate` | `rc.GroupedModel` + `rc.LLM` | Same as above; LLM is nil-tolerant | Yes — skeleton always populated; LLM optional | ✓ FLOWING |
| `releasebody.Publisher.Publish` | `rel.Body` from `GetReleaseByTag` | `vcs.ReleasePublisher.GetReleaseByTag` → actual VCS API | Yes — live release body read back | ✓ FLOWING |
| `changelogpr.Publisher.Publish` | existing CHANGELOG.md from `FetchFileFromRef` | `releasedocs.FileFetcher.FetchFileFromRef(repo, ToRef, ...)` | Yes — best-effort; logs on failure | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `cadoo release-docs -h` shows `--from` and `--to` flags | `go run ./cmd/cadoo-cli release-docs -h 2>&1 \| grep -qi "from"` | Shows `-from string` and usage block | ✓ PASS |
| All releasedocs tests pass | `go test ./internal/releasedocs/... -race -count=1` | 109 passed in 8 packages | ✓ PASS |
| All VCS capability tests pass | `go test ./internal/vcs/... -race -count=1` | 34 passed in 3 packages | ✓ PASS |
| CLI flag tests pass | `go test ./cmd/cadoo-cli/... -run TestReleaseDocsFlags -race -count=1` | 8 passed | ✓ PASS |
| Full build succeeds | `go build ./...` | Success | ✓ PASS |
| Lint clean | `make lint` | 0 issues | ✓ PASS |
| Full test suite passes | `go test ./...` | 333 passed in 65 packages | ✓ PASS |

### Probe Execution

No probes declared for this phase. Step 7c: SKIPPED (no probe-*.sh files declared or conventional).

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| REQ-release-artifact-generation | 01-01, 01-02, 01-04, 01-05, 01-07 | Changelog + release-notes generated from commits/PRs in range | ✓ SATISFIED | Two Generator implementations; grouped model; 26 generator tests pass |
| REQ-per-artifact-toggles | 01-01, 01-02, 01-05, 01-07 | `enabled` flag + `when:` condition per artifact; dispatcher gates on `Enabled()` | ✓ SATISFIED | `Enabled(cfg, bump)` in both generators; `TestDisabledNotGenerated` passes; dispatcher gate confirmed at line 84 |
| REQ-configurable-templates | 01-03 | Embedded presets; `template:` override from tag tree | ✓ SATISFIED | 4 embedded presets; `template.Resolve` with FetchFileFromRef; `TestTemplateOverride` passes |
| REQ-release-docs-idempotency | 01-01, 01-06, 01-07 | Stateless marker reconstruction; edit-in-place; no duplicates | ✓ SATISFIED | `SpliceReleaseBody` no-op guard; deterministic branch; `TestIdempotentTwiceRun` + `TestSinglePR` pass |
| REQ-configurable-trigger | 01-07 | Manual CLI entry point (`cadoo release-docs --repo … --from vX --to vY`) | ✓ SATISFIED | CLI subcommand with `--from`/`--to`/`--repo`; wired in `main.go` switch; `TestReleaseDocsFlags` 8 tests pass |
| REQ-publish-destinations | 01-01, 01-04, 01-06, 01-07 | `releasebody` upserts release body; `changelogpr` maintains single PR | ✓ SATISFIED | Both publishers implemented; capability degradation tested. CR-01 WARNING for GitLab releasebody path |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/vcs/gitlab/release.go` | 196–198 | `UpdateReleaseBody` always returns error (documented in CR-01 of 01-REVIEW.md) | ⚠️ Warning | GitLab repos with `publish.releaseBody.enabled:true` will get a hard error propagated through Publisher → Dispatcher → CLI os.Exit(1); GitHub and GHES unaffected |
| `internal/releasedocs/template/template.go` | 161 | `_ = err` silently discards non-404 fetch errors for template overrides (CR-02) | ⚠️ Warning | Non-404 failures (rate limit, auth) silently fall through to preset with no log entry; operator sees default output instead of their custom template |
| `internal/releasedocs/releasedocs.go` | 182–215 | `GeneratorRegistry.Generators()` and `PublisherRegistry.Publishers()` iterate over a map in non-deterministic order (WR-03); these registry types are dead code in production | ⚠️ Warning | Registry types unused in production (dispatcher uses plain slices); if used, would break golden-file tests due to non-deterministic ordering |
| `internal/releasedocs/marker.go` | 80–82 | `HasChangelogMarker` exported but never called in production code (WR-02) | ℹ️ Info | Dead code; misleads future maintainers about idempotency mechanism |
| `internal/releasedocs/changemodel.go` | ~279–288 | `classifyCommit` uses case-sensitive `strings.HasPrefix` (WR-04) | ℹ️ Info | Commits with `Feat:` or `FEAT:` fall to "Other" instead of "Features"; minor classification gap |
| `internal/releasedocs/marker.go` | 39–53 | `SpliceReleaseBody` does not guard against orphaned single-marker bodies (WR-05) | ℹ️ Info | Edge case: body with only end-marker causes progressive corruption on repeated runs |
| `internal/releasedocs/publishers/changelogpr/changelogpr.go` | 31 | `prBase = "main"` hardcoded (IN-01) | ℹ️ Info | Repos with `master` or custom trunk will have PR opened against wrong base; VCS API may reject |
| `cmd/cadoo-cli/releasedocs_test.go` | 135–150 | `TestReleaseDocs_FromToMapping` iterates 3 cases but asserts nothing about the actual mapping behavior (IN-03) | ℹ️ Info | Zero coverage of FromRef/ToRef mapping; will always pass regardless of bugs |

No `TBD`, `FIXME`, or `XXX` debt markers found in any phase-modified file.

### Human Verification Required

#### 1. Dogfood End-to-End Run (SC-6)

**Test:** Run `cadoo release-docs --repo payamqorbanpour/cadoo --from <prevTag> --to <tag>` with a valid `GITHUB_TOKEN`. Pick a real tag range from `git tag`. Then run the exact same command a second time.

**Expected:**
- First run: a single `cadoo/changelog/<tag>` branch + one CHANGELOG.md PR carrying `<!-- cadoo:changelog:<tag> -->` marker; release body updated inside `<!-- cadoo:release-notes:begin -->`/`:end` markers (if a release exists for `<tag>`); changelog section is grouped (Features/Fixes/Breaking/…).
- Second run: same PR/branch is updated in place (no second PR created); release body marker block replaced not duplicated; proving stateless idempotency.

**Why human:** Requires live `GITHUB_TOKEN` with repo-write on payamqorbanpour/cadoo. Cannot be verified with fake providers. Task 3 was declared "operator-approved skip" in 01-07-SUMMARY.md.

#### 2. GitLab Release-Body Publishing (CR-01)

**Test:** With a GitLab token configured, run `cadoo release-docs` against a GitLab repo that has an existing release for the target tag, with `publish.releaseBody.enabled: true` in `.cadoo.yaml`.

**Expected:** The run should either succeed (release body updated) or degrade gracefully (log a warning, continue). It must NOT exit 1.

**Why human:** CR-01 from 01-REVIEW.md documents that `gitlab.UpdateReleaseBody` unconditionally returns a non-nil error while still satisfying `vcs.ReleasePublisher`, causing `releasebody.Publisher.Publish` to hard-fail. The fix requires a code change (interface redesign or GitLab-specific path). This is a correctness issue for all GitLab users with `publish.releaseBody` enabled. Automated tests use fake providers and cannot catch this.

### Gaps Summary

No BLOCKER gaps exist. All six ROADMAP Success Criteria are either verified or deferred to human testing. The build compiles cleanly, 333 tests pass, and lint is clean.

**SC-6 (dogfood)** is the only unverified truth. It was a `checkpoint:human-verify` gate in Plan 07 Task 3 and was declared "operator-approved skip" in 01-07-SUMMARY.md. This means the live end-to-end path has not been observed.

**CR-01 (GitLab UpdateReleaseBody)** is a WARNING-level defect documented in the code review. It affects only GitLab repos with `publish.releaseBody.enabled:true`; GitHub/GHES users are unaffected. The interface satisfies the compile-time assertion, so the defect is silent until runtime. It is not a blocker for the overall phase goal (GitHub-centric dogfood), but it is a known correctness gap.

**WR-03 (unused registry types with non-deterministic iteration):** The `GeneratorRegistry`/`PublisherRegistry` types in `releasedocs.go` are dead code in production — all callers use plain slices from `defaults.DefaultGenerators()`. These should be removed to avoid misleading future maintainers and to prevent accidental use that would break determinism.

---

_Verified: 2026-06-05T20:00:00Z_
_Verifier: Claude (gsd-verifier)_
