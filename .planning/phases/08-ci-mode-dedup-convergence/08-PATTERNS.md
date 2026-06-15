# Phase 8: CI-Mode Dedup Convergence - Pattern Map

**Mapped:** 2026-06-15
**Files analyzed:** 8 modified files (no new files)
**Analogs found:** 8 / 8 (all modifications; each file is its own analog)

---

## File Classification

| Modified File | Role | Data Flow | Closest Analog (within same file) | Match Quality |
|---|---|---|---|---|
| `internal/findings/findings.go` | model + store | CRUD | `memoryStore.has` / `findingRec` struct / `ListPostedFindings` | self — surgical additions |
| `internal/findings/prior.go` | transform | CRUD | `NewFromPrior` loop (lines 34-53) | self — populate 2 new fields |
| `internal/vcs/vcs.go` | interface + model | request-response | `ReleaseRangeReader` optional interface (lines 215-226) + `PriorInline` struct (lines 144-153) | exact — same optional-interface pattern |
| `internal/vcs/marker.go` | utility | transform | `InlineMarker` / `ParseInlineMarker` (lines 32-64) | exact — add sibling render/parse pair |
| `internal/vcs/gitlab/gitlab.go` | adapter | request-response | `ListCadooArtifacts` (lines 229-298) + `ListCommits` via `Repositories.Compare` in `release.go` (lines 31-57) | exact — extend existing method + implement DiffBetween using Compare |
| `internal/vcs/github/github.go` | adapter | request-response | `ListCadooArtifacts` (lines 240-350) + `ListCommits` via `CompareCommits` in `release.go` (lines 27-57) | exact — extend existing method + implement DiffBetween using CompareCommits |
| `internal/orchestrator/consolidate.go` | utility | transform | `renderConsolidated` (lines 63-85) | self — add `headSHA` parameter + marker embed |
| `internal/orchestrator/reviewer.go` | orchestrator | request-response | `resolveStalePriors` (lines 475-503) + optional-capability probe via `loadCfg` FileFetcher type-assert (lines 507-524) | self — fix bug + add incremental dispatch block |
| `internal/tools/tools.go` | model | request-response | `Input` struct (lines 40-91) — additive fields follow existing `KBHits`, `Learnings`, `Issues` pattern | self — additive |

---

## Pattern Assignments

### `internal/findings/findings.go` — Part A, B, C changes

**Part A: Add `StructuralKey` to `PostedFinding`**

Analog pattern — existing `PostedFinding` struct (lines 35-43):
```go
type PostedFinding struct {
    Tool              string
    File              string
    LineStart         int
    LineEnd           int
    Severity          string
    Title             string
    ExternalCommentID string
    // ADD: StructuralKey string
}
```
Copy the field declaration style: plain `string`, no json tags (struct is never JSON-encoded — only `findingRec` is persisted).

**Part A: `ListPostedFindings` — add `structural_key` to SELECT and scan**

Analog pattern — existing column scan (lines 209-229):
```go
const q = `SELECT tool, coalesce(file, ''), coalesce(line_start, 0),
                  coalesce(line_end, 0), coalesce(severity, ''),
                  coalesce(title, ''), coalesce(external_comment_id, '')
    FROM posted_findings
    WHERE provider = $1 AND repo_full_name = $2 AND pr_number = $3
    ORDER BY file, line_start, line_end`
// ...
if err := rows.Scan(&f.Tool, &f.File, &f.LineStart, &f.LineEnd,
    &f.Severity, &f.Title, &f.ExternalCommentID); err != nil {
```
New version: append `coalesce(structural_key, '')` to the SELECT list and `&f.StructuralKey` to the `rows.Scan` call. Follow the `coalesce(..., '')` null-safety pattern already used for every other column.

**Part A: `memoryStore.list` — populate `StructuralKey` from `r.StructuralKey`**

Analog pattern — existing conversion loop (lines 582-601):
```go
for _, r := range recs {
    out = append(out, PostedFinding{
        Tool:              r.Tool,
        File:              r.File,
        LineStart:         r.LineStart,
        LineEnd:           r.LineEnd,
        Severity:          r.Severity,
        Title:             r.Title,
        ExternalCommentID: r.ExternalID,
        // ADD: StructuralKey: r.StructuralKey,
    })
}
```

