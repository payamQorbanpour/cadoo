---
phase: 03
slug: api-docs-openapi
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-05
---

# Phase 03 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `03-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (stdlib, `-race`; no external test framework) |
| **Config file** | none — uses `make test` |
| **Quick run command** | `go test -race -run TestAPIDocs ./internal/releasedocs/generators/apidocs/...` |
| **Full suite command** | `make test` (`go test -race -count=1 ./...`) |
| **Estimated runtime** | ~3–5 s (apidocs package) / full suite per repo norm |

---

## Sampling Rate

- **After every task commit:** Run `go test -race -run TestAPIDocs ./internal/releasedocs/generators/apidocs/...`
- **After every plan wave:** Run `make test` (full suite — includes the pages backward-compat regression)
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** ~5 seconds (apidocs package quick run)

---

## Per-Task Verification Map

> Task IDs (`03-PP-TT`) are assigned by the planner. Each apidocs task MUST map to one
> automated `go test -run` command below (or declare a Wave 0 dependency that creates the test).
> Requirement → test mapping is locked from RESEARCH.md § Phase Requirements → Test Map.

| Decision/Req | Behavior | Test Type | Automated Command | File Exists | Status |
|--------------|----------|-----------|-------------------|-------------|--------|
| D-01 spec from committed file | `FetchFileFromRef` called at `rc.ToRef`, result used as spec | unit | `go test -run TestGenerate_FetchesAtToRef ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-02 fallback discovery | Tries fixed path list in order; first hit wins; stops at success | unit (table) | `go test -run TestDiscoverSpec ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-04 three artifacts | `Generate` returns 3 artifacts with correct `Filename` | unit | `go test -run TestGenerate_ThreeArtifacts ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-05 HTML determinism | Same spec + bundle → byte-identical HTML | golden | `go test -run TestBuildRedocHTML_Deterministic ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-05 HTML offline (no CDN) | HTML contains no external script `src` / `cdn.redoc.ly` | unit | `go test -run TestBuildRedocHTML_NoCDN ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-06 Markdown determinism | Same parsed spec → byte-identical Markdown | golden | `go test -run TestRenderMarkdown_Golden ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-07 config gating | `Enabled(cfg, bump)` false when `apiDocs.enabled=false` | unit | `go test -run TestEnabled ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-09 Swagger 2.0 | Processes a Swagger 2.0 spec, emits 3 artifacts | unit | `go test -run TestGenerate_Swagger2 ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-09 OAS 3.0 | Processes an OAS 3.0 spec | unit | `go test -run TestGenerate_OAS3 ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-09 OAS 3.1 | Processes an OAS 3.1 spec | unit | `go test -run TestGenerate_OAS31 ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-10 skip: no spec | All fallback paths 404 → no artifacts, no error | unit | `go test -run TestGenerate_NoSpec_Skips ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-10 skip: parse fail | Malformed YAML/JSON → no artifacts, no error | unit | `go test -run TestGenerate_ParseFailure_Skips ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-10 skip: validation fail | Invalid OAS 3.x spec → no artifacts, no error | unit | `go test -run TestGenerate_ValidationFailure_Skips ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| D-10 skip: unsupported version | OAS 3.2/unknown → skip with logged reason | unit | `go test -run TestGenerate_UnsupportedVersion_Skips ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| Cross-cut: Artifact.Filename | pages publisher routes `.yaml`+`.html`+`.md` to correct paths | unit | `go test -run TestPublish_APIDocs_Paths ./internal/releasedocs/publishers/pages/...` | ❌ W0 | ⬜ pending |
| Cross-cut: backward compat | Existing changelog/releasenotes/blog `.md` paths unchanged | regression | `go test ./internal/releasedocs/publishers/pages/...` | ✅ exists | ⬜ pending |
| Idempotency | Two Publish calls, same inputs → same UpsertFile paths | unit | `go test -run TestIdempotent_APIDocs ./internal/releasedocs/publishers/pages/...` | ❌ W0 | ⬜ pending |
| Raw passthrough | `openapi.yaml` artifact == fetched bytes (no re-serialization) | unit | `go test -run TestGenerate_RawSpecPassthrough ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |
| Security: `$ref` SSRF | Remote/file `$ref` not resolved (`AllowRemoteReferences:false`) | unit | `go test -run TestGenerate_NoRemoteRef ./internal/releasedocs/generators/apidocs/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `go get github.com/pb33f/libopenapi && go get github.com/pb33f/libopenapi-validator` — add deps to `go.mod`
- [ ] `internal/releasedocs/generators/apidocs/assets/redoc.standalone.js` — vendored Redoc bundle (MIT, pinned)
- [ ] `internal/releasedocs/generators/apidocs/apidocs_test.go` — test stubs covering D-01 → D-10 + cross-cutting
- [ ] `internal/releasedocs/generators/apidocs/testdata/petstore_v2.yaml` — Swagger 2.0 fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/petstore_v3.yaml` — OAS 3.0 fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/petstore_v31.yaml` — OAS 3.1 fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/invalid.yaml` — malformed fixture
- [ ] `internal/releasedocs/generators/apidocs/testdata/golden/markdown_v3.golden` — Markdown golden
- [ ] `internal/releasedocs/generators/apidocs/testdata/golden/markdown_v2.golden` — Markdown golden (Swagger 2.0)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Redoc HTML visually renders offline | D-05 | Visual rendering can't be asserted in `go test`; the `NoCDN` + `Deterministic` unit tests cover the testable invariants | Open a generated `api-reference.html` in a browser with network disabled; confirm the API reference renders fully |

*All other phase behaviors have automated verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 5s (apidocs quick run)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
