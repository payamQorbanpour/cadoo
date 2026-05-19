# Prompt Optimization + Parallel CI Dispatch

**Date:** 2026-05-19
**Status:** Approved

## Problem

Three related issues observed in CI mode:

1. The `improve` tool's prompt grew unboundedly on large PRs because `BuildDiffPrompt` appended every section regardless of tool relevance. Combined with cross-tool `PriorFindings` accumulation, this caused `finish_reason="length"` context overflow.
2. The `improve` system prompt's suggestion cap ("prefer 2-5") was routinely ignored by the LLM, producing too many low-value suggestions.
3. The three CI tools (describe → review → improve) run sequentially, each spending ~5-15s on an LLM call, resulting in ~45s wall time when ~15s is achievable.

## Goals

- Give each tool explicit control over which prompt sections it includes.
- Cap `improve` suggestions reliably at the schema level, not just in prose instructions.
- Parallelize CI tool dispatch so LLM calls run concurrently; serialize only the VCS write path.
- Parallelize custom checks within the `check` tool.

## Non-Goals

- Changing any tool's behavior for the webhook/worker path — only CI mode gets the parallel dispatch.
- Rewriting prompt content for tools other than `improve`.
- Token counting or dynamic budget allocation (deferred; the cap + per-tool exclusions are sufficient).

---

## Part 1 — `PromptOptions`: tool-aware section control

### Interface

Add to `internal/tools/prompt.go`:

```go
// PromptOptions controls which optional sections BuildDiffPrompt includes.
// Zero value includes everything (backward-compatible default).
type PromptOptions struct {
    SkipTrackerIssues  bool // omit ## Linked tracker issues
    SkipSlopSignal     bool // omit ## Pre-review signal
    SkipStaticAnalysis bool // omit ## Static analysis findings
    MaxPRBodyRunes     int  // truncate PR description; 0 = unlimited
}
```

`BuildDiffPrompt` signature changes to:

```go
func BuildDiffPrompt(in Input, opts PromptOptions) string
```

Zero value of `PromptOptions{}` preserves current behavior — all sections included, no body truncation. All existing callers (review, describe, ask, changelog, etc.) pass `PromptOptions{}` or a named `DefaultPromptOptions()` helper.

### Per-tool options

| Tool | SkipTrackerIssues | SkipSlopSignal | SkipStaticAnalysis | MaxPRBodyRunes |
|------|:-----------------:|:--------------:|:------------------:|:--------------:|
| `improve` | ✓ | ✓ | — | 800 |
| `review` | — | — | — | 0 |
| `describe` | — | — | ✓ | 0 |
| all others | — | — | — | 0 |

Rationale for `improve`:
- Tracker issues are correctness/feature requirements; `improve` only needs the diff and conventions.
- Slop signal is a pre-review quality gate; it adds no value to code suggestion generation.
- PR body truncation at 800 runes keeps enough context while removing boilerplate.

Rationale for `describe`:
- Static analysis findings are bug-class signals; `describe` generates titles and summaries, not bug commentary.

### Migration

1. Update `BuildDiffPrompt` to accept `opts PromptOptions`.
2. Update all call sites to pass appropriate options (most pass `PromptOptions{}`).
3. Update `prompt_test.go` to cover opt-out combinations.

---

## Part 2 — `improve` system prompt: enforce suggestion cap

### Changes to `improve/improve.go`

Two changes to `systemPrompt`:

**Schema**: Add `"max": 5` as a top-level field to the JSON response schema. The LLM treats schema constraints more literally than prose guidance.

```json
{
  "summary": "...",
  "max": 5,
  "suggestions": [...]
}
```

**Rules block**: Replace:
> "Prefer 2-5 high-leverage suggestions over many trivial ones."

With:
> "Return AT MOST 5 suggestions. Rank all candidates by impact; drop everything outside the top 5. Only suggest a change if it materially improves correctness, performance, security, or API clarity — not cosmetic rewrites, renames, or adding comments."

