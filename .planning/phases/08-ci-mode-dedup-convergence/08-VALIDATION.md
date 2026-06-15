---
phase: 8
slug: ci-mode-dedup-convergence
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-15
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Go 1.26, `-race`) |
| **Config file** | none — standard `go test`; CI runs `make ci` (vet + test + build) |
| **Quick run command** | `go test -race -count=1 ./internal/orchestrator/... ./internal/findings/... ./internal/vcs/...` |
| **Full suite command** | `make test` (`go test -race -count=1 ./...`) |
| **Estimated runtime** | ~90 seconds (full suite) |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched package(s)
- **After every plan wave:** Run the full suite command
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

> Populated during planning/execution. Each task maps to an automated `go test` assertion or a Wave 0 stub.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 8-01-01 | 01 | 1 | REQ-cidedup-no-self-resolution | — | N/A | unit | `go test -race -run TestResolveStalePriors ./internal/orchestrator/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Convergence/fixed-point integration test scaffold (run review twice against unchanged head → assert 0 new threads + 0 resolved) for REQ-cidedup-convergent-review
- [ ] Resolved-thread suppression table-test fixtures (line-overlap + Jaccard ≥ `ResolvedSuppressThreshold`) for REQ-cidedup-honor-resolves
- [ ] Incremental change-set fixtures (`lastReviewedSHA..head`, first-run, non-ancestor fallback) for REQ-cidedup-incremental-review

*Existing `internal/orchestrator`, `internal/findings`, and `internal/vcs` test infrastructure covers framework setup; only new fixtures above are required.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| End-to-end fixed point on a real GitHub/GitLab PR/MR resync | REQ-cidedup-convergent-review | Requires a live VCS PR/MR and credentials; dogfood | Run `cadoo ci --pr <url>` twice with no code change between runs; confirm no new threads and no resolutions on the second run |

*Automated tests cover the deterministic logic; the live-PR fixed point is the one manual dogfood check (consistent with prior phases' dogfood criteria).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
