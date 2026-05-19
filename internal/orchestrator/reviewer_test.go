package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/findings"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

type fakeVCS struct {
	kind       vcs.Kind
	pr         *vcs.PullRequest
	files      []vcs.FileChange
	mu         sync.Mutex
	inlineCnt  int
	checks     []vcs.CheckRunStatus
	summary    string
	editedBody string
	resolved   []string
}

func (f *fakeVCS) Kind() vcs.Kind { return f.kind }
func (f *fakeVCS) FetchPullRequest(_ context.Context, _ string, _ int64) (*vcs.PullRequest, error) {
	return f.pr, nil
}
func (f *fakeVCS) ListChangedFiles(_ context.Context, _ *vcs.PullRequest) ([]vcs.FileChange, error) {
	return f.files, nil
}
func (f *fakeVCS) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, body string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary = body
	return "1", nil
}
func (f *fakeVCS) UpdateSummaryComment(_ context.Context, _ *vcs.PullRequest, _, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.summary = body
	return nil
}
func (f *fakeVCS) EditPullRequestBody(_ context.Context, _ *vcs.PullRequest, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editedBody = body
	return nil
}
func (f *fakeVCS) PostInlineComments(_ context.Context, _ *vcs.PullRequest, c []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inlineCnt += len(c)
	refs := make([]vcs.PostedInlineRef, len(c))
	for i, cc := range c {
		refs[i] = vcs.PostedInlineRef{Comment: cc}
	}
	return refs, nil
}
func (f *fakeVCS) ResolveThread(_ context.Context, _ *vcs.PullRequest, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, id)
	return nil
}
func (f *fakeVCS) UpsertCheckRun(_ context.Context, _ *vcs.PullRequest, run vcs.CheckRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, run.Status)
	return nil
}

type fakeLLM struct{ body string }

func (f *fakeLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: f.body}, nil
}

type capturingTool struct {
	name string
	res  *tools.Result
	got  *tools.Input
}

func (t *capturingTool) Name() string { return t.name }
func (t *capturingTool) Run(_ context.Context, in tools.Input) (*tools.Result, error) {
	t.got = &in
	return t.res, nil
}

func newDispatcher(reg *tools.Registry, pool map[vcs.Kind]vcs.Provider) *Dispatcher {
	return &Dispatcher{
		LLM:      &fakeLLM{},
		VCSPool:  pool,
		BaseCfg:  config.Default(),
		Registry: reg,
		Model:    "test-model",
	}
}

func TestDispatcherRoutesByName(t *testing.T) {
	fv := &fakeVCS{
		kind:  vcs.KindGitHub,
		pr:    &vcs.PullRequest{RepoFullName: "o/r", Number: 1, HeadSHA: "abc"},
		files: []vcs.FileChange{{Path: "x.go", Patch: "d"}},
	}
	tool := &capturingTool{name: "describe", res: &tools.Result{Summary: "hello"}}
	reg := tools.NewRegistry()
	reg.Register(tool)

	d := newDispatcher(reg, map[vcs.Kind]vcs.Provider{vcs.KindGitHub: fv})
	if err := d.Run(context.Background(), ToolJob{
		Tool: "describe", RepoFullName: "o/r", PRNumber: 1, Trigger: "opened",
	}); err != nil {
		t.Fatal(err)
	}
	if tool.got == nil {
		t.Fatal("tool was not invoked")
	}
	if fv.summary != "hello" {
		t.Errorf("summary: %q", fv.summary)
	}
}

func TestDispatcherUnknownTool(t *testing.T) {
	fv := &fakeVCS{kind: vcs.KindGitHub, pr: &vcs.PullRequest{}, files: []vcs.FileChange{}}
	d := newDispatcher(tools.NewRegistry(), map[vcs.Kind]vcs.Provider{vcs.KindGitHub: fv})
	if err := d.Run(context.Background(), ToolJob{Tool: "nope", RepoFullName: "o/r"}); err == nil {
		t.Fatal("expected unknown-tool error")
	}
}

func TestDispatcherUnknownProvider(t *testing.T) {
	fv := &fakeVCS{kind: vcs.KindGitHub, pr: &vcs.PullRequest{}, files: []vcs.FileChange{}}
	reg := tools.NewRegistry()
	reg.Register(&capturingTool{name: "review", res: &tools.Result{}})
	d := newDispatcher(reg, map[vcs.Kind]vcs.Provider{vcs.KindGitHub: fv})
	err := d.Run(context.Background(), ToolJob{Tool: "review", Provider: vcs.KindGitLab, RepoFullName: "o/r"})
	if err == nil {
		t.Fatal("expected no-adapter error")
	}
}

