---
phase: 07-engineering-diagrams
plan: 02
subsystem: releasedocs
tags: [diagrams, generator, mermaid, multigenerator, golden-test, release-docs]
requires:
  - "releasedocs.KindDiagrams ArtifactKind constant (07-01)"
  - "config.DiagramsConfig struct + cfg.Artifacts.Diagrams field (07-01)"
  - "releasedocs.MultiGenerator + Artifact.Filename + FileFetcher + Enabled (Phase 3)"
provides:
  - "diagrams.Generator implementing releasedocs.Generator + releasedocs.MultiGenerator"
  - "diagrams.New() constructor (consumed by plan 03 DefaultGenerators wiring)"
  - "sniffMermaid + firstSignificantToken + mermaidKeywords (Mermaid validity gate)"
  - "wrapMermaidFence (byte-stable fixed fence) + diagramName (path.Base-safe filename)"
  - "golden test harness + fixtures + committed golden files (sequence_login, class_domain)"
affects:
  - internal/releasedocs/generators/diagrams/
tech-stack:
  added: []
  patterns:
    - "Ordered-slice type iteration (diagramTypes) for deterministic artifact order — Pitfall 2"
    - "Family-level (nil,nil) skip on absent FileFetcher; per-source slog.Warn+continue — D-08"
    - "Fixed bytes.Buffer wrapper (no text/template, no embed) for byte-stable golden output — D-05/D-06"
    - "path.Base-only filename derivation (path-traversal-safe by construction) — T-07-03"
    - "Golden write-or-compare via TEST_UPDATE_GOLDEN=1 (cloned from apidocs harness)"
key-files:
  created:
    - internal/releasedocs/generators/diagrams/sniff.go
    - internal/releasedocs/generators/diagrams/render.go
    - internal/releasedocs/generators/diagrams/diagrams.go
    - internal/releasedocs/generators/diagrams/diagrams_test.go
    - internal/releasedocs/generators/diagrams/testdata/login.mmd
    - internal/releasedocs/generators/diagrams/testdata/domain.mmd
    - internal/releasedocs/generators/diagrams/testdata/frontmatter.mmd
    - internal/releasedocs/generators/diagrams/testdata/commented.mmd
    - internal/releasedocs/generators/diagrams/testdata/not-mermaid.txt
    - internal/releasedocs/generators/diagrams/testdata/golden/sequence_login.golden
    - internal/releasedocs/generators/diagrams/testdata/golden/class_domain.golden
  modified: []
decisions:
  - "Dependency keyword set adopted as {flowchart, graph, erDiagram} (RESEARCH Q3 confirmed)"
  - "sniffMermaid answers strict per-type validity only; first-listed wins on duplicate basename (Pitfall 3)"
  - "Generator NOT registered in DefaultGenerators (that is plan 03)"
metrics:
  duration: ~12 min
  completed: 2026-06-13
requirements: [DIAG-02, DIAG-04, DIAG-05]
---

# Phase 07 Plan 02: Diagrams Generator Summary

