# Convergent CI-mode review (dedup that reaches a fixed point)

**Date:** 2026-06-14
**Status:** Approved design, pending implementation plan
**Scope:** `cadoo-cli` CI-mode (`cadoo ci --mr <url>` / `--pr <url>`), stateless dedup path

## Problem

On a re-reviewed GitLab MR, each pipeline re-run posts *more* review threads than it
resolves, so the total thread count ratchets upward without bound (observed: 39 → 45 →
…). Resolving the open threads and pushing does not converge — a fresh batch of
near-duplicate threads reappears. The review never reaches a fixed point.

This is the **CI-mode** path: stateless, no database. Dedup state is reconstructed every
run by reading Cadoo's own prior comments back off the MR (`vcs.PriorReviewReader` →
`gitlab.ListCadooArtifacts`, seeded into an in-memory `findings.Store` via
`findings.NewFromPrior`).

## Root cause

Three independent gaps compound:

1. **Self-resolving bug (`reviewer.go:491`).** `resolveStalePriors` rebuilds each prior
   finding's `StructuralKey` from `p.Title` — which is only the *first line* of the body
   (`firstLine`, stored at `findings.go:184`). The current run's keys are built from the
   *full body* (`reviewer.go:481`). For any multi-line comment (`improve` suggestions,
   most `review` findings) `normalizeTitle(firstLine) ≠ normalizeTitle(fullBody)`, so a
   still-valid finding looks "stale" and Cadoo resolves its own thread every run. This is
   why ~37/39 threads show as resolved-by-Cadoo.

2. **Heuristic, prose-derived dedup identity.** A finding's identity (`StructuralKey`,
   `findings.go:108`) hashes `tool + file + severity + normalizeTitle(body)`; matching is
   *exact key OR Jaccard ≥ 0.5* on that normalized prose (`findings.go:152-155`). LLM
   non-determinism guarantees that some findings get reworded past the 0.5 token-overlap
   bar each run, so they escape dedup and post as new threads. Nothing prunes or caps, so
   leakage accumulates forever.

3. **Thread state is captured but ignored.** `gitlab.go:263` records `Resolved:
   n.Resolved` per thread, but `findings.NewFromPrior` (`prior.go:34-53`) drops the
   field, and read-back never captures the **anchor line**. A user resolving a thread
   therefore carries zero suppression weight — a reworded version of the same finding
   sails right back in.

## Design

Three coordinated changes. A and B stop the runaway loop; C is a structural ceiling on
growth. Built in this order so each is independently shippable and verifiable.

### Part A — Carry `StructuralKey` end-to-end (fix self-resolution)

Stop reconstructing the key from a lossy first line; thread the real key through.

- Add `StructuralKey string` to `findings.PostedFinding`.
- Extend `ListPostedFindings` to select `structural_key` (DB column already exists;
  CI read-back already has it in `pi.StructuralKey` from the `sk=` marker).
- `findings.NewFromPrior` already has `pi.StructuralKey` — populate
  `PostedFinding.StructuralKey` from it.
- In `resolveStalePriors`, compare `p.StructuralKey` directly against `currentKeys`
  instead of recomputing from `p.Title`.

**Effect:** Cadoo stops resolving its own still-valid multi-line threads.

### Part B — Thread state as durable memory (honor resolves)

Make resolution and location first-class dedup inputs.

- **Capture anchor line.** Add `Line int` (and optionally `EndLine`) to `vcs.PriorInline`;
  populate from `n.Position.NewLine` in `ListCadooArtifacts`. Plumb through
  `NewFromPrior` into the seeded `findingRec` (add a `Line` field).
- **Capture resolved flag in the store.** `NewFromPrior` reads `pi.Resolved` (already on
  `PriorInline`) into the seeded record.
- **Sticky suppression for resolved findings.** Extend `memoryStore.has` so that a new
  comment is suppressed when, for the same `(tool, file)`:
  - it matches an *open* prior by the existing rule (exact `StructuralKey` or Jaccard ≥
    `SimilarTitleThreshold`), **or**
  - it matches a *resolved* prior by a widened rule: line-range overlap with the resolved
    thread's anchor, **or** Jaccard ≥ a lower `ResolvedSuppressThreshold` (e.g. 0.3).

**Effect:** once a thread is resolved (by the user or by Cadoo), that finding stays gone
even if the LLM rewords it. Directly kills the "resolve 2 → 6 come back" loop.

**Guardrail:** widened suppression is scoped to `(tool, file)` and (for the line rule) to
overlapping lines, so it cannot hide a *genuinely new, different* finding elsewhere in the
same file.

### Part C — Incremental review (structural ceiling on growth)

Only re-review code that changed since Cadoo last reviewed.

