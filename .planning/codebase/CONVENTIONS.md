# Coding Conventions
_Last updated: 2026-05-19_

## Language and Toolchain

- Go **1.26** (see `go.mod` and `.github/workflows/ci.yml`)
- Formatter: `gofmt` + `goimports` (golangci-lint v2 formatters block)
- Linter: `golangci-lint` **v2.12.2** pinned in CI (`golangci/golangci-lint-action@v9`)
- Config: `.golangci.yml` (root)

---

## Import Grouping

Three groups, enforced by `goimports` with `local-prefixes: github.com/payamqorbanpour/cadoo`:

```go
import (
    // 1. stdlib
    "context"
    "fmt"
    "log/slog"

    // 2. third-party
    gogithub "github.com/google/go-github/v66/github"
    "github.com/jackc/pgx/v5/pgxpool"

    // 3. cadoo-internal (goimports local-prefixes group)
    "github.com/payamqorbanpour/cadoo/internal/findings"
    "github.com/payamqorbanpour/cadoo/internal/vcs"
)
```

Example: `internal/orchestrator/reviewer.go`, `cmd/cadoo-webhook/main.go`.

---

## Package Naming

- Packages match their directory name: `package vcs`, `package findings`, `package orchestrator`.
- Packages under `internal/tools/<name>/` use the tool name as the package name: `package review`, `package describe`.
- The `package_comments` revive rule is **disabled** — package-level doc comments on files are optional, but every **exported symbol** must have a doc comment (`exported` rule is **enabled**).

### Package doc comment style

Every non-trivial package has a single-line `// Package <name> ...` doc on the primary source file:

```go
// Package vcs defines the provider-agnostic interface every Git-host adapter
// implements (github, github_enterprise, gitlab, ...).
package vcs
```

Example: `internal/vcs/vcs.go:1`, `internal/tools/tools.go:1`, `internal/findings/findings.go:1`.

---

## Exported Symbol Docstrings

The `revive/exported` rule is **on**. Every exported type, function, method, and constant needs a doc comment. Pattern in use:

```go
// Kind identifies a VCS provider.
type Kind string

// New returns a ready Adapter authenticated either by bearer token or as a
// GitHub App installation, depending on which fields of cfg are set.
func New(cfg Config) (*Adapter, error) { ... }

// SimilarTitleThreshold is the Jaccard score above which two normalized
// titles are treated as the same finding.
const SimilarTitleThreshold = 0.5
```

Docstrings start with the symbol name. For interfaces, each method that has non-obvious semantics gets an inline or preceding comment explaining the contract (degradation behavior especially — see `vcs.Provider.ResolveThread` in `internal/vcs/vcs.go:113`).

---

## Linting Rules

Config: `.golangci.yml`.

**Enabled linters:**
| Linter | Purpose |
|--------|---------|
| `bodyclose` | HTTP response body must be closed |
| `errcheck` | All errors must be checked |
| `govet` | `go vet` checks |
| `ineffassign` | Catch useless assignments |
| `misspell` | Spelling in comments/strings |
| `revive` | Style + idiom rules (see below) |
| `rowserrcheck` | `sql.Rows.Err()` must be checked |
| `sqlclosecheck` | `sql.Rows`/`sql.Stmt` must be closed |
| `staticcheck` | Static analysis |
| `unused` | Dead code detection |

**Revive rules enabled:**
`blank-imports`, `context-as-argument`, `context-keys-type`, `empty-block`, `error-return`, `error-strings`, `errorf`, `exported`, `increment-decrement`, `indent-error-flow`, `range`, `receiver-naming`, `redefines-builtin-id`, `superfluous-else`, `time-naming`, `unexported-return`, `unreachable-code`, `var-declaration`

**Revive rules explicitly disabled:**
- `package-comments` — package doc comments on non-primary files are not required
- `unused-parameter` — unused function parameters do not fail lint (intentional for interface implementations)

**Test file exclusions:** `bodyclose` and `errcheck` are excluded for `_test.go` files.

---

## Error Handling

**Pattern: wrap with `%w` and a lowercase action prefix.**

```go
return fmt.Errorf("fetch pr: %w", err)
return fmt.Errorf("decode tool job: %w", err)
return fmt.Errorf("github app auth: %w", err)
```

Examples: `internal/orchestrator/reviewer.go:134,175,183`, `internal/vcs/github/github.go:62,66`.

**Non-wrapping errors use plain `fmt.Errorf` with context:**
```go
return fmt.Errorf("no adapter for provider %q (configured: %v)", provider, keys)
return fmt.Errorf("unknown tool %q (registered: %v)", job.Tool, d.Registry.Names())
```
Examples: `internal/orchestrator/reviewer.go:165,170`.

**Non-fatal errors are logged and swallowed, never propagated, when partial output is still valuable:**
```go
slog.Error("post inline review", "err", err, "pr", pr.URL)
// continues rather than returning
```
Example: `internal/orchestrator/reviewer.go:290-295`.

