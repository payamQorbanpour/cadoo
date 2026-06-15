---
phase: 01-generators-publishers-cli
reviewed: 2026-06-05T18:30:00Z
depth: standard
files_reviewed: 34
files_reviewed_list:
  - cmd/cadoo-cli/main.go
  - cmd/cadoo-cli/releasedocs.go
  - cmd/cadoo-cli/releasedocs_test.go
  - internal/config/config.go
  - internal/releasedocs/changemodel.go
  - internal/releasedocs/changemodel_test.go
  - internal/releasedocs/context.go
  - internal/releasedocs/context_test.go
  - internal/releasedocs/defaults/defaults.go
  - internal/releasedocs/dispatcher.go
  - internal/releasedocs/dispatcher_test.go
  - internal/releasedocs/generators/changelog/changelog.go
  - internal/releasedocs/generators/changelog/changelog_test.go
  - internal/releasedocs/generators/changelog/testdata/basic.golden
  - internal/releasedocs/generators/releasenotes/releasenotes.go
  - internal/releasedocs/generators/releasenotes/releasenotes_test.go
  - internal/releasedocs/marker.go
  - internal/releasedocs/publishers/changelogpr/changelogpr.go
  - internal/releasedocs/publishers/changelogpr/changelogpr_test.go
  - internal/releasedocs/publishers/releasebody/releasebody.go
  - internal/releasedocs/publishers/releasebody/releasebody_test.go
  - internal/releasedocs/registry.go
  - internal/releasedocs/releasedocs.go
  - internal/releasedocs/releasedocstest/fake.go
  - internal/releasedocs/template/presets/changelog.tmpl
  - internal/releasedocs/template/presets/release-notes-concise.tmpl
  - internal/releasedocs/template/presets/release-notes-detailed.tmpl
  - internal/releasedocs/template/presets/release-notes-marketing.tmpl
  - internal/releasedocs/template/template.go
  - internal/releasedocs/template/template_test.go
  - internal/vcs/github/release.go
  - internal/vcs/github/release_test.go
  - internal/vcs/gitlab/release.go
  - internal/vcs/gitlab/release_test.go
  - internal/vcs/vcs.go
findings:
  critical: 2
  warning: 5
  info: 3
  total: 10
status: issues_found
---

# Phase 01: Code Review Report

**Reviewed:** 2026-06-05T18:30:00Z
**Depth:** standard
**Files Reviewed:** 34
**Status:** issues_found

## Summary

Phase 01 delivers the release-docs subsystem: generator/publisher pipeline, VCS adapter extensions (GitHub + GitLab), CLI integration, and a comprehensive fake for testing. The architecture is clean and the idempotency discipline is generally sound. However, two blockers were identified that would make the release-body publisher systematically fail on GitLab and silently discard operator-configured template overrides on fetch errors, plus several warnings around correctness and dead code.

## Critical Issues

### CR-01: GitLab `UpdateReleaseBody` always returns an error — releasebody publisher hard-fails for all GitLab repos

**File:** `internal/vcs/gitlab/release.go:196-199`

**Issue:** The GitLab adapter's `UpdateReleaseBody` interface method unconditionally returns a non-nil error to direct callers toward the type-specific `UpdateReleaseBodyByTag`. However, `releasebody.Publisher.Publish` calls `UpdateReleaseBody` via the `vcs.ReleasePublisher` interface after asserting the type succeeds (the GitLab adapter satisfies the interface via compile-time assertion at line 310). Because the GitLab adapter does implement `vcs.ReleasePublisher`, the type assertion in `releasebody.Publish` (line 41) succeeds — graceful degradation is NOT triggered. The returned error propagates through `Publish` → `Dispatcher.Run` → `releaseDocsCmd` → `os.Exit(1)`. Every GitLab user with `publish.releaseBody.enabled: true` will hit a hard failure on every run.

No test exercises the GitLab + releasebody path, which is why this escaped detection.

