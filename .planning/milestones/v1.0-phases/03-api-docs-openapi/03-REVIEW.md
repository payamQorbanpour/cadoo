---
phase: 03-api-docs-openapi
reviewed: 2026-06-10T00:00:00Z
depth: standard
files_reviewed: 21
files_reviewed_list:
  - internal/config/config.go
  - internal/releasedocs/defaults/defaults.go
  - internal/releasedocs/dispatcher.go
  - internal/releasedocs/generators/apidocs/apidocs.go
  - internal/releasedocs/generators/apidocs/apidocs_test.go
  - internal/releasedocs/generators/apidocs/assets/redoc.standalone.js
  - internal/releasedocs/generators/apidocs/discover.go
  - internal/releasedocs/generators/apidocs/parse.go
  - internal/releasedocs/generators/apidocs/presets/apidocs.tmpl
  - internal/releasedocs/generators/apidocs/render_html.go
  - internal/releasedocs/generators/apidocs/render_markdown.go
  - internal/releasedocs/generators/apidocs/testdata/golden/markdown_v2.golden
  - internal/releasedocs/generators/apidocs/testdata/golden/markdown_v3.golden
  - internal/releasedocs/generators/apidocs/testdata/invalid.yaml
  - internal/releasedocs/generators/apidocs/testdata/petstore_v2.yaml
  - internal/releasedocs/generators/apidocs/testdata/petstore_v3.yaml
  - internal/releasedocs/generators/apidocs/testdata/petstore_v31.yaml
  - internal/releasedocs/generators/apidocs/testdata/remote_ref.yaml
  - internal/releasedocs/publishers/pages/pages.go
  - internal/releasedocs/publishers/pages/pages_apidocs_test.go
  - internal/releasedocs/releasedocs.go
findings:
  critical: 1
  warning: 3
  info: 3
  total: 7
status: issues_found
---

# Phase 3: Code Review Report

**Reviewed:** 2026-06-10T00:00:00Z
**Depth:** standard
**Files Reviewed:** 21
**Status:** issues_found

## Summary

Reviewed the Phase 3 API-docs generator family: spec discovery, parse/validate,
the offline Redoc HTML renderer, the deterministic Markdown renderer, the pages
publisher path-routing, and the config schema additions. The code is well
structured, well documented, deterministic, and the security guards (SSRF off,
OOM size cap, Markdown/HTML escaping, path-escape prefix guard) are present and
mostly effective. `go vet` is clean and all 138 tests in the three packages pass.

That said, adversarial review surfaced one correctness BLOCKER (the path-escape
prefix guard rejects all output for the common `dir: "docs/"` config, silently
publishing nothing), plus two real output-completeness/robustness defects (PATCH
and other verbs are silently dropped from the Markdown reference; a documented
defense-in-depth safety claim in the HTML renderer is false) and a validation
short-circuit gap. The vendored `redoc.standalone.js` (1.0 MB, webpack/UMD bundle
with the expected license banner and structure) shows no sign of tampering. The
SSRF/`</script>`-breakout vectors I probed are correctly mitigated (remote refs
disabled; `json.Marshal` HTML-escapes `<`/`>`/`&` in keys and string values).

## Critical Issues

### CR-01: Pages path-escape guard rejects all artifacts for trailing-slash / nested / "." dir configs

**File:** `internal/releasedocs/publishers/pages/pages.go:100-109`
**Issue:** The guard builds `expectedPrefix := dir + "/"` and checks
`strings.HasPrefix(p, expectedPrefix)` against the `path.Join`-cleaned result.
Because `path.Join` *cleans* `dir` (collapsing trailing slashes and normalizing),
the raw `dir + "/"` prefix no longer matches the cleaned path for several
perfectly valid configs. Verified empirically:

```
dir="docs/"      p="docs/releases/v1.0.0/openapi.yaml"      prefix="docs//"      pass=false
dir="docs/site/" p="docs/site/releases/v1.0.0/openapi.yaml" prefix="docs/site//" pass=false
dir="."          p="releases/v1.0.0/openapi.yaml"           prefix="./"          pass=false
dir="docs"       p="docs/releases/v1.0.0/openapi.yaml"      prefix="docs/"       pass=true
```

A user who sets `publish.pages.dir: "docs/"` (a trailing slash is extremely
common in path config) gets **every artifact skipped** — `Publish` logs a warning
and returns `nil`, so the run "succeeds" while publishing nothing. The feature is
silently broken for valid input. This is incorrect behavior, not a style nit.

**Fix:** Normalize the base with `path.Clean` and compare against the cleaned
base, handling the `.` (current-dir) case:

```go
base := path.Clean(dir)
// path.Join already cleaned p; accept p == base or p under base/.
if base != "." && p != base && !strings.HasPrefix(p, base+"/") {
    slog.Warn("pages: computed path escapes base dir; skipping artifact",
        "path", p, "dir", dir, "toRef", rc.ToRef, "kind", art.Kind)
    continue
}
// (for base == "." the join is already relative-rooted; no escape possible
//  unless p starts with "../", which path.Clean would surface — check that too)
```

A robust alternative is `rel, err := filepath.Rel(base, p); if err != nil || strings.HasPrefix(rel, "..") { skip }`.

## Warnings

### WR-01: PATCH/HEAD/OPTIONS/TRACE operations are silently dropped from the Markdown reference

