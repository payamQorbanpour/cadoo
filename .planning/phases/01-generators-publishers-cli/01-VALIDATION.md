---
phase: 1
slug: generators-publishers-cli
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-04
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `01-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (table-driven; `httptest.NewServer` for adapter tests — mirror `internal/vcs/github/github_test.go`) |
| **Config file** | none — Go built-in |
| **Quick run command** | `go test ./internal/releasedocs/...` |
| **Full suite command** | `make test` (= `go test -race -count=1 ./...`) |
| **Estimated runtime** | ~30–90 seconds (full suite, race) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/releasedocs/...` (+ the touched adapter package, e.g. `./internal/vcs/github/...`)
- **After every plan wave:** Run `make test` (`go test -race -count=1 ./...`)
- **Before `/gsd:verify-work`:** `make ci` (vet + test + build) green, **plus** a successful dogfood run on Cadoo's own repo (`cadoo release-docs --repo payamqorbanpour/cadoo --from <prev tag> --to <tag>`), run **twice** to prove idempotency.
- **Max feedback latency:** ~90 seconds

---

## Per-Task Verification Map

> Task IDs are provisional (`{plan}-{task}`) and will be reconciled to the actual PLAN.md task IDs during planning. Every row maps to a Phase-1 requirement and a net-new test established in Wave 0.

| Requirement | Behavior | Test Type | Automated Command | File Exists |
|-------------|----------|-----------|-------------------|-------------|
| REQ-release-artifact-generation | grouped change model parses conventional + labels | unit (table) | `go test ./internal/releasedocs/... -run TestGroupedModel` | ❌ W0 |
| REQ-release-artifact-generation | changelog renders expected section from fixture context | golden | `go test ./internal/releasedocs/generators/changelog/... -run TestChangelogGolden` | ❌ W0 |
| REQ-release-artifact-generation | release-notes builds skeleton with nil LLM | unit | `go test ./internal/releasedocs/generators/releasenotes/... -run TestSkeletonNoLLM` | ❌ W0 |
| REQ-release-artifact-generation | semver bump computation `vX.Y.Z → bump` | unit (table) | `go test ./internal/releasedocs/... -run TestBump` | ❌ W0 |
| REQ-per-artifact-toggles | `Enabled(cfg,bump)` matrix (enabled × `when:` × bump) | unit (table) | `go test ./internal/releasedocs/... -run TestEnabledMatrix` | ❌ W0 |
| REQ-configurable-templates | preset render vs custom `template:` override from tag tree | unit | `go test ./internal/releasedocs/template/... -run TestTemplateOverride` | ❌ W0 |
| REQ-configurable-templates | embedded presets load via `go:embed` | unit | `go test ./internal/releasedocs/template/... -run TestEmbeddedPresets` | ❌ W0 |
| REQ-release-docs-idempotency | run dispatcher twice → release body edited not duplicated; single changelog PR (marker reconstruction) | integration (fake provider) | `go test ./internal/releasedocs/... -run TestIdempotentTwiceRun` | ❌ W0 |
| REQ-configurable-trigger | `release-docs` CLI parses flags → ReleaseJob; URL forms (`--repo/--from/--to`, `--mr`) | unit | `go test ./cmd/cadoo-cli/... -run TestReleaseDocsFlags` | ❌ W0 |
| REQ-publish-destinations | releasebody splice preserves user content outside markers | unit | `go test ./internal/releasedocs/publishers/releasebody/... -run TestSplicePreserves` | ❌ W0 |
| REQ-publish-destinations | changelogpr opens-then-updates a single PR via marker key | integration (fake) | `go test ./internal/releasedocs/publishers/changelogpr/... -run TestSinglePR` | ❌ W0 |
| (cross-cutting) | provider missing a capability → generator/publisher skipped with logged reason | unit (fake w/o capability) | `go test ./internal/releasedocs/... -run TestGracefulDegradation` | ❌ W0 |

*Status legend: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

The repo today has **no `testdata/`/golden-file convention** and **no fake-provider helper** — both are net-new infrastructure this phase introduces. Adapter tests today use `httptest.NewServer` (not interface fakes).

- [ ] `internal/releasedocs/context_test.go` — grouped change model (conventional/labels) + bump matrix → REQ-release-artifact-generation
- [ ] `internal/releasedocs/generators/changelog/changelog_test.go` + `testdata/*.golden` — **establishes the repo's first golden-file convention**
- [ ] `internal/releasedocs/generators/releasenotes/releasenotes_test.go` — nil-LLM skeleton path
- [ ] `internal/releasedocs/template/template_test.go` + `presets/*.tmpl` — embed + override loading
- [ ] `internal/releasedocs/dispatcher_test.go` — fake `vcs.Provider` implementing/omitting capabilities; twice-run idempotency; graceful degradation
- [ ] `internal/releasedocs/publishers/{releasebody,changelogpr}/*_test.go` — splice + single-PR idempotency
- [ ] `internal/vcs/{github,gitlab}/*_test.go` additions — new capability methods via `httptest.NewServer` (mirror existing adapter test stubs)
- [ ] `cmd/cadoo-cli/releasedocs_test.go` — flag→ReleaseJob mapping, URL parsing (reuse `parseTargetURL`)
- [ ] **Shared fake provider** — a `releasedocs`-local fake implementing `vcs.Provider` + the three new capabilities, with toggles to omit a capability (for degradation tests)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| End-to-end dogfood on Cadoo's own repo | (Success Criterion 6) | Requires real GitHub auth, a real tag range, and a live PR/release; not reproducible as a pure unit test | Run `cadoo release-docs --repo payamqorbanpour/cadoo --from <prev tag> --to <tag>` against a real token; confirm a grouped changelog PR opens and the release body is marker-wrapped. Re-run; confirm the PR/branch and release body are edited in place (no duplicates). |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
