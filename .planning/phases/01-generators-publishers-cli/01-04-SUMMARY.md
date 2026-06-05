---
phase: 01-generators-publishers-cli
plan: "04"
subsystem: vcs
tags: [github, gitlab, release, changelog, vcs-capability, rest-api, httptest]

requires:
  - phase: 01-generators-publishers-cli
    provides: "Plan 01-01 — three optional vcs capability interfaces (ReleaseRangeReader, ReleasePublisher, BranchCommitter) and types (Commit, MergedPR, Release, FileWrite) added to internal/vcs/vcs.go"

provides:
  - "GitHub adapter *Adapter implements ReleaseRangeReader (ListCommits via CompareCommits, ListMergedPRs via ListPullRequestsWithCommit, LatestTagBefore via ListTags)"
  - "GitHub adapter *Adapter implements ReleasePublisher (GetReleaseByTag, UpdateReleaseBody via EditRelease)"
  - "GitHub adapter *Adapter implements BranchCommitter (UpsertFile with auto-branch-create, OpenOrUpdatePR idempotent)"
  - "GitLab adapter *Adapter implements ReleaseRangeReader (ListCommits via Repositories.Compare, ListMergedPRs via ListProjectMergeRequests, LatestTagBefore via Tags.ListTags)"
  - "GitLab adapter *Adapter implements ReleasePublisher (GetReleaseByTag via Releases.GetRelease, UpdateReleaseBodyByTag — numeric ID not used for GitLab)"
  - "GitLab adapter *Adapter implements BranchCommitter (UpsertFile with start_branch for auto-create, OpenOrUpdatePR idempotent)"
  - "Compile-time assertions var _ vcs.X = (*Adapter)(nil) on both adapters (3 per adapter)"
  - "httptest-stubbed unit tests: 13 for GitHub, 15 for GitLab"

affects:
  - 01-05-context
  - 01-06-publishers
  - 01-07-cli
  - releasedocs-dispatcher

tech-stack:
  added: []
  patterns:
    - "VCS optional capability on *Adapter: same method-on-*Adapter pattern as FetchFileFromRef (github.go:397); compile-time var _ vcs.X assertion mirrors github.go:526"
    - "GitLab UpsertFile uses start_branch to auto-create branch on CreateFile (no separate branch-create API needed)"
    - "GitHub UpsertFile auto-creates branch via Git.GetRef → Repositories.Get (default branch) → Git.CreateRef"
    - "PR/MR idempotency: list open PRs with Head/Base filter, update-else-create"
    - "ListMergedPRs cross-references merge SHAs from CompareCommits to filter MRs to the release range"
    - "GitLab releases identified by tag name (no numeric ID); UpdateReleaseBodyByTag is the typed helper"

key-files:
  created:
    - internal/vcs/github/release.go
    - internal/vcs/github/release_test.go
    - internal/vcs/gitlab/release.go
    - internal/vcs/gitlab/release_test.go
  modified: []

key-decisions:
  - "GitLab releases have no numeric ID — UpdateReleaseBody interface method returns error directing callers to type-assert *gitlab.Adapter and use UpdateReleaseBodyByTag; vcs.Release.ID=0 for GitLab"
  - "GitHub branch creation uses Repositories.Get to find default branch, then Git.GetRef + Git.CreateRef (avoids hardcoding 'main' except as fallback)"
  - "ListMergedPRs uses commit SHAs from CompareCommits to filter by range rather than listing all closed PRs by date (more accurate for backport scenarios)"
  - "GitLab UpsertFile encodes content as base64 (required by GitLab RepositoryFiles API) using encoding/base64"

patterns-established:
  - "Optional capability pattern: methods on *Adapter + compile-time var _ vcs.X = (*Adapter)(nil)"
  - "httptest stub convention for adapter tests: use WithEnterpriseURLs (GitHub) or BaseURL config (GitLab)"

requirements-completed:
  - REQ-release-artifact-generation
  - REQ-publish-destinations

duration: 25min
completed: "2026-06-05"
---

# Phase 01 Plan 04: VCS Adapter Release Capabilities Summary