- **Persist last-reviewed SHA.** Embed a marker in the summary wrapper comment, e.g.
  `<!-- cadoo:reviewed-sha:<head-sha> -->`. Add `LastReviewedSHA string` to
  `vcs.PriorReview`; parse it in `ListCadooArtifacts` from the summary note. Write it into
  the summary body when posting/editing the overview.
- **Compute the incremental change set.** When `LastReviewedSHA` is present and is an
  ancestor of the current head, fetch the `lastReviewedSHA..head` diff (new provider
  capability, e.g. `DiffBetween(ctx, pr, oldSHA, newSHA)`; GitLab compare API,
  GitHub compare API). The result is the set of files + hunks/lines touched since the last
  review.
- **Feed inline tools the incremental view.** Tools that emit inline comments (`review`,
  `improve`, security, etc.) receive a `tools.Input` whose files/packed context are
  filtered to the incremental change set. Summary tools (`describe`, `changelog`) keep the
  **full** PR view. This implies either:
  - a classification of tools into inline-emitting vs summary-emitting, or
  - carrying both a full and an incremental context on `tools.Input` and letting each tool
    select (preferred — avoids a brittle registry-wide classification).
- **Fallbacks.** First run (no prior SHA) → full review. `LastReviewedSHA` not reachable
  from head (force-push / rebase) → full review. Empty incremental diff → no inline tools
  run, summary refreshed only.

**Effect:** a push touching 3 lines cannot generate new threads on untouched code.
Pre-existing threads persist, so coverage from earlier full runs is not lost.

### Critical interaction: `resolveStalePriors` under incremental review

Under incremental review, findings on *unchanged* code are intentionally **not
regenerated** this run. The current `resolveStalePriors` would then see those priors
missing from `currentKeys` and resolve them all — re-introducing churn.

**Rule:** `resolveStalePriors` must only consider a prior "resolvable" when its anchor
line falls **inside the incremental change set** for this run. Threads anchored to
untouched code are neither re-posted nor resolved — they simply persist. (This requires
Part B's captured anchor line.) On a full run (no prior SHA / force-push fallback), the
change set is the entire diff, so behavior matches today's full-review semantics.

## Data / marker changes

| Artifact | Change |
|---|---|
| `vcs.PriorInline` | add `Line int` (and `EndLine int`), populated from `n.Position` |
| `vcs.PriorReview` | add `LastReviewedSHA string` |
| `findings.PostedFinding` | add `StructuralKey string` |
| `findings.findingRec` | add `Line int`, `Resolved bool` |
| Summary wrapper body | embed `<!-- cadoo:reviewed-sha:<sha> -->` |
| Inline marker | **unchanged** — `sk=` / `nt=` already sufficient |
| `vcs.Provider` | add `DiffBetween(ctx, pr, oldSHA, newSHA)` capability (GitLab + GitHub) |

No new DB migration: this is the CI-mode (memory store) path. The DB-backed worker path
is unaffected by Parts B/C but **benefits from Part A** (the `resolveStalePriors` fix and
`StructuralKey` on `PostedFinding` apply to both backends).

## Testing strategy

- **Part A:** unit test `resolveStalePriors` with a multi-line `improve`-style body —
  assert the still-present finding's thread is *not* resolved (regression test for the
  first-line bug).
- **Part B:** `memoryStore.has` table tests — resolved prior + reworded new finding
  (suppressed); resolved prior + unrelated new finding in same file (NOT suppressed);
  line-overlap suppression.
- **Part C:** orchestrator test driving two runs against a fake provider — run 1 full
  review posts N threads and stamps `reviewed-sha`; run 2 with an unchanged head posts
  **zero** new threads and resolves **zero** existing ones (fixed-point test). Run 2 with
  a 1-line change posts at most findings for that line.
- **End-to-end convergence:** simulated resync loop — assert thread count is monotonic
  non-increasing once code stops changing.

## Out of scope (YAGNI)

- DB schema changes / new migrations (CI-mode is memory-backed).
- Lowering LLM temperature or other model-determinism tuning — addressed structurally by
  C, not worth coupling here.
- Code-content-hash finding identity (hashing the flagged source span instead of prose).
  A stronger identity, but a larger change; Parts A–C make it unnecessary for the
  reported loop. Revisit only if leakage persists after C.
- GitHub/GHES behavioral parity testing beyond the shared `DiffBetween` capability — the
  reported bug is GitLab CI-mode; GitHub inherits Parts A/C generically.

## Open implementation questions (for the plan)

1. Tool classification vs dual-context on `tools.Input` for Part C — recommend
   dual-context (full + incremental) so tools opt in without a central registry edit.
2. Exact `ResolvedSuppressThreshold` value and line-overlap tolerance — start at 0.3 /
   exact-line-range overlap; make both constants for tuning.
3. `DiffBetween` reachability check — how to detect a non-ancestor `LastReviewedSHA`
   cheaply per provider before falling back to full review.
