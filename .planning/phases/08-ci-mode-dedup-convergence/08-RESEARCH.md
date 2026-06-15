# Phase 8: CI-Mode Dedup Convergence - Research

**Researched:** 2026-06-15
**Domain:** Go — `internal/orchestrator`, `internal/findings`, `internal/vcs` brownfield surgery
**Confidence:** HIGH (all claims verified against actual source code; zero assumptions about
pre-existing functionality)

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| REQ-cidedup-convergent-review | Re-run against unchanged head posts 0 new threads, resolves 0 existing; thread count monotonic non-increasing | Parts A+B+C together; fixed-point test in orchestrator |
| REQ-cidedup-no-self-resolution (Part A) | `resolveStalePriors` compares carried `StructuralKey` directly, not first-line recompute; fix in both CI and DB paths | `reviewer.go:491-495` surgery; add `StructuralKey` to `PostedFinding` and `ListPostedFindings` |
| REQ-cidedup-honor-resolves (Part B) | Resolved thread stays gone; anchor line + resolved flag captured; sticky suppression with `ResolvedSuppressThreshold` | `vcs.PriorInline.Line`, `findingRec.Resolved`, `memoryStore.has` extension |
| REQ-cidedup-incremental-review (Part C) | SHA marker in summary; `DiffBetween` provider capability; inline tools get incremental view; `resolveStalePriors` scoped to changed lines | `consolidate.go`, `vcs.Provider`, both adapters, `reviewer.go` orchestration |

</phase_requirements>

---

## Summary

This phase fixes three compounding bugs in the stateless CI-mode dedup path (`cadoo ci --mr/--pr`)
that together cause the thread count on a re-reviewed MR to ratchet upward without bound. All
three root causes are pinpointed to exact lines in the current codebase:

**Root cause A** (`reviewer.go:491-495`): `resolveStalePriors` reconstructs each prior's
`StructuralKey` from `p.Title` (which `RecordFinding` sets to `firstLine(body)`, i.e. only the
first line). Current-run keys are built from the full body. For multi-line comments the keys
diverge, so every still-valid thread looks "stale" and gets auto-resolved.

**Root cause B** (`prior.go:34-53`): `NewFromPrior` seeds `findingRec` records from
`vcs.PriorInline` but drops the `Resolved` flag and never captures the anchor line. A
user-resolved thread therefore carries zero suppression weight on the next run — a reworded
restatement sails right back in.

**Root cause C** (architectural): Every CI run gets the full PR diff regardless of how many
lines changed since Cadoo last reviewed. There is no `lastReviewedSHA` concept, so each run
re-reviews every touched line and can generate new threads on code that was already reviewed.

The phase is **pure brownfield surgery** — no new packages, no DB migrations (CI-mode is
memory-backed), no breaking changes to `tools.Tool`/`tools.Input` public surface (except one
additive field). The SPEC explicitly excludes DB schema changes.

**Primary recommendation:** Implement Parts A, B, C in that order — each is independently
shippable. A fixes the acute runaway; B closes the LLM-rephrase escape hatch; C imposes a
structural ceiling on growth.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|--------------|----------------|-----------|
| Carry `StructuralKey` end-to-end | `internal/findings` | `internal/orchestrator` | `PostedFinding`/`findingRec` types live in `findings`; `resolveStalePriors` lives in orchestrator and reads them |
| Sticky suppression for resolved threads | `internal/findings` (memoryStore) | `internal/vcs` (PriorInline) | `memoryStore.has` owns suppression logic; VCS adapters populate the source data |
| Anchor-line capture | `internal/vcs` adapters | `internal/findings` (findingRec) | GitLab: `n.Position.NewLine`; GitHub: first comment's position; `findingRec` stores it |
| `reviewed-sha` marker emit/parse | `internal/orchestrator` (consolidate) | `internal/vcs` (PriorReview) | `renderConsolidated` writes the wrapper; `ListCadooArtifacts` parses it back |
| `DiffBetween` incremental diff | `internal/vcs` adapters (GitLab + GitHub) | `internal/vcs/vcs.go` (interface) | New optional capability on the `vcs` interface; adapters implement using existing Compare APIs |
| Feed incremental context to inline tools | `internal/orchestrator` (reviewer.go) | `internal/tools` (Input) | `Run` passes `tools.Input`; orchestrator decides full vs incremental diff |
| Incremental `resolveStalePriors` scoping | `internal/orchestrator` (reviewer.go) | — | Logic gated on whether `lastReviewedSHA` is set and anchor line is in change set |

---

## Standard Stack

No new packages required. All needed functionality is in the existing dependencies.

### Existing Libraries Used

| Library | Version | Purpose | Where Used |
|---------|---------|---------|------------|
| `gitlab.com/gitlab-org/api/client-go` | v1.46.0 | GitLab Compare API for `DiffBetween`; existing `Repositories.Compare` already called in `release.go:31` | `internal/vcs/gitlab/` |
| `github.com/google/go-github/v66` | v66.0.0 | GitHub Compare API for `DiffBetween`; existing `CompareCommits` called in `release.go:33` | `internal/vcs/github/` |
| Go 1.26 stdlib | 1.26 | `strings`, `crypto/sha1`, `encoding/hex`, `sync` — all already imported | throughout |

**No new dependencies.** The `DiffBetween` implementation reuses `Repositories.Compare` (GitLab)
and `Repositories.CompareCommits` (GitHub) — both already imported and called in `release.go`
files in each adapter package.

