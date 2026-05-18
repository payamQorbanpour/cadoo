# CI-mode Stateless Dedup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `cadoo ci --mr/--pr` idempotent across pushes by reconstructing `findings.Store` dedup state from the PR/MR itself, so each push edits one overview, posts only new inline findings, and resolves fixed threads.

**Architecture:** A hidden HTML marker is stamped onto the wire copy of each inline comment (never the body used for key computation). A new optional `vcs.PriorReviewReader` capability lets the GitLab and GitHub adapters read Cadoo's prior artifacts back from the PR. `findings.NewFromPrior` seeds an in-memory `Store` from that snapshot so the existing `postSummary`/`postInline`/`resolveStalePriors` logic runs unchanged. GitHub gets a GraphQL path for read-back and `resolveReviewThread`.

**Tech Stack:** Go 1.26, `gitlab.com/gitlab-org/api/client-go`, `google/go-github/v66`, stdlib `net/http`/`encoding/json` for GitHub GraphQL (no new dependency).

**Spec:** `docs/superpowers/specs/2026-05-19-ci-mode-stateless-dedup-design.md`

---

## File Structure

- `internal/vcs/marker.go` (new) — marker format: `MarkerData`, `InlineMarker`, `ParseInlineMarker`, `FirstLine`, `SummaryWrapperBegin`. Lowest layer; no `findings` dependency.
- `internal/vcs/marker_test.go` (new) — marker round-trip / strip / malformed.
- `internal/vcs/vcs.go` (modify) — add `PriorReviewReader`, `PriorReview`, `PriorInline`.
- `internal/orchestrator/consolidate.go` (modify) — point `wrapperBegin` at `vcs.SummaryWrapperBegin`.
- `internal/findings/prior.go` (new) — `StampInline`, `NewFromPrior`.
- `internal/findings/prior_test.go` (new) — stamping isolation + seeding behavior.
- `internal/orchestrator/reviewer.go` (modify) — `postInline` stamped-copy + index-paired record.
- `internal/orchestrator/reviewer_test.go` (modify) — stamped isolation + two-run scenario.
- `internal/vcs/gitlab/gitlab.go` (modify) — implement `ListCadooArtifacts`.
- `internal/vcs/gitlab/gitlab_test.go` (modify) — read-back mapping (httptest).
- `internal/vcs/github/graphql.go` (new) — endpoint derivation + GraphQL POST helper.
- `internal/vcs/github/graphql_test.go` (new) — endpoint derivation + POST (httptest).
- `internal/vcs/github/github.go` (modify) — `ListCadooArtifacts` (GraphQL) + `ResolveThread` (GraphQL mutation).
- `internal/vcs/github/github_test.go` (new) — read-back + resolve (httptest).
- `cmd/cadoo-cli/ci.go` (modify) — wire read-back before the tool loop.
- `cmd/cadoo-cli/ci_test.go` (modify) — wiring helper test.
- `CLAUDE.md` (modify) — update the CI-mode gotcha line.

---

### Task 1: `vcs` marker format primitives

**Files:**
- Create: `internal/vcs/marker.go`
- Test: `internal/vcs/marker_test.go`

- [ ] **Step 1: Write the failing test**

```go
package vcs

import "testing"

func TestInlineMarkerRoundTrip(t *testing.T) {
	in := MarkerData{Tool: "review", SK: "9f3a1c2b7d4e5061", Sev: "warn"}
	body := "Something is wrong here.\nUse a buffered writer."
	stamped := body + "\n\n" + InlineMarker(in)

	got, stripped, ok := ParseInlineMarker(stamped)
	if !ok {
		t.Fatalf("ParseInlineMarker(%q) ok=false; want true", stamped)
	}
	if got != in {
		t.Errorf("marker = %+v; want %+v", got, in)
	}
	if stripped != body {
		t.Errorf("stripped = %q; want %q", stripped, body)
	}
}

func TestParseInlineMarkerAbsent(t *testing.T) {
	if _, _, ok := ParseInlineMarker("plain comment, no marker"); ok {
		t.Errorf("ParseInlineMarker on unmarked body: ok=true; want false")
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine("a\nb\nc"); got != "a" {
		t.Errorf("FirstLine = %q; want %q", got, "a")
	}
	if got := FirstLine("only"); got != "only" {
		t.Errorf("FirstLine = %q; want %q", got, "only")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vcs/ -run 'TestInlineMarker|TestParseInlineMarker|TestFirstLine' -v`
Expected: FAIL — `undefined: MarkerData` / `InlineMarker` / `ParseInlineMarker` / `FirstLine`.

- [ ] **Step 3: Write minimal implementation**

```go
package vcs

import (
	"fmt"
	"regexp"
	"strings"
)

// SummaryWrapperBegin is the HTML-comment sentinel that opens the
// consolidated overview comment. It is the single source of truth for the
// marker shared between the orchestrator (which writes it) and the VCS
// adapters (which grep for it during stateless read-back).
const SummaryWrapperBegin = "<!-- cadoo:wrapper:begin -->"

// MarkerData is the machine payload embedded in every Cadoo inline comment
// so a stateless CI run can recognise its own prior findings.
type MarkerData struct {
	Tool string
	SK   string // findings.StructuralKey of the original (pristine) comment
	Sev  string // vcs.Severity string
}

var inlineMarkerRe = regexp.MustCompile(
	`\n*<!-- cadoo:fp v=1 tool=(\S+) sk=(\S+) sev=(\S*) -->\s*$`)

// InlineMarker renders the hidden marker line. It is appended only to the
// wire copy of a comment body — never to the body used for key computation.
func InlineMarker(d MarkerData) string {
	return fmt.Sprintf("<!-- cadoo:fp v=1 tool=%s sk=%s sev=%s -->", d.Tool, d.SK, d.Sev)
}

// ParseInlineMarker extracts the marker from a comment body. It returns the
// parsed payload, the body with the marker (and its leading blank line)
// removed, and whether a marker was present.
func ParseInlineMarker(body string) (MarkerData, string, bool) {
	loc := inlineMarkerRe.FindStringSubmatchIndex(body)
	if loc == nil {
		return MarkerData{}, body, false
	}
	m := inlineMarkerRe.FindStringSubmatch(body)
	stripped := strings.TrimRight(body[:loc[0]], "\n")
	return MarkerData{Tool: m[1], SK: m[2], Sev: m[3]}, stripped, true
}

// FirstLine returns the first line of s (no trailing newline).
func FirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vcs/ -run 'TestInlineMarker|TestParseInlineMarker|TestFirstLine' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/marker.go internal/vcs/marker_test.go
git commit -m "feat(vcs): add inline marker format primitives for stateless dedup"
```

