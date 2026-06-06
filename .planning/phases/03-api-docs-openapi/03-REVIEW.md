---
phase: 03-api-docs-openapi
reviewed: 2026-06-06T09:05:00Z
depth: standard
files_reviewed: 11
files_reviewed_list:
  - internal/config/config.go
  - internal/releasedocs/defaults/defaults.go
  - internal/releasedocs/dispatcher.go
  - internal/releasedocs/generators/apidocs/apidocs.go
  - internal/releasedocs/generators/apidocs/discover.go
  - internal/releasedocs/generators/apidocs/parse.go
  - internal/releasedocs/generators/apidocs/render_html.go
  - internal/releasedocs/generators/apidocs/render_markdown.go
  - internal/releasedocs/generators/apidocs/presets/apidocs.tmpl
  - internal/releasedocs/publishers/pages/pages.go
  - internal/releasedocs/releasedocs.go
findings:
  critical: 1
  warning: 5
  info: 4
  total: 10
status: issues_found
---

# Phase 3: Code Review Report

**Reviewed:** 2026-06-06T09:05:00Z
**Depth:** standard
**Files Reviewed:** 11
**Status:** issues_found

## Summary

The apidocs OpenAPI→docs generator was reviewed with focus on the four security
guards this phase introduces. Empirical findings (verified by running probe
tests inside the package against `pb33f/libopenapi@v0.37.3` and `gopkg.in/yaml.v3`):

- **SSRF guard (parse.go): correct.** `DocumentConfiguration{AllowRemoteReferences:false, AllowFileReferences:false}` is set before parsing; the `remote_ref.yaml` test confirms refs are not resolved.
- **OOM guard (parse.go): correct in code, but UNTESTED.** The `len(specBytes) > maxSpecSize` check runs first. A 5.25 MB `oversized.yaml` fixture exists in testdata but no test references it (see WR-04).
- **HTML `<script>`-breakout / XSS: correctly mitigated.** Go's `json.Marshal` HTML-escapes `<`, `>`, `&` to `<` etc. by default, so a spec string field containing `</script>` cannot break out of the inlined `<script>` block. Verified directly.
- **Markdown injection: NOT mitigated as claimed (WR-02).** `escapeField` uses `html/template.HTMLEscapeString`, which does not escape Markdown control characters (`|`, newlines, `#`). The doc comments claim it prevents "Markdown-control-character injection" — it does not.
- **Determinism: one correctness bug breaks it (CR-01).** YAML numeric scalars such as `0xAF`, `.inf`, `.nan`, `0o17`, `0b101`, `1_000` are emitted raw into the inlined JSON, producing **invalid JSON** that breaks `Redoc.init()`. Reachable from a valid OAS 3.0 spec.
- **Path-traversal guard (pages.go): correct.** The `strings.HasPrefix(p, dir+"/")` guard rejects `../` and absolute-style escapes after `path.Join` cleaning; verified against `/etc/passwd`, `../../`, and sibling-dir bypass inputs.

One BLOCKER (invalid-JSON / broken HTML artifact), five warnings, four info items.

## Critical Issues

### CR-01: YAML numeric scalars emitted as raw, invalid JSON breaks the Redoc HTML artifact

**File:** `internal/releasedocs/generators/apidocs/render_html.go:186-188`
**Issue:**
`scalarToJSON` writes `n.Value` verbatim for `!!int` and `!!float` tags:

```go
case "!!int", "!!float":
    // Emit the raw numeric value; yaml.v3 has already validated it.
    b.WriteString(n.Value)
```

The comment "yaml.v3 has already validated it" conflates *valid YAML* with *valid
JSON*. YAML and JSON number grammars differ. yaml.v3 tags the following as
`!!int`/`!!float`, but their `Value` strings are **not legal JSON literals**:

| YAML input | yaml.v3 tag | raw `Value` written | valid JSON? |
|------------|-------------|---------------------|-------------|
| `0xAF`     | `!!int`     | `0xAF`              | no (hex)    |
| `0o17`     | `!!int`     | `0o17`              | no (octal)  |
| `0b101`    | `!!int`     | `0b101`             | no (binary) |
| `1_000`    | `!!int`     | `1_000`             | no (underscore) |
| `.inf`     | `!!float`   | `.inf`              | no          |
| `-.inf`    | `!!float`   | `-.inf`             | no          |
| `.nan`     | `!!float`   | `.nan`              | no          |

This is **reachable from a valid spec**: a probe test fed a schema-valid OAS 3.0
document containing `x-rate-limit: 0xAF`. `loadSpec` accepted it (libopenapi parse
+ validator both passed), and the emitted `api-reference.html` contained
`"x-rate-limit":0xAF`. `json.Unmarshal` of the inlined object returned
`invalid character 'x' after object key:value pair`. In a browser, `Redoc.init()`
throws on parse and the API reference renders blank — a silent, shipped-broken
artifact. It also breaks the D-05 "valid deterministic output" guarantee.

