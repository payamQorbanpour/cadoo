---
phase: 08-ci-mode-dedup-convergence
reviewed: 2026-06-15T00:00:00Z
depth: standard
files_reviewed: 16
files_reviewed_list:
  - internal/findings/findings.go
  - internal/findings/findings_test.go
  - internal/findings/prior.go
  - internal/findings/prior_test.go
  - internal/orchestrator/consolidate.go
  - internal/orchestrator/consolidate_test.go
  - internal/orchestrator/reviewer.go
  - internal/orchestrator/reviewer_test.go
  - internal/tools/tools.go
  - internal/vcs/github/github.go
  - internal/vcs/github/release.go
  - internal/vcs/gitlab/gitlab.go
  - internal/vcs/gitlab/release.go
  - internal/vcs/marker.go
  - internal/vcs/marker_test.go
  - internal/vcs/vcs.go
findings:
  critical: 3
  warning: 5
  info: 2
  total: 10
status: issues_found
---

# Phase 08: Code Review Report

**Reviewed:** 2026-06-15T00:00:00Z
**Depth:** standard
**Files Reviewed:** 16
**Status:** issues_found

## Summary

This review covers the CI-mode dedup convergence implementation across four subsystems: the in-memory findings store with resolved-thread suppression, the orchestrator's incremental dispatch block, the `StampInline`/`NewFromPrior` prior-seeding pipeline, and the VCS adapter implementations for GitHub and GitLab.

The core logic of every subsystem is sound. The resolved-prior suppression, incremental change-set gating, and marker round-trip are correct and well-tested. However, three bugs are present that will cause incorrect behaviour in production:

1. `resolveStalePriors` resolves already-resolved threads on every run, spamming the VCS API and potentially confusing the resolved-prior suppression logic it is supposed to cooperate with.
2. The GitLab `DiffBetween` does not populate `IsBinary`, `Additions`, or `Deletions` on returned `FileChange` records, so the `slop` detector and linter change-set filter silently mis-classify binary files as reviewable source.
3. `postSummary` calls `PutSection` before acquiring `summaryMu`, creating a race window between CI-mode parallel tool goroutines that can produce a stale consolidated comment whose sections are incomplete.

---

## Critical Issues

### CR-01: `resolveStalePriors` resolves already-resolved threads on every run

**File:** `internal/orchestrator/reviewer.go:559-563`

**Issue:** `resolveStalePriors` iterates every prior that belongs to the current tool and calls `provider.ResolveThread` for any prior whose `StructuralKey` is absent from the current run's output. It never checks whether the prior is already resolved. The `PostedFinding` struct does not carry a `Resolved` field (the field lives on `findingRec` and `PriorInline`, but is never projected through `ListPostedFindings`), so every already-resolved thread — seeded via `NewFromPrior` from the VCS — is re-resolved on every subsequent run. This causes:

- Redundant VCS API calls on every push (potential rate-limiting).
- For the resolved-prior suppression to work correctly (`memoryStore.has` resolved branch), the record must have `Resolved=true` in the in-memory store. But `ListPostedFindings` returns `PostedFinding` which has no `Resolved` field, so the resolver can never tell from the `Posted` store alone that a thread is already resolved. In the stateless CI-mode path the store is `NewFromPrior` (has `Resolved` correctly set) but `resolveStalePriors` reads from `Posted.ListPostedFindings` not from the seeded `findingRec` — it re-resolves threads that the user already resolved, fighting the sticky-suppression feature.

```go
// in resolveStalePriors, before the ResolveThread call:
for _, p := range prior {
    if p.Tool != tool || p.ExternalCommentID == "" {
        continue
    }
    // BUG: no check for p.Resolved — add this:
    // if p.Resolved {
    //     continue
    // }
    // ... (rest of logic)
    if err := provider.ResolveThread(ctx, pr, p.ExternalCommentID); err != nil {
```

**Fix:** Add `Resolved bool` to `findings.PostedFinding`, populate it from `findingRec.Resolved` in `memoryStore.list` (and from the `resolved` column in the DB query in `ListPostedFindings`), and skip already-resolved priors in `resolveStalePriors`:

```go
// findings/findings.go — PostedFinding struct
type PostedFinding struct {
    Tool              string
    File              string
    LineStart         int
    LineEnd           int
    Severity          string
    Title             string
    ExternalCommentID string
    StructuralKey     string
    Resolved          bool  // add this field
}
```

```go
// findings/findings.go — memoryStore.list
out = append(out, PostedFinding{
    // ... existing fields ...
    Resolved:          r.Resolved,  // add this line
})
```

