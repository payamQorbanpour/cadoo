# CI-mode stateless dedup — design

- **Date:** 2026-05-19
- **Status:** Approved (design); implementation plan to follow
- **Owner:** Payam Qorbanpour

## Problem

When Cadoo runs in **CI-mode** (`cadoo ci --mr <url>` on GitLab, `cadoo ci --pr <url>` on
GitHub), every pipeline run is a fresh, stateless process. `cmd/cadoo-cli/ci.go` constructs
the `orchestrator.Dispatcher` with **no `Posted` field** ("Stateless dispatcher: no DB, no
audit, no KB"), so `d.Posted == nil`. As a result:

- `postSummary` (reviewer.go) hits the legacy path (`tool == "" || d.Posted == nil`) and
  **posts a brand-new overview comment on every push**, never editing the prior one.
- `postInline` never calls `HasFinding` (no store), so **every finding is re-posted on
  every push**, including exact duplicates.
- `resolveStalePriors` has no priors and **never resolves threads** for findings that the
  author has since fixed.

Net effect for the user: each push to a merge request accumulates another overview comment
and a fresh copy of every inline finding. This is not a dedup *bug* — the dedup layer is
bypassed by design in stateless CI-mode.

The DB-backed path (webhook + worker with `DATABASE_URL`) already behaves correctly via
`internal/findings.Store`; CI-mode is the gap.

## Goal / behavioral contract

After any number of pushes to the same PR/MR, Cadoo's footprint must be:

- **Exactly one overview comment**, edited in place on each run.
- **Inline comments only for new findings.** A finding that persists across pushes keeps
  its existing thread; it is not re-posted.
- **Threads for fixed findings are resolved/collapsed.**

This is identical to the DB-backed behavior. The design goal is therefore a *stateless
equivalent of `findings.Store` whose state is reconstructed by reading the PR/MR itself*,
rather than a parallel dedup implementation.

**Scope:** both GitLab (`--mr`) and GitHub (`--pr`) CI-mode, including full GitHub
thread auto-resolution via GraphQL (decided: full parity, not deferred).

## Approach (selected: "MR-as-state-store")

Rebuild an in-memory `findings.Store` from the PR's own prior Cadoo artifacts. The PR/MR
is the single source of truth — no external cache, no state blob.

Rejected alternatives:

- **Single hidden state-blob comment** — desyncs from reality (a human deleting an inline
  comment leaves the blob claiming it was posted → never re-posted; silent false negative).
  This is the exact failure class already being fought. Also adds an ugly housekeeping
  comment, fragile to manual edits.
- **Derive state from rendered comment text (no markers)** — rendered markdown ≠ original
  `c.Body`, so `normalizeTitle`/`StructuralKey` mismatch; tool/severity often
  unrecoverable. High false-positive/negative risk.

## Detailed design

### 1. Provider capability interface

Add an **optional capability** (same pattern as the existing `FileFetcher`), implemented by
the GitLab and GitHub adapters, returning a *normalized* snapshot (not provider types) so
the dedup brain stays provider-agnostic. The orchestrator and tools continue to depend only
on `vcs.Provider`; this interface is type-asserted where needed.

```go
// vcs.go — optional; adapters that can enumerate Cadoo's prior artifacts
// implement this so stateless CI-mode can rebuild dedup state from the PR.
type PriorReviewReader interface {
    ListCadooArtifacts(ctx context.Context, pr *PullRequest) (PriorReview, error)
}

type PriorReview struct {
    SummaryCommentID string        // overview comment/note id (found via wrapper marker); "" if none
    Inline           []PriorInline
}

type PriorInline struct {
    Tool          string
    File          string
    Severity      string
    StructuralKey string // parsed from the hidden marker in the comment body
    Title         string // first visible line; for the Jaccard-similarity fallback
    ExternalID    string // discussion/thread id for ResolveThread; "" if unrecoverable
    Resolved      bool   // already resolved upstream — don't re-resolve
}
```

### 2. Hidden marker (the dedup anchor)

When Cadoo posts an inline comment, append a machine marker to the **outgoing wire body
only** — same idiom as the existing `consolidate.go` wrapper HTML comments, invisible in
rendered markdown:

```
<!-- cadoo:fp v=1 tool=review sk=9f3a1c2b7d4e5061 sev=warn -->
```

- Injected in one provider-agnostic place: `postInline` in `reviewer.go`, immediately
  before `provider.PostInlineComments(ctx, pr, delta)`, via a helper
  `findings.StampInline(tool, c) string`.
- `sk` is exactly `findings.StructuralKey(tool, c)`. Read-back recovers the *same* key the
  dedup logic compares against — no re-derivation from rendered text.
- The **overview** needs no new marker: `renderConsolidated` already wraps it in
  `wrapperBegin`/`wrapperEnd`. Read-back finds the comment containing `wrapperBegin` and
  uses its ID as `SummaryCommentID`.

**Correctness rule — the marker must never poison the keys.** `StructuralKey`,
`Fingerprint`, and `RecordFinding` all hash `c.Body`. The marker is appended only to a
separate `stampedBody` sent over the wire; `c.Body` stays pristine for every key
computation and for `RecordFinding`. On read-back the `sk` is parsed *from the marker*
(authoritative), not recomputed from rendered text. This keeps stateless dedup
byte-identical to DB-mode dedup. `Title` (first visible line) is unaffected because the
marker is appended last, so the existing `resolveStalePriors` re-derivation lands the same
value it does in DB-mode.

DB-mode is unchanged except that posted inline bodies now also carry the invisible marker —
a free self-healing upgrade for that path too.

### 3. Per-provider read-back

**GitLab `ListCadooArtifacts`:** `Discussions.ListMergeRequestDiscussions` (+ `Notes` for
the overview), paginated. Filter to artifacts whose body contains a `<!-- cadoo:… -->`
marker (token/author-agnostic — robust even if the CI token's user differs run-to-run).
Per inline discussion: parse marker → `Tool`/`StructuralKey`/`Severity`; GitLab discussion
ID → `ExternalID`; discussion resolved flag → `Resolved`. Overview note → `SummaryCommentID`.

**GitHub `ListCadooArtifacts`:** one paginated GraphQL query over
`pullRequest.comments` (overview, matched by `wrapperBegin`) **and**
`pullRequest.reviewThreads { id isResolved comments(first:1){ body path } }` (inline).
Thread node `id` → `ExternalID`; `isResolved` → `Resolved`; first comment body → marker
parse. (The REST create-path returns no per-comment IDs, but resolve only ever acts on
read-back priors, which carry GraphQL thread node IDs — consistent.)

**GitHub GraphQL (no new dependency):** reuse the existing `ghinstallation`-authenticated
transport via a small raw GraphQL POST helper (avoids pulling in `githubv4`, consistent
with the project's lean-deps style). Derive the GraphQL endpoint from the adapter's
existing base URL so GHES works (`<host>/api/graphql`). `ResolveThread` is rewritten to
`mutation { resolveReviewThread(input:{threadId:$id}) }` using the thread node ID.

### 4. State reconstruction + CI-mode wiring

New constructor in `internal/findings`:

```go
// NewFromPrior builds an in-memory Store (no disk path) pre-populated from a
// PR's own prior Cadoo artifacts, so stateless CI-mode reuses the exact
// HasFinding / ListPostedFindings / SummaryID dedup logic.
func NewFromPrior(key PRKey, pr vcs.PriorReview) *Store
```

It builds the existing `memoryStore` (no persist path) and seeds it so:

- `HasFinding(key, tool, c)` → true when `StructuralKey(tool,c)` matches a prior
  `StructuralKey` (plus the existing Jaccard-title fallback against `Title`) → inline
  dedup works unchanged.
- `ListPostedFindings(key)` → returns the priors with their `ExternalCommentID` →
  `resolveStalePriors` resolves fixed threads unchanged.
- `SummaryID(key, WrapperToolKey)` → returns `pr.SummaryCommentID` → `postSummary` takes
  the edit-in-place branch unchanged.
- `Enabled()` → true.

**Zero changes to `postSummary` / `postInline` / `resolveStalePriors`.** They already do
the right thing when `d.Posted` is a populated, enabled store. Only the backing data source
changes (PR contents instead of Postgres).

**Wiring** (`cmd/cadoo-cli/ci.go`, `ciCmd`): after `buildProvider`, before the tool loop,
type-assert the provider for `vcs.PriorReviewReader`; if present, call
`ListCadooArtifacts` **once**, build `findings.NewFromPrior(...)`, set `d.Posted = store`.
The same in-process store is shared across the `describe→review→improve` loop, so
within-run consolidation (`PutSection`/`AllSections`) and intra-run dedup also work, and
`RecordFinding` keeps the store current so `review` won't duplicate what `describe` just
posted in the same run. CI-mode already builds `findings.PRKey{Provider, RepoFullName,
PRNumber}` inside `applyResult`; `NewFromPrior` keys on the same tuple — no plumbing change.

### 5. Failure handling — degrade, never crash the build

- `ListCadooArtifacts` error → log `warn`, leave `d.Posted = nil`, proceed exactly as
  today (non-idempotent for that run only).
- Resolve errors → log `debug` (matches existing `resolveStalePriors` handling).
- Partial pagination failure → use what was retrieved; worst case a missed dedup re-posts;
  never a crash.

### 6. Edge cases

- Overview comment deleted by a human → `SummaryCommentID=""` → `postSummary` posts a
  fresh one (self-heals).
- Inline comment deleted by a human → absent from read-back → re-posted (correct; the PR
  is the source of truth — no blob desync).
- Marker absent/malformed → treated as non-Cadoo, skipped.
- GitLab unanchored notes (empty `ExternalID`) → still dedup via marker (no re-post); can't
  be resolved — same limitation as today.
- Token scope: read-back needs the same `api`/PR scope already required to *post*; no new
  permission. **Known risk:** GitLab `CI_JOB_TOKEN` is often insufficient for the notes API;
  users already hitting this for posting must use a project access token / PAT — unchanged
  by this work, documented as a prerequisite.
- No DB migration — entirely stateless path; DB-mode behavior unchanged (besides the
  benign embedded marker).

### 7. Testing

- Unit: `StampInline` stamp/parse round-trip; `NewFromPrior` seeding →
  `HasFinding`/`ListPostedFindings`/`SummaryID` assertions; marker never alters
  `StructuralKey`/`Fingerprint`.
- Adapter: GitLab `ListCadooArtifacts` against `httptest` fixtures; GitHub GraphQL
  query + `resolveReviewThread` mutation against `httptest` canned JSON; GHES endpoint
  derivation.
- Scenario (`ci_test.go`): two consecutive runs — assert run 2 posts only new findings,
  *edits* (not re-creates) the overview, and resolves a now-absent finding.
- Regression: all existing test packages stay green; CI migration up→down→up unaffected
  (no schema change).

## Out of scope

- Changing DB-mode behavior (only gains the embedded marker).
- KB / learnings in CI-mode (remains stateless by design).
- "Silent when clean" mode (post nothing on a zero-new-findings push) — explicitly not
  chosen; the overview is still refreshed on every run.
