# AI Prompt Block Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Append a collapsed "Prompt for AI Agents" `<details>` block to every inline review comment so users can copy a ready-made prompt directly into Claude or Cursor.

**Architecture:** The block is appended to the wire-copy body in `postInline()` after `StampInline()`, so fingerprint/dedup keys are computed from the pristine body and are unaffected. A prerequisite regex relaxation in `marker.go` allows content after the `<!-- cadoo:fp -->` marker without breaking `ParseInlineMarker`. The icon URL is constructed at post time from the VCS adapter's host URL.

**Tech Stack:** Go standard library only; touches `internal/vcs`, `internal/vcs/github`, `internal/vcs/gitlab`, `internal/orchestrator`.

---

## File Map

| File | Change |
|------|--------|
| `internal/vcs/marker.go` | Remove `\s*$` anchor from `inlineMarkerRe` |
| `internal/vcs/marker_test.go` | Add test for marker with trailing content |
| `internal/vcs/rawurl.go` | New — `RawContentURL` helper |
| `internal/vcs/rawurl_test.go` | New — table-driven URL tests |
| `internal/vcs/github/github.go` | Add `BaseURL() string` method |
| `internal/vcs/gitlab/gitlab.go` | Add `BaseURL() string` method |
| `internal/orchestrator/aiprompt.go` | New — `buildAIPromptBlock`, `aiIconURL` |
| `internal/orchestrator/aiprompt_test.go` | New — unit tests |
| `internal/orchestrator/reviewer.go` | Append AI block to wire body in `postInline()` |

---

## Task 1: Relax the inline-marker regex

The current `inlineMarkerRe` has `\s*$` which requires the marker to be at the very end of the body. After this feature, the `<details>` block follows the marker. Removing `\s*$` makes the regex match the marker wherever it appears; `ParseInlineMarker`'s `stripped` computation (`body[:loc[0]]`) is unaffected because it only looks at what comes before the marker.

**Files:**
- Modify: `internal/vcs/marker.go:66-67`
- Modify: `internal/vcs/marker_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/vcs/marker_test.go` (inside `package vcs`):

```go
func TestParseInlineMarkerWithTrailingContent(t *testing.T) {
	body := "Fix the leak."
	marker := InlineMarker(MarkerData{Tool: "review", SK: "abc123", Sev: "warn"})
	// Simulate an AI prompt block appended after the fp marker.
	full := body + "\n\n" + marker + "\n\n<details><summary>Prompt for AI Agents</summary>\n\ncontent\n\n</details>"

	got, stripped, ok := ParseInlineMarker(full)
	if !ok {
		t.Fatalf("ParseInlineMarker ok=false; want true")
	}
	if got.SK != "abc123" {
		t.Errorf("SK = %q; want abc123", got.SK)
	}
	if stripped != body {
		t.Errorf("stripped = %q; want %q", stripped, body)
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```
go test ./internal/vcs/... -run TestParseInlineMarkerWithTrailingContent -v
```

Expected: `FAIL` — `ok=false; want true` because the current regex requires `\s*$`.

- [ ] **Step 3: Remove the end-of-string anchor**

In `internal/vcs/marker.go`, change line 66-67:

```go
// Before:
var inlineMarkerRe = regexp.MustCompile(
	`\n*<!-- cadoo:fp v=1 tool=(\S+) sk=(\S+) sev=(\S*)(?:\s+nt=(\S+))? -->\s*$`)

// After:
var inlineMarkerRe = regexp.MustCompile(
	`\n*<!-- cadoo:fp v=1 tool=(\S+) sk=(\S+) sev=(\S*)(?:\s+nt=(\S+))? -->`)
```

- [ ] **Step 4: Run all marker tests**

```
go test ./internal/vcs/... -v
```

Expected: all pass, including the new `TestParseInlineMarkerWithTrailingContent`.

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/marker.go internal/vcs/marker_test.go
git commit -m "fix(vcs): allow content after inline fp marker in ParseInlineMarker"
```

---

## Task 2: Add `RawContentURL` helper

**Files:**
- Create: `internal/vcs/rawurl.go`
- Create: `internal/vcs/rawurl_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/vcs/rawurl_test.go`:

