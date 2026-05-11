package orchestrator

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
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
func (f *fakeVCS) PostInlineComments(_ context.Context, _ *vcs.PullRequest, c []vcs.InlineComment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inlineCnt += len(c)
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
