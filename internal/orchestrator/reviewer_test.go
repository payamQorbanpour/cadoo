package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestPostInlineCollapsesIntraBatchDuplicates covers the !238 incident:
// the improve tool renders every body with the static "**Suggestions:**"
// header, so two suggestions in the same file share a StructuralKey. Without
// intra-batch tracking both pass HasFinding (neither is recorded yet) and
// both get posted. The CLI path (Posted == nil) is the worst case because
// the cross-batch dedup branch is skipped entirely.
func TestPostInlineCollapsesIntraBatchDuplicates(t *testing.T) {
	ctx := context.Background()
	pr := &vcs.PullRequest{RepoFullName: "g/p", Number: 238, HeadSHA: "abc"}
	key := findings.PRKey{Provider: string(vcs.KindGitLab), RepoFullName: "g/p", PRNumber: 238}

	// Mirrors improve.go renderSuggestionBody output: identical "**Suggestions:**"
	// first line, different bullet text and suggestion code per comment.
	c1 := vcs.InlineComment{
		File: "internal/v1/api/run.go", LineStart: 302, LineEnd: 302,
		Body: "**Suggestions:**\n- Fail fast on Kafka producer init error when enabled\n\n```suggestion\nx := 1\n```",
	}
	c2 := vcs.InlineComment{
		File: "internal/v1/api/run.go", LineStart: 303, LineEnd: 303,
		Body: "**Suggestions:**\n- Fail fast on Kafka init error; silent continuation loses refresh tasks\n\n```suggestion\ny := 2\n```",
	}

	t.Run("cli path with nil Posted store", func(t *testing.T) {
		fv := &fakeVCS{kind: vcs.KindGitLab, pr: pr}
		d := &Dispatcher{VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: fv}}
		d.postInline(ctx, fv, pr, key, "improve", []vcs.InlineComment{c1, c2})
		if fv.inlineCnt != 1 {
			t.Fatalf("expected 1 comment posted (intra-batch dedup), got %d", fv.inlineCnt)
		}
	})

	t.Run("worker path with memory Posted store", func(t *testing.T) {
		fv := &fakeVCS{kind: vcs.KindGitLab, pr: pr}
		d := &Dispatcher{
			VCSPool: map[vcs.Kind]vcs.Provider{vcs.KindGitLab: fv},
			Posted:  findings.NewMemory(""),
		}
		d.postInline(ctx, fv, pr, key, "improve", []vcs.InlineComment{c1, c2})
		if fv.inlineCnt != 1 {
			t.Fatalf("expected 1 comment posted (intra-batch dedup), got %d", fv.inlineCnt)
		}
	})
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