```go
// orchestrator/reviewer.go — resolveStalePriors, after the ExternalCommentID check
if p.Resolved {
    continue
}
```

---

### CR-02: GitLab `DiffBetween` returns incomplete `FileChange` records (missing `IsBinary`, `Additions`, `Deletions`)

**File:** `internal/vcs/gitlab/release.go:346-353`

**Issue:** `gitlab.(*Adapter).DiffBetween` builds `FileChange` records from `cmp.Diffs` but omits the `IsBinary`, `Additions`, and `Deletions` fields. The `ListChangedFiles` implementation (`gitlab.go:80-90`) correctly sets these fields. The delta files fed to `fileSet` and then to the linter `byExt` loop in `runLinters` check `f.IsBinary` to skip binary files:

```go
// orchestrator/reviewer.go:660
if f.IsBinary || f.Status == "removed" {
    continue
}
```

When `DiffBetween` returns incremental files with `IsBinary=false` (zero value), binary files pass the guard and the linter is invoked on them. Additionally, the slop detector accumulates `f.Additions` and `f.Deletions` across all files:

```go
// orchestrator/reviewer.go:211-213
for _, f := range files {
    totalAdd += f.Additions
    totalDel += f.Deletions
}
```

On an incremental run the slop report will report 0 additions and 0 deletions regardless of the actual diff size.

**Fix:**

```go
// internal/vcs/gitlab/release.go — DiffBetween
add, del := countDiffLines(d.Diff)
out = append(out, vcs.FileChange{
    Path:      filePath,
    PrevPath:  d.OldPath,
    Status:    status,
    Patch:     d.Diff,
    Additions: add,
    Deletions: del,
    IsBinary:  d.Diff == "" && !d.DeletedFile,
})
```

Note: `countDiffLines` already exists in `gitlab.go` — it must either be exported or the `DiffBetween` code must duplicate the two-line counter inline.

---

### CR-03: `postSummary` races on `PutSection` before acquiring `summaryMu`

**File:** `internal/orchestrator/reviewer.go:394-398`

**Issue:** In the consolidated path, `postSummary` calls `d.Posted.PutSection` **before** acquiring `d.summaryMu`. When three tool goroutines call `postSummary` concurrently (as in `TestPostSummaryConcurrentNoDuplicates`), all three can interleave as follows:

1. Goroutine A: `PutSection("describe", ...)`
2. Goroutine B: `PutSection("review", ...)`
3. Goroutine A: acquires `summaryMu`, calls `AllSections` — sees only "describe" and "review" (not "improve" yet), posts a comment with two sections.
4. Goroutine C: `PutSection("improve", ...)`
5. Goroutine B: acquires `summaryMu`, calls `AllSections` — now sees all three, updates the comment correctly.
6. Goroutine C: acquires `summaryMu`, calls `AllSections`, updates again.

The result is not a duplicate comment (the mutex prevents that), but the first `PostSummaryComment` call creates a comment body that is missing one or more tool sections that had not yet been stored. This means the consolidated comment can briefly show an incomplete review. More critically: if `PostSummaryComment` succeeds and stores the comment ID, subsequent goroutines call `UpdateSummaryComment` — but the stored `rendered` body in step 3 above is stale. The final update in step 6 will be correct, but if Goroutine B's `UpdateSummaryComment` fails (network error, deleted comment), the fallback creates a second comment.

The `PutSection` call must be inside the mutex to prevent a goroutine from reading `AllSections` before another goroutine's section has been stored.

**Fix:**

```go
func (d *Dispatcher) postSummary(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, key findings.PRKey, tool, body string) {
    if tool == "" || d.Posted == nil || !d.Posted.Enabled() {
        if _, err := provider.PostSummaryComment(ctx, pr, body); err != nil {
            slog.Error("post summary", "err", err, "pr", pr.URL)
        }
        return
    }

    d.summaryMu.Lock()         // move lock to before PutSection
    defer d.summaryMu.Unlock()

    if err := d.Posted.PutSection(ctx, key, tool, body); err != nil {
        slog.Debug("put section", "err", err)
    }

    sections, err := d.Posted.AllSections(ctx, key)
    // ... rest unchanged
```

---

## Warnings

### WR-01: `normalizeTitle` uses `strings.SplitSeq` which may not exist in Go 1.23 stdlib

**File:** `internal/findings/findings.go:410`

**Issue:** `strings.SplitSeq` (a range-over iterator form of `strings.Split`) was added in Go 1.24. The `go.mod` declares `go 1.26`, but if the code is ever built or audited against an earlier toolchain, this will fail. More relevantly: the CLAUDE.md documents "Go toolchain: 1.26" and CI pins accordingly, so this is not a build blocker today. However, the function is unusual enough to flag: the `for raw := range strings.SplitSeq(body, "\n")` form is a forward-compat concern for any toolchain downgrade.