**Part B: Add `Resolved bool` and `Line int` to `findingRec`**

Analog pattern — existing `findingRec` struct (lines 494-505):
```go
type findingRec struct {
    Tool            string `json:"tool"`
    File            string `json:"file"`
    Severity        string `json:"severity"`
    StructuralKey   string `json:"sk"`
    Fingerprint     string `json:"fp"`
    NormalizedTitle string `json:"nt"`
    Title           string `json:"title"`
    LineStart       int    `json:"ls"`
    LineEnd         int    `json:"le"`
    ExternalID      string `json:"eid,omitempty"`
    // ADD: Resolved bool `json:"resolved,omitempty"`
    // ADD: Line     int  `json:"line,omitempty"`
}
```
Follow the `omitempty` convention used for `ExternalID` — zero-value fields do not bloat the persisted JSON.

**Part B: New `ResolvedSuppressThreshold` constant**

Analog pattern — existing `SimilarTitleThreshold` constant (line 48):
```go
const SimilarTitleThreshold = 0.5
// ADD (same style, adjacent):
const ResolvedSuppressThreshold = 0.3
```

**Part B: Extend `memoryStore.has` for resolved prior handling**

Analog pattern — existing `memoryStore.has` (lines 532-548):
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
        if r.StructuralKey == wantKey {
            return true
        }
        if jaccard(wantTokens, tokenize(r.NormalizedTitle)) >= SimilarTitleThreshold {
            return true
        }
    }
    return false
}
```
The new version splits the inner block on `r.Resolved`. The lock acquisition pattern, early-continue guard, and `StructuralKey`/`jaccard` calls are unchanged. Only the inner match logic gains an `else` branch for resolved priors.

**Part C: Add `lastReviewedSHA string` field to `memoryStore` + `LastReviewedSHA()` method on `Store`**

Analog pattern — `summaryID` method on `memoryStore` (lines 604-608) and dispatch on `pool`/`mem` in `Store.SummaryID` (lines 234-253):
```go
// memoryStore method pattern:
func (m *memoryStore) summaryID(key PRKey, tool string) string {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.summaries[summaryRefKey{PR: key, Tool: tool}]
}

// Store dispatch pattern:
func (s *Store) SummaryID(ctx context.Context, key PRKey, tool string) (string, error) {
    if s == nil { return "", nil }
    if s.mem != nil { return s.mem.summaryID(key, tool), nil }
    if s.pool == nil { return "", nil }
    // ... SQL ...
}
```
`LastReviewedSHA()` follows the same nil-safe dispatch shape. For the `mem` path it reads `m.lastReviewedSHA`. For the `pool` path it parses the marker from `posted_summaries.body` where `tool = WrapperToolKey`. For the `nil` / unconfigured path it returns `""`.

---

### `internal/findings/prior.go` — Part B, C changes

**Analog:** existing `NewFromPrior` loop body (lines 34-53):
```go
recs = append(recs, findingRec{
    Tool:            pi.Tool,
    File:            pi.File,
    Severity:        pi.Severity,
    StructuralKey:   pi.StructuralKey,
    NormalizedTitle: nt,
    Title:           pi.Title,
    ExternalID:      pi.ExternalID,
    // ADD Part B:
    // Resolved: pi.Resolved,
    // Line:     pi.Line,
})
```
The `pi` variable is `vcs.PriorInline`; all new field assignments follow the exact same `pi.FieldName` pattern already used for all existing fields.

**Part C: thread `LastReviewedSHA` into `NewFromPrior`**

After the `recs` loop, the function currently does:
```go
if pr.SummaryCommentID != "" {
    m.summaries[summaryRefKey{PR: key, Tool: WrapperToolKey}] = pr.SummaryCommentID
}
return &Store{mem: m}
```
Add, in the same style:
```go
if pr.LastReviewedSHA != "" {
    m.lastReviewedSHA = pr.LastReviewedSHA
}
```
Place it adjacent to the `SummaryCommentID` guard; same nil-check idiom.

---

### `internal/vcs/vcs.go` — Part B (PriorInline) + Part C (PriorReview, DiffBetweener)

**Part B: Add `Line int` and `EndLine int` to `PriorInline`**

Analog — existing `PriorInline` struct (lines 144-153):
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
    // ADD:
    // Line    int  // anchor line (n.Position.NewLine for GitLab, thread.line for GitHub)
    // EndLine int  // optional end-of-range; currently same as Line
}
```

