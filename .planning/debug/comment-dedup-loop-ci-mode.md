---
slug: comment-dedup-loop-ci-mode
status: resolved
trigger: "Cadoo posts 2 inline comments on first push. After fixing them and pushing again, comment count grows to 8 and keeps increasing on each subsequent push."
created: 2026-05-19
updated: 2026-05-19
---

## Symptoms

- expected: On re-push, prior inline comments should be deduped — existing comments edited in place, already-fixed issues suppressed, no multiplication
- actual: Comment count grows with each push (2 → 8 → more), creating a runaway loop
- errors: No errors visible in logs
- timeline: Always (from first run, not a regression)
- reproduction: Push → Cadoo reviews (2 comments) → Fix issues → Push again → 8 comments appear
- code_path: CI mode (cadoo-cli with --pr or --mr flag)

## Current Focus

hypothesis: "Root cause found and fixed."
test: ""
expecting: ""
next_action: "complete"
reasoning_checkpoint: ""
tdd_checkpoint: ""

## Evidence

- timestamp: 2026-05-19T00:00:00Z
  file: internal/vcs/marker.go
  observation: "InlineMarker only embedded tool/sk/sev in the hidden marker. No normalized title (NT) was stored. ParseInlineMarker returned MD with empty NT."

- timestamp: 2026-05-19T00:00:00Z
  file: internal/findings/prior.go
  observation: "NewFromPrior seeded findingRec.NormalizedTitle from normalizeTitle(pi.Title) where pi.Title is the first line only. For 'improve' suggestions, first line is always '**Suggestions:**' — normalizes to 'suggestions:' (1 token). Jaccard with full-body wantTokens is always <<0.5."

- timestamp: 2026-05-19T00:00:00Z
  file: internal/findings/findings.go
  observation: "memoryStore.has() uses titleTokens(c.Body) (full body, many tokens) for Jaccard against r.NormalizedTitle (1 token for seeded improve records). Jaccard = 1/N always fails for improve suggestions."

- timestamp: 2026-05-19T00:00:00Z
  file: internal/vcs/gitlab/gitlab.go
  observation: "ListCadooArtifacts silently dropped unanchored notes (ok && n.Position == nil) — markers in those notes were never parsed and those findings were never seeded into the dedup store."

## Eliminated Hypotheses

- priorStore returning nil: Ruled out — NewFromPrior never returns nil; only ListCadooArtifacts failure would do that (which logs a warning).
- PRKey mismatch: Ruled out — provider.Kind() and target.Provider string values match consistently.
- Pagination bug in ListCadooArtifacts: Ruled out — doneC/doneT flags correctly break after first page for small PRs.
- StructuralKey mismatch due to severity prefix: Ruled out — formatSeverity prefix is prepended in PostInlineComments and stripped in ListCadooArtifacts before marker parsing.

## Resolution

root_cause: "The hidden <!-- cadoo:fp --> inline marker did not store the full-body normalized title (NT). On push 2, NewFromPrior seeded dedup records using only the first-line title — normalizeTitle('**Suggestions:**') = 'suggestions:' (1 token). The Jaccard fallback in memoryStore.has() compared many-token full-body wantTokens against this 1-token NormalizedTitle, always scoring far below the 0.5 threshold. Since LLMs produce non-identical output across runs (rationale/code varies slightly), the StructuralKey also differed. Both dedup checks failed, so every push re-posted all improve suggestions, causing linear comment growth. Unanchored GitLab notes (lines outside the diff) were a secondary issue: their markers were silently ignored by ListCadooArtifacts (ok && n.Position == nil was unhandled), so those findings were always re-posted."

fix: "1. Added nt= field (base64url-encoded normalizeTitle(body)) to the <!-- cadoo:fp --> marker (InlineMarker/ParseInlineMarker in internal/vcs/marker.go). 2. StampInline now populates MarkerData.NT with normalizeTitle(c.Body). 3. PriorInline gained a NormalizedTitle field. 4. Both GitHub and GitLab ListCadooArtifacts extract md.NT into PriorInline.NormalizedTitle. 5. NewFromPrior seeds findingRec.NormalizedTitle from pi.NormalizedTitle when present, falling back to normalizeTitle(pi.Title) for legacy markers. 6. GitLab ListCadooArtifacts now handles unanchored notes (ok && n.Position == nil) by parsing the file path from the note header and seeding the dedup store (with empty ExternalID). Added parseUnanchoredFile helper."

verification: "go test -race -count=1 ./... → 162 passed. New regression tests: TestNewFromPriorJaccardWithNT, TestNewFromPriorJaccardLegacyFallback, TestInlineMarkerRoundTripWithNT, TestInlineMarkerLegacyNoNT, TestCIModeSuppressesRephrasedImproveOnPush2."

files_changed: "internal/vcs/marker.go, internal/vcs/vcs.go, internal/vcs/github/github.go, internal/vcs/gitlab/gitlab.go, internal/findings/prior.go, internal/vcs/marker_test.go, internal/findings/prior_test.go, internal/orchestrator/reviewer_test.go"