**Fix:** No action required if the team is committed to Go 1.24+. Document the minimum toolchain requirement in the CLAUDE.md `go.mod` note, or rewrite as a conventional loop to eliminate the concern:

```go
for _, raw := range strings.Split(body, "\n") {
```

---

### WR-02: `resolveStalePriors` does not use the change-set gate when `inChangeSet` is empty and `incrementalRun` is true

**File:** `internal/orchestrator/reviewer.go:539-543`

**Issue:** The guard is:

```go
if incrementalRun && len(changeSet) > 0 {
    if _, changed := changeSet[p.File]; !changed {
        continue
    }
}
```

When `incrementalRun=true` but `changeSet` is empty (the fixed-point case: same head SHA), `len(changeSet) == 0` so the outer `if` is false and the gate is skipped. This means on a fixed-point run, if `current` is non-empty (tool still emits findings), all priors whose structural key is absent from the current run will be resolved — including priors on files that were not changed. The empty-changeSet case is supposed to mean "nothing changed, skip everything" but only the posting side respects that; the resolution side does not.

The test `TestCIModeFixedPointUnchangedHead` passes because it passes `current = []vcs.InlineComment{c1, c2}` which matches the priors by structural key, so nothing is resolved. But if a tool emits a subset of findings on a fixed-point run (e.g. a time-based filter), the extra guard is silently absent.

**Fix:** Add an explicit fast-exit for the fixed-point case:

```go
if incrementalRun && len(changeSet) == 0 {
    // Fixed-point: nothing changed, never resolve.
    return
}
if incrementalRun {
    if _, changed := changeSet[p.File]; !changed {
        continue
    }
}
```

---

### WR-03: GitLab `ListCadooArtifacts` uses note-level `Resolved` instead of discussion-level resolution

**File:** `internal/vcs/gitlab/gitlab.go:263`

**Issue:** In GitLab, resolving a thread resolves the whole discussion (`d.ID`), not individual notes. The `glab.Discussion` struct has no `Resolved` field (it only carries `IndividualNote` and `Notes []*Note`). The code correctly reads `n.Resolved` from the note, which does reflect whether the note's discussion is resolved. However, only the first note in a discussion (`d.Notes[0]`) is checked by `vcs.ParseInlineMarker`, and the code iterates ALL notes `n` in `d.Notes` rather than only the first note:

```go
for _, n := range d.Notes {
    if n == nil || n.System {
        continue
    }
    md, stripped, ok := vcs.ParseInlineMarker(n.Body)
    if ok && n.Position != nil {
```

Since only the first note carries the marker, subsequent notes without a marker (but with `n.Position != nil`) will set `Resolved` from whatever note was last iterated. In practice GitLab threads have one note (the opener), but for multi-reply threads, `n.Resolved` on a reply note may differ from the opener's. The `ExternalID` is taken from `d.ID` (correct for `ResolveThread`), but `Resolved` is set from whatever note happens to match the `ParseInlineMarker` test.

**Fix:** Only examine the first note for the inline marker and its `Resolved` status:

```go
if len(d.Notes) == 0 {
    continue
}
first := d.Notes[0]
if first == nil || first.System {
    continue
}
md, stripped, ok := vcs.ParseInlineMarker(first.Body)
if ok && first.Position != nil {
    // ... use first.Resolved for Resolved field
```

---

### WR-04: `inlineMarkerRe` does not anchor the marker to the end of body, allowing forged mid-body markers

**File:** `internal/vcs/marker.go:66-67`

**Issue:** The regex is:

```
\n*<!-- cadoo:fp v=1 tool=(\S+) sk=(\S+) sev=(\S*)(?:\s+nt=(\S+))? -->\s*$
```

`FindStringSubmatchIndex` with `$` in POSIX semantics matches the end of the string. However, the `$` anchor in Go's `regexp` package matches at end of string OR before a newline at end. A comment body like:

```
Fix the leak.
<!-- cadoo:fp v=1 tool=review sk=abc sev=warn -->
Extra user text added after the marker.
```

will NOT match (correctly). But a body ending with the marker followed by only whitespace will match. This is intended. The concern is different: the regex uses `\S+` for `tool` and `sk` — these match any non-space character including special characters. If a VCS user edits a comment to inject `tool=../../etc/passwd` or a shell metacharacter, those values are parsed as-is into `MarkerData.Tool` and `MarkerData.SK` and then stored in the dedup maps. They are never executed, but if `Tool` is subsequently used in a SQL query or log context, the raw value is un-sanitized.