---

### Task 2: `vcs.PriorReviewReader` capability + types

**Files:**
- Modify: `internal/vcs/vcs.go` (add interface + structs near the `Provider` interface, line ~102-125)
- Test: `internal/vcs/marker_test.go` (append a compile/shape test)

- [ ] **Step 1: Write the failing test**

Append to `internal/vcs/marker_test.go`:

```go
func TestPriorReviewShape(t *testing.T) {
	pr := PriorReview{
		SummaryCommentID: "42",
		Inline: []PriorInline{{
			Tool: "review", File: "a.go", Severity: "warn",
			StructuralKey: "abc", Title: "boom", ExternalID: "d1", Resolved: false,
		}},
	}
	if pr.Inline[0].StructuralKey != "abc" || pr.SummaryCommentID != "42" {
		t.Fatalf("unexpected PriorReview round-trip: %+v", pr)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vcs/ -run TestPriorReviewShape -v`
Expected: FAIL — `undefined: PriorReview` / `PriorInline`.

- [ ] **Step 3: Write minimal implementation**

In `internal/vcs/vcs.go`, immediately after the `Provider interface { ... }` block, add:

```go
// PriorReviewReader is an OPTIONAL capability. Adapters that can enumerate
// Cadoo's own previously-posted artifacts on a PR/MR implement it so that
// stateless CI-mode (no DB) can rebuild dedup state from the PR itself.
// The orchestrator type-asserts for it; providers that don't implement it
// fall back to non-idempotent behaviour.
type PriorReviewReader interface {
	ListCadooArtifacts(ctx context.Context, pr *PullRequest) (PriorReview, error)
}

// PriorReview is a normalized snapshot of Cadoo's prior footprint on a PR.
type PriorReview struct {
	SummaryCommentID string // overview comment/note id (found via SummaryWrapperBegin); "" if none
	Inline           []PriorInline
}

// PriorInline is one previously-posted Cadoo inline finding.
type PriorInline struct {
	Tool          string
	File          string
	Severity      string
	StructuralKey string // parsed from the hidden marker (authoritative)
	Title         string // first visible line of the original body
	ExternalID    string // discussion/thread id for ResolveThread; "" if unrecoverable
	Resolved      bool   // already resolved upstream — don't re-resolve
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vcs/ -run TestPriorReviewShape -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/vcs.go internal/vcs/marker_test.go
git commit -m "feat(vcs): add PriorReviewReader optional capability and snapshot types"
```

---

### Task 3: Point `consolidate.go` overview marker at the shared constant

**Files:**
- Modify: `internal/orchestrator/consolidate.go:16` (the `wrapperBegin` const)

- [ ] **Step 1: Inspect current constant block**

Run: `sed -n '1,20p' internal/orchestrator/consolidate.go`
Expected: shows `wrapperBegin   = "<!-- cadoo:wrapper:begin -->"` and the import block.

- [ ] **Step 2: Make the change**