**Part C: Add `LastReviewedSHA string` to `PriorReview`**

Analog — existing `PriorReview` struct (lines 138-141):
```go
type PriorReview struct {
    SummaryCommentID string
    // ADD: LastReviewedSHA string  // parsed from <!-- cadoo:reviewed-sha:<sha> -->
    Inline           []PriorInline
}
```

**Part C: `DiffBetweener` optional interface**

Exact analog — `ReleaseRangeReader` (lines 215-226), `PriorReviewReader` (lines 133-135), `BranchCommitter` (lines 260-269). Copy the doc-comment style with capability probe instructions and the `(nil, nil)` fallback contract:
```go
// ReleaseRangeReader is an OPTIONAL capability. Adapters that can enumerate
// commits and merged pull-requests between two refs implement this interface...
// The orchestrator type-asserts for it; providers that don't implement it
// degrade gracefully with a logged reason (D-15).
type ReleaseRangeReader interface {
    ListCommits(ctx context.Context, repo, fromRef, toRef string) ([]Commit, error)
    ListMergedPRs(ctx context.Context, repo, fromRef, toRef string) ([]MergedPR, error)
    LatestTagBefore(ctx context.Context, repo, toRef, tagPattern string) (string, error)
}
```
New `DiffBetweener` follows the same pattern:
```go
// DiffBetweener is an OPTIONAL capability. Adapters implement it so the
// orchestrator can fetch only the incremental diff since the last review.
// The orchestrator type-asserts for it; providers that don't implement it
// fall back to a full review.
type DiffBetweener interface {
    // DiffBetween returns the file changes between oldSHA and newSHA.
    // Returns (nil, nil) when oldSHA is not reachable from newSHA (e.g.
    // after a force-push), signalling a full-review fallback.
    DiffBetween(ctx context.Context, repo, oldSHA, newSHA string) ([]FileChange, error)
}
```

---

### `internal/vcs/marker.go` — Part C (reviewed-sha helpers)

**Exact analog:** `InlineMarker` / `ParseInlineMarker` pair (lines 32-64).

Existing render pattern:
```go
func InlineMarker(d MarkerData) string {
    if d.NT == "" {
        return fmt.Sprintf("<!-- cadoo:fp v=1 tool=%s sk=%s sev=%s -->", d.Tool, d.SK, d.Sev)
    }
    encoded := base64.RawURLEncoding.EncodeToString([]byte(d.NT))
    return fmt.Sprintf("<!-- cadoo:fp v=1 tool=%s sk=%s sev=%s nt=%s -->", d.Tool, d.SK, d.Sev, encoded)
}
```

Existing parse pattern (regex + submatch extraction, lines 25-64):
```go
var inlineMarkerRe = regexp.MustCompile(
    `\n*<!-- cadoo:fp v=1 tool=(\S+) sk=(\S+) sev=(\S*)(?:\s+nt=(\S+))? -->\s*$`)

func ParseInlineMarker(body string) (MarkerData, string, bool) {
    loc := inlineMarkerRe.FindStringSubmatchIndex(body)
    if loc == nil {
        return MarkerData{}, body, false
    }
    // ... extract groups ...
}
```

New `reviewed-sha` helpers follow the simpler format (SHA is a flat string, no encoding needed):
```go
// Render:
func RenderReviewedSHA(sha string) string {
    return "<!-- cadoo:reviewed-sha:" + sha + " -->"
}
// Parse: strings.Contains + strings.TrimPrefix/TrimSuffix — no regex needed
// because the marker appears anywhere in the body (not at a specific position).
// Validate: 40 hex chars only (ASVS V5 — see RESEARCH.md security section).
func ParseReviewedSHA(body string) string {
    // extract SHA between prefix and suffix, validate [0-9a-f]{40}
}
```
Constants for prefix/suffix live adjacent to `SummaryWrapperBegin` in `marker.go`.

