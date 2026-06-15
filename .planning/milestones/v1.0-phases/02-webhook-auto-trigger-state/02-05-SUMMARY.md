---
phase: 02-webhook-auto-trigger-state
plan: "05"
subsystem: webhook-ingestion + riverq
tags:
  - webhook
  - river
  - release-docs
  - github
  - gitlab
  - dual-mode-queue
dependency_graph:
  requires:
    - 02-01  # releasedocs.ReleaseJob + releasedocs.Dispatcher declared
    - 02-02  # releasedocs state infra; ReleaseJob.Kind()
    - 02-03  # blog generator (releasedocs subsystem complete for dispatcher)
    - 02-04  # pages publisher (releasedocs subsystem complete for dispatcher)
  provides:
    - riverq.ReleaseArgs + EnqueueRelease + releaseWorker (consume side for 02-06)
    - webhook GitHub/GitLab release+tag event ingestion
    - dual-mode enqueueReleaseFn (River + in-memory)
  affects:
    - cmd/cadoo-webhook (new event routing)
    - internal/riverq (new job kind registration)
    - cmd/cadoo-worker (riverq.New call site updated)
tech_stack:
  added: []
  patterns:
    - TDD RED/GREEN for release/tag handler tests (22 tests)
    - Parallel job kind pattern (ReleaseArgs mirrors ToolArgs)
    - Dual typed enqueue functions (enqueueFn + enqueueReleaseFn, never merged)
    - Pitfall 1 mitigation: strings.TrimPrefix("refs/tags/") in both providers
    - Pitfall 2 mitigation: 202-always, no sync config load in HTTP handler
    - Pitfall 3 mitigation: two separate typed functions, no interface{}
    - Pitfall 4 mitigation: river.AddWorker called before river.NewClient
key_files:
  created:
    - cmd/cadoo-webhook/release.go
    - cmd/cadoo-webhook/release_test.go
  modified:
    - internal/riverq/queue.go
    - cmd/cadoo-webhook/main.go
    - cmd/cadoo-worker/main.go
decisions:
  - "Webhook-side trigger defaults (release/v*) are structural early-exits; authoritative check is in releasedocs.Dispatcher.Run from .cadoo.yaml"
  - "buildEnqueue returns two typed functions (enqueueFn + enqueueReleaseFn) not a struct, keeping each call site readable"
  - "In-memory branch registers release_docs as logged no-op for webhook dev mode; real consumer is worker (02-06)"
  - "releaseDispatcher param in riverq.New is nil at webhook; cadoo-worker (02-06) passes the real dispatcher"
metrics:
  duration_minutes: 7
  completed_date: "2026-06-05"
  tasks_completed: 3
  files_changed: 5
---

# Phase 02 Plan 05: Webhook Release/Tag Ingestion + Dual-Mode ReleaseJob Enqueue Summary

**One-liner:** GitHub/GitLab release and tag-push webhooks build typed ReleaseJobs and enqueue via River (EnqueueRelease) or in-memory queue, with trigger/tagPattern early-exit in handlers and releaseWorker registered in riverq.

## What Was Built

### Task 1: riverq ReleaseArgs + EnqueueRelease + releaseWorker (commit 1dcfe67)

Added a parallel release-job path to `internal/riverq/queue.go` that exactly mirrors the existing `ToolArgs`/`toolWorker` pattern:

- `ReleaseArgs` struct with `Kind()="release_docs"`, JSON-tagged Provider/Repo/Org/FromRef/ToRef fields
- `releaseWorker` embedding `river.WorkerDefaults[ReleaseArgs]`, converting ReleaseArgs → releasedocs.ReleaseJob and calling dispatcher.Run
- `New()` extended with `releaseDispatcher *releasedocs.Dispatcher` parameter — registers `releaseWorker` when non-nil (Pitfall 4 compliance: both workers registered before `river.NewClient`)
- `EnqueueRelease(ctx, ReleaseArgs) error` mirroring EnqueueTool
- Both existing call sites (cadoo-webhook, cadoo-worker) updated to `riverq.New(pool, dispatcher, nil)`

### Task 2: Webhook release/tag handlers + trigger/tagPattern early-exit (commits f9c447a + 6dacce0)

TDD execution (RED then GREEN):