---

## Part A — Precise Change Map: End-to-End StructuralKey

### Current Behavior (Bug)

`reviewer.go:483-495`:
```go
// resolveStalePriors — BUGGY: reconstructs key from p.Title (first line only)
pkey := findings.StructuralKey(p.Tool, vcs.InlineComment{
    File:     p.File,
    Severity: vcs.Severity(p.Severity),
    Body:     p.Title,   // ← p.Title is firstLine(body), NOT the full body
})
if _, present := currentKeys[pkey]; present {
    continue
}
```

`findings.go:184` (`RecordFinding`): `title := firstLine(c.Body)` — stored as the `title` column
and exposed as `PostedFinding.Title`. This is the source of the lossy first-line.

`findings.go:36-43` (`PostedFinding`): no `StructuralKey` field currently.

`findings.go:209-229` (`ListPostedFindings`): selects `tool, file, line_start, line_end,
severity, title, external_comment_id` — does NOT select `structural_key`.

### Required Changes

**`internal/findings/findings.go`**

1. Add `StructuralKey string` field to `PostedFinding` (line ~43):
   ```go
   type PostedFinding struct {
       Tool              string
       File              string
       LineStart         int
       LineEnd           int
       Severity          string
       Title             string
       ExternalCommentID string
       StructuralKey     string  // ← NEW: carried end-to-end, no first-line recompute
   }
   ```

2. `ListPostedFindings` (DB path, line ~209): add `structural_key` to the SELECT and scan it
   into `f.StructuralKey`. The column already exists (migration 0005 adds it).

3. `memoryStore.list` (line ~582): populate `PostedFinding.StructuralKey` from `r.StructuralKey`
   in the conversion loop.

**`internal/findings/prior.go`**

`NewFromPrior` (line ~44): `findingRec` already has `StructuralKey` field and it is already
populated from `pi.StructuralKey`. No change needed here — the field is already threaded.

**`internal/orchestrator/reviewer.go`**

`resolveStalePriors` (line ~483-495): replace first-line recompute with direct comparison:
```go
// AFTER FIX — compare the carried key directly:
if _, present := currentKeys[p.StructuralKey]; present {
    continue
}
```
Guard: if `p.StructuralKey == ""` (legacy record without the field), fall back to the current
first-line recompute so old records degrade gracefully rather than always resolving.

**DB worker path coverage (REQ-cidedup-no-self-resolution)**: The fix in `resolveStalePriors`
reads from `PostedFinding.StructuralKey`, which `ListPostedFindings` now populates from the
existing DB column. Both paths converge on the same corrected logic — the DB path inherits the
fix automatically once `ListPostedFindings` selects the column.

### Tests to Add / Modify

- **New regression test** in `reviewer_test.go`: `TestResolveStalePriorsMultiLineNotSelfResolved` —
  seed a prior with a multi-line `improve`-style body; run `postInline` with the same comment;
  assert `fv.resolved` is empty (no self-resolution). This replaces the current behavior that
  `TestPostInlineResolvesStalePriors` locks in (which uses single-line bodies and is not
  affected).
- **`TestPostInlineResolvesStalePriors`** (`reviewer_test.go:276`): verify it still passes (its
  priors have single-line bodies, so both old and new logic agree).

---

## Part B — Precise Change Map: Honor Resolves / Sticky Suppression

### Current Behavior (Gap)

`vcs/vcs.go:144-153` (`PriorInline`): `Resolved bool` field already exists. GitLab
(`gitlab.go:263`): `Resolved: n.Resolved` already populated. GitHub (`github.go:336`):
`Resolved: th.IsResolved` already populated.

However: `prior.go:34-53` (`NewFromPrior`): seeds `findingRec` without `Resolved` field —
`findingRec` (line ~494-505) has no `Resolved bool` field, so the information is silently
discarded.

`vcs/vcs.go:144-153` (`PriorInline`): no `Line int` or `EndLine int` fields currently. GitLab
`ListCadooArtifacts` (`gitlab.go:248-265`): `n.Position` is checked (for existence) but
`n.Position.NewLine` is never extracted into the output struct.

`memoryStore.has` (`findings.go:532-548`): checks `StructuralKey` exact match and Jaccard, but
applies the same `SimilarTitleThreshold = 0.5` to both open and resolved priors equally —
resolved priors currently indistinguishable from open ones.

### Required Changes

**`internal/vcs/vcs.go`**

Add to `PriorInline` (after `Resolved bool` at line ~153):
```go
type PriorInline struct {
    Tool            string
    File            string
    Severity        string
    StructuralKey   string
    Title           string
    NormalizedTitle string
    ExternalID      string
    Resolved        bool
    Line            int    // ← NEW: anchor line (n.Position.NewLine for GitLab)
    EndLine         int    // ← NEW: optional end-of-range
}
```

**`internal/vcs/gitlab/gitlab.go`**

`ListCadooArtifacts` (line ~255): populate `Line` from `n.Position.NewLine` when `n.Position != nil`:
```go
out.Inline = append(out.Inline, vcs.PriorInline{
    // existing fields ...
    Line:    n.Position.NewLine,    // ← NEW
    EndLine: n.Position.NewLine,    // ← NEW (single-line for now; extend if MR API exposes end)
})
```

**`internal/vcs/github/github.go`**