func TestDispatcherDefaultsProviderToGitHub(t *testing.T) {
	tool := &capturingTool{name: "review", res: &tools.Result{Summary: "ok"}}
	reg := tools.NewRegistry()
	reg.Register(tool)
	fv := &fakeVCS{kind: vcs.KindGitHub, pr: &vcs.PullRequest{RepoFullName: "o/r"}, files: []vcs.FileChange{}}
	d := newDispatcher(reg, map[vcs.Kind]vcs.Provider{vcs.KindGitHub: fv})
	if err := d.Run(context.Background(), ToolJob{RepoFullName: "o/r"}); err != nil {
		t.Fatal(err)
	}
	if tool.got == nil {
		t.Error("empty Tool/Provider should default to review on GitHub")
	}
}

func TestDispatcherDispatchesToCorrectProvider(t *testing.T) {
	gh := &fakeVCS{kind: vcs.KindGitHub, pr: &vcs.PullRequest{RepoFullName: "o/r"}, files: []vcs.FileChange{}}
	gl := &fakeVCS{kind: vcs.KindGitLab, pr: &vcs.PullRequest{RepoFullName: "g/p"}, files: []vcs.FileChange{}}
	reg := tools.NewRegistry()
	reg.Register(&capturingTool{name: "review", res: &tools.Result{Summary: "from gitlab"}})
	d := newDispatcher(reg, map[vcs.Kind]vcs.Provider{vcs.KindGitHub: gh, vcs.KindGitLab: gl})

	if err := d.Run(context.Background(), ToolJob{
		Tool: "review", Provider: vcs.KindGitLab, RepoFullName: "g/p",
	}); err != nil {
		t.Fatal(err)
	}
	if gl.summary != "from gitlab" {
		t.Errorf("expected gitlab adapter to receive summary, got gh=%q gl=%q", gh.summary, gl.summary)
	}
	if gh.summary != "" {
		t.Errorf("github adapter should not have been called, got %q", gh.summary)
	}
}

func TestDispatcherHandleDecodes(t *testing.T) {
	tool := &capturingTool{name: "review", res: &tools.Result{}}
	reg := tools.NewRegistry()
	reg.Register(tool)
	fv := &fakeVCS{kind: vcs.KindGitHub, pr: &vcs.PullRequest{RepoFullName: "o/r"}, files: []vcs.FileChange{}}
	d := newDispatcher(reg, map[vcs.Kind]vcs.Provider{vcs.KindGitHub: fv})
	payload, _ := json.Marshal(ToolJob{Tool: "review", RepoFullName: "o/r", PRNumber: 1})
	if err := d.Handle(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
}

// idVCS extends fakeVCS to return a non-empty external ID for each posted
// inline comment so the auto-resolve test has something to resolve against.
type idVCS struct {
	fakeVCS
	nextID int
}

func (f *idVCS) PostInlineComments(_ context.Context, _ *vcs.PullRequest, c []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inlineCnt += len(c)
	refs := make([]vcs.PostedInlineRef, len(c))
	for i, cc := range c {
		f.nextID++
		refs[i] = vcs.PostedInlineRef{Comment: cc, ExternalID: fmt.Sprintf("disc-%d", f.nextID)}
	}
	return refs, nil
}

// TestPostInlineKeepsDistinctSuggestionsInBatch is the !238 follow-up:
// two genuinely-different improve suggestions on the same file must both
// be posted, even though their bodies share the static "**Suggestions:**"
// header. An earlier fix keyed StructuralKey on just the first line and
// collapsed them to one — wrong. normalizeTitle now walks past the
// static header and into the action text so the keys differ.
func TestPostInlineKeepsDistinctSuggestionsInBatch(t *testing.T) {
	ctx := context.Background()
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 238, HeadSHA: "abc"}
	key := findings.PRKey{Provider: string(vcs.KindGitLab), RepoFullName: "g/p", PRNumber: 238}

	c1 := vcs.InlineComment{
		File: "internal/v1/api/run.go", LineStart: 302, LineEnd: 302,
		Body: "**Suggestions:**\n- Fail fast on Kafka producer init error when enabled\n\n```suggestion\nx := 1\n```",
	}
	c2 := vcs.InlineComment{
		File: "internal/v1/api/run.go", LineStart: 303, LineEnd: 303,
		Body: "**Suggestions:**\n- Reject invalid payload before queuing\n\n```suggestion\ny := 2\n```",
	}

	t.Run("cli path with nil Posted store", func(t *testing.T) {
		fv := &fakeVCS{kind: vcs.KindGitLab, pr: pr}
		d := &Dispatcher{VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: fv}}
		d.postInline(ctx, fv, pr, key, "improve", []vcs.InlineComment{c1, c2})
		if fv.inlineCnt != 2 {
			t.Fatalf("expected 2 distinct suggestions posted, got %d", fv.inlineCnt)
		}
	})

	t.Run("worker path with memory Posted store", func(t *testing.T) {
		fv := &fakeVCS{kind: vcs.KindGitLab, pr: pr}
		d := &Dispatcher{
			VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: fv},
			Posted:  findings.NewMemory(""),
		}
		d.postInline(ctx, fv, pr, key, "improve", []vcs.InlineComment{c1, c2})
		if fv.inlineCnt != 2 {
			t.Fatalf("expected 2 distinct suggestions posted, got %d", fv.inlineCnt)
		}
	})
}