---

### `internal/vcs/gitlab/gitlab.go` — Part B (anchor line) + Part C (LastReviewedSHA + DiffBetween)

**Part B: populate `Line` from `n.Position.NewLine` in `ListCadooArtifacts`**

Analog — existing anchored-note branch (lines 249-265):
```go
if ok && n.Position != nil {
    file := n.Position.NewPath
    if file == "" {
        file = n.Position.OldPath
    }
    orig := strings.TrimPrefix(stripped, formatSeverity(vcs.Severity(md.Sev)))
    out.Inline = append(out.Inline, vcs.PriorInline{
        Tool:            md.Tool,
        File:            file,
        Severity:        md.Sev,
        StructuralKey:   md.SK,
        Title:           vcs.FirstLine(strings.TrimSpace(orig)),
        NormalizedTitle: md.NT,
        ExternalID:      d.ID,
        Resolved:        n.Resolved,
        // ADD: Line:    n.Position.NewLine,
        // ADD: EndLine: n.Position.NewLine,
    })
```
`n.Position.NewLine` is already checked for nil (`n.Position != nil` guard). `n.Position.NewLine` is an `int` field on `glab.NotePosition` — no extra nil check needed inside the branch.

**Part C: parse `LastReviewedSHA` from summary note body**

Analog — existing summary-detection branch (lines 287-289):
```go
if n.Position == nil && strings.Contains(n.Body, vcs.SummaryWrapperBegin) {
    out.SummaryCommentID = strconv.FormatInt(n.ID, 10)
    // ADD: out.LastReviewedSHA = vcs.ParseReviewedSHA(n.Body)
}
```
One-liner addition inside the existing `if` block.

**Part C: `DiffBetween` implementation**

Exact analog — `ListCommits` in `internal/vcs/gitlab/release.go` (lines 31-57):
```go
func (a *Adapter) ListCommits(ctx context.Context, repo, fromRef, toRef string) ([]vcs.Commit, error) {
    cmp, _, err := a.client.Repositories.Compare(repo, &glab.CompareOptions{
        From: ptr(fromRef),
        To:   ptr(toRef),
    }, glab.WithContext(ctx))
    if err != nil {
        return nil, fmt.Errorf("compare commits %s %s..%s: %w", repo, fromRef, toRef, err)
    }
    if cmp == nil {
        return nil, nil
    }
    // ... convert cmp.Commits to []vcs.Commit ...
}
```
`DiffBetween` uses the same `Repositories.Compare` call but converts `cmp.Diffs` into `[]vcs.FileChange` instead of `cmp.Commits` into `[]vcs.Commit`. On non-nil error from Compare, return `(nil, nil)` (full-review fallback per RESEARCH pitfall §3).

Compile-time assertion to add (follows pattern at bottom of `release.go` lines 314-317):
```go
var _ vcs.DiffBetweener = (*Adapter)(nil)
```

---

### `internal/vcs/github/github.go` — Part B (anchor line + GraphQL) + Part C (LastReviewedSHA + DiffBetween)

**Part B: extend GraphQL query to fetch `line` on `ReviewThread`**

Analog — existing GraphQL query struct in `ListCadooArtifacts` (lines 245-285):
```go
const q = `query($owner:String!,$name:String!,$num:Int!,$tc:String,$rc:String){
  repository(owner:$owner,name:$name){ pullRequest(number:$num){
    comments(first:100,after:$tc){ nodes{ databaseId body }
      pageInfo{ hasNextPage endCursor } }
    reviewThreads(first:100,after:$rc){ nodes{ id isResolved
      comments(first:1){ nodes{ path body } } }
      pageInfo{ hasNextPage endCursor } }
  }}}`
```
Add `line originalLine` to the `reviewThreads` node selection:
```
reviewThreads(first:100,after:$rc){ nodes{ id isResolved line originalLine
  comments(first:1){ nodes{ path body } } }
```
Add matching fields to the anonymous struct:
```go
Nodes []struct {
    ID         string `json:"id"`
    IsResolved bool   `json:"isResolved"`
    Line       *int   `json:"line"`         // ADD: nullable per assumption A2
    OrigLine   *int   `json:"originalLine"` // ADD
    Comments   struct { ... } `json:"comments"`
} `json:"nodes"`
```
Populate `Line` in the `out.Inline` append (lines 328-337):
```go
out.Inline = append(out.Inline, vcs.PriorInline{
    // existing fields ...
    Resolved: th.IsResolved,
    // ADD:
    // Line:    ptrIntVal(th.Line),    // 0 when nil
    // EndLine: ptrIntVal(th.Line),
})
```