**Nil-tolerant receivers** — structs whose zero/nil value should be a safe no-op guard at the top of every method:
```go
func (s *Store) HasFinding(ctx context.Context, ...) (bool, error) {
    if s == nil {
        return false, nil
    }
    ...
}
```
This pattern is used throughout `internal/findings/findings.go` (lines 124, 168, 200, 235, 258, 295, 321) and `internal/kb/querydistill/distill.go:34`. A nil `*Store`, `*Distiller`, etc. is always a valid no-op — never panic.

---

## Logging

`log/slog` (stdlib structured logging) is the only logger used. No third-party logger dependency.

**Key/value pairs follow `slog` conventions:**
```go
slog.Error("post summary", "err", err, "pr", pr.URL)
slog.Debug("dedup lookup", "err", err)
slog.Debug("tracker lookup", "tracker", tracker.Name(), "err", err)
```

- Non-fatal side-effects (notify, KB search, learnings) log at `Debug`.
- Posting failures that degrade output quality log at `Error`.
- No `log.Printf` or `fmt.Println` in production code.

---

## Interface Design

**Provider-agnostic interfaces declared in the shared package; implementations in sub-packages:**
- `vcs.Provider` declared in `internal/vcs/vcs.go` — implemented by `internal/vcs/github` and `internal/vcs/gitlab`. The orchestrator and tools depend **only** on the interface, never on provider packages directly.
- `llm.Provider` declared in `internal/llm/provider.go` — implemented by `internal/llm/litellm`.
- `tools.Tool` declared in `internal/tools/tools.go` — each tool sub-package implements it.

**Optional capabilities use separate interfaces and type-assertion:**
```go
// PriorReviewReader is an OPTIONAL capability.
type PriorReviewReader interface {
    ListCadooArtifacts(ctx context.Context, pr *PullRequest) (PriorReview, error)
}
// Usage:
if r, ok := provider.(vcs.PriorReviewReader); ok {
    prior, _ = r.ListCadooArtifacts(ctx, pr)
}
```
Examples: `internal/vcs/vcs.go:128`, `internal/orchestrator/reviewer.go` FileFetcher capability.

**Interface methods always take `context.Context` as the first argument** (enforced by revive `context-as-argument` rule).

---

## Idempotency Pattern

All posting operations are idempotent across re-dispatches (e.g. a PR `synchronize` event triggers a new run). Mechanism:

1. `findings.Store` fingerprints each inline comment and tracks the summary comment ID per `(provider, repo, pr)`.
2. `postInline` calls `Posted.HasFinding` before posting and skips already-posted fingerprints.
3. `postSummary` edits the existing comment (`UpdateSummaryComment`) instead of creating a new one when `SummaryID` is found.
4. In CI-mode (no DB), `findings.NewFromPrior` seeds the in-memory store from `vcs.PriorReview` read back from the PR/MR comments.

Never bypass `Posted` when adding new tools that post comments.

---

## Wrapper Markers

HTML comment markers make cadoo-managed content machine-greppable across re-runs:

```
<!-- cadoo:pr-body:begin -->   ... <!-- cadoo:pr-body:end -->   (PR description section)
<!-- cadoo:wrapper:begin -->   ... <!-- cadoo:wrapper:end -->   (consolidated summary comment)
<!-- cadoo:fp v=1 tool=<name> sk=<hex16> sev=<level> nt=<normalized-title> -->  (inline comment fingerprint)
```

`internal/orchestrator/consolidate.go` is the canonical source for wrapper format constants. `internal/vcs/marker.go` owns `InlineMarker`/`ParseInlineMarker`. Do not invent alternative marker formats.

---

## Naming Conventions

**Types:** `PascalCase` (`PullRequest`, `InlineComment`, `ToolJob`)
**Functions/methods:** `camelCase` (`postInline`, `renderConsolidated`, `parseHunkHeader`)
**Exported constants:** `PascalCase` prefix matching their type (`KindGitHub`, `SeverityWarn`, `CheckFailed`)
**Unexported constants:** `camelCase` (`prSectionBegin`, `brandAvatar`)
**Receiver names:** Short — single or two letter abbreviations matching the type (`f *fakeVCS`, `d *Dispatcher`, `s *Store`)

**File names:** `snake_case` for multi-word files (`reviewer.go`, `consolidate.go`, `prior_test.go`). Single concept files use the concept name (`github.go`, `marker.go`).

---

## Comment Style

- Exported symbols: doc comment starts with the symbol name.
- Block of related constants: one group comment covering the set (see `vcs.go` severity constants).
- Inline comments on struct fields for non-obvious semantics (see `vcs.PullRequest`, `vcs.PostedInlineRef`).
- Test functions with non-obvious invariants include a prose comment explaining the regression/scenario being tested (see `TestCIModeSuppressesRephrasedImproveOnPush2` in `internal/orchestrator/reviewer_test.go:444`).