// TestPostInlineCollapsesIdenticalDuplicatesInBatch covers the defensive
// case the intra-batch seenKeys check exists for: a tool emits the same
// finding twice in one run. Both rows pass HasFinding (neither recorded
// yet) so only the in-batch tracking can drop the second one.
func TestPostInlineCollapsesIdenticalDuplicatesInBatch(t *testing.T) {
	ctx := context.Background()
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 239, HeadSHA: "abc"}
	key := findings.PRKey{Provider: string(vcs.KindGitLab), RepoFullName: "g/p", PRNumber: 239}

	body := "**Suggestions:**\n- Fail fast on Kafka producer init error\n\n```suggestion\nx := 1\n```"
	dup := vcs.InlineComment{File: "internal/v1/api/run.go", LineStart: 302, LineEnd: 302, Body: body}

	fv := &fakeVCS{kind: vcs.KindGitLab, pr: pr}
	d := &Dispatcher{VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: fv}}
	d.postInline(ctx, fv, pr, key, "improve", []vcs.InlineComment{dup, dup})
	if fv.inlineCnt != 1 {
		t.Fatalf("expected 1 comment posted (intra-batch dedup of identical bodies), got %d", fv.inlineCnt)
	}
}

func TestPostInlineResolvesStalePriors(t *testing.T) {
	ctx := context.Background()
	fv := &idVCS{fakeVCS: fakeVCS{
		kind: vcs.KindGitLab,
		pr:   &vcs.PullRequest{RepoFullName: "g/p", Number: 1, HeadSHA: "abc"},
	}}
	d := &Dispatcher{
		VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: &fv.fakeVCS},
		Posted:  findings.NewMemory(""),
	}
	pr := fv.pr
	key := findings.PRKey{Provider: string(vcs.KindGitLab), RepoFullName: "g/p", PRNumber: 1}

	// Run 1: post two findings, both get IDs.
	first := []vcs.InlineComment{
		{File: "a.go", LineStart: 1, Severity: "warn", Body: "Stale finding A"},
		{File: "b.go", LineStart: 2, Severity: "warn", Body: "Surviving finding B"},
	}
	d.postInline(ctx, fv, pr, key, "review", first)

	// Manually backfill IDs the way the real adapter would — fakeVCS
	// returns empty IDs, so we re-record with synthetic IDs to simulate
	// what idVCS-style adapters do in production.
	for i, c := range first {
		_ = d.Posted.RecordFinding(ctx, key, "review", fmt.Sprintf("disc-%d", i+1), c)
	}

	// Run 2: only the surviving finding remains.
	second := []vcs.InlineComment{
		{File: "b.go", LineStart: 2, Severity: "warn", Body: "Surviving finding B"},
	}
	d.postInline(ctx, fv, pr, key, "review", second)

	if len(fv.resolved) != 1 {
		t.Fatalf("expected exactly 1 resolved thread, got %d: %v", len(fv.resolved), fv.resolved)
	}
	if fv.resolved[0] != "disc-1" {
		t.Errorf("expected disc-1 resolved (the stale prior), got %q", fv.resolved[0])
	}
}

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
	cv := &captureVCS{}
	d := &Dispatcher{Posted: findings.NewMemory("")}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}
	c := vcs.InlineComment{File: "a.go", Body: "Fix the leak.", Severity: vcs.SeverityWarn}

	d.postInline(ctx, cv, pr, key, "review", []vcs.InlineComment{c})

	if len(cv.sentBodies) != 1 {
		t.Fatalf("sent %d bodies; want 1", len(cv.sentBodies))
	}
	if _, _, ok := vcs.ParseInlineMarker(cv.sentBodies[0]); !ok {
		t.Errorf("wire body missing marker: %q", cv.sentBodies[0])
	}
	has, _ := d.Posted.HasFinding(ctx, key, "review", c)
	if !has {
		t.Error("pristine comment not recorded / HasFinding=false")
	}
}

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
			Tool:            md.Tool,
			File:            c.File,
			Severity:        md.Sev,
			StructuralKey:   md.SK,
			Title:           vcs.FirstLine(stripped),
			NormalizedTitle: md.NT,
			ExternalID:      fmt.Sprintf("T%d", i+1),
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

	// --- Run 1: fresh PR, empty prior store (legacy CI behaviour seed). ---
	d1 := &Dispatcher{Posted: findings.NewFromPrior(key, vcs.PriorReview{})}
	d1.postSummary(ctx, sv, pr, key, "review", "## Overview\nfirst pass")
	d1.postInline(ctx, sv, pr, key, "review", []vcs.InlineComment{c1, c2})

	if len(sv.inline) != 2 || sv.summaryID != "S1" {
		t.Fatalf("run1: inline=%d summaryID=%q; want 2, S1", len(sv.inline), sv.summaryID)
	}

	// --- Run 2: c1 persists, c2 fixed (absent), new c3. ---
	prior := sv.replay()
	sv.inline = nil // count only NEW posts in run 2
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
	if !strings.Contains(sv.summaryBody, "second pass") {
		t.Errorf("run2 summaryBody = %q; want it to contain \"second pass\"", sv.summaryBody)
	}
}