**Part C: parse `LastReviewedSHA` from issue-comment body**

Analog — existing summary-comment detection (lines 306-309):
```go
for _, c := range p.Comments.Nodes {
    if out.SummaryCommentID == "" && strings.Contains(c.Body, vcs.SummaryWrapperBegin) {
        out.SummaryCommentID = strconv.FormatInt(c.DatabaseID, 10)
        // ADD: if out.LastReviewedSHA == "" { out.LastReviewedSHA = vcs.ParseReviewedSHA(c.Body) }
    }
}
```

**Part C: `DiffBetween` implementation**

Exact analog — `ListCommits` in `internal/vcs/github/release.go` (lines 27-57):
```go
func (a *Adapter) ListCommits(ctx context.Context, repo, fromRef, toRef string) ([]vcs.Commit, error) {
    owner, name, err := splitRepo(repo)
    if err != nil { return nil, err }
    cmp, _, err := a.client.Repositories.CompareCommits(ctx, owner, name, fromRef, toRef, nil)
    if err != nil {
        return nil, fmt.Errorf("compare commits %s %s..%s: %w", repo, fromRef, toRef, err)
    }
    // ... convert cmp.Commits ...
}
```
`DiffBetween` calls the same `CompareCommits`; on `err != nil` (including the 404-not-ancestor case) return `(nil, nil)`. On success, convert `cmp.Files` (type `[]*gogithub.CommitFile`) to `[]vcs.FileChange`.

Compile-time assertion to add:
```go
var _ vcs.DiffBetweener = (*Adapter)(nil)
```

---

### `internal/orchestrator/consolidate.go` — Part C

**Analog:** existing `renderConsolidated` function signature and wrapper emit (lines 63-85):
```go
func renderConsolidated(sections []findings.Section) string {
    // ... sort ...
    var b strings.Builder
    b.WriteString(wrapperBegin)
    b.WriteString("\n")
    for _, s := range sections {
        b.WriteString(renderSection(s))
        b.WriteString("\n")
    }
    b.WriteString(wrapperEnd)
    return b.String()
}
```
New signature adds `headSHA string` parameter. Marker is emitted immediately after `wrapperBegin`:
```go
func renderConsolidated(sections []findings.Section, headSHA string) string {
    // ... sort (unchanged) ...
    var b strings.Builder
    b.WriteString(wrapperBegin)
    b.WriteString("\n")
    if headSHA != "" {
        b.WriteString(vcs.RenderReviewedSHA(headSHA))  // NEW
        b.WriteString("\n")
    }
    for _, s := range sections {
        // ... unchanged ...
    }
    b.WriteString(wrapperEnd)
    return b.String()
}
```
**Call-site update (RESEARCH pitfall §2):** update the two call sites in `consolidate_test.go` to pass `""`, and the production call in `postSummary` to pass `pr.HeadSHA`.

**Constant placement:** `reviewedSHAPrefix` / `reviewedSHASuffix` constants live in `marker.go` alongside `SummaryWrapperBegin`, not in `consolidate.go`, to keep marker definitions co-located.

---

### `internal/orchestrator/reviewer.go` — Part A, C

**Part A: `resolveStalePriors` — replace first-line recompute with direct compare**