**File:** `internal/releasedocs/generators/apidocs/parse.go:179-184` (v3) and `264-269` (v2)
**Issue:** Both `v3PathItemToOps` and `v2PathItemToOps` only enumerate
`{GET, PUT, POST, DELETE}`. libopenapi's `v3high.PathItem` (and the Swagger 2.0
model) also expose `Patch`, `Head`, `Options`, and `Trace` operation fields
(confirmed in `pb33f/libopenapi@v0.37.3/datamodel/high/v3/path_item.go:53-60`).
Any endpoint defined *only* with `patch:` (or `options:`/`head:`) is silently
omitted from `api-reference.md` — the operation simply does not appear, with no
warning. PATCH is common in modern REST APIs, so the generated reference is
incomplete and misleading for real specs. The golden fixtures only exercise
GET/PUT/POST/DELETE, so the gap is not caught by tests.

**Fix:** Add the missing verbs to both candidate lists (and a fixture covering
PATCH to the golden tests):

```go
candidates := []namedOp{
    {"GET", item.Get},
    {"PUT", item.Put},
    {"POST", item.Post},
    {"DELETE", item.Delete},
    {"PATCH", item.Patch},
    {"HEAD", item.Head},
    {"OPTIONS", item.Options},
    {"TRACE", item.Trace}, // v3 only; omit for v2
}
```

### WR-02: nodeToJSON depth guard does not bound alias fan-out; "safe to call from any context" claim is false

**File:** `internal/releasedocs/generators/apidocs/render_html.go:62-67, 103-111`
**Issue:** The comment on `maxNodeDepth` asserts the guard "provides independent
defense-in-depth so `buildRedocHTML` is safe to call from any context." That
guarantee does not hold. `nodeToJSON` resolves `AliasNode`s by *recursing into
the alias target and re-emitting it*, so a billion-laughs–style YAML (aliases
each referencing the previous N times) amplifies exponentially in breadth, not
depth. Verified: a 478-byte input with 9 nesting levels expands to **~11.7 GB**
of emitted JSON while staying far under both `maxNodeDepth` (depth ~9) and
`maxSpecSize` (478 B « 5 MiB). The depth guard catches deep chains but never
fan-out.

In the *shipped* `GenerateMulti` path this is mitigated only because `loadSpec`
runs first and libopenapi rejects the payload with "document contains excessive
aliasing" (verified) before `buildRedocHTML` is reached. So today's risk is
contained — but the safety is entirely external to the documented guard, and any
future caller of `buildRedocHTML`/`yamlToJSON` that does not pre-run `loadSpec`
inherits an OOM-DoS. Severity is WARNING because the only current caller is gated.

**Fix:** Bound the emitted output (or node count), not just depth, inside
`yamlToJSON` / `nodeToJSON`, and correct the comment:

```go
const maxJSONOutputBytes = 16 * 1024 * 1024
// in nodeToJSON / scalarToJSON before each write:
if b.Len() > maxJSONOutputBytes {
    return fmt.Errorf("nodeToJSON: output exceeds %d bytes (alias expansion?)", maxJSONOutputBytes)
}
```

### WR-03: Validation short-circuit lets an invalid spec through when the validator returns no error detail

**File:** `internal/releasedocs/generators/apidocs/parse.go:132-135`
**Issue:** `if !valid && len(valErrs) > 0 { return error }`. If the validator
reports `valid == false` but returns an empty `valErrs` slice, the function
proceeds as though the document were valid and emits docs for a spec that failed
validation. The intent (per the docstring and D-10) is to *skip* on any
validation failure; the `len(valErrs) > 0` clause weakens that to "skip only if
we also got a message."

**Fix:** Gate on `!valid` alone and synthesize a message when the slice is empty:

```go
if !valid {
    msg := "document failed OpenAPI schema validation"
    if len(valErrs) > 0 {
        msg = valErrs[0].Message
    }
    return nil, fmt.Errorf("apidocs: spec validation failed: %s", msg)
}
```

## Info

### IN-01: All three apidocs artifacts get an identical commit message

**File:** `internal/releasedocs/publishers/pages/pages.go:111`
**Issue:** `commitMsg := "docs: release " + rc.ToRef + " " + string(art.Kind)`
uses the shared `Kind` ("apidocs"), so `openapi.yaml`, `api-reference.html`, and
`api-reference.md` all commit with the same message. Using `filename` would make
history more legible. Not a correctness issue (upserts are idempotent regardless).
**Fix:** `commitMsg := "docs: release " + rc.ToRef + " " + filename`.

### IN-02: GenerateMulti appends empty-content artifacts on render failure instead of omitting them

**File:** `internal/releasedocs/generators/apidocs/apidocs.go:88-111`
**Issue:** When `buildRedocHTML` or `renderMarkdown` fails, the code logs and sets
the content to `[]byte{}` but still appends the artifact to the returned slice.
Correctness depends on every downstream publisher skipping zero-length content
(the pages publisher does at `pages.go:89`). A future publisher that does not
check could write an empty file.
**Fix:** On render failure, skip appending the artifact (or document the
zero-length-skip contract on the `Artifact` type).

### IN-03: isMissingFile relies on substring matching of "404"/"not found"

**File:** `internal/releasedocs/generators/apidocs/discover.go:33-39` (also `dispatcher.go:189-198`)
**Issue:** 404 detection matches the substrings `"404"` and `"not found"` in the
error text. This is fragile (a non-404 error whose message happens to contain
"not found" would be treated as a missing file, and a 404 phrased differently
would be treated as a hard error). It is explicitly documented and mirrors the
existing orchestrator/template helpers, so this is a pre-existing convention, not
a new defect — noting for awareness.
**Fix:** Prefer a typed/sentinel 404 error from the VCS adapters surfaced through
the `FileFetcher` contract; fall back to the string heuristic only.

---

_Reviewed: 2026-06-10T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