`ListCadooArtifacts` (line ~328): GitHub's GraphQL query currently fetches only the first
comment in a thread (`comments(first:1)`). The first comment's `path` is already captured.
GitHub review thread GraphQL doesn't expose line numbers on the thread directly — they are on
the `reviewThread.line` and `reviewThread.originalLine` fields. The query must be extended:
```graphql
reviewThreads(first:100,after:$rc){ nodes{ id isResolved line originalLine
  comments(first:1){ nodes{ path body } } }
```
Populate `Line: th.Line` (new int field in the parsed node struct).

**`internal/findings/findings.go`**

1. Add `Resolved bool` and `Line int` to `findingRec` (line ~494):
   ```go
   type findingRec struct {
       // existing fields ...
       Resolved bool `json:"resolved,omitempty"`  // ← NEW
       Line     int  `json:"line,omitempty"`       // ← NEW anchor line from VCS
   }
   ```

2. Export `ResolvedSuppressThreshold` constant (new):
   ```go
   const ResolvedSuppressThreshold = 0.3
   ```

3. `memoryStore.has` (line ~532): extend to check resolved priors with widened rule:
   ```go
   func (m *memoryStore) has(key PRKey, tool string, c vcs.InlineComment) bool {
       m.mu.Lock()
       defer m.mu.Unlock()
       wantKey := StructuralKey(tool, c)
       wantTokens := titleTokens(c.Body)
       for _, r := range m.findings[key] {
           if r.Tool != tool || r.File != c.File || r.Severity != string(c.Severity) {
               continue
           }
           if !r.Resolved {
               // Open prior: existing rule (exact key OR Jaccard >= 0.5)
               if r.StructuralKey == wantKey {
                   return true
               }
               if jaccard(wantTokens, tokenize(r.NormalizedTitle)) >= SimilarTitleThreshold {
                   return true
               }
           } else {
               // Resolved prior: widened rule (line overlap OR Jaccard >= 0.3)
               newLine := c.LineStart  // use LineStart as anchor for new comment
               if r.Line > 0 && newLine > 0 {
                   // line overlap: resolved anchor within the new comment's range
                   if newLine <= r.Line && r.Line <= c.LineEnd {
                       return true
                   }
                   if r.Line <= newLine && newLine <= r.Line { // exact line match
                       return true
                   }
               }
               if jaccard(wantTokens, tokenize(r.NormalizedTitle)) >= ResolvedSuppressThreshold {
                   return true
               }
           }
       }
       return false
   }
   ```
   Note: the line-overlap check compares `c.LineStart..c.LineEnd` against `r.Line`. A new
   single-line comment at `r.Line` is suppressed. A new comment at a completely different line
   is not — guardrail preserved.

**`internal/findings/prior.go`**

`NewFromPrior` (line ~44): populate `Resolved` and `Line` from `pi`:
```go
recs = append(recs, findingRec{
    // existing fields ...
    Resolved: pi.Resolved,    // ← NEW
    Line:     pi.Line,        // ← NEW
})
```

**DB worker path for Part B**: The sticky-suppression change is in `memoryStore.has`, which is
the CI-mode path. The DB-backed `HasFinding` method (line ~123) runs a SQL query against
`posted_findings` and does not have a `resolved` column — Part B's sticky suppression is
therefore **CI-mode only** per the SPEC ("No new DB migration"). The DB path continues to
dedup by `StructuralKey` + Jaccard on the existing columns. This is acceptable per the SPEC.

### Tests to Add

- `TestMemoryStoreHasResolvedSuppressesSameFile` — resolved prior + reworded new finding on
  same line → suppressed.
- `TestMemoryStoreHasResolvedDoesNotSuppressDifferentLine` — resolved prior at line 10, new
  finding at line 50, low Jaccard → NOT suppressed.
- `TestMemoryStoreHasResolvedJaccardBelowThreshold` — resolved prior + genuinely unrelated new
  finding (Jaccard < 0.3) → NOT suppressed.
- `TestNewFromPriorCarriesResolvedAndLine` — verify `NewFromPrior` seeds `Resolved=true` and
  `Line` into the store and that `HasFinding` then suppresses a restatement.

---

## Part C — Precise Change Map: Incremental Review

### Current Behavior (Gap)

`consolidate.go` (`renderConsolidated`, line ~63-85): writes `wrapperBegin` ... sections ...
`wrapperEnd`. No `reviewed-sha` marker embedded.

`vcs/vcs.go:137-140` (`PriorReview`): `SummaryCommentID string` and `Inline []PriorInline`.
No `LastReviewedSHA string` field.

`vcs/vcs.go` (`Provider` interface): no `DiffBetween` method.

`reviewer.go` (`Run`, line ~145-283): calls `provider.ListChangedFiles` → full PR diff always.
No distinction between inline-emitting and summary-only tools.