The `Output` struct gets a `Max int` field (parsed but ignored at runtime — the constraint is enforced by the model at generation time, not validated server-side).

---

## Part 3 — Parallel CI dispatch

### Thread-safety prerequisites

Two shared structures need protection before goroutines are added:

**`findings.Store`** (`internal/findings/findings.go`):
- Add `mu sync.RWMutex`.
- `HasFinding` / `HasSummary` → `mu.RLock`.
- `RecordFinding` / `RecordSummary` → `mu.Lock`.

**Consolidated comment edit** (`internal/orchestrator/consolidate.go` or the `postSummary` path in `reviewer.go`):
- Add `summaryMu sync.Mutex` to `Dispatcher`.
- The lock is held only for the read-current-body → patch-section → write-back sequence. The LLM call happens entirely outside this lock.

### `ci.go` dispatch loop

Replace the sequential `for` loop with `errgroup`:

```go
import "golang.org/x/sync/errgroup"

g, gctx := errgroup.WithContext(ctx)
var firstErrMu sync.Mutex
var firstErr error

for _, name := range toolList {
    name := name // capture
    g.Go(func() error {
        fmt.Fprintf(os.Stderr, "ci: dispatching %s on %s%s%d\n", name, ...)
        job := orchestrator.ToolJob{..., Tool: name}
        if err := d.Run(gctx, job); err != nil {
            fmt.Fprintf(os.Stderr, "ci: %s failed: %v\n", name, err)
            firstErrMu.Lock()
            if firstErr == nil { firstErr = err }
            firstErrMu.Unlock()
        }
        return nil // keep-going semantics: don't cancel siblings on failure
    })
}
_ = g.Wait()
if firstErr != nil {
    os.Exit(1)
}
```

Keep-going semantics are preserved: a failure in one tool does not cancel sibling goroutines (mirrors the current `continue` behavior).

### `check` tool internal parallelism

`check.Run()` currently loops over `in.Config.Checks` sequentially, one LLM call per rule. Replace with an `errgroup`:

```go
type checkResult struct {
    name    string
    sev     vcs.Severity
    inlines []vcs.InlineComment
    run     vcs.CheckRun
}

g, gctx := errgroup.WithContext(ctx)
results := make([]checkResult, len(in.Config.Checks))
for i, c := range in.Config.Checks {
    i, c := i, c
    g.Go(func() error {
        findings, err := runOne(gctx, in.LLM, in.Model, c, in)
        if err != nil { return nil } // keep-going
        results[i] = buildCheckResult(c, findings)
        return nil
    })
}
g.Wait()
// merge results in original order
```

Results are merged in original rule order to keep output deterministic.

---

## Architecture impact

- `BuildDiffPrompt` gains a parameter — all call sites updated, no behavior change for callers passing zero value.
- `Dispatcher` gains `summaryMu sync.Mutex` — zero-value safe, no init required.
- `findings.Store` gains `mu sync.RWMutex` — zero-value safe.
- `ci.go` adds `golang.org/x/sync/errgroup` — `golang.org/x/sync v0.20.0` is already in `go.mod` as an indirect dependency; importing it directly promotes it to a direct dependency, no `go get` required.
- `check.go` adds internal `errgroup` fan-out — no external interface change.

---

## Testing

| Area | Tests to add/update |
|------|-------------------|
| `PromptOptions` | `TestBuildDiffPromptSkipsTrackerIssues`, `TestBuildDiffPromptTruncatesPRBody`, `TestBuildDiffPromptZeroValueIncludesAll` |
| `improve` system prompt | `TestImproveSuggestionCapEnforced` (mock LLM returning 8 suggestions; verify ≤5 posted — or unit-test the system prompt string) |
| `findings.Store` thread safety | `TestStoreConcurrentReadWrite` using `go test -race` |
| CI parallel dispatch | `TestCIConcurrentDispatch` using a mock dispatcher with artificial delay |
| `check` parallelism | `TestCheckRunsInParallel` with a slow-mock LLM |

All existing tests must continue to pass. `go test -race -count=1 ./...` is the acceptance gate.
