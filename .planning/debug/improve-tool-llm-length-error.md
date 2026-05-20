---
slug: improve-tool-llm-length-error
status: resolved
trigger: manual
created: 2026-05-19
---

# Debug Session: improve-tool-llm-length-error

## Symptoms

CI mode ran three tools in sequence — describe, review, improve — and only `improve` failed:

```
ci: dispatching describe on paas/ai/development/ai-ml-backend!256
ci: dispatching review on paas/ai/development/ai-ml-backend!256
ci: dispatching improve on paas/ai/development/ai-ml-backend!256
ci: improve failed: run tool "improve": llm: empty completion (finish_reason="length") — gateway returned no content
ERROR: Job failed: exit code 1
```

Error: `llm: empty completion (finish_reason="length") — gateway returned no content`

## Current Focus

**Root cause identified** — see Resolution section.

## Evidence

- timestamp: 2026-05-19
  file: internal/tools/llmjson.go
  lines: 50-51
  note: >
    The error is emitted at the FIRST guard (resp.Content == ""), not the second
    (resp.FinishReason == "length"). This means the model returned an empty
    response body, and finish_reason happened to be "length". This indicates the
    INPUT prompt exceeded the model's context window — the model was given more
    tokens than it could process, so it returned nothing.

- timestamp: 2026-05-19
  file: internal/tools/llmjson.go
  lines: 14-30
  note: >
    All tools share a single maxTokens() for OUTPUT. The value is 8192 by default,
    overridable via CADOO_MAX_TOKENS. This is the OUTPUT budget only; it does not
    control input size.

- timestamp: 2026-05-19
  file: internal/orchestrator/reviewer.go
  lines: 186-191
  note: >
    The CONTEXT WINDOW for the diff itself is 50,000 tokens (d.maxTokens()),
    with per-file cap of 8,000 (d.perFileTokens()). This is the input diff token
    budget, separate from the output token budget.

- timestamp: 2026-05-19
  file: internal/orchestrator/reviewer.go
  lines: 228-249
  note: >
    PriorFindings are loaded from d.Posted.ListPostedFindings() BEFORE each tool
    call. In CI mode, d.Posted is reconstructed from prior artifacts. After
    describe and review run, their findings are recorded. When improve runs THIRD,
    it receives PriorFindings from both describe and review, which are appended
    to the user prompt by BuildDiffPrompt (lines 93-109 of prompt.go). This
    GROWS the prompt for each successive tool.

- timestamp: 2026-05-19
  file: internal/tools/prompt.go
  lines: 93-109
  note: >
    BuildDiffPrompt appends all PriorFindings as a section. In CI mode, improve
    runs third and receives the findings from describe (walkthrough items) and
    review (inline findings with severity + title). For a large PR, this section
    alone can be substantial. Combined with the full 50K-token diff, the total
    input can exceed the model's context window.

- timestamp: 2026-05-19
  file: cmd/cadoo-cli/ci.go
  lines: 184-188, 211-233
  note: >
    CI mode constructs a SINGLE Dispatcher and runs all tools sequentially via
    d.Run(). The dispatcher's d.Posted store accumulates findings across tool
    calls within a single pipeline run. Each tool call to d.Run() re-reads
    d.Posted.ListPostedFindings() which will include findings from ALL prior
    tools in this run plus any prior CI runs.

- timestamp: 2026-05-19
  file: internal/tools/improve/improve.go
  lines: 14-35
  note: >
    The improve tool's system prompt (236 chars) is smaller than review's
    (3,396 chars) and describe's (2,089 chars). The system prompt is NOT the
    cause. The difference is exclusively in what PriorFindings accumulate to.

## Root Cause

The `improve` tool fails with `finish_reason="length"` and empty content because:

1. **The input prompt exceeds the model's total context window.**

2. The CI pipeline runs three tools sequentially — describe, review, improve — using a single `Dispatcher` with a shared `d.Posted` findings store.

3. After `describe` and `review` complete, their inline findings are recorded in `d.Posted`. When `improve` runs third, `BuildDiffPrompt` appends the `PriorFindings` section (all findings from describe + review), growing the user-message significantly beyond what the first two tools sent.

4. The context packer (`contextengine.Compress`) budgets up to 50,000 tokens for the diff itself. The `PriorFindings` section is NOT counted in this budget — it's added to the prompt AFTER compression, with no size guard.

5. For this specific PR (paas/ai/development/ai-ml-backend!256), the diff was already close to the context window limit. The additional `PriorFindings` text pushed the total input over the model's context window, causing the model to return an empty response with `finish_reason="length"` (which in this case means the REQUEST itself was too large, not the output).

**Key discriminator between tools:** The error does not reflect a low `max_tokens` output cap. `resp.Content == ""` (empty content) with `finish_reason="length"` indicates the REQUEST hit the context window. `describe` and `review` ran on the same diff without prior findings (or with fewer). `improve` ran third and accumulated all prior findings in its prompt.

## Resolution

- root_cause: >
    The improve tool's user-message prompt grows unboundedly as PriorFindings
    from earlier tools in the same CI pipeline run are appended by BuildDiffPrompt.
    For large PRs whose diff already consumes most of the model's context window,
    the additional PriorFindings section pushes the total input past the context
    limit, causing the model to return empty content with finish_reason="length".

- fix: >
    Two complementary fixes are recommended:
    
    1. TOKEN-GUARD the PriorFindings section in BuildDiffPrompt (or the caller):
       estimate PriorFindings token cost and truncate/cap the list before appending.
       Since PriorFindings exists to suppress duplicate output, only the last N
       (e.g. 50) most recent findings are needed.
    
    2. REDUCE what improve sends as PriorFindings: the improve tool only posts
       inline suggestions, so it only needs to suppress duplicate suggestions
       from prior improve runs, not findings from describe/review. Filter
       PriorFindings by Tool == "improve" before appending, OR skip the
       PriorFindings section for improve entirely (it has no semantic need for
       review/describe findings).
    
    Fix 2 is lower risk and more targeted: in tools/improve/improve.go, strip
    non-improve prior findings before calling BuildDiffPrompt, or pass a nil
    PriorFindings in a copy of the Input.