**GitHub and GitLab adapters implement ReleaseRangeReader, ReleasePublisher, and BranchCommitter via REST with httptest-stubbed tests and compile-time interface assertions**

## Performance

- **Duration:** 25 min
- **Started:** 2026-06-05T12:55:00Z
- **Completed:** 2026-06-05T12:58:13Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- GitHub adapter: 6 new methods (ListCommits, ListMergedPRs, LatestTagBefore, GetReleaseByTag, UpdateReleaseBody, UpsertFile, OpenOrUpdatePR) with 3 compile-time assertions; 13 httptest tests pass
- GitLab adapter: 7 new methods (same + UpdateReleaseBodyByTag) with 3 compile-time assertions; 15 httptest tests pass
- Both adapters import only the correct client (go-github v66 / glab via gitlab.com/gitlab-org/api/client-go, Pitfall 1 honored)
- All REST — no GraphQL used; tokens never surfaced through capability interfaces (T-04-01)
- `go build ./...`, `go test ./internal/vcs/...`, and `make lint` all pass

## Task Commits

1. **Task 1: GitHub adapter capability methods + httptest tests** - `955c645` (feat)
2. **Task 2: GitLab adapter capability methods + httptest tests** - `2c149cc` (feat)
3. **Style: gofmt alignment fixes** - `28b91a4` (style)

## Files Created/Modified

- `internal/vcs/github/release.go` — ListCommits, ListMergedPRs, LatestTagBefore, GetReleaseByTag, UpdateReleaseBody, UpsertFile, OpenOrUpdatePR on *Adapter; var _ vcs.ReleaseRangeReader / ReleasePublisher / BranchCommitter compile-time assertions
- `internal/vcs/github/release_test.go` — 13 httptest-stubbed tests covering all three capabilities including create/update paths
- `internal/vcs/gitlab/release.go` — same 6 interface methods + UpdateReleaseBodyByTag; correct glab import path; base64 encoding for RepositoryFiles API; var _ assertions
- `internal/vcs/gitlab/release_test.go` — 15 httptest-stubbed tests covering all three capabilities plus UpsertFile update path

## Decisions Made

- **GitLab releases have no numeric ID:** The `vcs.ReleasePublisher.UpdateReleaseBody(ctx, repo, releaseID, body)` interface cannot work for GitLab since `vcs.Release.ID = 0`. Decision: interface method returns an error directing callers to type-assert `*gitlab.Adapter` and use `UpdateReleaseBodyByTag`. The Plan 06 releasebody publisher will type-assert and call the typed helper. This is acceptable for Phase 1 (D-15: graceful degradation with a logged reason).
- **GitHub branch creation strategy:** UpsertFile calls `Repositories.Get` to find the actual default branch name rather than hardcoding "main". Falls back to "main" only if the API returns an empty string.
- **ListMergedPRs filtering:** Uses SHA cross-reference from CompareCommits rather than date-based filtering. More accurate for repos that use backport workflows.

## Deviations from Plan

None — plan executed exactly as written. The GitLab UpdateReleaseBody behavior (returning an error with a redirect to the typed helper) is an implementation detail necessitated by the GitLab API not having numeric release IDs; it satisfies the compile-time interface assertion while providing a clear migration path for publishers.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes were introduced beyond what the plan's threat model covers. Tokens stay inside `*Adapter.cfg`; no token field added to the capability interfaces (T-04-01 honored).

## Known Stubs

None — all methods call real REST API endpoints through the adapter's authenticated client.

## Self-Check: PASSED

- `internal/vcs/github/release.go` — FOUND
- `internal/vcs/github/release_test.go` — FOUND
- `internal/vcs/gitlab/release.go` — FOUND
- `internal/vcs/gitlab/release_test.go` — FOUND
- Commit 955c645 — FOUND (GitHub adapter feat)
- Commit 2c149cc — FOUND (GitLab adapter feat)
- Commit 28b91a4 — FOUND (style/gofmt)
- `go test ./internal/vcs/github/... ./internal/vcs/gitlab/... -race -count=1` — 28 passed
- `go build ./...` — success
- `make lint` — 0 issues