**Fix:** Either (a) make the GitLab adapter's `UpdateReleaseBody` call `UpdateReleaseBodyByTag` internally using a stored tag reference (requires plumbing the tag from `GetReleaseByTag` to `UpdateReleaseBody`), or (b) redesign the publisher to detect the GitLab adapter and call `UpdateReleaseBodyByTag` directly, or (c) the cleanest option — change the `vcs.Release` struct to carry the `TagName` (already present) and update `vcs.ReleasePublisher.UpdateReleaseBody` to accept tag-not-ID and let each adapter use what it needs. A minimal fix:

```go
// In gitlab/release.go — replace the always-error stub:
func (a *Adapter) UpdateReleaseBody(ctx context.Context, repo string, _ int64, body string) error {
    // GitLab releases have no numeric ID; the caller must pass TagName via rel.TagName.
    // This is called by releasebody.Publisher which already holds rel from GetReleaseByTag.
    // We cannot recover the tag from the int64 ID, so callers must use UpdateReleaseBodyByTag.
    // To make this safe: accept tag via a parallel field on vcs.Release and thread it through.
    return fmt.Errorf("gitlab.UpdateReleaseBody: no numeric ID available; %w", errTagRequired)
}
```

The correct long-term fix is to expose `TagName` through the interface call:

```go
// vcs/vcs.go
type ReleasePublisher interface {
    GetReleaseByTag(ctx context.Context, repo, tag string) (*Release, error)
    UpdateReleaseBody(ctx context.Context, repo string, rel *Release, body string) error
}
// GitLab can then use rel.TagName; GitHub can use rel.ID. Callers already hold *Release.
```

---

### CR-02: Non-404 template fetch errors are silently discarded — operator-configured overrides vanish without a log entry

**File:** `internal/releasedocs/template/template.go:159-161`

**Issue:** In `Resolve`, when `FetchFileFromRef` returns a non-404 error (rate limit, auth failure, network timeout), the error is discarded with `_ = err` and the function silently falls back to the built-in preset. The comment claims "fall through to preset with a logged reason" but no `slog.Warn` or any logging call exists. An operator who configured `artifacts.changelog.template: .cadoo/my-changelog.tmpl` will see the default preset output with no diagnostic — their custom template is silently ignored.

**Fix:**
```go
if !isMissingFile(err) {
    // Non-404 error: log the reason and fall through to preset.
    slog.Warn("releasedocs: fetch template override failed; using preset",
        "path", overridePath,
        "repo", rc.Repo,
        "ref", rc.ToRef,
        "err", err,
    )
}
```

---

## Warnings

### WR-01: `changelogpr` publisher reads CHANGELOG.md from the release tag tree, not from the working branch — loses all prior entries on the first run for a new repo

**File:** `internal/releasedocs/publishers/changelogpr/changelogpr.go:86`

**Issue:** When reading the existing `CHANGELOG.md` to prepend to, the publisher fetches from `rc.ToRef` (the newly created release tag), not from the publisher's working branch (`cadoo/changelog/<toRef>`). On the first run for a tag, the tag tree contains whatever was in the repository at release time — correct. But on subsequent runs for the same tag (e.g., a retry), it reads the same tag tree (which will never reflect what was written to the branch), prepends the same new section, and writes again. This is idempotent only because `UpsertFile` overwrites. However, the comment at line 81 says "Read the existing CHANGELOG.md from the branch **or** ToRef" — the "branch" part is never attempted. If there is a pre-existing CHANGELOG.md with multiple historical sections already in the repo, the publisher reads it from the tag and always prepends correctly. But if a concurrent run mutated the branch after the first run (e.g., a human amended it), the next run will overwrite that amendment.

For correctness the publisher should prefer reading from the branch, falling back to the tag:

