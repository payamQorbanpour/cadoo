---
phase: 02-webhook-auto-trigger-state
verified: 2026-06-05T19:23:53Z
status: human_needed
score: 5/5
overrides_applied: 0
human_verification:
  - test: "Confirm migration 0006 round-trips up→down→up against live Postgres"
    expected: "make migrate applies 0006, make migrate-down drops release_docs_state, make migrate re-applies it — UNIQUE constraint on (provider, repo_full_name, to_tag, artifact_kind) confirmed via psql"
    why_human: "Requires a running pg16 instance; automated checks cannot spin up the DB. The plan declared this a blocking checkpoint:human-verify; the SUMMARY states 'confirmed by operator' but the verifier cannot re-confirm without the DB."
---

# Phase 2: Webhook Auto-Trigger + State — Verification Report

**Phase Goal:** A published release (or configured tag push) on a customer repo automatically triggers release-docs generation through the dual-mode queue and worker, with DB-backed state so re-syncs edit in place, plus the pages publisher and blog generator.
**Verified:** 2026-06-05T19:23:53Z
**Status:** human_needed
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths (from ROADMAP Phase 2 Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| SC1 | A GitHub `release: published` / GitLab release webhook (and, when configured, a `v*` tag push filtered by `tagPattern`) builds a `ReleaseJob` and enqueues it via River when `DATABASE_URL` is set or the in-memory queue otherwise; the worker consumes it and runs the dispatcher | VERIFIED | `cmd/cadoo-webhook/release.go`: 4 handlers (handleGithubRelease, handleGithubTagPush, handleGitlabRelease, handleGitlabTagPush); `internal/riverq/queue.go`: ReleaseArgs + EnqueueRelease + releaseWorker; `cmd/cadoo-worker/main.go`: buildReleaseDispatcher + riverq.New(pool, dispatcher, releaseDispatcher); 22 handler tests pass |
| SC2 | If `releaseDocs.trigger` excludes the event kind, the webhook no-ops early | VERIFIED | `TestHandleGithubRelease_TriggerTagExcludesRelease`, `TestHandleGithubTagPush_TriggerReleaseExcludesTag`, `TestHandleGitlabRelease_TriggerTagExcludesRelease`, `TestHandleGitlabTagPush_TriggerReleaseExcludesTag`, `TestTriggerEarlyExit` all pass; early-return logic in release.go at lines 32-67, 77-128, 131-164, 173-211 |
| SC3 | Published state is recorded in a new `(provider, repo, to_tag, artifact_kind)` table (migration round-trips `up → down → up`) so re-runs edit the release body and changelog PR in place when DB-backed | VERIFIED (code) / HUMAN NEEDED (DB round-trip) | `db/migrations/0006_release_docs_state.sql`: CREATE TABLE with `UNIQUE (provider, repo_full_name, to_tag, artifact_kind)` and proper Down block; `internal/releasedocs/state/state.go`: `ON CONFLICT DO UPDATE` upsert; `internal/releasedocs/dispatcher.go`: `Store.Record` after publish success; migration DDL verified clean in file; DB round-trip requires human confirmation (see Human Verification) |
| SC4 | The pages publisher commits rendered artifacts to the configured `branch`/`dir` at deterministic paths (`docs/releases/vX.Y.Z/…`); re-runs overwrite the same paths | VERIFIED | `internal/releasedocs/publishers/pages/pages.go`: `path.Join(dir, "releases", rc.ToRef, string(art.Kind)+".md")` at line 78; `UpsertFile` per artifact; `TestDeterministicPaths` asserts exact strings `"docs/releases/v1.2.3/changelog.md"`, `"docs/releases/v1.2.3/release_notes.md"`, `"docs/releases/v1.2.3/blog.md"`; `TestIdempotentOverwrite` asserts two consecutive Publish calls use identical paths; 7 pages tests pass |
| SC5 | The blog generator produces a long-form announcement on minor/major releases (per its `when:` condition) and is routed to pages | VERIFIED | `internal/releasedocs/generators/blog/blog.go`: `Generate` function at line 52; `Enabled` coerces empty When to "minor_or_above" (5 grep matches); `internal/releasedocs/defaults/defaults.go`: `blog.New()` in DefaultGenerators, `pages.Publisher{}` in DefaultPublishers; dispatcher (dispatcher.go:104-124) passes all artifacts to all publishers including pages; 33 blog tests pass |

**Score:** 5/5 truths verified (SC3 has one human-only sub-check for DB round-trip)

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/vcs/vcs.go` | TagReleasePublisher optional capability interface | VERIFIED | Interface at lines 242-249 with `UpdateReleaseBodyByTag` method; docstring per house style |
| `internal/vcs/gitlab/release.go` | Compile-time assert `var _ vcs.TagReleasePublisher = (*Adapter)(nil)` | VERIFIED | Line 311 present |
| `internal/vcs/github/release.go` | Compile-time assert + `UpdateReleaseBodyByTag` method | VERIFIED | Assert at line 315; method at lines 191+ |
| `internal/releasedocs/publishers/releasebody/releasebody.go` | CR-01 fix: type assertion to TagReleasePublisher for zero-ID releases | VERIFIED | Lines 92-98: type assertion guards `rel.ID==0 && rel.TagName!=""`; no vcs/gitlab or vcs/github import (grep returns 0) |
| `internal/config/config.go` | `Blog ArtifactConfig` + `PagesPublishTarget` struct + `Pages PagesPublishTarget` field | VERIFIED | Line 84: `Blog ArtifactConfig`; lines 148-163: `type PagesPublishTarget struct{Enabled,Branch,Dir}`; line 139: `Pages PagesPublishTarget` |
| `internal/releasedocs/releasedocs.go` | `KindBlog ArtifactKind` + `TargetPages PublishTarget` constants | VERIFIED | Lines 25-28: `KindBlog ArtifactKind = "blog"`; lines 42-44: `TargetPages PublishTarget = "pages"` |
| `db/migrations/0006_release_docs_state.sql` | Goose Up/Down; UNIQUE composite; lookup index; `DROP TABLE IF EXISTS` | VERIFIED | 5/5 content checks pass; file exists at 1.2K |
| `internal/releasedocs/state/state.go` | `New`, `Record` (ON CONFLICT DO UPDATE), `Lookup`; nil-tolerant; no releasedocs import | VERIFIED | `ON CONFLICT` at line 52; `ArtifactKind` count = 0; releasedocs/orchestrator/riverq imports = 0; 4 unit tests pass |
| `internal/releasedocs/generators/blog/blog.go` | `blog.Generator` implementing Generator (Kind/Enabled/Generate); minor_or_above default; nil-LLM | VERIFIED | 154 lines; Generate at line 52; 4 KindBlog references; 5 minor_or_above references; 0 D-01 violations; 33 tests pass |
| `internal/releasedocs/publishers/pages/pages.go` | `pages.Publisher` (Target/Publish); path.Join; BranchCommitter type-assert; graceful degradation | VERIFIED | 1 TargetPages reference; 5 path.Join usages; 5 BranchCommitter references; 0 vcs/github|vcs/gitlab imports; 7 tests pass |
| `internal/riverq/queue.go` | `ReleaseArgs` + `EnqueueRelease` + `releaseWorker` + extended `New` | VERIFIED | ReleaseArgs at line 57 with `Kind()="release_docs"`; EnqueueRelease at line 143; releaseWorker at line 74; river.AddWorker at line 108 |
| `cmd/cadoo-webhook/release.go` | 4 handlers (handleGithubRelease/TagPush, handleGitlabRelease/TagPush); `strings.TrimPrefix`; `path.Match` | VERIFIED | All 4 handlers present; 2+ TrimPrefix calls for "refs/tags/"; path.Match at lines 105 and 197 |
| `cmd/cadoo-webhook/main.go` | 4 new case arms in type switches; `enqueueReleaseFn`; `EnqueueRelease` wiring | VERIFIED | 4 case arms confirmed (grep returns 4); EnqueueRelease grep = 1; handlers wired at lines 299, 301, 390, 392 |
| `internal/releasedocs/defaults/defaults.go` | `blog.New()` in DefaultGenerators; `pages.Publisher{}` in DefaultPublishers | VERIFIED | Both grep counts = 1 |
| `internal/releasedocs/dispatcher.go` | `PostedStore` interface; `Store PostedStore` field; `Record` call after publish loop; no state import | VERIFIED | PostedStore at lines 25-30; Store field at line 60; Record call at lines 130-143; `internal/releasedocs/state` import count = 0 |
| `cmd/cadoo-worker/main.go` | `buildReleaseDispatcher`; `defaults.DefaultGenerators/Publishers`; `state.New(pool)`; `riverq.New` with 3 args | VERIFIED | buildReleaseDispatcher at line 233; DefaultGenerators + DefaultPublishers at lines 268-269; state.New(pool) at line 272; riverq.New 3-arg call at line 115 |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `releasebody/releasebody.go` | `vcs.TagReleasePublisher` | type assertion when `rel.ID == 0` | WIRED | Lines 92-98: `tp, ok := rc.Provider.(vcs.TagReleasePublisher)` guarded by zero-ID check |
| `internal/vcs/gitlab/release.go` | `vcs.TagReleasePublisher` | compile-time assertion | WIRED | `var _ vcs.TagReleasePublisher = (*Adapter)(nil)` at line 311 |
| `cmd/cadoo-webhook/release.go` | `releasedocs.ReleaseJob` | build job from event fields, strip refs/tags/ prefix, enqueue | WIRED | All 4 handlers build ReleaseJob and call enqueueReleaseFn; TrimPrefix for tag prefix |
| `cmd/cadoo-webhook/main.go` | release.go handlers | case arms in GitHub/GitLab type switches | WIRED | Lines 299, 301, 390, 392 route events to handlers |
| `internal/releasedocs/state/state.go` | `release_docs_state` table | INSERT ON CONFLICT DO UPDATE | WIRED | SQL at lines 48-56: parameterized INSERT with ON CONFLICT on (provider, repo_full_name, to_tag, artifact_kind) |
| `internal/releasedocs/generators/blog/blog.go` | `releasedocs.Enabled` | Enabled gate with minor_or_above default coercion | WIRED | `releasedocs.Enabled` called after coercing empty When to "minor_or_above" |
| `internal/releasedocs/publishers/pages/pages.go` | `vcs.BranchCommitter.UpsertFile` | type assertion + UpsertFile per artifact at deterministic path | WIRED | Lines 50-56: type assert; line 82-87: UpsertFile call with path.Join path |
| `cmd/cadoo-worker/main.go` | `riverq.New(pool, toolDispatcher, releaseDispatcher)` | 3-arg call passes release dispatcher so releaseWorker registers | WIRED | Line 115 confirmed; releaseDispatcher built by buildReleaseDispatcher at line 80, passed to runRiver at line 84 |
| `internal/releasedocs/dispatcher.go` | `state.Store` (via PostedStore interface) | `d.Store.Record` after successful publish | WIRED | Lines 130-143: `if d.Store != nil { ... d.Store.Record(...) }` after publisher loop at lines 120-124 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|--------------------|--------|
| `dispatcher.go:Run` | `arts []Artifact` | `gen.Generate(ctx, rc)` in generator loop | Yes — generators produce real artifacts from rc.GroupedModel | FLOWING |
| `dispatcher.go:Run` | `d.Store.Record` call | `arts` from generator loop + `job.*` fields | Yes — real job metadata written to `release_docs_state` table via pgx parameterized query | FLOWING |
| `pages/pages.go:Publish` | `art.Content` | `arts []Artifact` passed from dispatcher | Yes — byte slice from generator; empty content guarded at line 71 | FLOWING |
| `state/state.go:Record` | INSERT params | `(org, provider, repoFullName, toTag, kind, externalID string)` | Yes — all 6 params are runtime values from job + artifact; ON CONFLICT upserts real row | FLOWING |
| `blog/blog.go:Generate` | skeleton + narrative | `rc.GroupedModel` (deterministic) + `rc.LLM.Chat` (optional) | Yes — GroupedModel populates sections; LLM narration called once when non-nil | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full module builds without errors | `go build ./...` | "Go build: Success" (all 5 binaries) | PASS |
| Plan 01 tests (vcs, config, releasebody) | `go test -race -count=1 ./internal/vcs/... ./internal/config/... ./internal/releasedocs/publishers/releasebody/...` | 42 tests pass in 5 packages | PASS |
| Plan 02 tests (state store) | `go test -race -count=1 ./internal/releasedocs/state/...` | 4 tests pass | PASS |
| Plan 03 + 04 tests (blog + pages) | `go test -race -count=1 ./internal/releasedocs/generators/blog/... ./internal/releasedocs/publishers/pages/...` | 40 tests pass in 2 packages | PASS |
| Plan 05 tests (riverq + webhook) | `go test -race -count=1 ./internal/riverq/... ./cmd/cadoo-webhook/...` | 22 tests pass in 2 packages | PASS |
| Plan 06 / dispatcher suite | `go test -race -count=1 ./internal/releasedocs/...` | 160 tests pass in 11 packages | PASS |
| PostedStore nil/success/error cases | `go test -race -count=1 -run TestPostedStore ./internal/releasedocs/...` | 3 tests pass | PASS |
| Webhook handler trigger early-exit | `go test -race -count=1 -run TestHandle ./cmd/cadoo-webhook/...` | 15 tests pass (including 4 trigger-exclusion tests) | PASS |

### Probe Execution

Step 7c: SKIPPED — no `scripts/*/tests/probe-*.sh` files found for this phase; phase does not declare probes in PLAN/SUMMARY frontmatter.

### Requirements Coverage

| Requirement | Plans | Description | Status | Evidence |
|-------------|-------|-------------|--------|----------|
| REQ-configurable-trigger | 02-05 | Release/tag webhook ingestion — GitHub + GitLab; trigger/tagPattern early-exit | SATISFIED | 4 webhook handlers, 16 tests, trigger early-exit `TestTriggerEarlyExit` passes; `releaseTrigger`/`tagPattern` defaults respected |
| REQ-release-docs-idempotency | 02-02, 02-06 | DB-backed state table (migration 0006) + nil-tolerant state.Store + PostedStore hook in dispatcher | SATISFIED (code) / HUMAN (migration round-trip) | Migration file verified; ON CONFLICT DO UPDATE in state.go; dispatcher Store.Record after publish; migration human round-trip noted in Human Verification section |
| REQ-publish-destinations | 02-01, 02-04 | Pages publisher commits artifacts to `{dir}/releases/{toRef}/{kind}.md` via BranchCommitter.UpsertFile | SATISFIED | pages.Publisher with path.Join; 7 tests including `TestDeterministicPaths`; `TestIdempotentOverwrite`; graceful degradation tested |
| REQ-release-artifact-generation | 02-01, 02-03, 02-06 | Blog generator (long-form, minor_or_above, nil-tolerant) wired into defaults | SATISFIED | blog.Generator 154 lines; 33 tests; defaults.go includes blog.New() + pages.Publisher{}; wired into worker via buildReleaseDispatcher |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | No TBD/FIXME/XXX/placeholder markers found in any phase-modified file | — | — |

No debt markers, no hardcoded empty returns in rendering paths, no orphaned artifacts. The `return nil` occurrences in `pages/pages.go` are intentional graceful-degradation returns (disabled config, BranchCommitter absent, loop success) — not stubs.

### Human Verification Required

The following item requires human confirmation before status can be upgraded to `passed`:

#### 1. Migration 0006 up→down→up Round-Trip

**Test:** With a live Postgres (pg16) reachable, run:
1. `make migrate` — expect migration 0006 to apply cleanly
2. `make migrate-down` — expect the `release_docs_state` table to be dropped
3. `make migrate` again — expect 0006 to re-apply cleanly
4. Optional: `psql $DATABASE_URL -c '\d release_docs_state'` to confirm the `UNIQUE` constraint on `(provider, repo_full_name, to_tag, artifact_kind)`

**Expected:** All three steps exit 0; `release_docs_state` table present after steps 1 and 3, absent after step 2.

**Why human:** This requires a running pg16 instance (not available in automated verification). The plan declared this a `checkpoint:human-verify gate="blocking"` task. The SUMMARY states it was confirmed by operator before phase completion; this human verification item is carried forward for documentation completeness. If the operator has already approved this (see SUMMARY.md for plan 02: "round-trips up→down→up confirmed by operator against live Postgres"), this item may be marked as resolved.

---

## Gaps Summary

No automated gaps found. All 5 ROADMAP success criteria are verified at the code level. The only remaining item is the DB round-trip human check (SC3 sub-check), which the SUMMARY states was already confirmed by operator during execution.

If the operator has already approved the migration round-trip (per 02-02-SUMMARY.md: "Goose migration 0006 creates release_docs_state... round-trips up→down→up confirmed by operator against live Postgres"), they may re-confirm and the status can be upgraded to `passed`.

---

_Verified: 2026-06-05T19:23:53Z_
_Verifier: Claude (gsd-verifier)_
