package review

import (
	"context"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/contextengine"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

type fakeLLM struct{ body string }

func (f *fakeLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: f.body}, nil
}

func TestReviewToolBuildsExpectedResult(t *testing.T) {
	tool := Tool{}
	if tool.Name() != "review" {
		t.Fatalf("name: %q", tool.Name())
	}
	in := tools.Input{
		PR:     &vcs.PullRequest{Title: "x", Author: "a"},
		Packed: contextengine.Compressed{Files: []vcs.FileChange{{Path: "x.go", Patch: "d"}}},
		Config: config.Default(),
		LLM: &fakeLLM{body: `{"summary":"ok","findings":[
			{"file":"x.go","line_start":1,"line_end":1,"severity":"warn","title":"t","body":"b"},
			{"file":"x.go","line_start":2,"severity":"block","title":"u","body":"c"}
		]}`},
		Model: "test",
	}
	res, err := tool.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.CheckRun == nil || res.CheckRun.Status != vcs.CheckFailed {
		t.Errorf("expected CheckFailed (block present), got %+v", res.CheckRun)
	}
	if len(res.InlineComments) != 2 {
		t.Errorf("expected 2 inlines, got %d", len(res.InlineComments))
	}
	if res.Summary == "" {
		t.Error("summary empty")
	}
}

func TestReviewToolFiltersBelowThreshold(t *testing.T) {
	cfg := config.Default()
	cfg.Review.SeverityThreshold = "block"
	in := tools.Input{
		PR:     &vcs.PullRequest{},
		Packed: contextengine.Compressed{},
		Config: cfg,
		LLM: &fakeLLM{body: `{"summary":"","findings":[
			{"file":"a","line_start":1,"severity":"warn","title":"t","body":"b"},
			{"file":"a","line_start":2,"severity":"nit","title":"u","body":"c"}
		]}`},
	}
	res, err := Tool{}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.InlineComments) != 0 {
		t.Errorf("expected 0 (all below block threshold), got %d", len(res.InlineComments))
	}
}

func TestReviewToolCleanRunPostsStatsSummary(t *testing.T) {
	in := tools.Input{
		PR: &vcs.PullRequest{},
		Packed: contextengine.Compressed{
			Files:     []vcs.FileChange{{Path: "x.go", Patch: "d"}, {Path: "y.go", Patch: "d"}},
			EstTokens: 1234,
		},
		Config: config.Default(),
		LLM:    &fakeLLM{body: `{"summary":"all good","findings":[]}`},
		Model:  "claude-sonnet-4-6",
	}
	res, err := Tool{}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.InlineComments) != 0 {
		t.Errorf("expected 0 inlines on clean run, got %d", len(res.InlineComments))
	}
	if res.Summary == "" {
		t.Fatal("expected stats summary on clean run with StatsOnClean default, got empty")
	}
	for _, want := range []string{"No findings", "Files", "2 (~1k tokens)", "claude-sonnet-4-6"} {
		if !strings.Contains(res.Summary, want) {
			t.Errorf("summary missing %q:\n%s", want, res.Summary)
		}
	}
	if res.CheckRun == nil || res.CheckRun.Status != vcs.CheckSucceeded {
		t.Errorf("expected CheckSucceeded, got %+v", res.CheckRun)
	}
}

func TestReviewToolCleanRunSilentWhenStatsOff(t *testing.T) {
	cfg := config.Default()
	cfg.CommentPolicy.StatsOnClean = false
	in := tools.Input{
		PR:     &vcs.PullRequest{},
		Packed: contextengine.Compressed{Files: []vcs.FileChange{{Path: "x.go", Patch: "d"}}},
		Config: cfg,
		LLM:    &fakeLLM{body: `{"summary":"clean","findings":[]}`},
	}
	res, err := Tool{}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "" {
		t.Errorf("expected no summary when StatsOnClean=false, got %q", res.Summary)
	}
	if res.CheckRun == nil || res.CheckRun.Status != vcs.CheckSucceeded {
		t.Errorf("expected green check-run, got %+v", res.CheckRun)
	}
}

func TestReviewToolNitOnlyStillSuppressed(t *testing.T) {
	cfg := config.Default()
	cfg.Review.SeverityThreshold = "nit" // let nits through convertFindings
	in := tools.Input{
		PR:     &vcs.PullRequest{},
		Packed: contextengine.Compressed{},
		Config: cfg,
		LLM: &fakeLLM{body: `{"summary":"nits","findings":[
			{"file":"a","line_start":1,"severity":"nit","title":"t","body":"b"}
		]}`},
	}
	res, err := Tool{}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary != "" {
		t.Errorf("expected suppressed summary on nit-only run, got %q", res.Summary)
	}
	if len(res.InlineComments) != 0 {
		t.Errorf("expected 0 inlines on nit-only run, got %d", len(res.InlineComments))
	}
	if res.CheckRun == nil || res.CheckRun.Status != vcs.CheckSucceeded {
		t.Errorf("expected green check-run, got %+v", res.CheckRun)
	}
	if res.CheckRun != nil && !strings.Contains(res.CheckRun.Title, "below post threshold") {
		t.Errorf("expected silentTitle on suppressed run, got %q", res.CheckRun.Title)
	}
}