Ensure `internal/orchestrator/consolidate.go` imports `"github.com/payamqorbanpour/cadoo/internal/vcs"` (it is in the orchestrator package which already imports vcs elsewhere; if this file's import block lacks it, add it). Then replace:

```go
	wrapperBegin   = "<!-- cadoo:wrapper:begin -->"
```

with:

```go
	wrapperBegin   = vcs.SummaryWrapperBegin
```

(Leave `wrapperEnd`, `prSectionBegin`, `prSectionEnd` unchanged.)

- [ ] **Step 3: Verify build + existing tests**

Run: `go build ./... && go test ./internal/orchestrator/ -run TestRenderConsolidated -v`
Expected: build OK; consolidated-render tests PASS (the constant value is byte-identical, so rendered output is unchanged).

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/consolidate.go
git commit -m "refactor(orchestrator): source overview wrapper marker from vcs package"
```

---

### Task 4: `findings.StampInline` + `NewFromPrior`

**Files:**
- Create: `internal/findings/prior.go`
- Test: `internal/findings/prior_test.go`

- [ ] **Step 1: Write the failing test**

```go
package findings

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestStampInlineDoesNotMutateOriginal(t *testing.T) {
	c := vcs.InlineComment{File: "a.go", Body: "Avoid the N+1 query.", Severity: vcs.SeverityWarn}
	wantSK := StructuralKey("review", c)

	stamped := StampInline("review", c)

	if c.Body != "Avoid the N+1 query." {
		t.Fatalf("original body mutated: %q", c.Body)
	}
	md, stripped, ok := vcs.ParseInlineMarker(stamped)
	if !ok || stripped != "Avoid the N+1 query." {
		t.Fatalf("stamped body not parseable: ok=%v stripped=%q", ok, stripped)
	}
	if md.SK != wantSK || md.Tool != "review" || md.Sev != "warn" {
		t.Errorf("marker = %+v; want sk=%s tool=review sev=warn", md, wantSK)
	}
}

func TestNewFromPriorSeedsDedupAndSummary(t *testing.T) {
	ctx := context.Background()
	key := PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 7}
	c := vcs.InlineComment{File: "a.go", Body: "Avoid the N+1 query.", Severity: vcs.SeverityWarn}
	sk := StructuralKey("review", c)

	s := NewFromPrior(key, vcs.PriorReview{
		SummaryCommentID: "99",
		Inline: []vcs.PriorInline{{
			Tool: "review", File: "a.go", Severity: "warn",
			StructuralKey: sk, Title: "Avoid the N+1 query.",
			ExternalID: "disc-1", Resolved: false,
		}},
	})

	if !s.Enabled() {
		t.Fatal("store should be Enabled()")
	}
	has, err := s.HasFinding(ctx, key, "review", c)
	if err != nil || !has {
		t.Fatalf("HasFinding = %v, %v; want true, nil", has, err)
	}
	id, _ := s.SummaryID(ctx, key, WrapperToolKey)
	if id != "99" {
		t.Errorf("SummaryID = %q; want %q", id, "99")
	}
	priors, _ := s.ListPostedFindings(ctx, key)
	if len(priors) != 1 || priors[0].ExternalCommentID != "disc-1" {
		t.Errorf("ListPostedFindings = %+v; want one with ExternalCommentID=disc-1", priors)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/findings/ -run 'TestStampInline|TestNewFromPrior' -v`
Expected: FAIL — `undefined: StampInline` / `NewFromPrior`.

- [ ] **Step 3: Write minimal implementation**

```go
package findings

import "github.com/payamqorbanpour/cadoo/internal/vcs"

// StampInline returns the comment body with the hidden dedup marker
// appended. The marker encodes the StructuralKey of the PRISTINE comment
// so a later stateless run recovers the exact same key. Callers must pass
// the original comment for key computation and recording; only the value
// returned here goes over the wire.
func StampInline(tool string, c vcs.InlineComment) string {
	return c.Body + "\n\n" + vcs.InlineMarker(vcs.MarkerData{
		Tool: tool,
		SK:   StructuralKey(tool, c),
		Sev:  string(c.Severity),
	})
}

// NewFromPrior builds an in-memory Store (no disk path) pre-populated from a
// PR's own prior Cadoo artifacts, so stateless CI-mode reuses the exact
// HasFinding / ListPostedFindings / SummaryID dedup logic with no changes
// to postSummary / postInline / resolveStalePriors.
func NewFromPrior(key PRKey, pr vcs.PriorReview) *Store {
	m := newMemoryStore("") // empty path => load()/persist() are no-ops
	recs := make([]findingRec, 0, len(pr.Inline))
	for _, pi := range pr.Inline {
		recs = append(recs, findingRec{
			Tool:            pi.Tool,
			File:            pi.File,
			Severity:        pi.Severity,
			StructuralKey:   pi.StructuralKey,
			NormalizedTitle: normalizeTitle(pi.Title),
			Title:           pi.Title,
			ExternalID:      pi.ExternalID,
		})
	}
	if len(recs) > 0 {
		m.findings[key] = recs
	}
	if pr.SummaryCommentID != "" {
		m.summaries[summaryRefKey{PR: key, Tool: WrapperToolKey}] = pr.SummaryCommentID
	}
	return &Store{mem: m}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/findings/ -run 'TestStampInline|TestNewFromPrior' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Run the whole findings package to check no regression**

Run: `go test ./internal/findings/ -count=1`
Expected: PASS (existing `findings_test.go` still green).

- [ ] **Step 6: Commit**

```bash
git add internal/findings/prior.go internal/findings/prior_test.go
git commit -m "feat(findings): StampInline marker helper and NewFromPrior in-memory store"
```

---

### Task 5: `postInline` stamps the wire copy, records from the pristine comment

**Files:**
- Modify: `internal/orchestrator/reviewer.go` (the `if len(delta) > 0 { ... }` block inside `postInline`, ~lines 419-435)
- Test: `internal/orchestrator/reviewer_test.go` (add a test)

- [ ] **Step 1: Write the failing test**

Append to `internal/orchestrator/reviewer_test.go` (the file already has `fakeLLM`, `idVCS`, and imports `findings`, `vcs`, `context`, `fmt`, `testing`):

```go
// captureVCS records exactly what bodies were sent over the wire and hands
// back per-comment external IDs (like the real GitLab adapter).
type captureVCS struct {
	idVCS
	sentBodies []string
}

func (c *captureVCS) PostInlineComments(_ context.Context, _ *vcs.PullRequest, cs []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	refs := make([]vcs.PostedInlineRef, len(cs))
	for i, cc := range cs {
		c.sentBodies = append(c.sentBodies, cc.Body)
		refs[i] = vcs.PostedInlineRef{Comment: cc, ExternalID: fmt.Sprintf("disc-%d", i+1)}
	}
	return refs, nil
}

func TestPostInlineStampsWireBodyButRecordsPristine(t *testing.T) {
	ctx := context.Background()
	cap := &captureVCS{}
	d := &Dispatcher{Posted: findings.NewMemory("")}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	c := vcs.InlineComment{File: "a.go", Body: "Fix the leak.", Severity: vcs.SeverityWarn}

	d.postInline(ctx, cap, pr, key, "review", []vcs.InlineComment{c})

	if len(cap.sentBodies) != 1 {
		t.Fatalf("sent %d bodies; want 1", len(cap.sentBodies))
	}
	if _, _, ok := vcs.ParseInlineMarker(cap.sentBodies[0]); !ok {
		t.Errorf("wire body missing marker: %q", cap.sentBodies[0])
	}
	// Recorded under the PRISTINE structural key (marker did not poison it).
	has, _ := d.Posted.HasFinding(ctx, key, "review", c)
	if !has {
		t.Error("pristine comment not recorded / HasFinding=false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/ -run TestPostInlineStampsWireBody -v`
Expected: FAIL — wire body has no marker (current code posts `delta` unstamped).

- [ ] **Step 3: Write the implementation**

In `internal/orchestrator/reviewer.go`, replace this exact block inside `postInline`:

```go
	if len(delta) > 0 {
		refs, err := provider.PostInlineComments(ctx, pr, delta)
		if err != nil {
			slog.Error("post inline review", "err", err, "pr", pr.URL)
			// Fall through: refs may still hold the partial set the adapter
			// managed to post (especially the per-comment GitLab path).
		}
		if tool != "" && d.Posted != nil {
			for _, ref := range refs {
				_ = d.Posted.RecordFinding(ctx, key, tool, ref.ExternalID, ref.Comment)
			}
		}
	}
```

with:

```go
	if len(delta) > 0 {
		// Stamp a separate wire copy with the hidden dedup marker. The
		// original delta entries stay pristine — they are what we hash for
		// keys and what we record, so the marker can never poison
		// StructuralKey/Fingerprint.
		wire := make([]vcs.InlineComment, len(delta))
		for i, c := range delta {
			wc := c
			if tool != "" {
				wc.Body = findings.StampInline(tool, c)
			}
			wire[i] = wc
		}
		refs, err := provider.PostInlineComments(ctx, pr, wire)
		if err != nil {
			slog.Error("post inline review", "err", err, "pr", pr.URL)
			// Fall through: refs may still hold the partial set the adapter
			// managed to post (especially the per-comment GitLab path).
		}
		if tool != "" && d.Posted != nil {
			// Adapters return exactly one ref per input comment, in input
			// order. Pair each ref with its PRISTINE delta entry by index.
			for i := range refs {
				if i >= len(delta) {
					break
				}
				_ = d.Posted.RecordFinding(ctx, key, tool, refs[i].ExternalID, delta[i])
			}
		}
	}
```

(`findings` and `vcs` are already imported by `reviewer.go`.)

- [ ] **Step 4: Run the new test + the full orchestrator suite**

Run: `go test ./internal/orchestrator/ -run TestPostInlineStampsWireBody -v`
Expected: PASS.

Run: `go test ./internal/orchestrator/ -count=1`
Expected: PASS — including `TestPostInlineKeepsDistinctSuggestionsInBatch`, `TestPostInlineCollapsesIdenticalDuplicatesInBatch`, `TestPostInlineResolvesStalePriors` (these assert behaviour on `delta`/dedup, which is unchanged because keys are still computed from pristine comments).

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrator/reviewer.go internal/orchestrator/reviewer_test.go
git commit -m "feat(orchestrator): stamp dedup marker on wire copy, record pristine comment"
```

---

### Task 6: GitLab `ListCadooArtifacts`

**Files:**
- Modify: `internal/vcs/gitlab/gitlab.go` (add method + a small title helper; uses existing `formatSeverity`)
- Test: `internal/vcs/gitlab/gitlab_test.go` (add httptest-backed test; file currently has no httptest import)

- [ ] **Step 1: Write the failing test**

Append to `internal/vcs/gitlab/gitlab_test.go`. Add imports `context`, `net/http`, `net/http/httptest`, `github.com/payamqorbanpour/cadoo/internal/vcs` to the existing import block (keep `glab` and `testing`):

```go
func TestListCadooArtifactsGitLab(t *testing.T) {
	// One inline discussion with our marker (resolved=false), and one plain
	// MR note that is the overview (carries SummaryWrapperBegin, no position).
	const discJSON = `[
	  {"id":"disc-abc","notes":[{"id":11,"system":false,"resolved":false,
	    "position":{"new_path":"a.go","old_path":"a.go"},
	    "body":"**[WARN]** Fix the leak.\n\n<!-- cadoo:fp v=1 tool=review sk=deadbeefdeadbeef sev=warn -->"}]},
	  {"id":"disc-ov","notes":[{"id":22,"system":false,"resolved":false,
	    "body":"` + vcs.SummaryWrapperBegin + ` consolidated overview here"}]}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Total-Pages", "1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(discJSON))
	}))
	defer srv.Close()

	a, err := New(Config{BaseURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 7}
	got, err := a.ListCadooArtifacts(context.Background(), pr)
	if err != nil {
		t.Fatalf("ListCadooArtifacts: %v", err)
	}
	if got.SummaryCommentID != "22" {
		t.Errorf("SummaryCommentID = %q; want 22", got.SummaryCommentID)
	}
	if len(got.Inline) != 1 {
		t.Fatalf("inline count = %d; want 1", len(got.Inline))
	}
	in := got.Inline[0]
	if in.StructuralKey != "deadbeefdeadbeef" || in.Tool != "review" ||
		in.File != "a.go" || in.Severity != "warn" ||
		in.ExternalID != "disc-abc" || in.Title != "Fix the leak." || in.Resolved {
		t.Errorf("inline = %+v; want sk=deadbeefdeadbeef tool=review file=a.go sev=warn id=disc-abc title=%q resolved=false", in, "Fix the leak.")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vcs/gitlab/ -run TestListCadooArtifactsGitLab -v`
Expected: FAIL — `a.ListCadooArtifacts undefined`.

- [ ] **Step 3: Write the implementation**

Add to `internal/vcs/gitlab/gitlab.go` (the file already imports `context`, `strconv`, `strings`, `glab`, `vcs`):

```go
// ListCadooArtifacts implements vcs.PriorReviewReader. It walks the MR's
// discussions, recognising Cadoo's own comments by the hidden marker (inline
// findings) and by SummaryWrapperBegin (the overview note), so stateless
// CI-mode can rebuild dedup state from the MR itself.
func (a *Adapter) ListCadooArtifacts(ctx context.Context, pr *vcs.PullRequest) (vcs.PriorReview, error) {
	var out vcs.PriorReview
	opt := &glab.ListMergeRequestDiscussionsOptions{PerPage: 100, Page: 1}
	for {
		discs, resp, err := a.client.Discussions.ListMergeRequestDiscussions(
			pr.RepoFullName, pr.Number, opt, glab.WithContext(ctx))
		if err != nil {
			return vcs.PriorReview{}, fmt.Errorf("list mr discussions: %w", err)
		}
		for _, d := range discs {
			for _, n := range d.Notes {
				if n == nil || n.System {
					continue
				}
				md, stripped, ok := vcs.ParseInlineMarker(n.Body)
				if ok && n.Position != nil {
					file := n.Position.NewPath
					if file == "" {
						file = n.Position.OldPath
					}
					orig := strings.TrimPrefix(stripped, formatSeverity(vcs.Severity(md.Sev)))
					out.Inline = append(out.Inline, vcs.PriorInline{
						Tool:          md.Tool,
						File:          file,
						Severity:      md.Sev,
						StructuralKey: md.SK,
						Title:         vcs.FirstLine(strings.TrimSpace(orig)),
						ExternalID:    d.ID,
						Resolved:      n.Resolved,
					})
					continue
				}
				if n.Position == nil && strings.Contains(n.Body, vcs.SummaryWrapperBegin) {
					out.SummaryCommentID = strconv.Itoa(n.ID)
				}
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return out, nil
}
```

- [ ] **Step 4: Run the test + package suite**

Run: `go test ./internal/vcs/gitlab/ -run TestListCadooArtifactsGitLab -v`
Expected: PASS.

Run: `go test ./internal/vcs/gitlab/ -count=1`
Expected: PASS (existing diff/index tests unaffected).

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/gitlab/gitlab.go internal/vcs/gitlab/gitlab_test.go
git commit -m "feat(gitlab): implement PriorReviewReader read-back from MR discussions"
```

---

### Task 7: GitHub GraphQL helper (endpoint + POST)

**Files:**
- Create: `internal/vcs/github/graphql.go`
- Test: `internal/vcs/github/graphql_test.go`

- [ ] **Step 1: Write the failing test**

```go
package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGraphQLEndpoint(t *testing.T) {
	cases := []struct{ rest, want string }{
		{"https://api.github.com/", "https://api.github.com/graphql"},
		{"https://ghe.example.com/api/v3/", "https://ghe.example.com/api/graphql"},
	}
	for _, tc := range cases {
		u, _ := url.Parse(tc.rest)
		if got := graphqlEndpoint(u); got != tc.want {
			t.Errorf("graphqlEndpoint(%s) = %s; want %s", tc.rest, got, tc.want)
		}
	}
}

func TestDoGraphQLDecodesDataAndErrors(t *testing.T) {
	ctx := context.Background()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"x":{"n":7}}}`))
	}))
	defer ok.Close()
	var out struct {
		X struct{ N int } `json:"x"`
	}
	if err := doGraphQL(ctx, ok.Client(), ok.URL, "query{}", nil, &out); err != nil {
		t.Fatalf("doGraphQL ok: %v", err)
	}
	if out.X.N != 7 {
		t.Errorf("decoded N = %d; want 7", out.X.N)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"boom"}]}`))
	}))
	defer bad.Close()
	if err := doGraphQL(ctx, bad.Client(), bad.URL, "query{}", nil, &out); err == nil {
		t.Error("doGraphQL with GraphQL errors: err=nil; want error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vcs/github/ -run 'TestGraphQLEndpoint|TestDoGraphQL' -v`
Expected: FAIL — `undefined: graphqlEndpoint` / `doGraphQL`.

- [ ] **Step 3: Write the implementation**

```go
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// graphqlEndpoint derives the GraphQL URL from go-github's REST base URL.
// github.com REST base is https://api.github.com/ ; GHES is
// https://<host>/api/v3/ and its GraphQL endpoint is https://<host>/api/graphql.
func graphqlEndpoint(rest *url.URL) string {
	if rest.Host == "api.github.com" {
		return "https://api.github.com/graphql"
	}
	return rest.Scheme + "://" + rest.Host + "/api/graphql"
}

type graphqlError struct {
	Message string `json:"message"`
}

// doGraphQL POSTs a GraphQL request using the supplied (already
// authenticated) http client and decodes the `data` field into out.
func doGraphQL(ctx context.Context, hc *http.Client, endpoint, query string, vars map[string]any, out any) error {
	reqBody, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("graphql marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("graphql post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("graphql http %d", resp.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphqlError  `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("graphql decode: %w", err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("graphql data unmarshal: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vcs/github/ -run 'TestGraphQLEndpoint|TestDoGraphQL' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/github/graphql.go internal/vcs/github/graphql_test.go
git commit -m "feat(github): add GraphQL endpoint derivation and POST helper"
```

---

### Task 8: GitHub `ListCadooArtifacts` + GraphQL `ResolveThread`

**Files:**
- Modify: `internal/vcs/github/github.go` (add `ListCadooArtifacts`; replace the no-op `ResolveThread` at ~line 231; uses existing `formatSeverity`, `splitRepo`, `a.client`)
- Test: `internal/vcs/github/github_test.go`

- [ ] **Step 1: Write the failing test**

```go
package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestGitHubListCadooArtifactsAndResolve(t *testing.T) {
	var resolvedWith string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var q struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &q)
		switch {
		case strings.Contains(q.Query, "resolveReviewThread"):
			resolvedWith, _ = q.Variables["id"].(string)
			_, _ = w.Write([]byte(`{"data":{"resolveReviewThread":{"thread":{"id":"T1"}}}}`))
		default: // read-back query
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{
			  "comments":{"nodes":[{"databaseId":55,"body":"` + vcs.SummaryWrapperBegin + ` overview"}],
			    "pageInfo":{"hasNextPage":false,"endCursor":null}},
			  "reviewThreads":{"nodes":[{"id":"T1","isResolved":false,
			    "comments":{"nodes":[{"path":"a.go","body":"**[WARN]** Fix the leak.\n\n<!-- cadoo:fp v=1 tool=review sk=deadbeefdeadbeef sev=warn -->"}]}}],
			    "pageInfo":{"hasNextPage":false,"endCursor":null}}
			}}}}`))
		}
	}))
	defer srv.Close()

	a, err := New(Config{Token: "t"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.gqlEndpoint = srv.URL    // test seam (added in Step 3)
	a.gqlClient = srv.Client() // test seam (added in Step 3)

	ctx := context.Background()
	pr := &vcs.PullRequest{RepoFullName: "o/r", Number: 3}
	got, err := a.ListCadooArtifacts(ctx, pr)
	if err != nil {
		t.Fatalf("ListCadooArtifacts: %v", err)
	}
	if got.SummaryCommentID != "55" {
		t.Errorf("SummaryCommentID = %q; want 55", got.SummaryCommentID)
	}
	if len(got.Inline) != 1 || got.Inline[0].ExternalID != "T1" ||
		got.Inline[0].StructuralKey != "deadbeefdeadbeef" ||
		got.Inline[0].File != "a.go" || got.Inline[0].Title != "Fix the leak." {
		t.Fatalf("inline = %+v; unexpected", got.Inline)
	}

	if err := a.ResolveThread(ctx, pr, "T1"); err != nil {
		t.Fatalf("ResolveThread: %v", err)
	}
	if resolvedWith != "T1" {
		t.Errorf("resolveReviewThread called with %q; want T1", resolvedWith)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vcs/github/ -run TestGitHubListCadooArtifactsAndResolve -v`
Expected: FAIL — `a.gqlEndpoint undefined` / `a.ListCadooArtifacts undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/vcs/github/github.go`, add two unexported fields to the `Adapter` struct so the GraphQL endpoint/client are overridable in tests and default to the authenticated go-github client:

```go
// Adapter is the GitHub vcs.Provider implementation.
type Adapter struct {
	cfg    Config
	client *gogithub.Client

	// GraphQL seams. Default to the authenticated go-github http client and
	// the endpoint derived from its REST base URL; overridable in tests.
	gqlClient   *http.Client
	gqlEndpoint string
}
```

At the end of `New`, before `return &Adapter{...}`, replace the final return with:

```go
	ad := &Adapter{cfg: cfg, client: client}
	ad.gqlClient = client.Client()
	ad.gqlEndpoint = graphqlEndpoint(client.BaseURL)
	return ad, nil
```

Replace the existing no-op `ResolveThread` (the `func (a *Adapter) ResolveThread(_ context.Context, _ *vcs.PullRequest, _ string) error { return nil }` and its doc comment) with:

```go
// ResolveThread resolves a GitHub review thread via the GraphQL
// resolveReviewThread mutation. threadID is the GraphQL node ID captured by
// ListCadooArtifacts. Empty id is a no-op (e.g. unrecoverable thread).
func (a *Adapter) ResolveThread(ctx context.Context, _ *vcs.PullRequest, threadID string) error {
	if threadID == "" {
		return nil
	}
	const m = `mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{id}}}`
	return doGraphQL(ctx, a.gqlClient, a.gqlEndpoint, m,
		map[string]any{"id": threadID}, nil)
}
```

