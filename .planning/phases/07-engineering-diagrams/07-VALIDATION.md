---
phase: 7
slug: engineering-diagrams
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-13
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `07-RESEARCH.md` → Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (1.26) |
| **Config file** | none — `go test` convention |
| **Quick run command** | `go test -count=1 ./internal/releasedocs/generators/diagrams/... ./internal/config/...` |
| **Full suite command** | `make test` (`go test -race -count=1 ./...`) |
| **Estimated runtime** | ~quick <5s / full ~60s+ |

---

## Sampling Rate

- **After every task commit:** Run `go test -count=1 ./internal/releasedocs/generators/diagrams/... ./internal/config/...`
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** `make ci` (vet + test + build) must be green
- **Max feedback latency:** ~5 seconds (quick), ~60 seconds (full)

---

## Per-Task Verification Map

> Populated by the planner/executor as task IDs are assigned. Requirement → behavior anchors below come from RESEARCH.md.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 07-NN-NN | NN | N | DIAG-01 | — | Enable gate + per-type selection honored | unit | `go test -run TestDiagrams_Enabled ./internal/releasedocs/generators/diagrams/...` | ❌ W0 | ⬜ pending |
| 07-NN-NN | NN | N | DIAG-02 | — | Each configured source → one artifact fetched at ToRef | unit | `go test -run TestDiagrams_GenerateMulti ./internal/releasedocs/generators/diagrams/...` | ❌ W0 | ⬜ pending |
| 07-NN-NN | NN | N | DIAG-03 | — | Deterministic idempotent pages paths | unit | `go test -run 'TestPublish_Diagrams\|TestIdempotent' ./internal/releasedocs/publishers/pages/...` | ❌ W0 | ⬜ pending |
| 07-NN-NN | NN | N | DIAG-04 | — | Per-source skip with logged reason, siblings unaffected | unit | `go test -run TestDiagrams_Skip ./internal/releasedocs/generators/diagrams/...` | ❌ W0 | ⬜ pending |
| 07-NN-NN | NN | N | DIAG-05 | — | LLM-off deterministic golden output | unit (golden) | `go test -run TestDiagrams_Golden ./internal/releasedocs/generators/diagrams/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/releasedocs/generators/diagrams/diagrams_test.go` — stubs for DIAG-01, DIAG-02, DIAG-04, DIAG-05 (reuse the `fakeFetcher` shape from `apidocs_test.go`)
- [ ] `internal/releasedocs/generators/diagrams/testdata/` + `testdata/golden/` fixtures
- [ ] `internal/releasedocs/publishers/pages/pages_diagrams_test.go` — DIAG-03 path + idempotency (mirror `pages_apidocs_test.go`)
- [ ] Framework install: none — stdlib `testing` already present

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Dogfood: published diagram pages render on github.com file view | DIAG-02 / SC-5 | Rendering is github.com UI behavior, not asserted by Go tests | Commit a couple of `.mmd` sources, run `cadoo release-docs` against Cadoo's repo, open the published `releases/<tag>/diagrams/<type>/<name>.md` page on github.com and confirm the mermaid fence renders |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