```go
package vcs

import "testing"

func TestRawContentURL(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		base string
		repo string
		ref  string
		path string
		want string
	}{
		{
			name: "github.com",
			kind: KindGitHub,
			base: "https://github.com",
			repo: "owner/repo",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://raw.githubusercontent.com/owner/repo/main/docs/assets/AI.png",
		},
		{
			name: "ghes",
			kind: KindGitHubEnterprise,
			base: "https://ghe.example.com",
			repo: "owner/repo",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://ghe.example.com/owner/repo/raw/main/docs/assets/AI.png",
		},
		{
			name: "gitlab.com",
			kind: KindGitLab,
			base: "https://gitlab.com",
			repo: "group/project",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://gitlab.com/group/project/-/raw/main/docs/assets/AI.png",
		},
		{
			name: "gitlab self-managed",
			kind: KindGitLab,
			base: "https://gitlab.example.com",
			repo: "group/subgroup/project",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "https://gitlab.example.com/group/subgroup/project/-/raw/main/docs/assets/AI.png",
		},
		{
			name: "unknown kind returns empty",
			kind: Kind("unknown"),
			base: "https://example.com",
			repo: "owner/repo",
			ref:  "main",
			path: "docs/assets/AI.png",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RawContentURL(tc.kind, tc.base, tc.repo, tc.ref, tc.path)
			if got != tc.want {
				t.Errorf("RawContentURL = %q; want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

```
go test ./internal/vcs/... -run TestRawContentURL -v
```

Expected: compile error — `RawContentURL undefined`.

- [ ] **Step 3: Implement `RawContentURL`**

Create `internal/vcs/rawurl.go`:

```go
package vcs

import "fmt"

// RawContentURL returns the provider-specific URL for fetching a raw file
// from a repository at a given ref. For KindGitHub, baseURL is ignored.
// For KindGitHubEnterprise and KindGitLab, baseURL is the scheme+host of the
// instance with no trailing slash (e.g. "https://ghe.example.com").
// Returns "" for unrecognised kinds.
func RawContentURL(kind Kind, baseURL, repoFullName, ref, path string) string {
	switch kind {
	case KindGitHub:
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repoFullName, ref, path)
	case KindGitHubEnterprise:
		return fmt.Sprintf("%s/%s/raw/%s/%s", baseURL, repoFullName, ref, path)
	case KindGitLab:
		return fmt.Sprintf("%s/%s/-/raw/%s/%s", baseURL, repoFullName, ref, path)
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests**

```
go test ./internal/vcs/... -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/rawurl.go internal/vcs/rawurl_test.go
git commit -m "feat(vcs): add RawContentURL helper for per-provider raw file URLs"
```

---

## Task 3: Add `BaseURL()` to VCS adapters

The orchestrator needs each adapter's scheme+host to build the icon URL. We add `BaseURL() string` to the two concrete adapters without touching the `vcs.Provider` interface.

**Files:**
- Modify: `internal/vcs/github/github.go`
- Modify: `internal/vcs/gitlab/gitlab.go`

- [ ] **Step 1: Add `BaseURL()` to the GitHub adapter**

In `internal/vcs/github/github.go`, add after the existing `Kind()` method (around line 94):

```go
// BaseURL returns the scheme+host of this GitHub instance.
// For GitHub.com it returns "https://github.com".
// For GHES, cfg.BaseURL is the API URL (e.g. "https://ghe.example.com/api/v3");
// this method strips the "/api/v3" suffix to return the raw-content host.
func (a *Adapter) BaseURL() string {
	if a.cfg.BaseURL == "" {
		return "https://github.com"
	}
	return strings.TrimSuffix(strings.TrimRight(a.cfg.BaseURL, "/"), "/api/v3")
}
```

(`strings` is already imported in the file.)

- [ ] **Step 2: Add `BaseURL()` to the GitLab adapter**

In `internal/vcs/gitlab/gitlab.go`, add after the existing `Kind()` method (around line 45):

```go
// BaseURL returns the scheme+host of this GitLab instance.
// For GitLab.com it returns "https://gitlab.com"; for self-managed it
// returns cfg.BaseURL as-is (already the scheme+host, no API suffix).
func (a *Adapter) BaseURL() string {
	if a.cfg.BaseURL == "" {
		return "https://gitlab.com"
	}
	return a.cfg.BaseURL
}
```

(`strings` is already imported in the file.)

- [ ] **Step 3: Build to verify no compile errors**

```
go build ./internal/vcs/...
```

Expected: success.

- [ ] **Step 4: Commit**

```bash
git add internal/vcs/github/github.go internal/vcs/gitlab/gitlab.go
git commit -m "feat(vcs): add BaseURL() method to GitHub and GitLab adapters"
```

---

## Task 4: Implement `buildAIPromptBlock` and `aiIconURL`

**Files:**
- Create: `internal/orchestrator/aiprompt.go`
- Create: `internal/orchestrator/aiprompt_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/orchestrator/aiprompt_test.go`:

```go
package orchestrator

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestBuildAIPromptBlockContainsRequiredParts(t *testing.T) {
	c := vcs.InlineComment{
		File:      "internal/auth/middleware.go",
		LineStart: 42,
		LineEnd:   45,
		Severity:  vcs.SeverityWarn,
		Body:      "**Missing capability check**\n\nThe handler lacks an admin scope guard.",
	}
	iconURL := "https://raw.githubusercontent.com/org/repo/main/docs/assets/AI.png"
	block := buildAIPromptBlock(c, iconURL)

	must := []struct {
		label string
		want  string
	}{
		{"opening details tag", "<details>"},
		{"opening summary tag", "<summary>"},
		{"img tag with src", `<img src="` + iconURL + `"`},
		{"prompt label", "Prompt for AI Agents"},
		{"closing summary tag", "</summary>"},
		{"file path in backticks", "`internal/auth/middleware.go`"},
		{"line range with en-dash", "lines 42–45"},
		{"severity label", "warn"},
		{"body title verbatim", "**Missing capability check**"},
		{"opening code fence", "```"},
		{"closing details tag", "</details>"},
	}
	for _, m := range must {
		if !strings.Contains(block, m.want) {
			t.Errorf("missing %s: want %q in output:\n%s", m.label, m.want, block)
		}
	}
	if strings.Contains(block, "<details open") {
		t.Error("block must be collapsed by default (no 'open' attribute on <details>)")
	}
}