```go
// Prefer the branch (reflects prior Cadoo writes); fall back to tag.
raw, err := ff.FetchFileFromRef(ctx, rc.Repo, branch, changelogPath)
if err != nil {
    // Branch may not exist yet — try the tag tree.
    raw, err = ff.FetchFileFromRef(ctx, rc.Repo, rc.ToRef, changelogPath)
    if err != nil {
        slog.Warn("changelogpr: FetchFileFromRef failed; proceeding best-effort", ...)
    }
}
```

---

### WR-02: `HasChangelogMarker` is exported but never called in production code — the marker-based idempotency described in comments is not enforced

**File:** `internal/releasedocs/marker.go:80-82`

**Issue:** `HasChangelogMarker(body, toRef string) bool` is exported and documented as the mechanism by which the publisher decides whether to update vs. create a PR. However, the `changelogpr` publisher never calls it. The actual single-PR invariant is enforced by `OpenOrUpdatePR`'s branch-based find-then-update-else-create logic in the adapters. The exported function is dead code that misleads future maintainers into thinking the publisher reads back and checks the PR body marker before writing.

The dead function should either be called (to add a second idempotency layer at the PR body level) or removed. Leaving it exported and undocumented as unused creates maintenance debt.

**Fix:** Either integrate the check into `Publish`:
```go
// After finding existing PR body, skip if marker already present with same content.
```
Or unexport/remove if the branch-based invariant is sufficient.

---

### WR-03: `GeneratorRegistry` and `PublisherRegistry` in `releasedocs.go` are unused infrastructure with non-deterministic iteration order

**File:** `internal/releasedocs/releasedocs.go:163-216`

**Issue:** `NewGeneratorRegistry`, `NewPublisherRegistry`, and their `Register`/`Get`/`Generators()`/`Publishers()` methods are never called in production code. The dispatcher uses plain `[]Generator` and `[]Publisher` slices directly. The `Generators()` and `Publishers()` methods iterate over a map and return items in non-deterministic order — if these registries were ever used where order matters (D-09 requires canonical slice order), results would be non-deterministic and break golden-file tests.

**Fix:** Remove the registry types entirely from this package. They were implemented speculatively and contradict the slice-order discipline enforced everywhere else. If a registry is needed in Phase 2, it can be introduced then with a sort-by-priority scheme.

---

### WR-04: `classifyCommit` is case-sensitive for Conventional Commit prefixes but `stripConventionalPrefix` is case-insensitive — classification and stripping disagree for uppercase prefixes

**File:** `internal/releasedocs/changemodel.go:265-298` and `internal/releasedocs/generators/changelog/changelog.go:119-136`

**Issue:** In `classifyCommit`, `strings.HasPrefix(subject, "feat:")` uses the raw message subject — case-sensitive. A commit with subject `"Feat: new feature"` or `"FEAT: new feature"` will not match and will fall to the "Other" bucket. In contrast, `stripConventionalPrefix` lower-cases the title before matching, so if such an entry somehow reached it, the prefix would be stripped. In practice this inconsistency means case-variations of conventional prefixes are silently classified as "Other" rather than their intended section, with no warning.

**Fix:** Normalize the subject to lowercase before the prefix comparisons in `classifyCommit`:
```go
func classifyCommit(msg string) string {
    // ... body check unchanged ...
    subject, _, _ := strings.Cut(msg, "\n")
    subject = strings.ToLower(strings.TrimSpace(subject)) // normalize
    for _, breaking := range []string{"feat!:", "fix!:"} {
        if strings.HasPrefix(subject, breaking) {
            return "Breaking Changes"
        }
    }
    // etc.
}
```

---

### WR-05: `SpliceReleaseBody` produces a corrupted body when the original contains only the end marker (no begin marker)

**File:** `internal/releasedocs/marker.go:39-53`