Currently in the codebase the parsed `md.Tool` flows to `findings.NewFromPrior` → `findingRec.Tool` and is compared in `memoryStore.has` (safe string comparison). The risk is low but the ASVS V5 validation comment in `marker.go` specifically calls out input validation — the SHA case is validated but the tool/sk fields are not.

**Fix:** Validate `md.Tool` against the known tool registry after parsing, or add a simple character whitelist:

```go
var validToolRe = regexp.MustCompile(`^[a-z_]{1,32}$`)
var validSKRe = regexp.MustCompile(`^[0-9a-f]{1,32}$`)

func ParseInlineMarker(body string) (MarkerData, string, bool) {
    // ... after successful match ...
    if !validToolRe.MatchString(md.Tool) || !validSKRe.MatchString(md.SK) {
        return MarkerData{}, body, false
    }
    return md, stripped, true
}
```

---

### WR-05: `memoryStore.persist()` is called while holding `m.mu`, holding the lock across I/O

**File:** `internal/findings/findings.go:629,635,671,679,682`

**Issue:** Every mutation method (`record`, `putSummaryID`, `putSection`) acquires `m.mu`, performs its mutation, then calls `m.persist()` — still inside the lock. `persist()` calls `os.MkdirAll`, `os.WriteFile`, and `os.Rename`, all of which are blocking system calls that can stall for milliseconds. During that stall the mutex blocks all concurrent reads (`has`, `list`, `summaryID`, `allSections`) and writes.

On a parallel CI dispatch (three tools running concurrently, each recording findings), one goroutine recording a finding will block the other two goroutines for the duration of an `fsync`-like rename. This is a latency issue rather than a correctness issue, but it degrades the throughput of the concurrent-dispatch path.

**Fix:** Release the mutex before persisting:

```go
func (m *memoryStore) record(key PRKey, tool, externalID string, c vcs.InlineComment) {
    rec := findingRec{ /* ... */ }
    m.mu.Lock()
    // mutation
    m.findings[key] = append(m.findings[key], rec)
    m.mu.Unlock()   // release before I/O
    m.persist()
}
```

This requires that `persist` reads the maps safely — it should acquire the lock read-only, snapshot the maps into the persisted state, release, then write to disk.

---

## Info

### IN-01: `Fingerprint` and `StructuralKey` use SHA1, which is cryptographically weak

**File:** `internal/findings/findings.go:107-126`

**Issue:** Both `Fingerprint` and `StructuralKey` use `crypto/sha1`. SHA1 is not used here for any security purpose — it is used as a content hash for dedup identity. However, `crypto/sha1` is on the Go "deprecated for security" list, and golangci-lint v2 with `gosec` enabled will flag `G401` (`use of weak hash`). For a dedup use-case, SHA1 is fine functionally, but the warning will appear in lint output. `crypto/sha256` truncated to 16 hex chars achieves the same dedup properties with no lint warning.

**Fix:** Replace `sha1.New()` with `sha256.New()` in both `Fingerprint` and `StructuralKey`. The truncated 16-char hex prefix remains collision-resistant at the dedup scale.

---

### IN-02: `github.(*Adapter).Token` auth path creates a plain `http.Client` without the token set until after `gogithub.NewClient`

**File:** `internal/vcs/github/github.go:58-81`

**Issue:** In the token auth branch:

```go
if cfg.Token != "" {
    httpClient = &http.Client{Timeout: 30 * time.Second}
} else {
    // ... app auth sets Transport
}
// ...
if cfg.Token != "" {
    client = client.WithAuthToken(cfg.Token)
}
```

The `httpClient` created for the token path is a bare `&http.Client{}` with no transport — the token is added later via `WithAuthToken`. If `cfg.BaseURL != ""` is also set (GHES with token auth), then `gogithub.NewClient(httpClient).WithEnterpriseURLs(...)` is called first, then `client.WithAuthToken(cfg.Token)` is called on the result. `WithAuthToken` returns a new client, not mutating in place — this is fine. But the bare `httpClient` is also stored in `ad.gqlClient = client.Client()` after `WithAuthToken` has run, so the GraphQL client does get the token transport. The flow is correct but non-obvious. A future refactor that reorders these lines would silently break token auth.

**Fix (code clarity only):** Combine the two token-auth branches into one:

```go
if cfg.Token != "" {
    httpClient = &http.Client{Timeout: 30 * time.Second}
    // Token is applied to the go-github client via WithAuthToken below.
} else {
    // ... app auth ...
}
```

This is a readability suggestion, not a functional bug in the current code.

---

_Reviewed: 2026-06-15T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