func TestBuildAIPromptBlockSingleLine(t *testing.T) {
	c := vcs.InlineComment{
		File:      "main.go",
		LineStart: 10,
		LineEnd:   10,
		Severity:  vcs.SeverityNit,
		Body:      "Rename for clarity.",
	}
	block := buildAIPromptBlock(c, "")
	if !strings.Contains(block, "line 10") {
		t.Errorf("single-line comment should use singular 'line 10', got:\n%s", block)
	}
	if strings.Contains(block, "lines 10") {
		t.Errorf("single-line comment must not say 'lines 10–10', got:\n%s", block)
	}
}

func TestBuildAIPromptBlockEmptyIconOmitsImgTag(t *testing.T) {
	c := vcs.InlineComment{File: "x.go", LineStart: 1, LineEnd: 1, Severity: vcs.SeverityWarn, Body: "issue"}
	block := buildAIPromptBlock(c, "")
	if strings.Contains(block, "<img") {
		t.Errorf("empty iconURL: <img> tag must be absent, got:\n%s", block)
	}
	if !strings.Contains(block, "Prompt for AI Agents") {
		t.Error("summary label 'Prompt for AI Agents' must still be present without icon")
	}
}
```

- [ ] **Step 2: Run the tests to confirm they fail**

```
go test ./internal/orchestrator/... -run "TestBuildAIPromptBlock" -v
```

Expected: compile error — `buildAIPromptBlock undefined`.

- [ ] **Step 3: Implement `aiprompt.go`**

Create `internal/orchestrator/aiprompt.go`:

```go
package orchestrator

