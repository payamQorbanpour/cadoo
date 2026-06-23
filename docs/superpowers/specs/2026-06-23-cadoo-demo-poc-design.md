# `cadoo demo` — offline end-to-end reviewer PoC

**Date:** 2026-06-23
**Status:** Approved design, pre-implementation
**Author:** brainstorming session

## Goal

A zero-dependency, runnable proof-of-concept that drives the **real** Cadoo
review pipeline (`orchestrator.Dispatcher.Run`) against a **local diff** and
"posts" review comments to the terminal and a markdown file — with no Postgres,
no webhooks, no GitHub App, and no live VCS.

This single artifact satisfies two intents:

1. **End-to-end demo of the existing reviewer** — it exercises the genuine
   pipeline (config load, context compression, `/review` tool dispatch,
   consolidate/wrapper rendering, inline dedup), proving the actual product
   works.
2. **A stripped-down, standalone PoC of the core idea** — it runs offline and
   instantly, by default with a built-in mock LLM and a built-in sample diff,
   so anyone can watch "diff in → review comments out" with zero setup.

**Non-goal:** replacing or modifying any existing core code. The PoC is purely
additive and uses the architecture's blessed extension seam — the
`vcs.Provider` interface — so the orchestrator and tools are untouched.

## Why this approach

Two facts from the codebase make this clean:

- `llm.Provider` is a one-method interface — `Chat(ctx, ChatRequest)
  (*ChatResponse, error)`. A mock is trivial and idiomatic; `review_test.go`
  already ships a `fakeLLM` that returns a canned `ChatResponse.Content`.
- The orchestrator and tools depend **only** on `vcs.Provider`. An offline
  provider that serves a local diff and captures "posted" comments is exactly
  what that interface abstracts. `vcs.FileChange.Patch` holds the per-file
  unified-diff hunk — the same shape the GitHub API supplies — so an offline
  provider just splits a multi-file diff into per-file `FileChange`s.

Rejected alternative (**Approach B**): call `review.Tool{}.Run` directly with a
hand-built `tools.Input`. ~1/3 the code, but it skips the orchestrator /
consolidate / dedup layer, so it demonstrates the *tool*, not the *reviewer* —
weaker on intent #1. We chose the full-pipeline approach.

## Command surface

A new `demo` subcommand in `cmd/cadoo-cli` (add a `case "demo"` to
`main.go`, implement in a new `demo.go`):

```
cadoo demo [--diff PATH] [--llm mock|real] [--out PATH] [--config PATH] [--model NAME]
```

| Flag       | Default                  | Meaning |
|------------|--------------------------|---------|
| `--diff`   | built-in sample fixture  | Unified diff file to review. With no `--diff`, the embedded `testdata/sample.diff` is used, so `cadoo demo` with no args Just Works. |
| `--llm`    | `mock`                   | `mock` = built-in canned-findings provider (no network). `real` = genuine LiteLLM path, reading the same `$LLM_GATEWAY_*` env that `ci` uses. |
| `--out`    | unset                    | If set, also write the consolidated review markdown to this path. Terminal output is always produced. |
| `--config` | unset                    | Optional `.cadoo.yaml` to load; otherwise pipeline defaults apply. |
| `--model`  | config/default           | Model name passed through in `--llm=real` mode. |

Help text in `usage()` is updated to list `demo`.

## Components (3 additions, 0 core changes)

### 1. `internal/vcs/demo` — offline `vcs.Provider`

`demo.Provider` implements the full `vcs.Provider` interface:

- `Kind()` → a demo/synthetic kind (reuse an existing `vcs.Kind`; no new
  provider kind needs to be registered in pools).
- `FetchPullRequest(ctx, repo, number)` → returns a synthetic `*vcs.PullRequest`
  populated from the diff fixture's metadata (title, base/head ref, base/head
  SHA, URL placeholder).
- `ListChangedFiles(ctx, pr)` → parses the unified diff and returns
  `[]vcs.FileChange`, one per file, with `Path`, `Status`, `Patch` (that file's
  hunks), and `Additions`/`Deletions` counts.
- `PostSummaryComment` / `UpdateSummaryComment` / `PostInlineComments` →
  **capture** into an in-memory sink (no I/O). `PostInlineComments` returns
  `PostedInlineRef`s with synthetic `ExternalID`s.
- `ResolveThread` / `EditPullRequestBody` / `UpsertCheckRun` → capture-or-noop.

A `Captured()` accessor exposes the captured summary body, inline comments, and
check runs so the command can render them. The provider holds no network state
and is safe to construct in tests.

