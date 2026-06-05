---
phase: 01-generators-publishers-cli
plan: "03"
subsystem: releasedocs/template
tags: [template, embed, go-embed, text-template, presets, override, releasedocs]
dependency_graph:
  requires:
    - releasedocs.ArtifactKind (01-01)
    - releasedocs.ReleaseContext (01-01)
    - releasedocs.FileFetcher (01-01)
    - releasedocstest.Fake (01-01)
  provides:
    - template.LoadPreset(kind, tone) (*text/template.Template, error)
    - template.Render(tmpl, data) (string, error)
    - template.Resolve(ctx, rc, kind, overridePath, tone) (*text/template.Template, error)
    - template.Data (TemplateData: ToRef, FromRef, Groups []ChangeGroup)
    - template.ChangeGroup (Title, Items []ChangeItem)
    - template.ChangeItem (Summary, Author, PR *PRRef)
    - template.PRRef (Number int64, URL string)
    - template.ErrUnknownKind
    - presets/changelog.tmpl (keep-a-changelog style, grouped sections)
    - presets/release-notes-concise.tmpl
    - presets/release-notes-detailed.tmpl
    - presets/release-notes-marketing.tmpl
  affects:
    - internal/releasedocs/releasedocstest/fake.go (added FetchErr field)
tech_stack:
  added:
    - embed (stdlib) — go:embed presets/*.tmpl into embed.FS (new pattern, first usage in repo)
    - text/template (stdlib) — template.New().Parse() + Execute (new pattern, first usage in repo)
  patterns:
    - go:embed presets/*.tmpl into embed.FS (no existing analog in codebase)
    - text/template with no custom FuncMap (security T-03-01)
    - isMissingFile helper mirrors orchestrator.isMissingFile (404-tolerant fallback)
    - Override loaded from rc.ToRef tree via releasedocs.FileFetcher (T-03-02)
key_files:
  created:
    - internal/releasedocs/template/template.go
    - internal/releasedocs/template/template_test.go
    - internal/releasedocs/template/presets/changelog.tmpl
    - internal/releasedocs/template/presets/release-notes-concise.tmpl
    - internal/releasedocs/template/presets/release-notes-detailed.tmpl
    - internal/releasedocs/template/presets/release-notes-marketing.tmpl
  modified:
    - internal/releasedocs/releasedocstest/fake.go (added FetchErr field for error injection)
decisions:
  - "Data (not TemplateData) — revive stutter rule: template.TemplateData stutters; renamed to template.Data per exported revive convention"
  - "noFileFetcherProvider local test type (not OmitFileFetcher) — all releasedocstest.Fake wrapper types embed *Fake and inherit FetchFileFromRef; OmitFileFetcher cannot prevent method-set from satisfying releasedocs.FileFetcher; local concrete type with no FetchFileFromRef correctly tests the fallback path"
  - "FetchErr field added to releasedocstest.Fake — needed for TestTemplateOverride/fallback_on_missing_file to simulate 404 errors without importing VCS adapter error types (Rule 2 auto-fix)"
metrics:
  duration_minutes: 25
  completed_date: "2026-06-05"
  tasks_completed: 2
  files_created: 6
  files_modified: 1
---

# Phase 1 Plan 03: Template Subsystem (go:embed + Loader + Override Resolver) Summary

**One-liner:** Embedded preset templates (changelog + 3 release-notes tones) via go:embed, LoadPreset/Render/Resolve API with 404-tolerant tag-tree override, no OS-exposing FuncMap.

## What Was Built

### internal/releasedocs/template/template.go

Net-new infrastructure — `go:embed` and `text/template` had zero prior usage in the codebase.

- `//go:embed presets/*.tmpl` into `embed.FS` (`presetFS`).
- `LoadPreset(kind ArtifactKind, tone string) (*template.Template, error)` — dispatches on kind/tone to select the embedded preset file name, reads from `presetFS`, parses with `text/template.New().Parse()`.
- `Render(tmpl *template.Template, data any) (string, error)` — executes into a `strings.Builder`; no I/O.
- `Resolve(ctx, rc ReleaseContext, kind, overridePath, tone)` — when `overridePath` is non-empty, type-asserts `rc.Provider` for `releasedocs.FileFetcher`, fetches from `rc.ToRef` (never main — T-03-02); 404-tolerant via `isMissingFile` (mirrors `orchestrator.isMissingFile`); falls back to `LoadPreset` on missing file or absent capability.
- `Data` struct (ToRef, FromRef, `Groups []ChangeGroup`) — the public rendering contract; templates receive only this (no custom FuncMap — T-03-01).
- `ChangeGroup`, `ChangeItem`, `PRRef` — structured change model entries.
- `ErrUnknownKind` — sentinel for unrecognized artifact kind.

Security: no `FuncMap` registered; `text/template` (not `html/template`) correct for Markdown output.

### Preset templates (presets/*.tmpl)

Four embedded presets, all deterministic:

- `changelog.tmpl` — keep-a-changelog style; grouped `## [vX.Y.Z]` with `### Section` headings and `- [#N](URL) Summary (author)` items.
- `release-notes-concise.tmpl` — compact single-line-per-group format.
- `release-notes-detailed.tmpl` — bold item summaries with PR link + author attribution.
- `release-notes-marketing.tmpl` — headline `## What's New in vX.Y.Z` with per-group bullet lists.

### internal/releasedocs/template/template_test.go

- `TestEmbeddedPresets` — 5 subtests (changelog + 3 tones + empty-tone default); each asserts LoadPreset succeeds, renders non-empty, and two renders are identical (determinism).
- `TestTemplateOverride` — 4 subtests:
  1. Override present and fetched from ToRef tree — override template rendered, not preset.
  2. Fetch returns "404: not found" — 404-tolerant fallback to embedded preset.
  3. No overridePath — preset used directly; output matches direct `LoadPreset` call.
  4. Provider does not implement `releasedocs.FileFetcher` — type-assertion fails, preset used.
- `noFileFetcherProvider` — minimal `vcs.Provider` concrete type with no `FetchFileFromRef` method; compile-time asserted against `vcs.Provider`.

### internal/releasedocs/releasedocstest/fake.go (modified)

Added `FetchErr error` field: when non-nil, `FetchFileFromRef` returns that error instead of `FileContent`. Required to inject 404-style errors in `TestTemplateOverride/fallback_on_missing_file`.

## Task Commits

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1+2 | Embedded presets + go:embed loader + Resolve override resolver | b0bf2c2 | template.go, template_test.go, presets/*.tmpl (6 files) |
| deviation | Add FetchErr to releasedocstest.Fake for error injection | 4aa10f6 | releasedocstest/fake.go |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical Functionality] Added FetchErr to releasedocstest.Fake**
- **Found during:** Task 2 — TestTemplateOverride/fallback_on_missing_file needed to inject a 404-style error into FetchFileFromRef
- **Issue:** releasedocstest.Fake.FetchFileFromRef always returned `(f.FileContent, nil)` — no way to simulate errors without importing VCS adapter error types
- **Fix:** Added `FetchErr error` field; FetchFileFromRef returns it when non-nil
- **Files modified:** internal/releasedocs/releasedocstest/fake.go
- **Commit:** 4aa10f6

**2. [Rule 1 - Bug] Renamed TemplateData to Data (revive stutter)**
- **Found during:** Task 1 — lint check revealed `template.TemplateData` stutters under `exported` revive rule
- **Issue:** Package is named `template`; `TemplateData` at call sites becomes `template.TemplateData` (stutter violation)
- **Fix:** Renamed to `Data` throughout; callers use `rdtemplate.Data` (cleaner)
- **Files modified:** template.go, template_test.go
- **Commit:** b0bf2c2 (applied before commit)

**3. [Rule 1 - Bug] noFileFetcherProvider test type instead of OmitFileFetcher**
- **Found during:** Task 2 — TestTemplateOverride/provider_without_file_fetcher_uses_preset failed (rendered empty output)
- **Issue:** All releasedocstest.Fake wrapper types embed `*Fake` and inherit `FetchFileFromRef`. Type-asserting to `releasedocs.FileFetcher` succeeds even with `OmitFileFetcher()` because the method is present on the concrete type. The option only narrows the `vcs.Provider` interface view, not the full method set. With `FileContent == nil`, FetchFileFromRef returned `nil, nil`, so `template.New("override:...").Parse("")` parsed an empty template that rendered empty string.
- **Fix:** Added `noFileFetcherProvider` minimal concrete type in template_test.go that implements `vcs.Provider` but has no `FetchFileFromRef` method, making the type-assertion correctly return `(nil, false)`.
- **Files modified:** template_test.go
- **Commit:** b0bf2c2

## Known Stubs

None — all four preset templates render deterministic content from the `Data` struct. No hardcoded empty values, no placeholder text, no unconnected data sources.

## Threat Flags

None — no new network endpoints, no auth paths introduced. The template package is a pure in-process renderer: it reads embedded files at startup and executes text/template against caller-supplied data only.

## Self-Check: PASSED

Files exist on disk:
- internal/releasedocs/template/template.go: FOUND
- internal/releasedocs/template/template_test.go: FOUND
- internal/releasedocs/template/presets/changelog.tmpl: FOUND
- internal/releasedocs/template/presets/release-notes-concise.tmpl: FOUND
- internal/releasedocs/template/presets/release-notes-detailed.tmpl: FOUND
- internal/releasedocs/template/presets/release-notes-marketing.tmpl: FOUND
- internal/releasedocs/releasedocstest/fake.go (modified): FOUND

Commits verified in git log: b0bf2c2, 4aa10f6
Tests: 11 passed (TestEmbeddedPresets + TestTemplateOverride, -race -count=1)
Lint: 0 issues (golangci-lint v2 on ./internal/releasedocs/...)
Vet: clean (go vet ./internal/releasedocs/template/...)
