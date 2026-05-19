# Silent-when-clean (no-effective-change) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When post-LLM review finds nothing effective, Cadoo posts no `describe` overview / MR-body rewrite and no `improve` filler — only one consolidated `✅ Cadoo: no issues in this change` line plus a passing check-run.

**Architecture:** Extend the existing `config.CommentPolicy.SilentOnClean` mechanism (today only `review` honours it) to `describe` and `improve`; add a fail-safe `*bool` `substantive` signal to `describe`; make `applyResult` record each tool's consolidated section every run (empty clears a prior one); add one idempotent `Dispatcher.finalizeClean` that re-renders the consolidated comment from final section state (collapsing to the ack line when empty) and ensures a success check-run, gated to never run after a tool error.

**Tech Stack:** Go 1.26; builds on PR #6 (`internal/findings` section store, `internal/orchestrator/consolidate.go`, `resolveStalePriors`, `cmd/cadoo-cli/ci.go` `priorStore`).

**HARD DEPENDENCY:** Execute on a branch where **PR #6 (`worktree-ci-mode-stateless-dedup`) is merged or rebased on**. The symbols below (`findings.Store.PutSection/AllSections/SummaryID/PutSummaryID/WrapperToolKey`, `renderConsolidated`, post-#6 `applyResult`/`postSummary`, `ci.go` `priorStore`/`firstErr`) only exist after PR #6. First task verifies this.

**Spec:** `docs/superpowers/specs/2026-05-19-silent-when-clean-design.md`

---

## File Structure

- `internal/tools/describe/describe.go` (modify) — add `Substantive *bool` to `Output` + prompt; silent path.
- `internal/tools/describe/describe_test.go` (modify/create) — substantive nil/true/false × SilentOnClean.
- `internal/tools/improve/improve.go` (modify) — empty `Result` on 0 suggestions + SilentOnClean.
- `internal/tools/improve/improve_test.go` (modify/create) — clean vs suggestions.
- `internal/orchestrator/reviewer.go` (modify) — `applyResult` records section every run; new `finalizeClean`; call at end of `Dispatcher.Run`.
- `internal/orchestrator/reviewer_test.go` (modify) — section-clear, finalizeClean, two-run scenario.
- `cmd/cadoo-cli/ci.go` (modify) — call `finalizeClean` after the tool loop when `firstErr == nil`.
- `cmd/cadoo-cli/ci_test.go` (modify) — finalize wired only on success.
- `.cadoo.yaml.example` (modify) — document that `silent_on_clean` now also governs describe/improve + the ack.

---

### Task 1: Verify PR #6 baseline is present

**Files:** none (verification only)

- [ ] **Step 1: Confirm PR #6 symbols exist**