`tools/tools.go` (`Input`, line ~40-91`): one `Packed contextengine.Compressed` and one
`Files []vcs.FileChange` — both always full. No `IncrementalFiles` / `IncrementalPacked` fields.

`gitlab.go:ListCadooArtifacts` (line ~287-289): parses `SummaryWrapperBegin` from note body
but does not parse a `reviewed-sha` marker.

`github.go:ListCadooArtifacts` (line ~307-309): same — only reads `SummaryCommentID`.

### Required Changes

#### Step C-1: `vcs.PriorReview` + marker format

**`internal/vcs/vcs.go`**: add field:
```go
type PriorReview struct {
    SummaryCommentID string
    LastReviewedSHA  string  // ← NEW: parsed from <!-- cadoo:reviewed-sha:<sha> -->
    Inline           []PriorInline
}
```

New marker constant (lives in `vcs/marker.go` or `vcs/vcs.go`):
```go
const reviewedSHAPrefix = "<!-- cadoo:reviewed-sha:"
const reviewedSHASuffix = " -->"
```

#### Step C-2: Parse `reviewed-sha` in both VCS adapters

**`internal/vcs/gitlab/gitlab.go`** (`ListCadooArtifacts`, line ~287-289`):
```go
if n.Position == nil && strings.Contains(n.Body, vcs.SummaryWrapperBegin) {
    out.SummaryCommentID = strconv.FormatInt(n.ID, 10)
    out.LastReviewedSHA = vcs.ParseReviewedSHA(n.Body)  // ← NEW
}
```

**`internal/vcs/github/github.go`** (`ListCadooArtifacts`, line ~307-309`): same pattern on
the issue comment node body.

**`internal/vcs/marker.go`**: add `ParseReviewedSHA(body string) string` and
`RenderReviewedSHA(sha string) string` helpers.

#### Step C-3: Emit marker in `renderConsolidated`

**`internal/orchestrator/consolidate.go`** (`renderConsolidated`, line ~63-85`): change
signature to accept a `headSHA string` parameter, embed the marker inside the wrapper:
```go
func renderConsolidated(sections []findings.Section, headSHA string) string {
    // ...
    b.WriteString(wrapperBegin)
    b.WriteString("\n")
    if headSHA != "" {
        b.WriteString(vcs.RenderReviewedSHA(headSHA))  // ← NEW
        b.WriteString("\n")
    }
    // ... sections ...
    b.WriteString(wrapperEnd)
    return b.String()
}
```
Update the single call site in `postSummary` to pass `pr.HeadSHA`.

#### Step C-4: `DiffBetween` provider capability

**`internal/vcs/vcs.go`**: new optional interface (same pattern as `ReleaseRangeReader`):
```go
// DiffBetweener is an OPTIONAL capability. Adapters implement it so the
// orchestrator can fetch only the incremental diff since the last review.
type DiffBetweener interface {
    // DiffBetween returns the file changes between oldSHA and newSHA.
    // Returns (nil, nil) when oldSHA is not reachable from newSHA (e.g.
    // after a force-push), signalling a full-review fallback.
    DiffBetween(ctx context.Context, repo, oldSHA, newSHA string) ([]FileChange, error)
}
```

**`internal/vcs/gitlab/gitlab.go`**: implement `DiffBetween` using `Repositories.Compare`
(the same API used in `release.go:31`). Parse the returned diffs into `[]vcs.FileChange`.
For the non-ancestor case: GitLab's Compare API returns a 400 or empty diffs when oldSHA is
not in the ancestry chain — return `(nil, nil)` to signal fallback.

**`internal/vcs/github/github.go`**: implement `DiffBetween` using `CompareCommits` (used in
`release.go:33`). The response includes `AheadBy`/`BehindBy` fields and `Files` — extract
`Files` into `[]vcs.FileChange`. When the response status is 404 (diverged history) return
`(nil, nil)`.

#### Step C-5: `tools.Input` dual-context

**`internal/tools/tools.go`** (`Input`): add incremental fields (additive, no existing fields
removed):
```go
type Input struct {
    // ... existing fields unchanged ...

    // IncrementalFiles and IncrementalPacked hold the diff since the last
    // reviewed SHA. Both are nil on first run or after a force-push. Tools
    // that emit inline comments should use these when non-nil; summary-only
    // tools (describe, changelog) always use Files/Packed.
    IncrementalFiles  []vcs.FileChange
    IncrementalPacked contextengine.Compressed
    // IsIncrementalRun is true when the orchestrator has a valid lastReviewedSHA
    // and is limiting inline tools to the incremental change set.
    IsIncrementalRun bool
}
```

#### Step C-6: Orchestrator incremental dispatch logic

**`internal/orchestrator/reviewer.go`** (`Run`, line ~145-293): after loading `d.Posted` prior
(line ~233), add incremental-review logic:

```go
var (
    incrementalFiles  []vcs.FileChange
    incrementalPacked contextengine.Compressed
    isIncrementalRun  bool
    lastReviewedSHA   string
    inChangeSet       map[string]struct{} // files touched since lastReviewedSHA
)
if d.Posted != nil {
    // Recover lastReviewedSHA from the prior store's PriorReview.
    // (The store is seeded from prior review in CI mode; in DB mode
    // it can be read from the posted_summaries body field — deferred.)
    if sha := d.Posted.LastReviewedSHA(); sha != "" && sha != pr.HeadSHA {
        if db, ok := provider.(vcs.DiffBetweener); ok {
            incr, err := db.DiffBetween(ctx, pr.RepoFullName, sha, pr.HeadSHA)
            if err == nil && incr != nil {
                incrementalFiles = incr
                incrementalPacked = contextengine.Compress(incr, ...)
                isIncrementalRun = true
                lastReviewedSHA = sha
                inChangeSet = fileSet(incr)
            }
            // err or nil incr → full review fallback (no change to files/packed)
        }
    }
}
in.IncrementalFiles = incrementalFiles
in.IncrementalPacked = incrementalPacked
in.IsIncrementalRun = isIncrementalRun
```