Add `ListCadooArtifacts` (place it near `PostInlineComments`). It needs `strconv` and `strings`, already imported:

```go
// ListCadooArtifacts implements vcs.PriorReviewReader using a single
// paginated GraphQL query over the PR's issue comments (overview, matched
// by SummaryWrapperBegin) and review threads (inline findings, matched by
// the hidden marker on the first comment).
func (a *Adapter) ListCadooArtifacts(ctx context.Context, pr *vcs.PullRequest) (vcs.PriorReview, error) {
	owner, name, err := splitRepo(pr.RepoFullName)
	if err != nil {
		return vcs.PriorReview{}, err
	}
	const q = `query($owner:String!,$name:String!,$num:Int!,$tc:String,$rc:String){
	  repository(owner:$owner,name:$name){ pullRequest(number:$num){
	    comments(first:100,after:$tc){ nodes{ databaseId body }
	      pageInfo{ hasNextPage endCursor } }
	    reviewThreads(first:100,after:$rc){ nodes{ id isResolved
	      comments(first:1){ nodes{ path body } } }
	      pageInfo{ hasNextPage endCursor } }
	  }}}`

	type gqlResp struct {
		Repository struct {
			PullRequest struct {
				Comments struct {
					Nodes []struct {
						DatabaseID int64  `json:"databaseId"`
						Body       string `json:"body"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"comments"`
				ReviewThreads struct {
					Nodes []struct {
						ID         string `json:"id"`
						IsResolved bool   `json:"isResolved"`
						Comments   struct {
							Nodes []struct {
								Path string `json:"path"`
								Body string `json:"body"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}

	var out vcs.PriorReview
	var tc, rc string
	for {
		var r gqlResp
		vars := map[string]any{"owner": owner, "name": name, "num": int(pr.Number)}
		if tc != "" {
			vars["tc"] = tc
		}
		if rc != "" {
			vars["rc"] = rc
		}
		if err := doGraphQL(ctx, a.gqlClient, a.gqlEndpoint, q, vars, &r); err != nil {
			return vcs.PriorReview{}, err
		}
		p := r.Repository.PullRequest
		for _, c := range p.Comments.Nodes {
			if out.SummaryCommentID == "" && strings.Contains(c.Body, vcs.SummaryWrapperBegin) {
				out.SummaryCommentID = strconv.FormatInt(c.DatabaseID, 10)
			}
		}
		for _, th := range p.ReviewThreads.Nodes {
			if len(th.Comments.Nodes) == 0 {
				continue
			}
			first := th.Comments.Nodes[0]
			md, stripped, ok := vcs.ParseInlineMarker(first.Body)
			if !ok {
				continue
			}
			orig := strings.TrimPrefix(stripped, formatSeverity(vcs.Severity(md.Sev)))
			out.Inline = append(out.Inline, vcs.PriorInline{
				Tool:          md.Tool,
				File:          first.Path,
				Severity:      md.Sev,
				StructuralKey: md.SK,
				Title:         vcs.FirstLine(strings.TrimSpace(orig)),
				ExternalID:    th.ID,
				Resolved:      th.IsResolved,
			})
		}
		moreC := p.Comments.PageInfo.HasNextPage
		moreT := p.ReviewThreads.PageInfo.HasNextPage
		if !moreC && !moreT {
			break
		}
		if moreC {
			tc = p.Comments.PageInfo.EndCursor
		}
		if moreT {
			rc = p.ReviewThreads.PageInfo.EndCursor
		}
	}
	return out, nil
}
```

Add `"net/http"` to the import block if not already present (the file already imports `net/http`).

- [ ] **Step 4: Run test + package suite**

Run: `go test ./internal/vcs/github/ -run TestGitHubListCadooArtifactsAndResolve -v`
Expected: PASS.

Run: `go test ./internal/vcs/github/ -count=1`
Expected: PASS (graphql_test still green; no other github tests regress).

- [ ] **Step 5: Commit**

```bash
git add internal/vcs/github/github.go internal/vcs/github/github_test.go
git commit -m "feat(github): GraphQL read-back and resolveReviewThread for stateless dedup"
```

---

### Task 9: Wire read-back into CI-mode

**Files:**
- Modify: `cmd/cadoo-cli/ci.go` (add `priorStore` helper; call it in `ciCmd` before the tool loop)
- Test: `cmd/cadoo-cli/ci_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/cadoo-cli/ci_test.go`. Add imports `context`, `github.com/payamqorbanpour/cadoo/internal/findings`, `github.com/payamqorbanpour/cadoo/internal/vcs` to its import block:

```go
type fakePriorReader struct{ snap vcs.PriorReview }