**Fix:** Re-encode every numeric scalar through `json.Marshal` (or `json.Number`
round-trip) so non-JSON YAML number forms are normalized or rejected, instead of
passing `n.Value` through raw. Example:

```go
case "!!int", "!!float":
    // YAML number grammar is a superset of JSON's (hex 0xAF, .inf, .nan,
    // underscores, octal/binary). Re-encode through encoding/json so the
    // output is always valid JSON.
    var num float64
    if err := n.Decode(&num); err != nil {
        // Not representable as a JSON number → emit as a JSON string so the
        // page still parses, or return an error to skip the HTML artifact.
        enc, _ := json.Marshal(n.Value)
        b.Write(enc)
        return nil
    }
    enc, err := json.Marshal(json.Number(strconv.FormatFloat(num, 'g', -1, 64)))
    if err != nil {
        return fmt.Errorf("scalarToJSON: numeric %q: %w", n.Value, err)
    }
    b.Write(enc)
```

(Prefer decoding to `json.Number` via `n.Decode` to preserve integer precision;
the snippet above is illustrative. The key requirement: never write `n.Value`
unescaped for a numeric scalar.) Add a regression fixture covering `0xAF`,
`.inf`, `.nan`, and `1_000`.

## Warnings

### WR-01: `yamlToJSON`/`nodeToJSON` has no cycle or recursion-depth guard (latent stack-overflow crash)