Exact analog — the buggy block being replaced (lines 491-495):
```go
// BEFORE (buggy):
pkey := findings.StructuralKey(p.Tool, vcs.InlineComment{
    File:     p.File,
    Severity: vcs.Severity(p.Severity),
    Body:     p.Title,   // p.Title is firstLine(body) — lossy
})
if _, present := currentKeys[pkey]; present {
    continue
}

// AFTER:
var pkey string
if p.StructuralKey == "" {
    // Legacy record: fall back to first-line recompute for backward compat.
    pkey = findings.StructuralKey(p.Tool, vcs.InlineComment{
        File:     p.File,
        Severity: vcs.Severity(p.Severity),
        Body:     p.Title,
    })
} else {
    pkey = p.StructuralKey
}
if _, present := currentKeys[pkey]; present {
    continue
}
```
The surrounding function signature, loop structure, and `ResolveThread` call are unchanged.

**Part C: optional-capability probe pattern for `DiffBetween`**

Exact analog — `FileFetcher` and `ReleaseRangeReader` capability probes already in the codebase:
```go
// FileFetcher probe (reviewer.go:507-511):
ff, ok := provider.(FileFetcher)
if !ok || pr.HeadSHA == "" {
    return d.BaseCfg
}

// ReleaseRangeReader probe (in release-docs dispatcher):
rrr, ok := provider.(vcs.ReleaseRangeReader)
if !ok { /* degrade */ }
```
New incremental-diff probe (after loading prior, around `Run` line ~233):
```go
if db, ok := provider.(vcs.DiffBetweener); ok {
    incr, err := db.DiffBetween(ctx, pr.RepoFullName, sha, pr.HeadSHA)
    if err == nil && incr != nil {
        // use incremental path
    }
    // err != nil or incr == nil → full-review fallback (no action)
}
```

**Part C: incremental `resolveStalePriors` — add `changeSet` + `incrementalRun` parameters**

Analog — current function signature and the existing tool-scoping guard (lines 475-478):
```go
func (d *Dispatcher) resolveStalePriors(
    ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest,
    tool string, prior []findings.PostedFinding, current []vcs.InlineComment,
) {
    if tool == "" || len(prior) == 0 {
        return
    }
    // ...
}
```
New signature appends two parameters; the early-return guard is extended:
```go
func (d *Dispatcher) resolveStalePriors(
    ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest,
    tool string, prior []findings.PostedFinding, current []vcs.InlineComment,
    changeSet map[string]struct{}, incrementalRun bool,
) {
    if tool == "" || len(prior) == 0 {
        return
    }
    // ... existing currentKeys build ...
    for _, p := range prior {
        // ... existing tool/ExternalCommentID guard ...
        // NEW: incremental change-set gate
        if incrementalRun && len(changeSet) > 0 {
            if _, changed := changeSet[p.File]; !changed {
                continue // prior on unchanged code — leave thread open
            }
        }
        // ... existing pkey compare ...
    }
}
```
Update the single call site at `reviewer.go:467` to pass `inChangeSet` and `isIncrementalRun`.

---

### `internal/tools/tools.go` — Part C

**Additive field pattern:** existing `Input` fields `KBHits`, `Learnings`, `Issues` (lines 64-90) all follow the pattern of a comment block explaining purpose/nil semantics, then a field declaration. Copy exactly:
```go
// KBHits is the orchestrator's pre-fetched semantic-search hits from
// the per-repo knowledge base. May be empty if the KB is not configured
// or no relevant chunks were found.
KBHits []kb.Hit
```
New fields follow the same style:
```go
// IncrementalFiles and IncrementalPacked hold the diff since the last
// reviewed SHA. Both are nil on first run or after a force-push. Tools
// that emit inline comments should use these when non-nil; summary-only
// tools (describe, changelog) always use Files/Packed.
IncrementalFiles  []vcs.FileChange
IncrementalPacked contextengine.Compressed
// IsIncrementalRun is true when the orchestrator has a valid lastReviewedSHA
// and is limiting inline tools to the incremental change set.
IsIncrementalRun bool
```
No existing `Input` fields are removed or renamed.

---

## Shared Patterns

### Optional-capability interface declaration (applies to `vcs.go`)
**Source:** `internal/vcs/vcs.go` lines 132-135 (`PriorReviewReader`) and lines 215-226 (`ReleaseRangeReader`)

Pattern:
```go
// XxxCapability is an OPTIONAL capability. Adapters that can ... implement
// this interface so ... The orchestrator type-asserts for it; providers
// that don't implement it degrade gracefully ...
type XxxCapability interface {
    Method(ctx context.Context, ...) (..., error)
}
```
Apply to: `DiffBetweener` in `vcs.go`.

