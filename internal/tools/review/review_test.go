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
		Packed: contextengine.Compressed{Files: []vcs.FileChange{{Path: "x.go", Patch: "@@ -0,0 +1,2 @@\n+a\n+b"}}},
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
	if res.Summary != "" {
		t.Errorf("expected no top-level summary comment from /review, got %q", res.Summary)
	}
}

func TestReviewToolDropsOffDiffFindings(t *testing.T) {
	// Patch adds only new-file line 5. The model returns one finding on that
	// line (in scope), one on line 99 (a context/unchanged line — out of
	// scope), and one file-level finding (line 0, always allowed). Only the
	// in-scope and file-level findings should survive the hard diff-anchor
	// filter.
	in := tools.Input{
		PR:     &vcs.PullRequest{},
		Packed: contextengine.Compressed{Files: []vcs.FileChange{{Path: "x.go", Patch: "@@ -4,0 +5,1 @@\n+added"}}},
		Config: config.Default(),
		LLM: &fakeLLM{body: `{"summary":"s","findings":[
			{"file":"x.go","line_start":5,"severity":"warn","title":"in","body":"kept"},
			{"file":"x.go","line_start":99,"severity":"warn","title":"off","body":"dropped"},
			{"file":"x.go","line_start":0,"severity":"warn","title":"file","body":"kept"}
		]}`},
	}
	res, err := Tool{}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.InlineComments) != 2 {
		t.Fatalf("expected 2 inlines (in-scope + file-level), got %d", len(res.InlineComments))
	}
	for _, c := range res.InlineComments {
		if c.LineStart == 99 {
			t.Errorf("off-diff finding on line 99 should have been dropped")
		}
	}
}

func TestReviewToolFiltersBelowThreshold(t *testing.T) {
	cfg := config.Default()
	cfg.Review.SeverityThreshold = "block"
	in := tools.Input{
		PR:     &vcs.PullRequest{},
		Packed: contextengine.Compressed{Files: []vcs.FileChange{{Path: "a", Patch: "@@ -0,0 +1,2 @@\n+x\n+y"}}},
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

func TestReviewToolCleanRunEmitsNoSummary(t *testing.T) {
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
	if res.Summary != "" {
		t.Errorf("expected no top-level summary on clean run, got %q", res.Summary)
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
		Packed: contextengine.Compressed{Files: []vcs.FileChange{{Path: "a", Patch: "@@ -0,0 +1,1 @@\n+x"}}},
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