func (f fakePriorReader) ListCadooArtifacts(_ context.Context, _ *vcs.PullRequest) (vcs.PriorReview, error) {
	return f.snap, nil
}

func TestPriorStoreSeedsFromReader(t *testing.T) {
	ctx := context.Background()
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 4}
	c := vcs.InlineComment{File: "a.go", Body: "Fix the leak.", Severity: vcs.SeverityWarn}
	sk := findings.StructuralKey("review", c)

	r := fakePriorReader{snap: vcs.PriorReview{
		SummaryCommentID: "12",
		Inline: []vcs.PriorInline{{
			Tool: "review", File: "a.go", Severity: "warn",
			StructuralKey: sk, Title: "Fix the leak.", ExternalID: "T1",
		}},
	}}

	st := priorStore(ctx, r, "g/p", 4, vcs.KindGitLab)
	if st == nil || !st.Enabled() {
		t.Fatal("priorStore returned nil/disabled store")
	}
	has, _ := st.HasFinding(ctx, key, "review", c)
	if !has {
		t.Error("seeded finding not found via HasFinding")
	}
	id, _ := st.SummaryID(ctx, key, findings.WrapperToolKey)
	if id != "12" {
		t.Errorf("SummaryID = %q; want 12", id)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cadoo-cli/ -run TestPriorStoreSeedsFromReader -v`
Expected: FAIL — `undefined: priorStore`.

- [ ] **Step 3: Write the implementation**

In `cmd/cadoo-cli/ci.go`, add `"context"`, `"log/slog"`, the `findings` and `vcs` imports if missing (`vcs` is already imported; add `"github.com/payamqorbanpour/cadoo/internal/findings"` and `"log/slog"`/`"context"`). Add this helper:

```go
// priorStore reconstructs an in-memory findings.Store from the PR's own
// prior Cadoo artifacts so stateless CI-mode is idempotent across pushes.
// Returns nil when the provider can't read artifacts back or the read
// fails — callers then degrade to the non-idempotent legacy behaviour.
func priorStore(ctx context.Context, r vcs.PriorReviewReader, repo string, number int64, provider vcs.Kind) *findings.Store {
	pr := &vcs.PullRequest{Provider: provider, RepoFullName: repo, Number: number}
	snap, err := r.ListCadooArtifacts(ctx, pr)
	if err != nil {
		slog.Warn("ci: prior-artifact read-back failed; comments may duplicate this run",
			"err", err, "repo", repo, "number", number)
		return nil
	}
	key := findings.PRKey{Provider: string(provider), RepoFullName: repo, PRNumber: number}
	return findings.NewFromPrior(key, snap)
}
```

In `ciCmd`, immediately AFTER the dispatcher `d := &orchestrator.Dispatcher{...}` literal and BEFORE the `ctx := context.Background()` line, insert:

```go
	ctx := context.Background()
	if rr, ok := provider.(vcs.PriorReviewReader); ok {
		if st := priorStore(ctx, rr, target.ProjectPath, target.Number, target.Provider); st != nil {
			d.Posted = st
		}
	}
```

Then delete the now-duplicate later `ctx := context.Background()` line (there is exactly one further down — remove that one so `ctx` is declared once, here).

- [ ] **Step 4: Run test + build**

Run: `go test ./cmd/cadoo-cli/ -run TestPriorStoreSeedsFromReader -v`
Expected: PASS.

Run: `go build ./... && go test ./cmd/cadoo-cli/ -count=1`
Expected: build OK; all cli tests PASS (`TestParseTargetURL`, `TestSplitCSV` unaffected).

- [ ] **Step 5: Commit**

```bash
git add cmd/cadoo-cli/ci.go cmd/cadoo-cli/ci_test.go
git commit -m "feat(cli): seed findings.Store from PR read-back in CI-mode"
```

---

### Task 10: Two-run idempotency scenario test

**Files:**
- Modify: `internal/orchestrator/reviewer_test.go` (add an end-to-end-ish test using `postSummary`/`postInline` directly with a capturing fake provider that also implements `vcs.PriorReviewReader`)

- [ ] **Step 1: Write the failing test**

Append to `internal/orchestrator/reviewer_test.go`:

```go
// scenarioVCS records inline posts and summary create/update calls, and can
// replay them as a vcs.PriorReview for the second run.
type scenarioVCS struct {
	idVCS
	inline      []vcs.InlineComment
	summaryID   string
	summaryBody string
	updated     bool
	resolved    []string
}

func (s *scenarioVCS) PostInlineComments(_ context.Context, _ *vcs.PullRequest, cs []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	refs := make([]vcs.PostedInlineRef, len(cs))
	for i, cc := range cs {
		s.inline = append(s.inline, cc)
		refs[i] = vcs.PostedInlineRef{Comment: cc, ExternalID: fmt.Sprintf("T%d", len(s.inline))}
	}
	return refs, nil
}
func (s *scenarioVCS) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, body string) (string, error) {
	s.summaryID, s.summaryBody = "S1", body
	return s.summaryID, nil
}
func (s *scenarioVCS) UpdateSummaryComment(_ context.Context, _ *vcs.PullRequest, id, body string) error {
	s.updated, s.summaryBody = true, body
	return nil
}
func (s *scenarioVCS) ResolveThread(_ context.Context, _ *vcs.PullRequest, id string) error {
	s.resolved = append(s.resolved, id)
	return nil
}
func (s *scenarioVCS) replay() vcs.PriorReview {
	pr := vcs.PriorReview{SummaryCommentID: s.summaryID}
	for i, c := range s.inline {
		md, stripped, _ := vcs.ParseInlineMarker(c.Body)
		pr.Inline = append(pr.Inline, vcs.PriorInline{
			Tool: md.Tool, File: c.File, Severity: md.Sev,
			StructuralKey: md.SK, Title: vcs.FirstLine(stripped),
			ExternalID: fmt.Sprintf("T%d", i+1),
		})
	}
	return pr
}