**Issue:** When `original` contains `ReleaseNotesEnd` but not `ReleaseNotesBegin` (e.g., a user who accidentally copied only the closing marker into their release description), `startIdx` is -1 and the guarded-replace path is not taken. The function falls through to the first-time-write path, which appends a complete `Begin…End` block after the original. The result contains one orphaned `End` marker plus a well-formed `Begin…End` block — future runs will attempt the replace path, find the last `Begin`, use it as `startIdx`, find the first `End` (the orphaned one) as `endIdx`, and `endIdx > startIdx` will be false (the orphaned end appears before the appended begin). This causes progressive corruption on repeated runs without an obvious error.

**Fix:** Add a guard: if only one marker is present, treat as corrupted and strip both before appending:
```go
func SpliceReleaseBody(original, section string) string {
    section = strings.TrimSpace(section)
    startIdx := strings.Index(original, ReleaseNotesBegin)
    endIdx := strings.Index(original, ReleaseNotesEnd)

    if startIdx >= 0 && endIdx > startIdx {
        // Well-formed managed block: replace inner section.
        head := strings.TrimRight(original[:startIdx], " \n\t")
        tail := original[endIdx+len(ReleaseNotesEnd):]
        return joinReleaseBody(head, section, tail)
    }

    // Malformed or first-time: strip any orphaned markers before appending.
    cleaned := strings.ReplaceAll(original, ReleaseNotesBegin, "")
    cleaned = strings.ReplaceAll(cleaned, ReleaseNotesEnd, "")
    return joinReleaseBody(strings.TrimRight(cleaned, " \n\t"), section, "")
}
```

---

## Info

### IN-01: `prBase` hardcoded to `"main"` in changelogpr publisher — will silently target wrong branch on repos that use `master` or custom defaults

**File:** `internal/releasedocs/publishers/changelogpr/changelogpr.go:28-32`

**Issue:** The base branch for the changelog PR is hardcoded to `"main"`. Repositories that use `"master"` or a custom trunk branch name will have the PR opened against a non-existent base, which the VCS API will reject (or silently accept against a branch that doesn't track default). The comment says "Phase 2 can override via config" but there is no current path for operators to configure this without code changes.

**Fix:** Read the default branch from the VCS at runtime (GitHub adapter already does this in `UpsertFile`) or expose it as a config field in `config.ReleasePublish`. As a minimal improvement, fall back to the repo's detected default branch rather than a hardcoded constant.

---

### IN-02: `releaseDocsCmd` does not set `ReleaseJob.Org` — multi-tenancy field is always empty for CLI invocations

**File:** `cmd/cadoo-cli/releasedocs.go:83-88`

**Issue:** `ReleaseJob.Org` is propagated into `ReleaseContext.Org` (context.go:79) for multi-tenancy. In `releaseDocsCmd`, the `ReleaseJob` struct literal never sets `Org`, leaving it as `""`. Currently no generators or publishers consume `ReleaseContext.Org`, so this is not a runtime failure. However, Phase 2 worker integration will route jobs by `Org`; if the CLI job shape is used as a template, the missing `Org` will cause silent routing failures. Worth documenting or surfacing as a required flag for non-single-tenant use.

---

### IN-03: `TestReleaseDocs_FromToMapping` contains a test loop that asserts nothing

**File:** `cmd/cadoo-cli/releasedocs_test.go:135-151`

**Issue:** The test function iterates over three `{from, to}` cases and performs a single check per iteration (`if c.from == "" && c.to == ""`), which only catches invalid test cases — not actual mapping behavior. The body comment acknowledges this is a contract assertion rather than a functional test, but the loop provides no actual coverage of the mapping logic. It will always pass regardless of whether `FromRef`/`ToRef` are correctly threaded, because the mapping is a string assignment that this test never exercises with the actual flag-parsed value.

**Fix:** Either delete the test (it provides zero coverage) or make it meaningful by actually calling the relevant parsing code and asserting `job.FromRef == c.from` and `job.ToRef == c.to` — though this requires refactoring `releaseDocsCmd` to extract its flag→job mapping into a testable function.

---

_Reviewed: 2026-06-05T18:30:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
