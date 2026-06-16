# AI Prompt Block — Design Spec

**Date:** 2026-06-16  
**Status:** Approved

## Problem

Cadoo inline review comments contain actionable findings, but getting an AI coding tool (Claude, Cursor) to act on one requires the user to manually compose a prompt from the comment text. There is no ready-made, copy-pasteable version.

## Goal

Append a collapsed "Prompt for AI Agents" block to every inline review comment. The user clicks to expand, copies the fenced block, and pastes it directly into Claude or Cursor with no further editing.

## Non-goals

- Including the diff hunk or file contents in the prompt (the finding body is self-contained; Cursor has workspace context).
- A configurable toggle per repo — always on.
- Changing the fingerprint or dedup logic.
- Changing the `vcs.InlineComment` struct or the tool interface.

---

## Data Flow

Current inline comment lifecycle in `postInline()` (`internal/orchestrator/reviewer.go`):

1. Tool produces pristine `vcs.InlineComment` — Body already contains `**Title**\n\nDescription...`
2. `findings.StampInline()` appends `<!-- cadoo:fp … -->` → wire copy
3. Wire copy is posted to VCS

The AI prompt block is appended to `wire.Body` **after step 2**, before posting. Fingerprint and structural-key computation happen in step 2 on the pristine body and are unaffected. No changes to `vcs.InlineComment`, `tools.Tool`, or any of the 13 registered tools.

---

## Prompt Format

```markdown
<details>
<summary><img src="{iconURL}" width="16" height="16" alt="AI"/> Prompt for AI Agents</summary>

```
Fix the following code review finding.

**File:** `{c.File}` lines {c.LineStart}–{c.LineEnd}
**Severity:** {c.Severity}

{c.Body}
```

</details>
```

Rules:
- No `open` attribute — collapsed by default.
- The fenced block uses no language tag (content is prose, not code).
- `c.Body` is included verbatim; each tool already prepends the bold title, so no duplication is needed.
- Severity is re-stated explicitly so the AI has it without parsing the bold prefix.

---

## Icon URL

`docs/assets/AI.png` is served as a raw-content URL constructed per VCS host:

| Kind | Pattern |
|------|---------|
| `KindGitHub` | `https://raw.githubusercontent.com/{repo}/main/docs/assets/AI.png` |
| `KindGitHubEnterprise` | `https://{host}/{repo}/raw/main/docs/assets/AI.png` |
| `KindGitLab` | `https://{host}/{repo}/-/raw/main/docs/assets/AI.png` |

`ref` is always `"main"`. The path `docs/assets/AI.png` is a constant.

---

## New Code

### `internal/vcs/rawurl.go` (new file)

```go
// RawContentURL returns the provider-specific URL for fetching a raw file
// from a repository at a given ref.
func RawContentURL(kind Kind, baseURL, repoFullName, ref, path string) string
```

`baseURL` is the scheme+host of the VCS instance (e.g. `"https://github.com"`).
For `KindGitHub`, `baseURL` is ignored and `raw.githubusercontent.com` is used directly.

### `internal/orchestrator/aiprompt.go` (new file)

```go
// buildAIPromptBlock returns the collapsed <details> block appended to
// every inline comment wire copy. iconURL may be empty, in which case
// the img tag is omitted.
func buildAIPromptBlock(c vcs.InlineComment, iconURL string) string
```

### `postInline()` change (`internal/orchestrator/reviewer.go`)

After `StampInline` produces the wire copy, derive `iconURL` from the provider
kind + base URL + `pr.RepoFullName`, then:

```go
wire.Body += "\n\n" + buildAIPromptBlock(pristine, iconURL)
```

The base URL is obtained via a type assertion on the concrete adapter stored in
`d.VCSPool[pr.Provider]`. Both `github.Adapter` and `gitlab.Adapter` expose a
`BaseURL() string` method (to be added).

---

## BaseURL on VCS Adapters

Add `BaseURL() string` to each concrete adapter (not to the `vcs.Provider`
interface — avoids bloating the interface for a single use case):

- `internal/vcs/github/github.go` — returns `cfg.BaseURL` (already stored; `"https://github.com"` for GitHub.com)
- `internal/vcs/gitlab/gitlab.go` — returns `cfg.BaseURL`

`postInline()` type-asserts the concrete type to call `BaseURL()`. If the
assertion fails (unknown adapter in tests), `iconURL` falls back to `""` and
the `<img>` tag is omitted — the summary still reads "Prompt for AI Agents".

---

## Testing

| Test | Location | What it checks |
|------|----------|----------------|
| `TestBuildAIPromptBlock` | `internal/orchestrator/aiprompt_test.go` | Output contains `<details>`, `<summary>`, file/line/severity, body verbatim, fenced block, no `open` attribute |
| `TestBuildAIPromptBlock_EmptyIcon` | same | `<img>` tag absent when `iconURL == ""` |
| `TestRawContentURL` | `internal/vcs/rawurl_test.go` | Correct URL per Kind for GitHub, GHES, GitLab |
| Existing `postInline` integration tests | `internal/orchestrator/reviewer_test.go` | Wire body contains `<details>` block; fingerprint still computed from pristine body |

---

## Files Changed

| File | Change |
|------|--------|
| `internal/vcs/rawurl.go` | New — `RawContentURL` helper |
| `internal/vcs/rawurl_test.go` | New — unit tests |
| `internal/orchestrator/aiprompt.go` | New — `buildAIPromptBlock` |
| `internal/orchestrator/aiprompt_test.go` | New — unit tests |
| `internal/orchestrator/reviewer.go` | Append AI block to wire body in `postInline()` |
| `internal/vcs/github/github.go` | Add `BaseURL() string` method |
| `internal/vcs/gitlab/gitlab.go` | Add `BaseURL() string` method |
