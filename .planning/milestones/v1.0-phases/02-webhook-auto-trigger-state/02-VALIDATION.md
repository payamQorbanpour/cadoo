---
phase: 2
slug: webhook-auto-trigger-state
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-05
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) |
| **Config file** | none — `go test -race -count=1 ./...` |
| **Quick run command** | `go test -race -count=1 ./internal/releasedocs/... ./internal/vcs/... ./internal/riverq/...` |
| **Full suite command** | `make test` |
| **Estimated runtime** | ~30 seconds (quick), ~60 seconds (full) |

---

## Sampling Rate

- **After every task commit:** Run `go test -race -count=1 ./internal/releasedocs/... ./internal/vcs/... ./internal/riverq/...`
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|--------|
| 02-01-01 | 02-01 | 1 | REQ-publish-destinations | — | TagReleasePublisher fixes GitLab release-body | unit | `go build ./internal/vcs/... && go test ./internal/releasedocs/publishers/releasebody/...` | ⬜ pending |
| 02-01-02 | 02-01 | 1 | REQ-release-artifact-generation | — | Blog ArtifactConfig + KindBlog constant present | unit | `go test -race -count=1 ./internal/config/...` | ⬜ pending |
| 02-02-01 | 02-02 | 1 | REQ-release-docs-idempotency | — | migration round-trips up→down→up cleanly | integration | `make migrate-down && make migrate` | ⬜ pending |
| 02-03-01 | 02-03 | 2 | REQ-release-artifact-generation (blog) | — | blog generates only on minor/major bump | unit | `go test ./internal/releasedocs/generators/blog/...` | ⬜ pending |
| 02-04-01 | 02-04 | 2 | REQ-publish-destinations (pages) | — | pages publisher overwrites same paths on re-run | unit | `go test ./internal/releasedocs/publishers/pages/...` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Existing `internal/releasedocs/releasedocstest/fake.go` extended with pages/blog stubs
- [ ] Existing `make test` infrastructure covers all new packages

*Existing go test infrastructure covers all phase requirements — no new test framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| GitHub `release: published` webhook end-to-end | REQ-configurable-trigger | Requires live GitHub webhook delivery | Set up ngrok or smee.io, publish a test release, confirm `ReleaseJob` enqueued and processed |
| GitLab release webhook end-to-end | REQ-configurable-trigger | Requires live GitLab webhook delivery | Same as above with GitLab project |
| Pages branch content after publish | REQ-publish-destinations | Requires live GitHub/GitLab token | Run dispatcher with pages publisher enabled; verify `docs/releases/vX.Y.Z/` committed to target branch |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
