# Silent-when-clean (no-effective-change) — design

- **Date:** 2026-05-19
- **Status:** Approved (design); implementation plan to follow
- **Owner:** Payam Qorbanpour
- **Depends on:** PR #6 (CI-mode stateless dedup — consolidate wrapper, `findings.Store` read-back, `resolveStalePriors`). Benefits from PR #7 (clear LLM errors for the safety rule). Implement after PR #6 merges, or rebase onto it.

## Problem

On a merge request whose changes are real but **not effective** (renames, reformatting, comment/log-text rewording, dead-code moves, docs-only — *not* limited to literal whitespace), Cadoo still posts noise. Verified current behaviour (unchanged by PR #6/#7):

- `review` — already noise-averse: `config.CommentPolicy` defaults to `SilentOnClean: true`, so zero post-threshold findings → returns **only** a green check-run, no comment. Not the source of noise.
- `describe` (`describe.go:104-107`) — **unconditionally** returns `EditPRBody` + `Summary`: it rewrites the MR description and posts an overview on every run, never consults `CommentPolicy`, has no notion of "no effective change".
- `improve` (`improve.go` `buildSection`) — returns `"No high-leverage improvements found in this diff."` when there are zero suggestions → a non-empty `Summary` → posts a section.
- There is no single consolidated "Cadoo ran, nothing to say" acknowledgement.

So on a non-effective MR the author still sees a rewritten description, an overview comment, and an "improve" filler line. The desired behaviour — *be silent when, after real review, there is genuinely nothing worth saying* — is the "Silent when clean" option explicitly deferred in the CI-mode dedup spec.

"Not effective" is a **semantic** judgment (the model must read the change and conclude it alters nothing meaningful); it is **not** a diff-size or whitespace heuristic. A 400-line pure rename is as silent as a one-space README edit; a one-line logic change is not.

## Requirements (from brainstorming decisions)

- **Scope of silence:** fully silent — no review/improve filler, no describe overview, **no MR-description rewrite** — but keep one minimal acknowledgement.
- **Ack location:** a passing check-run **and** one minimal consolidated comment line (`✅ Cadoo: no issues in this change`), edited in place across re-pushes via the PR #6 wrapper.
- **Detection:** **post-LLM** — all enabled tools run normally (full LLM review); suppression is decided from their structured outputs, never from a pre-LLM heuristic.
- **Clean predicate:** a run is clean iff every tool that ran produced no substantive output: `review` 0 post-threshold findings **and** `improve` 0 suggestions **and** `describe` explicitly `substantive: false`. `describe` can veto silence.

## Approach (selected: "per-tool empty + consolidated finalization")

Extend the **existing** `config.CommentPolicy` / `SilentOnClean` mechanism to `describe` and `improve` (today only `review` honours it), add a `substantive` signal to `describe`, and add **one** new small cross-tool finalization that posts/maintains the consolidated ack line + ensures the success check-run. Everything else reuses existing infrastructure (PR #6 consolidate wrapper, `findings.Store`, `resolveStalePriors`, `applyResult` gating, the existing `CheckRunName` check-run).

Rejected: render-time-only collapse (emergent, hard to test; and nothing triggers `postSummary` when all sections are empty, so a clean-from-start MR would never get the ack). CI-mode-only (diverges self-host/SaaS code paths, violating the single-code-path rule).

## Detailed design

### 1. The `substantive` signal + per-tool "empty" contract

**`describe` schema/prompt change.** Add one field to `describe`'s JSON contract and `Output`:

```
"substantive": <true|false>
```

`Output.Substantive` is a **`*bool`** (not a bare `bool`). Default-safety rule: `nil` (field absent / model non-compliant) **or** `true` → treated as **substantive ⇒ not clean**. Cadoo goes silent only on an explicit `"substantive": false`. Fail-safe is *noisy*, never silent-by-accident (a bare `bool` would zero-value to `false` and wrongly silence real changes — explicitly disallowed).

Prompt guidance states this is a *semantic* judgment: changes exist but none alter behaviour/meaning ⇒ `false`; any behavioural/semantic effect ⇒ `true`. Not a size/whitespace heuristic.

**Per-tool empty contract** (each tool decides its own emptiness from its LLM output, consulting the existing `in.Config.CommentPolicy`):

- `review` — **unchanged**. Already returns check-run-only when zero post-threshold findings and `SilentOnClean` (and likewise for below-`MinFindingsToPost` / nit-only via `shouldSuppress`).
- `improve` — when `len(out.Suggestions) == 0` and `SilentOnClean`, return an **empty `tools.Result`** (no `Summary`) instead of the `"No high-leverage improvements found"` filler. Mirrors review's existing pattern.
- `describe` — when `out.Substantive != nil && *out.Substantive == false` and `SilentOnClean`, return a `tools.Result` with **no `EditPRBody` and no `Summary`**. Otherwise unchanged (its veto is implicit: it posts its overview → the run is not clean).

**Silent ⇒ clear the prior section (required for dirty→clean).** `applyResult` today skips `postSummary` when `Summary == ""`, which means a tool going silent would leave its *prior* consolidated section (from an earlier dirty push) **uncleared** in PR #6's per-tool section store — the stale `improve`/`describe` section would persist and `finalizeClean` would wrongly see a non-empty section. Therefore the contract is: when a run's tool is silent, the orchestrator must **explicitly clear that tool's stored section** (PR #6 per-tool section store is last-write-wins per `(PR, tool)`; writing an empty section removes it). Concretely, `applyResult` records the tool's section **every run** — an empty string clears it — instead of skipping the store write when `Summary == ""`. Inline-comment and PR-body suppression are unchanged (still gated by `len(InlineComments) > 0` / `EditPRBody != nil`); only the *section store write* becomes unconditional so silence reliably erases a prior section. This keeps the consolidated state a faithful reflection of the **latest** run per tool.

### 2. Consolidated clean-run finalization

A new `Dispatcher.finalizeClean(ctx, provider, pr, key)`. It does **not** re-run tools; it derives state from PR #6's `findings.Store`/consolidate aggregation for this PR.

- **CI-mode (`ci.go`):** called once **after the tool loop**, only when `firstErr == nil`.
- **webhook/worker:** called at the end of each `Dispatcher.Run`, **idempotent** — it makes the consolidated comment reflect current stored state. A later tool adding a real section is overwritten by normal `postSummary`; if all stay empty the ack persists. No global barrier needed (decision is state-derived, not in-process run order).

"Clean" is judged from the **latest run's** per-tool outputs, not prior-push state. This is well-defined because the per-tool section store is last-write-wins per `(PR, tool)` and silent tools now explicitly clear their section (§1) — so "zero substantive sections in the consolidated state" exactly means "the latest run produced nothing", never a stale prior section. The prior wrapper-comment ID (for edit-in-place collapse) still comes from PR #6's store/read-back; that is *prior state used only to locate the comment to edit*, distinct from the cleanliness decision.

When the PR has **zero substantive sections** after this run (every tool that ran returned empty / cleared its section) **and no tool errored**:

1. Upsert the consolidated comment via the existing PR #6 wrapper (same `findings.WrapperToolKey` summary-ID, edit-in-place) to exactly: `✅ Cadoo: no issues in this change`.
2. Ensure a `success` check-run exists under the **existing `CheckRunName`** (no new check name). Reuse: if `review` ran it already posted the green check (unchanged). `finalizeClean` emits one itself only when gating is on (`d.ReportStatus`) and no success check was already posted — so a `review`-less `--tools` run still satisfies branch protection.

**Hard safety rule:** if *any* tool errored (e.g. LLM gateway unreachable — cf. PR #7), `finalizeClean` is a **no-op**; the existing `failCheck` (`CheckFailed`, exit 1) stands. Cadoo must never post "✅ no issues" / green-check an MR it failed to actually review. "Clean" = *reviewed and genuinely empty*, never *could not review*.

### 3. Re-push transitions, dedup interaction & branch protection

All via existing PR #6 machinery:

| Push N → N+1 | Behaviour |
|---|---|
| dirty → clean | `resolveStalePriors` (PR #6, unchanged) resolves now-absent inline finding threads. `finalizeClean` upserts the existing consolidated comment (same summary-ID) → **collapses to** the ack line. No stale overview remains. |
| clean → dirty | A tool produces a real section → normal `postSummary` overwrites the ack line in place; inline posted. `finalizeClean` no-op. |
| clean → clean | `finalizeClean` upserts the same ack line in place — idempotent, no duplicate. |
| dirty → dirty | Unchanged from PR #6. |

**Branch protection:** clean run yields `success` under the existing `CheckRunName` instead of a missing/failed check; gating governed entirely by the existing `d.ReportStatus`; when gating off, behaviour unchanged (no check-run at all).

**Pre-existing caveat (not fixed here):** doc/code naming discrepancy — code uses `CheckRunName = "cadoo"`; `CLAUDE.md` documents `cadoo/review` as the branch-protection name. Predates this feature. Design rule: reuse exactly what `review`/`failCheck` already emit; do not introduce a new check name. Operators must point branch protection at whatever name Cadoo currently posts — flagged verification item, out of scope.

### 4. Edge cases

- *Any tool errored* → `finalizeClean` no-op; red `failCheck`; exit 1. Never "✅" an un-reviewed MR.
- *`substantive: true`, review 0, improve 0* → not clean; describe posts overview; finalize no-op (= today minus review/improve filler).
- *Tool subset* (`--tools`) → "clean = zero substantive sections among tools that ran", state-derived; generalises to review-only / describe-only / improve-only.
- *`SilentOnClean: false`* → entire ack behaviour disabled; existing chatty / `StatsOnClean` paths unchanged. Feature wholly gated by the existing policy.
- *`substantive` missing/garbled* → `*bool` nil ⇒ treated substantive ⇒ not silenced (fail-safe noisy).
- *Below-`MinFindingsToPost` / nit-only* → review already returns check-only; counts as clean → ack (desirable; matches noise-averse intent).
- *webhook/worker async ordering* → ack is eventually-consistent; if a later separate tool job errors after an earlier job posted the ack, the **check-run gate correctly goes red** (gate = source of truth; comment is informational). Accepted, documented limitation. **CI-mode (the primary use case) is exact** (synchronous; finalize-after-loop with `firstErr == nil`).
- *Empty diff / no files* → naturally clean → ack.

### 5. Testing

- **Unit:** `describe.Output.Substantive` `*bool` nil/true/false × `SilentOnClean` → empty `Result` only on explicit `false`+`SilentOnClean`; `improve` 0-suggestions + `SilentOnClean` → empty `Result`; `review` unchanged (existing tests stay green).
- **Orchestrator:** `applyResult` writes the per-tool section every run (empty clears it) — a previously non-empty `improve`/`describe` section is **removed** when that tool is silent on the next run. `finalizeClean` — zero sections + no error → ack via wrapper + success check; a substantive section → no-op; error flagged → no-op (no ack, no success); idempotent (called twice → one comment).
- **Scenario (PR #6-style two-run):** dirty→clean: prior inline threads resolved + consolidated comment collapses to ack + success check; clean→dirty: ack overwritten by real comment.
- **Config & subset matrix:** `SilentOnClean` true/false; `--tools` = describe-only / review-only / improve-only; `substantive` missing → not silenced.
- **CI-mode integration:** `ci.go` finalize runs only when `firstErr == nil`; tool-error path → no ack, exit 1.
- **Regression gate:** `go vet ./...`, `go test ./...`, `golangci-lint run ./...`, `go build ./...` all green; PR #6/#7 tests unregressed.

## Out of scope

- Pre-LLM trivial-diff heuristics / short-circuit (explicitly rejected: detection is post-LLM).
- Changing the check-run name or fixing the `cadoo` vs `cadoo/review` doc/code naming discrepancy.
- Any change to `review`'s existing `SilentOnClean`/`shouldSuppress` behaviour.
- KB/learnings behaviour.
