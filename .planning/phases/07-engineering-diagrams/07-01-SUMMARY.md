---
phase: 07-engineering-diagrams
plan: 01
subsystem: releasedocs
tags: [config, contract, diagrams, release-docs]
requires: []
provides:
  - "releasedocs.KindDiagrams ArtifactKind constant"
  - "config.DiagramsConfig struct (five per-type Mermaid path lists + inline ArtifactConfig gate)"
  - "config.ReleaseArtifacts.Diagrams field reachable at cfg.Artifacts.Diagrams"
  - ".cadoo.yaml.example documented diagrams: block"
affects:
  - internal/releasedocs/releasedocs.go
  - internal/config/config.go
  - .cadoo.yaml.example
tech-stack:
  added: []
  patterns:
    - "Inline-embed ArtifactConfig (single family gate) — mirrors APIDocsConfig/ReleaseNotesConfig"
    - "MultiGenerator artifact kind (one Artifact per resolved source) — mirrors KindAPIDocs"
key-files:
  created: []
  modified:
    - internal/releasedocs/releasedocs.go
    - internal/config/config.go
    - .cadoo.yaml.example
decisions:
  - "Five diagram types are FIXED (sequence, dependency, state, flowchart, class) — D-04"
  - "One family gate (inline enabled + when), no per-type toggles — D-07"
  - "Preset/Template carried by embedded ArtifactConfig but ignored by the v1 diagrams generator (fixed wrapper)"
metrics:
  duration: ~6 min
  completed: 2026-06-13
requirements: [DIAG-01]
---

# Phase 07 Plan 01: Diagrams Contract/Config Layer Summary

Foundation declarations for the engineering-diagrams generator: the `KindDiagrams` artifact-kind constant, the `DiagramsConfig` struct (five fixed per-type Mermaid path lists gated by one inline `ArtifactConfig`), and a documented `diagrams:` block in `.cadoo.yaml.example` — mirroring the Phase 3 `APIDocsConfig`/`KindAPIDocs` pattern exactly. No behavior; pure declarations the generator (plan 02) and wiring (plan 03) read.

## What Was Built

- **Task 1 — `KindDiagrams` constant** (`internal/releasedocs/releasedocs.go`, commit `859e820`): appended `KindDiagrams ArtifactKind = "diagrams"` to the recognized-kinds const block with a docstring describing the MultiGenerator one-page-per-Mermaid-source contract (`Filename` differentiates artifacts, e.g. `diagrams/sequence/login.md`). No interface touched.
- **Task 2 — `DiagramsConfig` + wiring** (`internal/config/config.go`, commit `9209387`): added `Diagrams DiagramsConfig \`yaml:"diagrams"\`` to `ReleaseArtifacts` (after `APIDocs`) and a new `DiagramsConfig` struct embedding `ArtifactConfig` inline plus five `[]string` fields (`Sequence`, `Dependency`, `State`, `Flowchart`, `Class`). Single family gate (D-07); no per-type enabled/when. All exported symbols and fields documented (revive `exported` on).
- **Task 3 — `.cadoo.yaml.example` docs** (commit `cfb2fc0`): added a `diagrams:` block under `releaseDocs.artifacts:` (after `apiDocs:`, before `publish:`) with `enabled`/`when` plus the five per-type path lists, each annotated with the Mermaid keyword(s) it accepts. Preceding comment documents mermaid-fence render behavior, per-source graceful skip (D-08), and the Pages-served-site client-side-mermaid caveat.

## Verification

- `go build ./internal/releasedocs/... ./internal/config/...` — success.
- `go test ./internal/config/...` — 2 passed.
- `golangci-lint run` (releasedocs + config) — no issues (no missing-docstring violations for `KindDiagrams`, `DiagramsConfig`, or its fields).
- `go vet` — no issues.
- `.cadoo.yaml.example` parses (`yaml.safe_load`).

## Must-Haves Confirmed

- A user can set `releaseDocs.artifacts.diagrams.enabled` and per-type path lists in `.cadoo.yaml` (D-01, D-04, D-07) — documented block + struct fields present.
- An empty/absent diagram type key carries no paths and produces nothing for that type (D-04) — fields are plain `[]string`, default nil.
- The diagrams family is gated by one `enabled` + `when:` condition via inline `ArtifactConfig`, not per-type toggles (D-07).

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None. These are declaration-only changes by design; the generator that consumes them is plan 02.

## Self-Check: PASSED

- `internal/releasedocs/releasedocs.go` — FOUND, contains `KindDiagrams ArtifactKind = "diagrams"`.
- `internal/config/config.go` — FOUND, contains `Diagrams DiagramsConfig` and `type DiagramsConfig struct`.
- `.cadoo.yaml.example` — FOUND, contains `diagrams:` block.
- Commits `859e820`, `9209387`, `cfb2fc0` — all present in `git log`.