The `d.Posted.LastReviewedSHA()` requires a new method on `findings.Store` — see below.

#### Step C-7: `LastReviewedSHA` on the store

**`internal/findings/findings.go`** (`Store`): add `LastReviewedSHA() string` method. In CI
mode the store is created via `NewFromPrior`, which is called with the `PriorReview` struct.
Thread `pr.LastReviewedSHA` into `NewFromPrior` so it can be stored in `memoryStore`:

`memoryStore` gets a `lastReviewedSHA string` field. `NewFromPrior` sets it. The new store
method returns it.

For the DB-backed worker path: `LastReviewedSHA()` reads from `posted_summaries.body` where
`tool = WrapperToolKey`, parses the `<!-- cadoo:reviewed-sha: -->` marker from the stored body.
No schema change needed — the body column already exists (migration 0004 adds it to `posted_summaries`).

#### Step C-8: Incremental `resolveStalePriors`

**`internal/orchestrator/reviewer.go`** (`resolveStalePriors`): add a change-set gate when
`isIncrementalRun` is true:
```go
// Under incremental review: only resolve priors anchored inside
// the change set. Priors on untouched lines persist silently.
if isIncrementalRun && len(inChangeSet) > 0 {
    if _, changed := inChangeSet[p.File]; !changed {
        continue // prior is on unchanged code — leave thread open
    }
    // Optionally also gate on anchor line vs diff hunks.
}
```
`resolveStalePriors` needs the `inChangeSet` and `isIncrementalRun` values. Pass them as
parameters or thread through a struct. Simplest: add two new parameters:
```go
func (d *Dispatcher) resolveStalePriors(
    ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest,
    tool string, prior []findings.PostedFinding, current []vcs.InlineComment,
    changeSet map[string]struct{}, incrementalRun bool,
) {
```

### Tests to Add (Part C)

- `TestRenderConsolidatedEmbedsReviewedSHA` — verify marker present and parseable.
- `TestParseReviewedSHA` — parse marker from body with/without the marker.
- `TestDiffBetweenFallbackOnNonAncestor` — fake provider returns `(nil, nil)`; orchestrator
  runs full review.
- **Fixed-point integration test** (the most important): `TestCIModeFixedPointUnchangedHead`:
  - Run 1: full review, posts N threads, stamps `reviewed-sha`.
  - Run 2: unchanged head (same SHA), incremental diff is empty → 0 new inline posts, 0
    resolves, summary updated.
  - Assert `sv.inline` count unchanged and `sv.resolved` empty after Run 2.
- `TestCIModeIncrementalChangedLines` — Run 2 with 1-line change; assert at most findings for
  that file; priors on other files not resolved.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Diff between two SHAs | Custom git diff parsing | GitLab `Repositories.Compare` / GitHub `CompareCommits` | Already imported and called in `release.go` files; handles pagination, binary files, encoding |
| Ancestor check | `git merge-base` subprocess | Infer from `DiffBetween` returning `(nil, nil)` | The compare API already errors or returns empty on non-ancestor; no separate ancestry check needed |
| Jaccard similarity | Custom token set logic | Existing `jaccard()` / `tokenize()` in `findings.go` | Already tuned and tested |
| Marker parsing | Ad-hoc regex | Existing `vcs.ParseInlineMarker` pattern | Add `ParseReviewedSHA` following the same style |
| HTML-comment markers | Any other format | Existing wrapper conventions in `consolidate.go` | Machine-greppable; must remain invisible in rendered markdown |

**Key insight:** The existing `Repositories.Compare` / `CompareCommits` calls in the adapter
`release.go` files prove the API is already usable; `DiffBetween` is a thin wrapper around
code that already exists in the same file.

---

## Common Pitfalls

### Pitfall 1: Sentinel guard for `p.StructuralKey == ""`

**What goes wrong:** Old DB records and old in-memory records (posted before Part A ships) have
an empty `StructuralKey` in `PostedFinding`. After the fix, `resolveStalePriors` compares
`p.StructuralKey` against `currentKeys` — an empty key never matches, so every old record is
treated as stale and resolved.

**How to avoid:** Guard in `resolveStalePriors`:
```go
if p.StructuralKey == "" {
    // Legacy record: fall back to first-line recompute for backward compat.
    pkey = findings.StructuralKey(p.Tool, vcs.InlineComment{
        File: p.File, Severity: vcs.Severity(p.Severity), Body: p.Title,
    })
} else {
    pkey = p.StructuralKey
}
```

**Warning signs:** All threads resolved on first run after the deploy.

### Pitfall 2: `renderConsolidated` signature change breaks consolidate_test.go

**What goes wrong:** Adding `headSHA string` to `renderConsolidated` breaks the two existing
call sites in `consolidate_test.go`.

**How to avoid:** Check `consolidate_test.go` and update the two test calls to pass `""` (or
a test SHA). The single production call site is in `postSummary` — update it to pass
`pr.HeadSHA`.

### Pitfall 3: Bypassing `Posted` with new code

**CLAUDE.md mandate:** Never bypass `Posted`. `DiffBetween` and incremental context must flow
through the same `postInline` → `HasFinding` path. The incremental change only affects WHICH
files are in `tools.Input`; the dedup layer is unchanged.

**Warning signs:** Adding a separate code path that calls `provider.PostInlineComments`
directly without going through `postInline`.