func TestCIModeTwoRunIdempotency(t *testing.T) {
	ctx := context.Background()
	sv := &scenarioVCS{}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}

	c1 := vcs.InlineComment{File: "a.go", Body: "Leak here.", Severity: vcs.SeverityWarn}
	c2 := vcs.InlineComment{File: "b.go", Body: "Off by one.", Severity: vcs.SeverityWarn}

	// --- Run 1: fresh PR, no prior store (legacy CI behaviour). ---
	d1 := &Dispatcher{Posted: findings.NewFromPrior(key, vcs.PriorReview{})}
	d1.postSummary(ctx, sv, pr, key, "review", "## Overview\nfirst pass")
	d1.postInline(ctx, sv, pr, key, "review", []vcs.InlineComment{c1, c2})

	if len(sv.inline) != 2 || sv.summaryID != "S1" {
		t.Fatalf("run1: inline=%d summaryID=%q; want 2, S1", len(sv.inline), sv.summaryID)
	}

	// --- Run 2: same c1 persists, c2 fixed (absent), new c3. ---
	prior := sv.replay()
	sv.inline = nil // only count NEW posts in run 2
	c3 := vcs.InlineComment{File: "c.go", Body: "Nil deref.", Severity: vcs.SeverityWarn}

	d2 := &Dispatcher{Posted: findings.NewFromPrior(key, prior)}
	d2.postSummary(ctx, sv, pr, key, "review", "## Overview\nsecond pass")
	d2.postInline(ctx, sv, pr, key, "review", []vcs.InlineComment{c1, c3})

	if len(sv.inline) != 1 || sv.inline[0].File != "c.go" {
		t.Errorf("run2 inline = %+v; want only c.go (c1 deduped)", sv.inline)
	}
	if !sv.updated {
		t.Error("run2: overview was not edited in place (expected UpdateSummaryComment)")
	}
	if len(sv.resolved) != 1 || sv.resolved[0] != "T2" {
		t.Errorf("run2 resolved = %v; want [T2] (the fixed b.go thread)", sv.resolved)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (or surfaces gaps)**

Run: `go test ./internal/orchestrator/ -run TestCIModeTwoRunIdempotency -v`
Expected: Initially may FAIL only if a prior task is incomplete. With Tasks 1–9 done it should PASS. If it fails, fix the offending task's code (do NOT weaken the assertions).

- [ ] **Step 3: Run the full orchestrator suite**

Run: `go test ./internal/orchestrator/ -count=1`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/reviewer_test.go
git commit -m "test(orchestrator): two-run CI idempotency scenario (dedup + edit + resolve)"
```

---

### Task 11: Docs + full CI gate

**Files:**
- Modify: `CLAUDE.md` (the `cadoo-cli` bullet and/or the gotchas list)

- [ ] **Step 1: Update CLAUDE.md**

In `CLAUDE.md`, find the `cadoo-cli` binary bullet that reads:

```
- `cadoo-cli` — local pre-commit review and the **CI-mode** entry point (`cadoo ci --pr <url>` / `--mr <url>`). CI-mode is stateless — no DB, no KB/learnings, no idempotent edits across resyncs.
```

Replace its last sentence so it reads:

```
- `cadoo-cli` — local pre-commit review and the **CI-mode** entry point (`cadoo ci --pr <url>` / `--mr <url>`). CI-mode is stateless — no DB, no KB/learnings — but is idempotent across pushes: it reconstructs dedup state by reading its own prior comments back from the PR/MR (hidden `<!-- cadoo:fp … -->` marker + `vcs.PriorReviewReader`), so the overview is edited in place, duplicate inline findings are suppressed, and fixed threads are resolved.
```

- [ ] **Step 2: Run the full CI gate**

Run: `make ci`
Expected: `vet` clean, all tests PASS, all five binaries build.

- [ ] **Step 3: Run the linter on changed files**

Run: `make lint`
Expected: no new findings. (Note: `exported` revive rule is on — every new exported symbol added in Tasks 1–9 has a docstring; verify if lint flags any.)

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: note CI-mode is now idempotent via PR read-back"
```

---

## Self-Review

**Spec coverage:**

- Capability interface (spec §1) → Task 2.
- Hidden marker + correctness rule (spec §2) → Task 1 (format), Task 4 (`StampInline`), Task 5 (`postInline` stamped copy / pristine record).
- Per-provider read-back (spec §3): GitLab → Task 6; GitHub GraphQL → Tasks 7–8.
- State reconstruction + CI wiring (spec §4) → Task 4 (`NewFromPrior`), Task 9 (`ciCmd`).
- Failure handling — degrade never crash (spec §5) → Task 9 (`priorStore` returns nil on error → legacy behaviour); GraphQL/HTTP errors surface as returned errors, not panics.
- Edge cases (spec §6): overview deleted → empty `SummaryCommentID` → legacy `postSummary` create path (unchanged behaviour, covered conceptually; `NewFromPrior` leaves summary map empty). Inline deleted → absent from read-back → re-posted. Marker absent/malformed → `ParseInlineMarker` ok=false → skipped (Task 1 test). GitLab unanchored note → empty `ExternalID`, still deduped by marker. No DB migration — none added.
- Testing (spec §7) → Tasks 1,4,5,6,7,8,9 unit tests + Task 10 two-run scenario; Task 11 `make ci` regression gate.
- GitHub full-parity resolve (spec scope) → Task 8 GraphQL `resolveReviewThread`.

No gaps found.

**Placeholder scan:** No TBD/TODO; every code step contains complete code; every command has an expected result. OK.

**Type consistency:** `MarkerData{Tool,SK,Sev}`, `InlineMarker(MarkerData)`, `ParseInlineMarker(string)→(MarkerData,string,bool)`, `FirstLine(string)`, `vcs.SummaryWrapperBegin`, `vcs.PriorReview{SummaryCommentID,Inline}`, `vcs.PriorInline{Tool,File,Severity,StructuralKey,Title,ExternalID,Resolved}`, `findings.StampInline(string,vcs.InlineComment)`, `findings.NewFromPrior(PRKey,vcs.PriorReview)`, `priorStore(ctx,vcs.PriorReviewReader,string,int64,vcs.Kind)` are used identically across Tasks 1–10. `findingRec`/`summaryRefKey`/`newMemoryStore`/`normalizeTitle`/`WrapperToolKey` match the existing `internal/findings/findings.go` symbols. GitHub `Adapter` gains `gqlClient`/`gqlEndpoint`; `New` sets them and is the only constructor. Consistent.