### Optional-capability probe + graceful degrade (applies to `reviewer.go`)
**Source:** `internal/orchestrator/reviewer.go` lines 507-513 (`loadCfg`)

Pattern:
```go
ff, ok := provider.(CapabilityInterface)
if !ok {
    // degrade: use fallback path, no error logged at warn level
    return fallback
}
// use ff
```
Apply to: `DiffBetweener` probe in `Run`.

### Compile-time interface assertion (applies to both adapter `release.go` files)
**Source:** `internal/vcs/gitlab/release.go` lines 314-317, `internal/vcs/github/release.go` lines 312-316

Pattern:
```go
var _ vcs.SomeInterface = (*Adapter)(nil)
```
Add `var _ vcs.DiffBetweener = (*Adapter)(nil)` to the assertion block at the bottom of each `release.go` (or at the bottom of the new `DiffBetween` method file if split).

### HTML-comment marker in summary wrapper (applies to `marker.go` + `consolidate.go`)
**Source:** `internal/vcs/marker.go` lines 14, 32-37 — all markers are HTML comments, invisible in rendered markdown, machine-greppable

Pattern: `<!-- cadoo:<name>:<value> -->` where value is a flat token (no spaces). New `reviewed-sha` marker follows: `<!-- cadoo:reviewed-sha:<40-hex-chars> -->`.

### Nil-safe Store dispatch (applies to all new `Store` methods)
**Source:** `internal/findings/findings.go` — every `Store` method opens with:
```go
func (s *Store) MethodName(...) (T, error) {
    if s == nil { return zero, nil }
    if s.mem != nil { return s.mem.methodName(...), nil }
    if s.pool == nil { return zero, nil }
    // ... SQL ...
}
```
Apply this pattern to the new `LastReviewedSHA() string` method.

### Table-driven unit tests in `findings` package (applies to new Part B tests)
**Source:** `internal/findings/findings_test.go` lines 68-80:
```go
cases := map[string]string{
    "input": "expected output",
    // ...
}
for in, want := range cases {
    if got := funcUnderTest(in); got != want {
        t.Errorf("funcUnderTest(%q) = %q, want %q", in, got, want)
    }
}
```
Apply to `TestMemoryStoreHasResolvedSuppressesSameFile` and siblings — use a `cases` slice or map of `{name, input, resolved, line, expectSuppressed}` structs.

### Two-run scenario test (applies to new Part C integration tests)
**Source:** `internal/orchestrator/reviewer_test.go` lines 411-450 (`TestCIModeTwoRunIdempotency`):
```go
// Run 1: fresh prior store
d1 := &Dispatcher{Posted: findings.NewFromPrior(key, vcs.PriorReview{})}
d1.postSummary(ctx, sv, pr, key, "review", "## Overview\nfirst pass")
d1.postInline(ctx, sv, pr, key, "review", []vcs.InlineComment{c1, c2})

// Replay prior from sv.replay()
prior := sv.replay()
sv.inline = nil

// Run 2: seed from prior
d2 := &Dispatcher{Posted: findings.NewFromPrior(key, prior)}
// ... assertions on sv.inline count and sv.resolved ...
```
`TestCIModeFixedPointUnchangedHead` copies this exact two-run structure. `sv.replay()` needs to be extended to populate `LastReviewedSHA` from the stamped summary body so the incremental path can be exercised.

---

## No Analog Found

All modified files have close existing analogs. No file is created from scratch.

---

## Metadata

**Analog search scope:** `internal/findings/`, `internal/vcs/`, `internal/orchestrator/`, `internal/tools/`
**Files scanned:** 12 (findings.go, prior.go, prior_test.go, findings_test.go, vcs.go, marker.go, marker_test.go, gitlab/gitlab.go, gitlab/release.go, github/github.go, github/release.go, orchestrator/consolidate.go, orchestrator/reviewer.go, orchestrator/consolidate_test.go, orchestrator/reviewer_test.go, tools/tools.go)
**Pattern extraction date:** 2026-06-15