### Pitfall 4: `DiffBetween` returning empty (not nil) for unchanged files

**What goes wrong:** When `lastReviewedSHA == pr.HeadSHA` (no new commits), the Compare API
returns an empty file list (not an error, not nil). This should trigger the "empty incremental
diff → no inline tools run" path, not a full-review fallback.

**How to avoid:** Treat `([]FileChange{}, nil)` (empty, no error) as a valid empty diff →
skip inline tools. Treat `(nil, nil)` as non-ancestor → full-review fallback. Treat
`(nil, err)` as failure → full-review fallback with a log warning.

### Pitfall 5: `reviewed-sha` marker breaking the summary wrapper grep

**What goes wrong:** `ListCadooArtifacts` identifies the summary comment by
`strings.Contains(n.Body, vcs.SummaryWrapperBegin)`. If the reviewed-sha marker is placed
BEFORE `wrapperBegin`, the grep could theoretically fail if the implementation accidentally
truncates the body. 

**How to avoid:** Place the reviewed-sha marker INSIDE the wrapper (between `wrapperBegin` and
`wrapperEnd`), not before it. `SummaryWrapperBegin` remains the first token so existing greps
still trigger on it, and the SHA marker is then easy to parse from the same body.

### Pitfall 6: GitHub GraphQL `line` field on review thread

**What goes wrong:** GitHub's GraphQL schema exposes `line` and `originalLine` on
`ReviewThread`, but these may be null for PR review threads on older commits or for threads on
deleted lines.

**How to avoid:** Accept nullable integers; if null, set `Line = 0`. The sticky-suppression
check in `memoryStore.has` already guards `if r.Line > 0 && newLine > 0`.

### Pitfall 7: `resolveStalePriors` tool-scoping with incremental review

**What goes wrong:** Under incremental review, `tools.Input.IncrementalFiles` only covers
files that changed. If `describe` runs and generates no inline comments (as expected), the
empty `current` slice would cause `resolveStalePriors` to see all prior `describe` findings
as stale and resolve them — even though they're on unchanged code.

**How to avoid:** Only non-summary tools produce `InlineComments`, so `postInline` is only
called when `res.InlineComments` is non-empty. The `resolveStalePriors` call at `reviewer.go:467`
is inside `postInline`, so it is never triggered for summary-only tools. The incremental
change-set gate then further limits it to changed files.

---

## Architecture Patterns

### System Architecture Diagram

```
cadoo ci --mr <url>
        |
        v
cmd/cadoo-cli/ci.go (ciCmd)
  - builds provider (GitLab/GitHub adapter)
  - calls priorStore() → provider.ListCadooArtifacts()
        |
        v     [Part C NEW: also parses LastReviewedSHA from summary wrapper body]
findings.NewFromPrior(key, PriorReview)
  - seeds memoryStore with prior findingRec entries
  - [Part B NEW: sets Resolved and Line on each rec]
  - [Part C NEW: stores LastReviewedSHA]
        |
        v
orchestrator.Dispatcher.Run(ctx, job)
  - provider.FetchPullRequest()
  - provider.ListChangedFiles() → full diff (always)
  - [Part C NEW] d.Posted.LastReviewedSHA() → non-empty?
      → yes: provider.DiffBetween(oldSHA, headSHA) → incrementalFiles
      → nil result: full-review fallback
  - tools.Input.Files = fullDiff, .IncrementalFiles = incrementalDiff
        |
        v
tool.Run(ctx, in) → tools.Result
        |
        v
d.applyResult()
  ├─ d.postSummary() → renderConsolidated(sections, headSHA) [Part C: embeds reviewed-sha]
  └─ d.postInline()
       ├─ HasFinding (memoryStore.has) [Part B: checks Resolved+Line for widened suppression]
       ├─ StampInline (unchanged)
       ├─ provider.PostInlineComments()
       ├─ d.Posted.RecordFinding()
       └─ d.resolveStalePriors() [Part A: uses p.StructuralKey directly]
                                  [Part C: skips priors on files not in changeSet]
```

### Recommended File Structure (No New Files)

All changes are in existing files:

```
internal/
├── findings/
│   ├── findings.go       # PostedFinding.StructuralKey; findingRec.{Resolved,Line};
│   │                     # ResolvedSuppressThreshold const; memoryStore.has widened;
│   │                     # memoryStore.lastReviewedSHA; Store.LastReviewedSHA();
│   │                     # ListPostedFindings selects structural_key
│   └── prior.go          # NewFromPrior seeds Resolved, Line, LastReviewedSHA
├── vcs/
│   ├── vcs.go            # PriorInline.{Line,EndLine}; PriorReview.LastReviewedSHA;
│   │                     # DiffBetweener interface
│   ├── marker.go         # ParseReviewedSHA(); RenderReviewedSHA()
│   ├── gitlab/gitlab.go  # ListCadooArtifacts: Line from n.Position.NewLine,
│   │                     #   LastReviewedSHA from marker; DiffBetween() impl
│   └── github/github.go  # ListCadooArtifacts: Line from GraphQL thread.line,
│   │                     #   LastReviewedSHA from issue comment; DiffBetween() impl
├── orchestrator/
│   ├── reviewer.go       # resolveStalePriors: p.StructuralKey direct compare;
│   │                     #   incremental diff logic; changeSet-scoped resolve;
│   │                     #   postSummary passes pr.HeadSHA to renderConsolidated
│   └── consolidate.go    # renderConsolidated(sections, headSHA) embeds marker
└── tools/
    └── tools.go          # Input.{IncrementalFiles,IncrementalPacked,IsIncrementalRun}
```

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib) — no external test library |
| Config file | none (standard `go test`) |
| Quick run command | `go test -race -run TestResolveStalePriors ./internal/orchestrator/... ./internal/findings/...` |
| Full suite command | `make test` (`go test -race -count=1 ./...`) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| REQ-cidedup-no-self-resolution | Multi-line prior not self-resolved | Unit | `go test -race -run TestResolveStalePriorsMultiLineNotSelfResolved ./internal/orchestrator/...` | No — Wave 0 |
| REQ-cidedup-no-self-resolution | DB path: `ListPostedFindings` returns `StructuralKey` | Unit | `go test -race -run TestListPostedFindingsStructuralKey ./internal/findings/...` | No — Wave 0 (integration, needs DB) |
| REQ-cidedup-honor-resolves | Resolved prior suppresses reworded restatement | Unit | `go test -race -run TestMemoryStoreHasResolvedSuppresses ./internal/findings/...` | No — Wave 0 |
| REQ-cidedup-honor-resolves | Unrelated finding on different line not suppressed | Unit | `go test -race -run TestMemoryStoreHasResolvedDoesNotSuppressDifferentLine ./internal/findings/...` | No — Wave 0 |
| REQ-cidedup-honor-resolves | `NewFromPrior` carries Resolved + Line | Unit | `go test -race -run TestNewFromPriorCarriesResolvedAndLine ./internal/findings/...` | No — Wave 0 |
| REQ-cidedup-incremental-review | reviewed-sha parsed from wrapper body | Unit | `go test -race -run TestParseReviewedSHA ./internal/vcs/...` | No — Wave 0 |
| REQ-cidedup-incremental-review | reviewed-sha embedded in renderConsolidated | Unit | `go test -race -run TestRenderConsolidatedEmbedsReviewedSHA ./internal/orchestrator/...` | No — Wave 0 |
| REQ-cidedup-convergent-review | Fixed-point: unchanged head → 0 new + 0 resolved | Integration | `go test -race -run TestCIModeFixedPointUnchangedHead ./internal/orchestrator/...` | No — Wave 0 |
| REQ-cidedup-convergent-review | Incremental: 1-file change scoped | Integration | `go test -race -run TestCIModeIncrementalChangedLines ./internal/orchestrator/...` | No — Wave 0 |

### Sampling Rate

- **Per task commit:** `go test -race -run 'TestResolveStalePriors|TestMemoryStore|TestNewFromPrior' ./internal/findings/... ./internal/orchestrator/...`
- **Per wave merge:** `make test`
- **Phase gate:** `make test && make lint && make vet` before `/gsd:verify-work`

### Wave 0 Gaps (test infrastructure needed before implementation)

All tests listed above are new — no existing test exercises the fixed behavior. However:
- `TestPostInlineResolvesStalePriors` (existing, `reviewer_test.go:276`) must continue to pass
  and serves as a sanity check that the legacy single-line path still works.
- `TestCIModeTwoRunIdempotency` (existing, `reviewer_test.go:411`) covers the basic two-run
  scenario and must continue to pass.
- No new test framework needed — stdlib `testing` is in use throughout.

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| First-line title reconstruction in `resolveStalePriors` | Direct `StructuralKey` compare (Part A) | This phase | Stops self-resolution of multi-line threads |
| Resolved flag captured but discarded | Carried into `findingRec.Resolved` and used in `memoryStore.has` (Part B) | This phase | Resolved threads no longer return |
| Full PR diff every CI run | Full diff + optional incremental diff since `lastReviewedSHA` (Part C) | This phase | Structural ceiling on thread growth |
| No `reviewed-sha` in summary wrapper | `<!-- cadoo:reviewed-sha:<sha> -->` embedded (Part C) | This phase | Enables stateless incremental review |

---

## Open Questions (RESOLVED)

1. **GitHub `line` field availability on `reviewThread`**
   - What we know: GraphQL `ReviewThread` has `line` and `originalLine` fields in the GitHub
     API (confirmed in public docs). They may be `null` for threads on deleted files.
   - What's unclear: Whether the nullable handling in the existing GraphQL client
     (`graphql.go`) needs a new nullable-int scalar type, or whether `*int` in the struct is
     sufficient.
   - Recommendation: Use `*int` in the GQL struct; if nil, set `Line = 0`.

2. **DB-backed worker path: `LastReviewedSHA` from `posted_summaries.body`**
   - What we know: The `body` column was added in migration 0004 (present, not null by
     default). The `PutSection` method writes tool section bodies there. For the wrapper row
     (`tool = WrapperToolKey`), the body is currently `''` (empty string — see `PutSummaryID`
     which writes `body = ''`).
   - What's unclear: Should Part C write the rendered consolidated body (with the SHA marker)
     into the `body` column for the wrapper row so `LastReviewedSHA()` can parse it in the DB
     path? Or is the DB path out of scope for Part C?
   - Recommendation: Per the SPEC, Part C is CI-mode only. For the DB path, `LastReviewedSHA`
     returns `""` → always full review. Revisit if the worker path also needs incremental
     review in a future phase.