// TestCIModeSuppressesRephrasedImproveOnPush2 is the regression test for the
// CI-mode runaway duplication bug: "improve" suggestions were re-posted on
// every push because the seeded in-memory store only stored the first-line
// normalised title ("suggestions:"), which made the Jaccard fallback useless
// (intersection of 1 token vs many tokens is well below the 0.5 threshold).
//
// With the fix, StampInline embeds the full-body NT in the marker, ListCadoo-
// Artifacts parses it, and NewFromPrior seeds NormalizedTitle from md.NT so
// the Jaccard check can correctly catch rephrased suggestions.
func TestCIModeSuppressesRephrasedImproveOnPush2(t *testing.T) {
	ctx := context.Background()
	sv := &scenarioVCS{}
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 1}
	key := findings.PRKey{Provider: "gitlab", RepoFullName: "g/p", PRNumber: 1}

	// Push-1 improve suggestion.
	body1 := "**Suggestions:**\n- Fail fast on Kafka producer init error when enabled\n\n```suggestion\nx := 1\n```"
	c1 := vcs.InlineComment{File: "a.go", Body: body1, Severity: vcs.SeverityNit}

	d1 := &Dispatcher{Posted: findings.NewFromPrior(key, vcs.PriorReview{})}
	d1.postInline(ctx, sv, pr, key, "improve", []vcs.InlineComment{c1})
	if len(sv.inline) != 1 {
		t.Fatalf("push1: expected 1 inline, got %d", len(sv.inline))
	}

	// --- Push 2: same suggestion, LLM slightly rephrases the rationale. ---
	prior := sv.replay()
	sv.inline = nil

	// Rephrased body: same issue, slightly different wording.
	body2 := "**Suggestions:**\n- Fail fast on Kafka producer init error when it is enabled\n\n```suggestion\nx := 1\n```"
	c2 := vcs.InlineComment{File: "a.go", Body: body2, Severity: vcs.SeverityNit}

	d2 := &Dispatcher{Posted: findings.NewFromPrior(key, prior)}
	d2.postInline(ctx, sv, pr, key, "improve", []vcs.InlineComment{c2})

	if len(sv.inline) != 0 {
		t.Errorf("push2: rephrased improve suggestion was not deduped; got %d new posts (expected 0)", len(sv.inline))
	}
}
