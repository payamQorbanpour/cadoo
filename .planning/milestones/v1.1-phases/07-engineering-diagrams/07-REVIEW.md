---
phase: 07-engineering-diagrams
reviewed: 2026-06-13T18:53:13Z
depth: standard
files_reviewed: 8
files_reviewed_list:
  - internal/releasedocs/releasedocs.go
  - internal/config/config.go
  - internal/releasedocs/generators/diagrams/diagrams.go
  - internal/releasedocs/generators/diagrams/sniff.go
  - internal/releasedocs/generators/diagrams/render.go
  - internal/releasedocs/generators/diagrams/diagrams_test.go
  - internal/releasedocs/defaults/defaults.go
  - internal/releasedocs/publishers/pages/pages_diagrams_test.go
findings:
  critical: 0
  warning: 4
  info: 3
  total: 7
status: issues_found
---

# Phase 7: Code Review Report

**Reviewed:** 2026-06-13T18:53:13Z
**Depth:** standard
**Files Reviewed:** 8
**Status:** issues_found

## Summary

Reviewed the new `internal/releasedocs/generators/diagrams/` package (diagrams.go, sniff.go, render.go, tests) plus the in-scope diagrams wiring in `releasedocs.go`, `config.go`, and `defaults.go`. The design is clean: deterministic, no LLM, graceful per-source skips, fixed type ordering, and a path-traversal-resistant `diagramName` that uses `path.Base`. The pages publisher's own escape guard (out of scope, but it backstops this generator) means the worst path-traversal outcomes are contained downstream.

No BLOCKER-tier defects were found. The path-traversal threat the design calls out (Pitfall 4 / T-07-03) is genuinely defended: `diagramName` collapses `../../../etc/passwd` to `passwd`, and the pages publisher applies a second prefix guard. However, `diagramName` mishandles several non-malicious inputs that produce degenerate or wrong filenames, the Mermaid sniffer silently drops valid-looking sources with unbalanced frontmatter, and a couple of robustness gaps in `GenerateMulti` warrant fixing before this ships. Several edge cases that the code comments claim to handle are not exercised by any test.

## Warnings

### WR-01: `diagramName` produces degenerate filenames for edge-case paths

**File:** `internal/releasedocs/generators/diagrams/render.go:28-33`

**Issue:** `diagramName` strips directories and a trailing `.mmd`/`.mermaid`, but several realistic config inputs collapse to broken filenames that flow straight into the artifact `Filename` (`diagrams/<type>/<base>.md`):

- `""` (empty path) → `path.Base("")` returns `"."` → artifact `diagrams/sequence/..md`
- `"docs/.mmd"` → base `""` → artifact `diagrams/sequence/.md` (a dotfile, no name)
- `"/"` → base `"/"` → artifact `diagrams/sequence//.md` (double slash)
- `"docs/login.md"` (user points at a `.md` Mermaid file — neither suffix stripped) → artifact `diagrams/sequence/login.md.md` (doubled extension)
- `"a/b/login.MMD"` (uppercase ext) → suffix not stripped → `login.MMD.md`

The first three are emitted with empty/whitespace `Content` only if fetch+sniff somehow pass, but an attacker or a typo'd config produces a malformed page path that the pages publisher will happily commit (its prefix guard passes since the path still starts with `{dir}/`). At minimum these are silent data-quality bugs; a user who lists a real `.md`-extension Mermaid source gets a `.md.md` page name with no warning.

**Fix:** Guard against degenerate bases and strip extensions case-insensitively / more generally:

```go
func diagramName(srcPath string) string {
	base := path.Base(srcPath)
	// path.Base returns "." for "" and "/" for "/" — treat as no name.
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if ext := path.Ext(base); strings.EqualFold(ext, ".mmd") || strings.EqualFold(ext, ".mermaid") {
		base = base[:len(base)-len(ext)]
	}
	return base
}
```

Then in `GenerateMulti`, skip with a warning when `base == ""` instead of emitting `diagrams/<type>/.md`.

### WR-02: Unbalanced frontmatter silently drops a valid Mermaid source

**File:** `internal/releasedocs/generators/diagrams/sniff.go:44-52`

**Issue:** When a source's first significant line is `---` but there is no matching closing `---`, the inner loop consumes the rest of the file, `i` runs off the end, and `firstSignificantToken` returns `""`, so `sniffMermaid` returns false and the diagram is dropped with a "not valid Mermaid" warning. Verified:

```
input:  "---\ntitle: x\nsequenceDiagram\n  A->>B: hi\n"
result: firstSignificantToken => ""  (whole file consumed; diagram dropped)
```

A user with a malformed-but-recoverable header, or any source whose body legitimately begins with a lone `---` divider before the diagram keyword, gets their diagram silently discarded. The skip is logged only as a generic "source not valid Mermaid for type" — misleading, since the source may be perfectly valid Mermaid with a frontmatter typo.

**Fix:** Only consume the frontmatter block when a closing fence is actually found; otherwise fall back to scanning from after the opening fence (or from the top). Track whether the closing `---` was located and, if not, do not skip past content:

```go
if i < len(lines) && strings.TrimSpace(lines[i]) == "---" {
	open := i
	j := i + 1
	for j < len(lines) && strings.TrimSpace(lines[j]) != "---" {
		j++
	}
	if j < len(lines) { // closing fence found
		i = j + 1
	} else { // unbalanced — don't swallow the body
		i = open + 1
	}
}
```

### WR-03: No length/empty-content guard before sniffing or emitting

**File:** `internal/releasedocs/generators/diagrams/diagrams.go:88-112`

**Issue:** A fetched source that is empty or whitespace-only passes through `sniffMermaid` (which returns false on `first == ""`, so empty is skipped) — that part is fine. But there is no defense against an oversized or binary blob: `FetchFileFromRef` is trusted to return a sane Mermaid file, and the entire content is `strings.Split` line-by-line in `firstSignificantToken` and then copied verbatim into the artifact. A misconfigured path pointing at a large binary asset (e.g. a committed image that happens to start with bytes matching a keyword prefix is unlikely, but a multi-MB text file is plausible) is wrapped and published unvalidated. While not a correctness bug per se, the generator should bound what it treats as a "diagram source."

**Fix:** Add a size sanity check (e.g. skip+warn if `len(b)` exceeds a few hundred KB) before sniffing, and document the bound. This keeps the deterministic contract while preventing accidental publication of non-diagram blobs.

### WR-04: Claimed edge cases are undocumented-by-test (regression risk)

**File:** `internal/releasedocs/generators/diagrams/diagrams_test.go:124-307`

**Issue:** The package comments and method docs assert several behaviors that no test exercises, so a future refactor can silently break them:

- **Duplicate-basename-within-type skip** (diagrams.go:101-106, "Pitfall 3, first-listed wins"): no test configures two same-type paths resolving to the same base (e.g. `a/login.mmd` + `b/login.mmd` both under `Sequence`). The `seen` map and its determinism are untested.
- **Cross-type sniff rejection** (sniff.go: a `classDiagram` source listed under `Sequence` should be skipped): `TestDiagrams_Skip` only covers a `.txt` prose file, not a valid-Mermaid-but-wrong-type source. The `mermaidKeywords` per-type mapping — the core of the sniffer — has no negative-type test.
- **Unclosed-frontmatter / lone-`---`** behavior (WR-02): untested, which is how the drop slipped through.

**Fix:** Add table-driven cases to `diagrams_test.go`:
1. Two `Sequence` entries with the same basename → exactly one artifact, first wins.
2. A `classDiagram` source listed under `Sequence` → skipped (and the same source under `Class` → emitted).
3. A source with unbalanced frontmatter (after WR-02 fix) → emitted.

## Info

### IN-01: `diagramTypes` rebuilt as a package-level slice on every reference is fine, but the `seen` key format is implicit

**File:** `internal/releasedocs/generators/diagrams/diagrams.go:84,100`

**Issue:** The dedup key `t.name + "/" + base` is constructed inline and the artifact `Filename` is constructed separately as `"diagrams/" + t.name + "/" + base + ".md"`. The two share the `t.name + "/" + base` substring but are built independently, so a future edit to one path-construction site can desync them (dedup keyed on one shape, filenames on another).

**Fix:** Compute `rel := t.name + "/" + base` once and derive both the `seen` key and `Filename = "diagrams/" + rel + ".md"` from it.

### IN-02: `Generate` returns a non-empty Kind for an "unreachable" stub

**File:** `internal/releasedocs/generators/diagrams/diagrams.go:41-46`

**Issue:** The required-by-interface `Generate` stub returns `Artifact{Kind: KindDiagrams}` with empty `Content`. If a future dispatcher path ever calls `Generate` (e.g. a code path that does not type-assert `MultiGenerator`), it returns a zero-content artifact with a real Kind rather than an obviously-empty sentinel, which the pages publisher's `len(art.Content) == 0` guard would skip — benign today, but it would mask the misuse silently. The doc comment says "unreachable," yet the value returned is not inert.

**Fix:** Return a bare `releasedocs.Artifact{}` (zero Kind too), or have `Generate` log a `slog.Warn` that it was called unexpectedly, so accidental use of the single-artifact path is observable rather than silent.

### IN-03: `wrapMermaidFence` does not normalize interior CRLF or leading blank lines

**File:** `internal/releasedocs/generators/diagrams/render.go:15-22`

**Issue:** `wrapMermaidFence` only trims trailing newlines (`bytes.TrimRight(src, "\n")`). A source committed with CRLF line endings, or with leading blank lines, is wrapped verbatim. Because the artifact is golden-file tested for byte stability, a repo that commits CRLF Mermaid will produce a different byte output than the LF golden — not a crash, but it undermines the "byte-identical across re-runs" guarantee the doc comment claims when the same logical source is checked out with different EOL settings (e.g. Windows `core.autocrlf`).

**Fix:** Normalize line endings before wrapping, e.g. `body := bytes.TrimRight(bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n")), "\n")`, and optionally trim leading blank lines, so output is EOL-independent.

---

_Reviewed: 2026-06-13T18:53:13Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