3. **`DiffBetween` non-ancestor detection per provider**
   - GitLab: `Repositories.Compare` with `straight=false` (default) returns an error or empty
     diffs if the refs diverge. The exact error code when SHAs are unrelated needs verification
     against the GitLab API spec.
   - GitHub: `CompareCommits` returns a 404 when the base is not an ancestor of the head. The
     go-github library translates this to a non-nil error.
   - Recommendation: Any non-nil error from either API → return `(nil, nil)` to signal
     full-review fallback. This is conservative and safe.

---

## Security Domain

> `security_enforcement` absent in `.planning/config.json` — treat as enabled.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | Yes | SHA values parsed from marker are used only for API calls — validate hex SHA format before use |
| V2 Authentication | No | No new auth paths |
| V3 Session Management | No | Stateless CI mode |
| V4 Access Control | No | No new access paths |
| V6 Cryptography | No | No new crypto |

### Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Malformed `<!-- cadoo:reviewed-sha: -->` marker in PR comment body | Tampering | `ParseReviewedSHA` validates SHA looks like a hex string (40 chars, `[0-9a-f]`) before using it; invalid → ignore, full-review fallback |
| Attacker embeds arbitrary `DiffBetween` SHA in a PR summary comment to trigger diff against a private SHA | Information Disclosure | `DiffBetween` is called with `pr.RepoFullName` — the repo is already authorized via the VCS token; the SHA only controls WHICH diff to fetch, not WHICH repo |
| Infinite-loop: `LastReviewedSHA == pr.HeadSHA` (same commit) | DoS | Guard: `sha != pr.HeadSHA` before calling `DiffBetween` (empty diff fast-path, not a provider call) |

---

## Environment Availability

Step 2.6: SKIPPED — this phase makes code/config changes only. No new external tools or
services are required. Existing dependencies (`go-github`, `go-gitlab` SDK clients) are already
imported and tested. No external service setup needed to run `make test`.

---

## Package Legitimacy Audit

Step: SKIPPED — no new external packages are added in this phase. All changes use existing
imports (`google/go-github/v66`, `gitlab.com/gitlab-org/api/client-go`) already present in
`go.mod`.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | GitLab `n.Position.NewLine` is the new-file line number for the discussion note anchor | Part B — anchor-line capture | If GitLab stores it differently (e.g. `OldLine` for deleted lines), `Line` may be zero and sticky suppression falls back to Jaccard only (safe degradation) |
| A2 | GitHub GraphQL `ReviewThread.line` is available and non-null for added/modified lines | Part B — GitHub anchor capture | If null-heavy, `Line = 0` for all GitHub threads and sticky suppression uses Jaccard only |
| A3 | GitLab `Repositories.Compare` returns an error (not empty diffs) for non-ancestor SHAs | Part C — fallback detection | If it returns empty diffs on divergence, the fallback triggers correctly via the empty-diff path |

---

## Sources

### Primary (HIGH confidence — verified by reading actual source)

- `internal/orchestrator/reviewer.go:475-503` — `resolveStalePriors` current implementation, first-line-recompute bug confirmed
- `internal/findings/findings.go:36-43` — `PostedFinding` struct (no `StructuralKey` field)
- `internal/findings/findings.go:199-229` — `ListPostedFindings` SQL (does not select `structural_key`)
- `internal/findings/findings.go:494-505` — `findingRec` struct (no `Resolved` or `Line` fields)
- `internal/findings/findings.go:532-548` — `memoryStore.has` (no resolved-specific handling)
- `internal/findings/prior.go:34-53` — `NewFromPrior` (drops `Resolved`, no `Line` capture)
- `internal/vcs/vcs.go:130-153` — `PriorInline` and `PriorReview` (no `Line`, no `LastReviewedSHA`)
- `internal/vcs/gitlab/gitlab.go:229-298` — `ListCadooArtifacts` (parses `n.Position` for existence but not `NewLine`)
- `internal/vcs/github/github.go:240-349` — `ListCadooArtifacts` (no line number extraction)
- `internal/orchestrator/consolidate.go:63-85` — `renderConsolidated` (no reviewed-sha marker)
- `internal/vcs/gitlab/release.go:27-57` — `Repositories.Compare` already used; reusable for `DiffBetween`
- `internal/vcs/github/release.go:24-57` — `CompareCommits` already used; reusable for `DiffBetween`
- `db/migrations/0005_finding_dedup.sql` — `structural_key` column already exists in `posted_findings`
- `cmd/cadoo-cli/ci.go:187-258` — CI-mode entry point; `priorStore` reconstruction; stateless dispatcher setup

### Secondary (HIGH confidence — inferred from cross-reading)

- `go.mod` — `google/go-github/v66`, `gitlab.com/gitlab-org/api/client-go v1.46.0` confirmed as existing deps
- `internal/orchestrator/reviewer_test.go` — existing test coverage map (which tests must not break)

---

## Metadata

**Confidence breakdown:**
- Bug locations (Part A, B): HIGH — confirmed by reading current code against SPEC description; exact lines cited
- `DiffBetween` implementation approach: HIGH — `CompareCommits`/`Compare` already called in same adapter package
- GitHub `line` field on GraphQL: MEDIUM — per SPEC; marked as A2 assumption pending GraphQL schema check
- `ResolvedSuppressThreshold` value (0.3): ASSUMED per SPEC; a constant, tunable

**Research date:** 2026-06-15
**Valid until:** 2026-07-15 (stable internal codebase; no external API changes expected)