import (
	"fmt"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// aiIconPath is the repo-relative path of the AI agent icon served as a raw
// URL inside the <details> summary line.
const aiIconPath = "docs/assets/AI.png"

// baseURLer is satisfied by VCS adapters that expose their instance host URL.
// Kept unexported to avoid polluting the vcs.Provider interface.
type baseURLer interface {
	BaseURL() string
}

// aiIconURL constructs the raw-content URL for the AI icon from the provider
// kind and the adapter's host URL. Returns "" when the adapter does not
// implement baseURLer (e.g. test stubs), causing buildAIPromptBlock to omit
// the <img> tag gracefully.
func aiIconURL(provider vcs.Provider, pr *vcs.PullRequest) string {
	bu, ok := provider.(baseURLer)
	if !ok {
		return ""
	}
	return vcs.RawContentURL(pr.Provider, bu.BaseURL(), pr.RepoFullName, "main", aiIconPath)
}

// buildAIPromptBlock returns the collapsed <details> block appended to every
// inline comment wire copy. The prompt is fenced in a code block so GitHub
// and GitLab render a native copy button. iconURL may be empty; when it is,
// the <img> tag is omitted from the summary line.
func buildAIPromptBlock(c vcs.InlineComment, iconURL string) string {
	summaryLabel := "Prompt for AI Agents"
	if iconURL != "" {
		summaryLabel = fmt.Sprintf(
			`<img src="%s" width="16" height="16" alt="AI"/> %s`,
			iconURL, summaryLabel,
		)
	}

	lineRef := fmt.Sprintf("lines %d–%d", c.LineStart, c.LineEnd)
	if c.LineStart == c.LineEnd {
		lineRef = fmt.Sprintf("line %d", c.LineStart)
	}

	prompt := fmt.Sprintf(
		"Fix the following code review finding.\n\n**File:** `%s` %s\n**Severity:** %s\n\n%s",
		c.File, lineRef, string(c.Severity), c.Body,
	)

	return fmt.Sprintf(
		"<details>\n<summary>%s</summary>\n\n```\n%s\n```\n\n</details>",
		summaryLabel, prompt,
	)
}
```

- [ ] **Step 4: Run the tests**

```
go test ./internal/orchestrator/... -run "TestBuildAIPromptBlock" -v
```

Expected: all three tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/aiprompt.go internal/orchestrator/aiprompt_test.go
git commit -m "feat(orchestrator): add buildAIPromptBlock and aiIconURL"
```

---

## Task 5: Wire `buildAIPromptBlock` into `postInline()`

**Files:**
- Modify: `internal/orchestrator/reviewer.go:481-488`

- [ ] **Step 1: Locate the wire-copy loop**

Open `internal/orchestrator/reviewer.go`. Find the block starting around line 481:

```go
wire := make([]vcs.InlineComment, len(delta))
for i, c := range delta {
    wc := c
    if tool != "" {
        wc.Body = findings.StampInline(tool, c)
    }
    wire[i] = wc
}
```

- [ ] **Step 2: Append the AI block to the wire body**

Replace that block with:

```go
wire := make([]vcs.InlineComment, len(delta))
iconURL := aiIconURL(provider, pr)
for i, c := range delta {
    wc := c
    if tool != "" {
        wc.Body = findings.StampInline(tool, c) + "\n\n" + buildAIPromptBlock(c, iconURL)
    }
    wire[i] = wc
}
```

`aiIconURL` is called once per `postInline` call (not once per comment) because all comments in a batch share the same provider and PR.

- [ ] **Step 3: Run the full orchestrator test suite**

```
go test ./internal/orchestrator/... -v
```

Expected: all existing tests pass. Key ones to watch:
- `TestPostInlineStampsWireBodyButRecordsPristine` — wire body now contains the marker followed by the `<details>` block; `ParseInlineMarker` (relaxed in Task 1) still finds the marker and returns `ok=true`.
- `TestPostInlineKeepsDistinctSuggestionsInBatch` — two distinct comments each get their own AI block; dedup still collapses identical structural keys.
- `TestPostInlineCollapsesIdenticalDuplicatesInBatch` — duplicate still collapsed to one; the AI block doesn't affect StructuralKey comparison.

- [ ] **Step 4: Run the full test suite**

```
make test
```

Expected: green. If any test fails, read the error — do not patch blindly.

- [ ] **Step 5: Run lint**

```
make lint
```

Expected: no new issues.

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/reviewer.go
git commit -m "feat(orchestrator): append AI prompt block to inline comment wire copies"
```

---

## Task 6: Final verification

- [ ] **Step 1: Full CI check**

```
make ci
```

Expected: vet + test + build all green.

- [ ] **Step 2: Spot-check the rendered output**

Run the aiprompt tests in verbose mode and read the failure message (if any) to eyeball the rendered block structure:

```
go test ./internal/orchestrator/... -run "TestBuildAIPromptBlock" -v
```

Confirm the output contains `<details>`, the en-dash line reference, the fenced block delimiters, and `</details>`.

- [ ] **Step 3: Verify linter on all changed files**

```
golangci-lint run internal/vcs/marker.go internal/vcs/rawurl.go internal/vcs/github/github.go internal/vcs/gitlab/gitlab.go internal/orchestrator/aiprompt.go internal/orchestrator/reviewer.go
```

Expected: no issues.