**RED phase:** `cmd/cadoo-webhook/release_test.go` — 22 tests covering:
- `TestHandleGithubRelease_Published` / `TestHandleGithubRelease_NonPublished`
- `TestHandleGithubRelease_TriggerTagExcludesRelease`
- `TestHandleGithubTagPush_Created` / `TestHandleGithubTagPush_Deletion` / `TestHandleGithubTagPush_NonTagRef`
- `TestHandleGithubTagPush_TriggerReleaseExcludesTag` / `TestHandleGithubTagPush_TagPatternMismatch`
- `TestHandleGitlabRelease_Create` / `TestHandleGitlabRelease_NonCreate` / `TestHandleGitlabRelease_TriggerTagExcludesRelease`
- `TestHandleGitlabTagPush_Created` / `TestHandleGitlabTagPush_Deletion` / `TestHandleGitlabTagPush_EmptyAfter` / `TestHandleGitlabTagPush_TriggerReleaseExcludesTag`
- `TestTriggerEarlyExit` (consolidated across all 4 handlers)

**GREEN phase:** `cmd/cadoo-webhook/release.go` — implements `enqueueReleaseFn` type and four handlers:
- `handleGithubRelease`: enqueues on `action=="published"`, trigger early-exit when `trigger!="release"`
- `handleGithubTagPush`: enqueues on creation only (created=true, deleted=false), ref must start with `refs/tags/`, `path.Match(tagPattern, tag)`, trigger must be `"tag"`
- `handleGitlabRelease`: enqueues on `action=="create"`, trigger early-exit
- `handleGitlabTagPush`: enqueues when `After` is non-empty and non-zero-SHA, `path.Match`, trigger must be `"tag"`
- All handlers strip `"refs/tags/"` via `strings.TrimPrefix` (Pitfall 1)
- All handlers return without enqueuing on no-match (202 is returned by the caller, Pitfall 2)

### Task 3: Wire handlers into webhook switches + dual-mode release enqueue (commit 2bbe77d)

Updated `cmd/cadoo-webhook/main.go`:

- GitHub switch: added `case *gogithub.ReleaseEvent` → `handleGithubRelease(...)` and `case *gogithub.PushEvent` → `handleGithubTagPush(...)`
- GitLab switch: added `case *glab.ReleaseEvent` → `handleGitlabRelease(...)` and `case *glab.TagEvent` → `handleGitlabTagPush(...)`
- `buildEnqueue` extended to return `(enqueueFn, enqueueReleaseFn, func())` — River branch uses `rq.EnqueueRelease` to build ReleaseArgs; in-memory branch routes to the memory queue with a logged no-op handler
- Both webhook handler constructors (`githubWebhookHandler`, `gitlabWebhookHandler`) accept and pass `enqueueReleaseFn`
- Webhook-side constants `releaseTrigger="release"` and `tagPattern="v*"` used as structural defaults; authoritative check is in the dispatcher

## Deviations from Plan

None — plan executed exactly as written.

The in-memory branch for `enqueueRelease` uses a logged no-op handler (not a real releasedocs dispatcher) per the plan's guidance: "if no releasedocs dispatcher can be built (no VCS), enqueueRelease becomes a logged no-op." The real dispatcher wiring for the worker binary happens in 02-06.

## TDD Gate Compliance

- RED gate commit: f9c447a (`test(02-05): add failing tests...`) — build fails with undefined: handleGithubRelease etc.
- GREEN gate commit: 6dacce0 (`feat(02-05): implement webhook release/tag handlers...`) — all 22 tests pass
- No REFACTOR phase needed

## Known Stubs

None. The `releaseTrigger` and `tagPattern` constants in the webhook handlers are documented design choices (not stubs): the authoritative config comes from `.cadoo.yaml` at job-run time, which the dispatcher handles.

## Threat Flags

No new threat surface beyond what is in the plan's `<threat_model>`. The four handlers run only after existing HMAC-SHA256 (GitHub) / constant-time token (GitLab) verification. Tag names pass through `strings.TrimPrefix` + `path.Match` before being stored in `ReleaseJob.ToRef`. No raw tag name interpolation into filesystem paths or SQL queries in this plan.

## Self-Check: PASSED

| Item | Status |
|------|--------|
| cmd/cadoo-webhook/release.go | FOUND |
| cmd/cadoo-webhook/release_test.go | FOUND |
| 02-05-SUMMARY.md | FOUND |
| commit 1dcfe67 (Task 1) | FOUND |
| commit f9c447a (Task 2 RED) | FOUND |
| commit 6dacce0 (Task 2 GREEN) | FOUND |
| commit 2bbe77d (Task 3) | FOUND |