**Diff parsing.** A small unified-diff splitter converts a multi-file diff into
per-file `FileChange`s (split on `diff --git` / `+++`/`---` headers; collect
`@@` hunks per file; tally `+`/`-` line counts). If an existing helper in
`internal/tools/diffanchor.go` or elsewhere can be reused, prefer it; otherwise
this is a self-contained ~40-line parser with its own unit test.

### 2. `internal/llm/mockllm` — offline `llm.Provider`

`mockllm.Provider` implements `Chat`. It returns a deterministic
`ChatResponse{Content: <canned review JSON>}` shaped exactly as the `/review`
tool expects (parsed by the existing `internal/tools/llmjson.go`).

- For the **sample fixture**, the canned JSON contains findings tuned to the
  fixture's planted bugs (stable, so the golden test is deterministic).
- For an **arbitrary user `--diff`** in mock mode, it emits one generic,
  clearly-labeled illustrative finding (e.g. severity `info`, title noting this
  is mock output) so the pipeline still runs and renders end-to-end.

The mock never performs network I/O and is safe for concurrent use.

### 3. `cmd/cadoo-cli/demo.go` + `testdata/sample.diff`

- `demo.go` parses flags, loads the diff (embedded default via `go:embed` or
  `--diff` file), constructs the `demo.Provider` and the chosen LLM provider
  (`mockllm` or the real `litellm` client), builds a stateless `Dispatcher`
  with `Posted = nil`, and calls `Dispatcher.Run` for the `/review` tool.
- After the run, it reads the provider's `Captured()` output and renders:
  - **Terminal:** a human-readable list — `file:line  severity — title` per
    inline finding, followed by the summary section.
  - **`--out` markdown:** the consolidated review markdown exactly as captured
    from `PostSummaryComment` (the real wrapper-rendered body).
- `testdata/sample.diff` is a small, self-contained diff with a couple of
  obvious planted issues (the canned mock findings target these). It carries
  enough metadata (or accompanying constants in `demo.go`) to populate the
  synthetic PR.

## Data flow

```
local diff (+ metadata)
  → demo.Provider.ListChangedFiles  (parse → []vcs.FileChange)
  → orchestrator.Dispatcher.Run
       → contextengine.Compressed   (real PR-compression)
       → /review tool               (mock or real llm.Provider)
       → consolidate / wrapper render
       → demo.Provider.PostSummaryComment / PostInlineComments  (capture)
  → demo.Provider.Captured()
  → renderer → terminal + optional --out markdown
```

## Error handling

- Missing or unreadable `--diff` file → clear error, non-zero exit.
- Malformed diff (no parseable file hunks) → explicit error naming the problem.
- `--llm=real` with missing `$LLM_GATEWAY_*` credentials → explicit message:
  "set LLM gateway credentials or use --llm=mock".
- Mock mode never touches the network, so it cannot fail on connectivity.

Exit codes follow the existing CLI convention (non-zero on usage/runtime
errors).

## Testing

All tests run under `make test` with **no external dependencies**:

- **Unit:** diff → `[]vcs.FileChange` split (paths, status, per-file patch,
  add/del counts) in `internal/vcs/demo`.
- **Unit:** `mockllm` returns review JSON that `llmjson.go` parses into the
  expected findings.
- **Golden end-to-end:** `cadoo demo --llm=mock` over `testdata/sample.diff`
  produces a stable consolidated markdown, compared against a checked-in golden
  file. This single test guards the whole pipeline wiring (provider → dispatcher
  → tool → consolidate → capture → render).

## Scope guards (YAGNI)

- Only the `/review` tool is wired (not all 13 built-ins).
- No `FileReader`/`deepreview` capability on the demo provider.
- Check runs are captured but not richly rendered.
- No new `vcs.Kind` registration in pools; the provider is constructed directly
  by the command.

Each of these can be added later without reworking the design.

## Files touched

**New:**
- `cmd/cadoo-cli/demo.go`
- `cmd/cadoo-cli/testdata/sample.diff`
- `cmd/cadoo-cli/demo_test.go` (golden end-to-end)
- `internal/vcs/demo/demo.go`
- `internal/vcs/demo/demo_test.go`
- `internal/llm/mockllm/mockllm.go`
- `internal/llm/mockllm/mockllm_test.go`

**Modified:**
- `cmd/cadoo-cli/main.go` (add `case "demo"`, update `usage()`)

No changes to `internal/orchestrator`, `internal/tools`, `internal/vcs/vcs.go`,
or any provider adapter.