**File:** `internal/releasedocs/generators/apidocs/render_html.go:91-95`
**Issue:**
`nodeToJSON` recurses through the yaml.Node tree and follows `AliasNode.Alias`
with no visited-set or depth cap. A self-referential anchor (`&a {x: *a}`) drives
unbounded recursion. A direct probe call to `yamlToJSON` on
`a: &anchor {self: *anchor}` produced `fatal error: stack overflow` (an
unrecoverable runtime crash, not a panic — it cannot be `recover()`'d).

This is currently **not reachable through `GenerateMulti`**, because `loadSpec`
runs first and libopenapi rejects recursive anchors ("anchor 'a' value contains
itself") and alias bombs ("document contains excessive aliasing") before
`buildRedocHTML` is ever called. So today it is a latent defense-in-depth gap,
not an active BLOCKER. But the safety depends entirely on libopenapi's gate
running first and detecting the *same* cycle forms yaml.v3 expands — a fragile
coupling. If call order is ever reordered, or `buildRedocHTML` is called from a
new path, or libopenapi's anchor handling diverges, this becomes an
unrecoverable crash on untrusted committed input.

**Fix:** Give `nodeToJSON` its own bound, independent of libopenapi: pass a depth
counter and return an error past a sane limit (e.g. 1000), and/or track visited
`*yaml.Node` pointers to detect alias cycles. Do not rely on an upstream parser
for memory safety of this function.

### WR-02: Markdown renderer does not escape Markdown control characters despite claiming to

**File:** `internal/releasedocs/generators/apidocs/render_markdown.go:66-72`
**Issue:**
`escapeField` calls `html/template.HTMLEscapeString`, which escapes only
`< > & ' "`. The function and type doc comments repeatedly claim this prevents
"Markdown-control-character injection (T-03-07)". It does not. A probe confirmed
`escapeField("evil | injected\n## Fake Heading")` returns the string unchanged
(no escaping of `|`, `\n`, or `#`).

Consequences for spec-derived Summary/Description/param Name/In fields, which are
rendered into a Markdown table (`presets/apidocs.tmpl:12-16`) and into body text:
- A `|` in any field corrupts the `| Parameter | In |` table layout.
- An embedded newline plus `## ` injects fake headings / arbitrary Markdown
  structure into the rendered reference (content-spoofing, not code-exec — it is
  Markdown, not HTML — hence WARNING not BLOCKER).

**Fix:** Either (a) escape Markdown-significant characters in `escapeField`
(backslash-escape `|`, `` ` ``, `*`, `_`, `#`, `[`, `]`, and replace newlines with
`<br>`/spaces for table cells), or (b) correct the doc comments to state plainly
that only HTML metacharacters are escaped and Markdown structure is intentionally
preserved. Given T-03-07 is cited as a security task, (a) is expected.

### WR-03: `parse.go` doc comment claims parameters are "sorted" but they are not

**File:** `internal/releasedocs/generators/apidocs/parse.go:45-46`
**Issue:**
`// Parameters is the sorted list of parameters for this operation.` The code at
lines 197-204 and 282-289 appends parameters in spec source order with no sort.
Determinism still holds (libopenapi exposes `Parameters` as an index-ordered
`[]*Parameter`, not a map), so output is reproducible — but the comment is
factually wrong and will mislead a future maintainer into assuming alphabetical
ordering they can rely on. The render_markdown.go comment (lines 79-80) correctly
says "preserved from parsed order", contradicting parse.go.

**Fix:** Change the parse.go comment to "ordered list of parameters in spec
source order" to match reality and render_markdown.go.

### WR-04: OOM guard (maxSpecSize) has zero test coverage despite a fixture existing

**File:** `internal/releasedocs/generators/apidocs/parse.go:84-87`
**Issue:**
The 5 MiB OOM guard is a named security task (T-03-04). A 5,250,738-byte
`testdata/oversized.yaml` fixture was committed for it, but `grep` shows no test
references `oversized` anywhere in the package. The guard is therefore unverified,
and a regression that removes or reorders it (e.g. moving the size check after
`NewDocumentWithConfiguration`) would pass CI silently. The boundary is also
untested: the guard is `len > maxSpecSize` (strict), so a spec of exactly
5 MiB is accepted — intended, but unasserted.

**Fix:** Add a test that feeds `oversized.yaml` through `loadSpec`/`GenerateMulti`
and asserts the spec is skipped (no artifacts, no error per D-10), plus a
boundary test at exactly `maxSpecSize`.

### WR-05: 5.25 MB binary fixture committed to the repo for an unused test path

**File:** `internal/releasedocs/generators/apidocs/testdata/oversized.yaml`
**Issue:**
A 5.25 MB file is checked into git but referenced by no test (see WR-04). If WR-04
is fixed by generating the oversized input programmatically in the test (e.g.
`bytes.Repeat`), this fixture should be deleted rather than carried as permanent
repo weight. Committing multi-MB blobs for a guard that can be exercised with a
generated buffer is avoidable bloat. (Flagged as a quality/maintainability
concern, not performance.)

**Fix:** Either wire the fixture into a test (WR-04) or delete it and generate the
oversized buffer in-test with `bytes.Repeat([]byte("a: b\n"), n)`.

## Info

### IN-01: Dead branch in `scalarToJSON` bool handling

**File:** `internal/releasedocs/generators/apidocs/render_html.go:178-183`
**Issue:** The `!!bool` branch tests `n.Value == "true" || n.Value == "1"`. yaml.v3
never tags `1` as `!!bool` (it is `!!int`); only `true`/`false`/`yes`/`no`/etc.
resolve to `!!bool`, and yaml.v3 normalizes the `Value` to `true`/`false`. The
`|| n.Value == "1"` arm is unreachable. Harmless but misleading.
**Fix:** Drop the `== "1"` arm, or document why it is defensive.

### IN-02: Heuristic 404 detection by substring match is fragile

**File:** `internal/releasedocs/generators/apidocs/discover.go:33-39` (and `dispatcher.go:189-198`)
**Issue:** `isMissingFile` matches `"404"` or `"not found"` anywhere in the error
string. A non-404 error whose message happens to contain "not found" (e.g. a
500 "upstream service not found") would be misclassified as a missing file and
silently skipped. This is acknowledged as a "soft heuristic" in the comments and
mirrors existing orchestrator code, so it is consistent with project convention —
flagged for awareness only.
**Fix:** Long-term, define a typed sentinel error in the vcs layer; not required
for this phase.

### IN-03: `Generator.Generate` single-artifact path is dead code by design

**File:** `internal/releasedocs/generators/apidocs/apidocs.go:46-51`
**Issue:** `Generate` returns an empty artifact and is documented unreachable
because the dispatcher type-asserts `MultiGenerator` first (dispatcher.go:112).
This is intentional interface-satisfaction, but the empty `Artifact{Kind:...}` it
returns would be published as a zero-content artifact if any future caller invoked
it directly. pages.go skips zero-`Content` artifacts (line 89), so the blast
radius is contained today.
**Fix:** Consider returning an explicit error (`"apidocs: use GenerateMulti"`)
from `Generate` so misuse fails loudly instead of emitting an empty artifact.

### IN-04: Duplicate YAML keys would produce duplicate JSON keys

**File:** `internal/releasedocs/generators/apidocs/render_html.go:115-153`
**Issue:** `mappingToJSON` emits every key/value pair in `Content` without
de-duplicating. A spec with a duplicate mapping key (libopenapi/yaml.v3 may
accept some duplicate-key forms depending on strictness) would yield JSON with a
repeated key. Most JSON parsers (incl. browsers' `JSON.parse`) accept this and
keep the last value, so it is tolerated — but it is technically non-canonical
JSON and worth noting against the byte-identical-determinism goal.
**Fix:** Optional: keep the last occurrence per key during pair collection.

---

_Reviewed: 2026-06-06T09:05:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