A deterministic, LLM-free `MultiGenerator` for the engineering-diagrams artifact family. It fetches each user-configured committed Mermaid source at the release tag, sniffs it for a type-appropriate Mermaid keyword, wraps valid sources in a fixed ` ```mermaid ` markdown fence, and emits one `Artifact` per resolved source with a `diagrams/<type>/<base>.md` sub-path — a mechanical clone of the Phase 3 `apidocs` generator, simpler (no `text/template`, no `embed`). Ships with the golden-file test harness, fixtures, and committed golden files.

## What Was Built

- **Task 1 — `sniff.go` + `render.go`** (commit `05589d1`): `sniff.go` defines the package-level `mermaidKeywords` map (sequence→`{sequenceDiagram}`, class→`{classDiagram}`, state→`{stateDiagram}` prefix-match, flowchart→`{flowchart,graph}`, dependency→`{flowchart,graph,erDiagram}`), `firstSignificantToken` (skips leading blanks, `%%` comment lines, and a `---`…`---` frontmatter block — Pitfall 1), and `sniffMermaid(src, type)` using `strings.HasPrefix`. `render.go` defines `wrapMermaidFence` (fixed `bytes.Buffer`, `bytes.TrimRight` body, normalized single trailing newline — byte-stable) and `diagramName` (`path.Base` ONLY, strips `.mmd`/`.mermaid` — Pitfall 4 / T-07-03). Package doc comment lives in `sniff.go` only.
- **Task 2 — `diagrams.go`** (commit `7a5c0a5`): `Generator` with `New`/`Kind`/`Enabled`/`Generate`/`GenerateMulti`, mirroring `apidocs.go`. `Enabled` reads `cfg.Artifacts.Diagrams.ArtifactConfig`, coerces empty `When`→`"always"` (D-08), delegates to `releasedocs.Enabled`. Package-level ordered `diagramTypes` slice (sequence, dependency, state, flowchart, class — NOT a map, Pitfall 2). `GenerateMulti` type-asserts `rc.Provider.(releasedocs.FileFetcher)` → `(nil,nil)` family skip on absence; for each `(type, path)` fetches at `rc.ToRef`, skips on fetch error / non-Mermaid / duplicate same-type basename via `slog.Warn`+`continue` (never an error); emits `Artifact{Kind:KindDiagrams, Filename:"diagrams/<type>/<base>.md", Content:wrapMermaidFence(b)}`. Both compile-time interface assertions present.
- **Task 3 — test harness + fixtures + golden** (commit `5d69d13`): `diagrams_test.go` (`package diagrams_test`) clones the apidocs harness — `updateGolden`, `fakeFetcher` (embeds `releasedocstest.NewFake()` provider, 404 on absent path), `fixtureDiagramsRC`, `mustReadFixture`, `findArtifact`. Seven test functions: `TestDiagrams_Kind`, `TestDiagrams_Enabled` (table, DIAG-01), `TestDiagrams_GenerateMulti` (2 artifacts, sequence before class, DIAG-02), `TestDiagrams_Skip` (missing + non-Mermaid skipped, valid sibling emitted, no error, DIAG-04), `TestDiagrams_NoFileFetcher` (family skip via `noFetchProvider` interface-embed), `TestDiagrams_Golden` (byte-stable vs committed golden, DIAG-05), `TestDiagrams_Frontmatter_And_Comments` (Pitfall 1). Fixtures: `login.mmd`, `domain.mmd`, `frontmatter.mmd`, `commented.mmd`, `not-mermaid.txt`. Golden files generated with `TEST_UPDATE_GOLDEN=1` and committed.

## Verification

- `go build ./internal/releasedocs/generators/diagrams/...` — success.
- `go vet ./internal/releasedocs/generators/diagrams/...` — no issues.
- `go test -race -count=1 ./internal/releasedocs/generators/diagrams/...` — 12 passed.
- Golden re-run WITHOUT `TEST_UPDATE_GOLDEN` is byte-identical (DIAG-05 determinism).
- `golden/sequence_login.golden` starts with ` ```mermaid\n ` and ends with ` ```\n ` (od-verified).
- `golangci-lint run` on the package — no issues (all exported symbols documented; revive `exported` on).
- No `text/template`, no `embed`, no `/llm` import in the package (only a comment mentions text/template).

## Must-Haves Confirmed

- For each configured `(type, path)`, the source is fetched at `rc.ToRef` and emitted as one `Artifact` (DIAG-02, D-01, D-03) — `TestDiagrams_GenerateMulti`.
- A missing or non-Mermaid source is skipped with a `slog` reason and `continue`, never an error, never aborting siblings (DIAG-04, D-08) — `TestDiagrams_Skip`.
- When the provider does not implement `FileFetcher`, `GenerateMulti` returns `(nil, nil)` (D-08) — `TestDiagrams_NoFileFetcher`.
- Output is byte-stable given the same source + wrapper, no LLM call on any path (DIAG-05, D-05/D-06) — `TestDiagrams_Golden` + import audit.
- Diagram types iterate via a fixed ordered slice, not a Go map (deterministic artifact order) — `diagramTypes`, asserted by sequence-before-class ordering.

## Deviations from Plan

None — plan executed exactly as written. One additive test (`TestDiagrams_NoFileFetcher`) was included to directly cover the family-level no-FileFetcher skip behavior named in the plan's `<behavior>` block; the plan listed five required-name tests and this is an extra, not a substitution. (Rule 2 — completeness for a stated correctness path; no source-code change implied.)

## Known Stubs

None. The generator is fully implemented. It is intentionally NOT yet registered in `DefaultGenerators` — that wiring is plan 03 (documented in the plan objective), so the generator is unreachable from the dispatcher until then. This is a planned sequencing boundary, not a stub.

## Self-Check: PASSED

- `internal/releasedocs/generators/diagrams/sniff.go` — FOUND, contains `func sniffMermaid(` and `mermaidKeywords`.
- `internal/releasedocs/generators/diagrams/render.go` — FOUND, contains `func wrapMermaidFence(` and `func diagramName(`.
- `internal/releasedocs/generators/diagrams/diagrams.go` — FOUND, contains `func (g *Generator) GenerateMulti(` and both interface assertions.
- `internal/releasedocs/generators/diagrams/diagrams_test.go` — FOUND, contains `func TestDiagrams_Golden(`.
- `testdata/golden/sequence_login.golden` + `class_domain.golden` — FOUND.
- Commits `05589d1`, `7a5c0a5`, `5d69d13` — all present in `git log`.