Run:
```bash
grep -n 'func (s \*Store) PutSection\|func (s \*Store) AllSections\|func (s \*Store) SummaryID\|func (s \*Store) PutSummaryID\|WrapperToolKey' internal/findings/findings.go
grep -n 'func renderConsolidated\|wrapperBegin' internal/orchestrator/consolidate.go
grep -n 'func (d \*Dispatcher) applyResult\|func (d \*Dispatcher) postSummary\|func (d \*Dispatcher) resolveStalePriors\|CheckRunName\|ReportStatus' internal/orchestrator/reviewer.go
grep -n 'priorStore\|firstErr' cmd/cadoo-cli/ci.go
```
Expected: every pattern matches (PR #6 present). If any is missing, **STOP** — this plan cannot be executed until PR #6 is merged/rebased on; report BLOCKED.

- [ ] **Step 2: Confirm clean baseline**

Run: `go build ./... && go test ./internal/tools/... ./internal/orchestrator/... ./cmd/cadoo-cli/... -count=1 2>&1 | tail -3`
Expected: build OK, all pass.

---

### Task 2: `describe` — add fail-safe `Substantive *bool` + silent path

**Files:**
- Modify: `internal/tools/describe/describe.go` (`systemPrompt`, `Output`, `Run`)
- Test: `internal/tools/describe/describe_test.go`

- [ ] **Step 1: Write the failing test**

Create/append `internal/tools/describe/describe_test.go`:

```go
package describe

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

type stubLLM struct{ json string }

func (s stubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: s.json, FinishReason: "stop"}, nil
}

func runDescribe(t *testing.T, body string, silent bool) *tools.Result {
	t.Helper()
	cfg := config.Default()
	cfg.CommentPolicy.SilentOnClean = silent
	in := tools.Input{
		LLM:    stubLLM{json: body},
		Model:  "m",
		Config: cfg,
		PR:     &vcs.PullRequest{RepoFullName: "g/p", Number: 1},
		Files:  []vcs.FileChange{{Path: "a.txt"}},
	}
	res, err := Tool{}.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestDescribeSilentWhenExplicitlyNonSubstantive(t *testing.T) {
	res := runDescribe(t, `{"title":"t","intent":"i","type":"Docs","changes":[],"risks":[],"walkthrough":[],"substantive":false}`, true)
	if res.EditPRBody != nil || res.Summary != "" {
		t.Errorf("non-substantive + SilentOnClean: want empty Result, got EditPRBody=%v Summary=%q", res.EditPRBody, res.Summary)
	}
}

func TestDescribeNotSilentWhenSubstantiveTrue(t *testing.T) {
	res := runDescribe(t, `{"title":"t","intent":"i","type":"Enhancement","changes":["x"],"risks":[],"walkthrough":[],"substantive":true}`, true)
	if res.EditPRBody == nil || res.Summary == "" {
		t.Errorf("substantive=true: want overview posted, got EditPRBody=%v Summary=%q", res.EditPRBody, res.Summary)
	}
}

func TestDescribeNotSilentWhenSubstantiveMissing(t *testing.T) {
	// Field absent → *bool nil → fail-safe noisy (post as today).
	res := runDescribe(t, `{"title":"t","intent":"i","type":"Docs","changes":[],"risks":[],"walkthrough":[]}`, true)
	if res.EditPRBody == nil || res.Summary == "" {
		t.Errorf("substantive missing: want overview posted (fail-safe), got EditPRBody=%v Summary=%q", res.EditPRBody, res.Summary)
	}
}

func TestDescribeNotSilentWhenPolicyDisabled(t *testing.T) {
	res := runDescribe(t, `{"title":"t","intent":"i","type":"Docs","changes":[],"risks":[],"walkthrough":[],"substantive":false}`, false)
	if res.EditPRBody == nil || res.Summary == "" {
		t.Errorf("SilentOnClean=false: want overview posted, got EditPRBody=%v Summary=%q", res.EditPRBody, res.Summary)
	}
}
```

(If `config.Default()` / `tools.Input` field names differ, read `internal/config/config.go` and `internal/tools/tools.go` and adapt the test setup to the real constructor/fields — intent: build an `Input` with a stub LLM returning the JSON and `CommentPolicy.SilentOnClean` toggled. Report any adaptation.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/describe/ -run TestDescribe -v 2>&1 | tail -20`
Expected: FAIL — `Output` has no `Substantive`; `Run` always returns `EditPRBody`+`Summary`.

- [ ] **Step 3: Implement**

In `internal/tools/describe/describe.go`:

(3a) In `systemPrompt`, add the field to the JSON object (after `"intent"`) and a rule. Replace the `"intent": ...` line and the `Rules:` block by inserting:

```
  "substantive": <true|false>,
```
immediately after the `"intent": "..."` line in the JSON template, and add this bullet to the `Rules:` list:

```
- substantive: true if any change alters behaviour, logic, output, config effect, or meaning; false ONLY if the diff has no effective change (pure rename/reformat/whitespace, comment or log-text reword, dead-code move, docs-only). This is a semantic judgment, NOT about diff size — a large pure-rename is false; a one-line logic change is true. When unsure, return true.
```

(3b) Add the field to `Output` (after `Intent`):

```go
	Substantive *bool             `json:"substantive"`
```

(3c) Replace the tail of `Run` (from `body := buildSection(out, in.Files, true)` to the `return &tools.Result{...}` ) with:

```go
	if in.Config.CommentPolicy.SilentOnClean && out.Substantive != nil && !*out.Substantive {
		// No effective change and policy is noise-averse: post nothing.
		// The consolidated ack + check-run are handled by the orchestrator's
		// finalizeClean once review/improve also come back empty.
		return &tools.Result{}, nil
	}
	body := buildSection(out, in.Files, true)
	comment := buildSection(out, in.Files, false)
	return &tools.Result{
		EditPRBody: &body,
		Summary:    comment,
	}, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/describe/ -run TestDescribe -v 2>&1 | tail -15`
Expected: PASS (4 tests).

- [ ] **Step 5: Run package + lint**

Run: `go test ./internal/tools/describe/ -count=1 && go vet ./internal/tools/describe/ && golangci-lint run ./internal/tools/describe/ 2>&1 | tail -3`
Expected: all pass/clean (existing describe tests unregressed).

- [ ] **Step 6: Commit**

```bash
git add internal/tools/describe/describe.go internal/tools/describe/describe_test.go
git commit -m "feat(describe): fail-safe substantive flag; silent on no-effective-change"
```

---

### Task 3: `improve` — empty Result on zero suggestions + SilentOnClean

**Files:**
- Modify: `internal/tools/improve/improve.go` (`Run`)
- Test: `internal/tools/improve/improve_test.go`

- [ ] **Step 1: Write the failing test**

Create/append `internal/tools/improve/improve_test.go`:

```go
package improve

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

type stubLLM struct{ json string }

func (s stubLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: s.json, FinishReason: "stop"}, nil
}

func run(t *testing.T, body string, silent bool) *tools.Result {
	t.Helper()
	cfg := config.Default()
	cfg.CommentPolicy.SilentOnClean = silent
	res, err := Tool{}.Run(context.Background(), tools.Input{
		LLM: stubLLM{json: body}, Model: "m", Config: cfg,
		PR:    &vcs.PullRequest{RepoFullName: "g/p", Number: 1},
		Files: []vcs.FileChange{{Path: "a.go"}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func TestImproveSilentWhenNoSuggestions(t *testing.T) {
	res := run(t, `{"summary":"","suggestions":[]}`, true)
	if res.Summary != "" || len(res.InlineComments) != 0 {
		t.Errorf("0 suggestions + SilentOnClean: want empty Result, got Summary=%q inline=%d", res.Summary, len(res.InlineComments))
	}
}

func TestImproveNotSilentWhenPolicyDisabled(t *testing.T) {
	res := run(t, `{"summary":"","suggestions":[]}`, false)
	if res.Summary == "" {
		t.Errorf("SilentOnClean=false: want the existing 'no improvements' summary, got empty")
	}
}

func TestImprovePostsSuggestions(t *testing.T) {
	res := run(t, `{"summary":"s","suggestions":[{"path":"a.go","line":1,"action":"do x","code":"y"}]}`, true)
	if len(res.InlineComments) == 0 {
		t.Errorf("suggestions present: want inline comments, got none")
	}
}
```

(Adapt the suggestion JSON shape to the real `improve` `Output`/`Suggestion` struct — read `internal/tools/improve/improve.go`. Intent unchanged: 0 suggestions+silent → empty; policy off → existing text; suggestions → inline.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/improve/ -run TestImprove -v 2>&1 | tail -15`
Expected: FAIL — `TestImproveSilentWhenNoSuggestions` fails (today returns `"No high-leverage improvements found in this diff."`).

- [ ] **Step 3: Implement**

In `internal/tools/improve/improve.go` `Run`, replace the final `return &tools.Result{Summary: buildSection(out), InlineComments: inlines}, nil` with:

```go
	if in.Config.CommentPolicy.SilentOnClean && len(out.Suggestions) == 0 {
		return &tools.Result{}, nil
	}
	return &tools.Result{Summary: buildSection(out), InlineComments: inlines}, nil
```

(`buildSection`/`out`/`inlines` are unchanged. If `Run` doesn't have `in` in scope as `tools.Input` or the var names differ, read the function and adapt to the real names; report.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/improve/ -run TestImprove -v 2>&1 | tail -10`
Expected: PASS (3 tests).

- [ ] **Step 5: Package + lint**

Run: `go test ./internal/tools/improve/ -count=1 && go vet ./internal/tools/improve/ && golangci-lint run ./internal/tools/improve/ 2>&1 | tail -3`
Expected: pass/clean.

- [ ] **Step 6: Commit**

```bash
git add internal/tools/improve/improve.go internal/tools/improve/improve_test.go
git commit -m "feat(improve): silent (empty Result) on zero suggestions when SilentOnClean"
```

---

### Task 4: `applyResult` records each tool's section every run (empty clears prior)

**Files:**
- Modify: `internal/orchestrator/reviewer.go` (`applyResult`)
- Test: `internal/orchestrator/reviewer_test.go`

**Why:** post-PR-#6 `applyResult` calls `postSummary` only when `res.Summary != ""`. A tool going silent then leaves its *prior* consolidated section uncleared (stale `improve`/`describe` from an earlier dirty push). The fix: when a run's tool is silent, still clear that tool's stored section so the consolidated state reflects the latest run.

- [ ] **Step 1: Write the failing test**

Append to `internal/orchestrator/reviewer_test.go`:

```go
func TestApplyResultClearsSectionWhenToolSilent(t *testing.T) {
	ctx := context.Background()
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	d := &Dispatcher{Posted: findings.NewMemory("")}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}

	// Prior run: improve produced a section.
	d.applyResult(ctx, &recordVCS{}, pr, "improve", &tools.Result{Summary: "OLD improve section"})
	if got, _ := d.Posted.AllSections(ctx, key); len(got) == 0 {
		t.Fatal("precondition: improve section should be recorded")
	}
	// Next run: improve is silent (empty Result) → its section must be cleared.
	d.applyResult(ctx, &recordVCS{}, pr, "improve", &tools.Result{})
	secs, _ := d.Posted.AllSections(ctx, key)
	for _, s := range secs {
		if s.Tool == "improve" && s.Body != "" {
			t.Errorf("improve section not cleared on silent run: %q", s.Body)
		}
	}
}
```

Add this minimal fake near the other test fakes if not already present (a no-op provider whose `PostSummaryComment`/`UpdateSummaryComment` succeed):

```go
type recordVCS struct{ idVCS }

func (recordVCS) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, _ string) (string, error) {
	return "S1", nil
}
func (recordVCS) UpdateSummaryComment(_ context.Context, _ *vcs.PullRequest, _, _ string) error {
	return nil
}
```

(If `idVCS` already provides those, just use `idVCS` directly and drop `recordVCS`. Read the existing test fakes first.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/ -run TestApplyResultClearsSection -v 2>&1 | tail -15`
Expected: FAIL — silent run leaves the prior `improve` section in `AllSections`.

- [ ] **Step 3: Implement**

In `internal/orchestrator/reviewer.go` `applyResult`, find the existing summary block:

```go
	if res.Summary != "" {
		d.postSummary(ctx, provider, pr, key, tool, res.Summary)
	}
```

Replace it with:

```go
	if res.Summary != "" {
		d.postSummary(ctx, provider, pr, key, tool, res.Summary)
	} else if tool != "" && d.Posted != nil && d.Posted.Enabled() {
		// Tool was silent this run: clear any prior consolidated section
		// for this tool so the stored state reflects the latest run.
		// The comment itself is reconciled by finalizeClean (Task 5).
		_ = d.Posted.PutSection(ctx, key, tool, "")
	}
```

(Leave the `InlineComments`, `CheckRun`/`CheckRuns`, `EditPRBody` blocks unchanged.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/orchestrator/ -run TestApplyResultClearsSection -v 2>&1 | tail -8`
Expected: PASS.

- [ ] **Step 5: Full orchestrator suite (no PR #6 regression)**

Run: `go test ./internal/orchestrator/ -count=1 2>&1 | tail -5`
Expected: all pass — including PR #6's `TestCIModeTwoRunIdempotency`, `TestPostInline*`, `TestRenderConsolidated*`.

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/reviewer.go internal/orchestrator/reviewer_test.go
git commit -m "fix(orchestrator): clear a tool's stored section when it is silent"
```

---

### Task 5: `Dispatcher.finalizeClean` — reconcile consolidated comment + success check

**Files:**
- Modify: `internal/orchestrator/reviewer.go` (new method `finalizeClean`)
- Test: `internal/orchestrator/reviewer_test.go`

**Contract:** `finalizeClean` is the authoritative end-of-run comment reconcile. Callers MUST only invoke it when **no tool errored this run** (callers gate; never call after a tool error). Given that:
- Read `AllSections`. If **all sections empty/absent** (clean run) → upsert the consolidated comment to exactly `✅ Cadoo: no issues in this change`; if gating is on (`d.ReportStatus`), upsert a `success` check-run under `CheckRunName`.
- If some section is non-empty (not clean) → re-render via `renderConsolidated` and upsert (this drops sections cleared after an earlier tool already posted — fixes the stale-section ordering case). No ack, no forced success check.
- Idempotent: edits in place via the existing `WrapperToolKey` summary-ID; calling twice yields one comment.

- [ ] **Step 1: Write the failing test**

Append to `internal/orchestrator/reviewer_test.go`:

```go
func TestFinalizeCleanPostsAckWhenAllEmpty(t *testing.T) {
	ctx := context.Background()
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	sv := &finalizeVCS{}
	d := &Dispatcher{Posted: findings.NewMemory(""), ReportStatus: true}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}

	d.FinalizeClean(ctx, sv, pr, key)

	if sv.summaryBody != "✅ Cadoo: no issues in this change" {
		t.Errorf("ack body = %q; want the clean ack line", sv.summaryBody)
	}
	if sv.checkStatus != vcs.CheckSucceeded {
		t.Errorf("check status = %q; want success", sv.checkStatus)
	}
	// Idempotent: second call must not create a second comment.
	calls := sv.summaryCalls
	d.FinalizeClean(ctx, sv, pr, key)
	if sv.summaryCalls != calls && !sv.lastWasUpdate {
		t.Errorf("finalizeClean not idempotent: extra create call")
	}
}

func TestFinalizeCleanNoAckWhenSectionPresent(t *testing.T) {
	ctx := context.Background()
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	sv := &finalizeVCS{}
	d := &Dispatcher{Posted: findings.NewMemory(""), ReportStatus: true}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}
	_ = d.Posted.PutSection(ctx, key, "review", "## Findings\n- real issue")

	d.FinalizeClean(ctx, sv, pr, key)

	if sv.summaryBody == "✅ Cadoo: no issues in this change" {
		t.Error("must NOT post the clean ack when a substantive section exists")
	}
	if sv.checkForcedSuccess {
		t.Error("must NOT force a success check when not clean")
	}
}
```

Add this fake near the others:

```go
type finalizeVCS struct {
	idVCS
	summaryID          string
	summaryBody        string
	summaryCalls       int
	lastWasUpdate      bool
	checkStatus        vcs.CheckRunStatus
	checkForcedSuccess bool
}

func (f *finalizeVCS) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, body string) (string, error) {
	f.summaryCalls++
	f.summaryID, f.summaryBody, f.lastWasUpdate = "S1", body, false
	return f.summaryID, nil
}
func (f *finalizeVCS) UpdateSummaryComment(_ context.Context, _ *vcs.PullRequest, _, body string) error {
	f.summaryBody, f.lastWasUpdate = body, true
	return nil
}
func (f *finalizeVCS) UpsertCheckRun(_ context.Context, _ *vcs.PullRequest, r vcs.CheckRun) error {
	f.checkStatus = r.Status
	if r.Status == vcs.CheckSucceeded {
		f.checkForcedSuccess = true
	}
	return nil
}
```

(If `idVCS` lacks a needed method to satisfy `vcs.Provider`, embed whatever the existing full fake is — read the test file first.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/ -run TestFinalizeClean -v 2>&1 | tail -15`
Expected: FAIL — `d.finalizeClean` undefined.

- [ ] **Step 3: Implement**

Add to `internal/orchestrator/reviewer.go` (near `postSummary`):

```go
const cleanAckLine = "✅ Cadoo: no issues in this change"

// FinalizeClean reconciles the consolidated comment from the final per-tool
// section state at the end of a run. Callers MUST only call this when no
// tool errored this run. When every section is empty the comment collapses
// to a single ack line and (if status reporting is on) a success check-run
// is ensured; otherwise the comment is re-rendered from the surviving
// sections (dropping any cleared after an earlier tool posted). Idempotent:
// edits the existing wrapper comment in place. Exported because cmd/cadoo-cli
// calls it after the CI tool loop.
func (d *Dispatcher) FinalizeClean(ctx context.Context, provider vcs.Provider, pr *vcs.PullRequest, key findings.PRKey) {
	if d.Posted == nil || !d.Posted.Enabled() {
		return
	}
	sections, err := d.Posted.AllSections(ctx, key)
	if err != nil {
		slog.Debug("finalizeClean: all sections", "err", err)
		return
	}
	substantive := false
	for _, s := range sections {
		if s.Body != "" {
			substantive = true
			break
		}
	}

	body := cleanAckLine
	if substantive {
		body = renderConsolidated(sections)
	}

	existing, _ := d.Posted.SummaryID(ctx, key, findings.WrapperToolKey)
	if existing != "" {
		if err := provider.UpdateSummaryComment(ctx, pr, existing, body); err != nil {
			slog.Debug("finalizeClean: update summary", "err", err)
		}
	} else if !substantive {
		// Clean from the start: no prior wrapper comment — create the ack.
		id, perr := provider.PostSummaryComment(ctx, pr, body)
		if perr != nil {
			slog.Error("finalizeClean: post ack", "err", perr, "pr", pr.URL)
			return
		}
		if id != "" {
			_ = d.Posted.PutSummaryID(ctx, key, findings.WrapperToolKey, id)
		}
	}

	if !substantive && d.ReportStatus {
		if err := provider.UpsertCheckRun(ctx, pr, vcs.CheckRun{
			Name:    CheckRunName,
			Status:  vcs.CheckSucceeded,
			Title:   "no issues",
			Summary: cleanAckLine,
		}); err != nil {
			slog.Debug("finalizeClean: check run", "err", err)
		}
	}
}
```

(`slog`, `renderConsolidated`, `CheckRunName`, `findings.WrapperToolKey`, `vcs.CheckSucceeded` all already exist post-PR-#6. If `renderConsolidated` is unexported in another file of the same package it is still in scope — confirm with `grep -n 'func renderConsolidated' internal/orchestrator/consolidate.go`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/orchestrator/ -run TestFinalizeClean -v 2>&1 | tail -12`
Expected: PASS (2 tests).

- [ ] **Step 5: Full orchestrator suite**

Run: `go test ./internal/orchestrator/ -count=1 2>&1 | tail -5`
Expected: all pass (PR #6 tests unregressed; `finalizeClean` isn't called by them yet).

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/reviewer.go internal/orchestrator/reviewer_test.go
git commit -m "feat(orchestrator): finalizeClean reconciles consolidated comment + ack/check"
```

---

### Task 6: Wire `finalizeClean` into `Dispatcher.Run` (webhook/worker, idempotent)

**Files:**
- Modify: `internal/orchestrator/reviewer.go` (`Dispatcher.Run`)
- Test: `internal/orchestrator/reviewer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/orchestrator/reviewer_test.go`:

```go
func TestDispatcherRunCallsFinalizeOnSuccessNotOnError(t *testing.T) {
	ctx := context.Background()
	// Success path: a silent tool run → finalize posts the ack.
	sv := &finalizeVCS{}
	d := &Dispatcher{
		Posted:   findings.NewMemory(""),
		VCSPool:  map[vcs.Kind]vcs.Provider{vcs.KindGitLab: sv},
		Registry: registryWith(t, "improve", &tools.Result{}), // tool returns empty, no error
		LLM:      fakeLLM{body: "{}"},
	}
	job := ToolJob{Provider: vcs.KindGitLab, Tool: "improve", RepoFullName: "g/p", PRNumber: 1, Trigger: "ci"}
	if err := d.Run(ctx, job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sv.summaryBody != "✅ Cadoo: no issues in this change" {
		t.Errorf("expected ack after successful silent run, got %q", sv.summaryBody)
	}
}
```

(`registryWith` is a small test helper that returns a `Registry` whose named tool yields the given `*tools.Result`. If the existing test file already has a registry/tool fake pattern — it does, used by `TestDispatcherRoutesByName` — reuse that exact pattern instead of `registryWith`; read the file and adapt. The assertion that matters: a successful silent `d.Run` results in the ack via `finalizeClean`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrator/ -run TestDispatcherRunCallsFinalize -v 2>&1 | tail -15`
Expected: FAIL — `Run` does not call `FinalizeClean`, so no ack posted.

- [ ] **Step 3: Implement**

In `internal/orchestrator/reviewer.go` `Dispatcher.Run`, locate where it applies the tool result and returns nil on success (after `applyResult`). Immediately before the successful `return nil`, add the finalize call so it runs only on the success path (never after an error return):

```go
	d.FinalizeClean(ctx, provider, pr, key)
	return nil
```

Place it after the existing `if err := d.applyResult(...); err != nil { ... return ... }` and any check-run handling, at the end of the successful path. Do NOT add it before/at any `return err` (the safety rule: finalize must not run after a tool error). `provider`, `pr`, `key` are already in scope at that point in `Run` (same values passed to `applyResult`). If `key` is constructed inside `applyResult` only, construct it in `Run` the same way `applyResult` does (`findings.PRKey{Provider: string(provider.Kind()), RepoFullName: pr.RepoFullName, PRNumber: pr.Number}`) and pass it.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/orchestrator/ -run TestDispatcherRunCallsFinalize -v 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Full orchestrator suite (critical regression gate)**

Run: `go test ./internal/orchestrator/ -count=1 2>&1 | tail -6`
Expected: ALL pass. In particular PR #6's `TestCIModeTwoRunIdempotency` must still pass — `finalizeClean` running at the end of a dirty run re-renders the *same* consolidated content (sections non-empty → `renderConsolidated`, same wrapper id) so the asserted `UpdateSummaryComment` behaviour holds. If that test now fails, STOP and report (do not weaken it) — it means finalize's not-clean re-render diverges from PR #6's `postSummary` output; reconcile by ensuring `finalizeClean` uses the same `renderConsolidated(sections)` PR #6 uses.

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/reviewer.go internal/orchestrator/reviewer_test.go
git commit -m "feat(orchestrator): call finalizeClean at end of successful Dispatcher.Run"
```

---

### Task 7: Wire `finalizeClean` into CI-mode after the tool loop (only when `firstErr == nil`)

**Files:**
- Modify: `cmd/cadoo-cli/ci.go` (`ciCmd`)
- Test: `cmd/cadoo-cli/ci_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/cadoo-cli/ci_test.go`:

```go
func TestCIFinalizeHelperGatedOnNoError(t *testing.T) {
	// finalizeAfterRun must invoke finalizeClean only when firstErr == nil.
	called := false
	fn := func() { called = true }

	finalizeAfterRun(nil, fn)
	if !called {
		t.Error("firstErr==nil: finalize should run")
	}
	called = false
	finalizeAfterRun(errSentinel, fn)
	if called {
		t.Error("firstErr!=nil: finalize must NOT run (no '✅ no issues' on a failed review)")
	}
}

var errSentinel = errorString("boom")

type errorString string

func (e errorString) Error() string { return string(e) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/cadoo-cli/ -run TestCIFinalizeHelperGatedOnNoError -v 2>&1 | tail -10`
Expected: FAIL — `finalizeAfterRun` undefined.

- [ ] **Step 3: Implement**

In `cmd/cadoo-cli/ci.go` add the tiny gate helper (file scope, near `priorStore`):

```go
// finalizeAfterRun runs fn only when the CI run had no tool error, so a
// failed review never collapses to a "no issues" ack / green check.
func finalizeAfterRun(firstErr error, fn func()) {
	if firstErr == nil {
		fn()
	}
}
```

Then, in `ciCmd`, immediately AFTER the `for _, name := range toolList { ... }` loop and BEFORE the `if firstErr != nil { os.Exit(1) }` line, add:

```go
	finalizeAfterRun(firstErr, func() {
		if rr, ok := provider.(vcs.PriorReviewReader); ok {
			_ = rr // provider already used for read-back; finalize uses d.Posted
		}
		key := findings.PRKey{
			Provider:     string(target.Provider),
			RepoFullName: target.ProjectPath,
			PRNumber:     target.Number,
		}
		d.FinalizeClean(ctx, provider, &vcs.PullRequest{
			Provider: target.Provider, RepoFullName: target.ProjectPath, Number: target.Number,
		}, key)
	})
```

`FinalizeClean` is already exported (Task 5), so no rename is needed — `cmd/cadoo-cli` (package `main`) can call `d.FinalizeClean(...)` directly. Ensure `cmd/cadoo-cli/ci.go` imports `github.com/payamqorbanpour/cadoo/internal/findings` and `github.com/payamqorbanpour/cadoo/internal/vcs` (both already imported after PR #6's `priorStore` wiring — verify; add to the cadoo-internal import group if missing). `ctx`, `d`, `provider`, `target` are already in scope at that point in `ciCmd`.

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
go test ./cmd/cadoo-cli/ -run TestCIFinalizeHelperGatedOnNoError -v 2>&1 | tail -8
go build ./... 2>&1 | tail -2
go test ./internal/orchestrator/ ./cmd/cadoo-cli/ -count=1 2>&1 | tail -4
```
Expected: helper test PASS; build OK; orchestrator + cli suites all pass.

- [ ] **Step 5: Lint**

Run: `go vet ./cmd/cadoo-cli/ ./internal/orchestrator/ && golangci-lint run ./cmd/cadoo-cli/ ./internal/orchestrator/ 2>&1 | tail -3`
Expected: clean (`FinalizeClean` has a docstring).

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/reviewer.go internal/orchestrator/reviewer_test.go cmd/cadoo-cli/ci.go cmd/cadoo-cli/ci_test.go
git commit -m "feat(cli): run FinalizeClean after CI tool loop only on success"
```

---

### Task 8: Two-run scenario — dirty→clean collapse + resolve; clean→dirty overwrite

**Files:**
- Modify: `internal/orchestrator/reviewer_test.go`

- [ ] **Step 1: Write the failing test**

Append:

```go
func TestSilentWhenCleanTwoRunScenario(t *testing.T) {
	ctx := context.Background()
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	sv := &finalizeVCS{}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}

	// Run 1 (dirty): review posts a section + an inline finding.
	d1 := &Dispatcher{Posted: findings.NewFromPrior(key, vcs.PriorReview{}), ReportStatus: true,
		VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: sv}}
	d1.applyResult(ctx, sv, pr, "review", &tools.Result{Summary: "## Findings\n- bug"})
	d1.FinalizeClean(ctx, sv, pr, key)
	if sv.summaryBody == "✅ Cadoo: no issues in this change" {
		t.Fatal("run1 dirty: must not be the ack")
	}

	// Run 2 (clean): replay prior state; review now silent.
	prior := vcs.PriorReview{SummaryCommentID: sv.summaryID}
	d2 := &Dispatcher{Posted: findings.NewFromPrior(key, prior), ReportStatus: true,
		VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: sv}}
	d2.applyResult(ctx, sv, pr, "review", &tools.Result{}) // silent → clears section
	d2.FinalizeClean(ctx, sv, pr, key)

	if sv.summaryBody != "✅ Cadoo: no issues in this change" {
		t.Errorf("run2 clean: overview should collapse to ack, got %q", sv.summaryBody)
	}
	if !sv.lastWasUpdate {
		t.Error("run2: ack should EDIT the existing comment in place, not create a new one")
	}
	if sv.checkStatus != vcs.CheckSucceeded {
		t.Errorf("run2 clean: check = %q; want success", sv.checkStatus)
	}
}
```

(If `findings.NewFromPrior` signature differs, use whatever PR #6 provides to seed the store with a prior `SummaryCommentID`; intent: run2 must find the existing wrapper id and `UpdateSummaryComment` it to the ack.)

- [ ] **Step 2: Run to verify it fails (or passes if Tasks 4–6 complete)**

Run: `go test ./internal/orchestrator/ -run TestSilentWhenCleanTwoRunScenario -v 2>&1 | tail -15`
Expected: With Tasks 4–6 done it should PASS. If it FAILS, fix the offending task's code (do NOT weaken assertions); if it reveals a real ordering bug in `finalizeClean`/`applyResult`, STOP and report with the failure + diagnosis.

- [ ] **Step 3: Full suite**

Run: `go test ./internal/orchestrator/ -count=1 2>&1 | tail -4`
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/reviewer_test.go
git commit -m "test(orchestrator): silent-when-clean two-run scenario (collapse + resolve + check)"
```

---

### Task 9: Docs + full CI gate

**Files:**
- Modify: `.cadoo.yaml.example`

- [ ] **Step 1: Update `.cadoo.yaml.example`**

Find the `comment_policy:` block and the `silent_on_clean: true` line. Replace the inline comment on that line with:

```
  silent_on_clean: true        # clean run posts only a green check-run + one "✅ Cadoo: no issues" line; also suppresses describe overview / MR-body rewrite and improve filler when the change has no effective content
```

(Only edit that comment text; do not change the key or other lines.)

- [ ] **Step 2: Full gate**

Run:
```bash
go vet ./... 2>&1 | tail -2
go test ./... 2>&1 | tail -3
golangci-lint run ./... 2>&1 | tail -3
go build ./... 2>&1 | tail -1
```
Expected: vet clean; all tests pass (PR #6/#7 + new tests); lint clean; build OK. If any pre-existing-PR test fails, STOP and report (do not mass-fix).

- [ ] **Step 3: Commit**

```bash
git add .cadoo.yaml.example
git commit -m "docs: document silent_on_clean now governs describe/improve + ack"
```

---

## Self-Review

**Spec coverage:**
- §1 `substantive` `*bool` fail-safe + describe silent path → Task 2. improve silent → Task 3. review unchanged → noted (no task; Task 1 baseline). `applyResult` records section every run / clears prior → Task 4.
- §2 `finalizeClean` (state-derived, idempotent, ack + success check, no-op-on-error via caller gating) → Tasks 5–7.
- §3 re-push transitions (dirty→clean collapse + `resolveStalePriors` reused unchanged; clean→dirty overwrite; idempotent) → Task 8; branch-protection success check → Task 5; `CheckRunName` reuse, no new name → Task 5 (uses existing constant).
- §4 edge cases: error → caller-gated no-op (Tasks 6,7); substantive=true (Task 2 `TestDescribeNotSilentWhenSubstantiveTrue`); tool subset (state-derived `AllSections`, Task 5); `SilentOnClean:false` (Tasks 2,3 policy tests); substantive missing → noisy (Task 2 `TestDescribeNotSilentWhenSubstantiveMissing`); below-threshold/nit (review unchanged — its existing check-only path leaves no section → finalize treats as clean, covered by Task 5 all-empty logic); webhook/worker async (Task 6 documents via caller-gating). 
- §5 testing → Tasks 2–8; regression gate → Task 9.

No gaps. (Below-threshold/nit-only: review already returns check-run-only with no `Summary`, so `applyResult` Task-4 path clears/leaves no review section → `finalizeClean` correctly treats as clean → ack. This is the desired noise-averse behaviour and is exercised by Task 5's all-empty test.)

**Placeholder scan:** no TBD/TODO; every code step has complete code; every command has an expected result. Adaptation notes ("if X differs, read Y and adapt, report") are explicit fallbacks, not placeholders — the primary code is concrete.

**Type consistency:** `Output.Substantive *bool` (Task 2) used consistently. `tools.Result{}` empty contract (Tasks 2,3,4). `d.Posted.PutSection(ctx,key,tool,string)` / `AllSections(ctx,key) []findings.Section` with `.Tool`/`.Body` (Tasks 4,5,8) — matches PR #6. `FinalizeClean(ctx, provider, *vcs.PullRequest, findings.PRKey)` is **exported from the start** (Task 5 defines `func (d *Dispatcher) FinalizeClean`), and used identically in Tasks 5 (defined), 6 (called in `Dispatcher.Run`), 7 (called from `cmd/cadoo-cli` package `main`), 8 (called in scenario test). No rename anywhere — consistent throughout. `cleanAckLine`/`✅ Cadoo: no issues in this change` literal identical in Tasks 5,8 and the `finalizeVCS` assertions. `CheckRunName`, `vcs.CheckSucceeded`, `findings.WrapperToolKey`, `renderConsolidated` are PR #6/existing symbols (Task 1 verifies).
