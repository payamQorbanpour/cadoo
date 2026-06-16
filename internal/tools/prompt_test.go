package tools_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/analysis"
	"github.com/payamqorbanpour/cadoo/internal/issuetrackers"
	"github.com/payamqorbanpour/cadoo/internal/slop"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func minInput() tools.Input {
	return tools.Input{PR: &vcs.PullRequest{Title: "Add feature", Author: "alice"}}
}

func TestBuildDiffPromptZeroValueIncludesAll(t *testing.T) {
	in := minInput()
	in.Issues = []issuetrackers.Issue{{Key: "PROJ-1", Title: "bug"}}
	in.Slop = &slop.Report{IsSlop: true, Score: 0.9}
	in.Analysis = []analysis.Finding{{Linter: "revive", Rule: "exported", File: "a.go", LineStart: 10, Message: "missing doc"}}
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{})
	if !strings.Contains(got, "PROJ-1") {
		t.Error("zero-value opts: expected tracker issues section")
	}
	if !strings.Contains(got, "0.90") {
		t.Error("zero-value opts: expected slop signal section")
	}
	if !strings.Contains(got, "revive") {
		t.Error("zero-value opts: expected static analysis section")
	}
}

func TestBuildDiffPromptSkipTrackerIssues(t *testing.T) {
	in := minInput()
	in.Issues = []issuetrackers.Issue{{Key: "PROJ-1", Title: "bug"}}
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{SkipTrackerIssues: true})
	if strings.Contains(got, "PROJ-1") {
		t.Error("SkipTrackerIssues=true: tracker issues should be omitted")
	}
}

func TestBuildDiffPromptSkipSlopSignal(t *testing.T) {
	in := minInput()
	in.Slop = &slop.Report{IsSlop: true, Score: 0.88}
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{SkipSlopSignal: true})
	if strings.Contains(got, "0.88") {
		t.Error("SkipSlopSignal=true: slop signal should be omitted")
	}
}

func TestBuildDiffPromptSkipStaticAnalysis(t *testing.T) {
	in := minInput()
	in.Analysis = []analysis.Finding{{Linter: "staticcheck", Rule: "S1000", File: "a.go", LineStart: 5, Message: "simplify"}}
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{SkipStaticAnalysis: true})
	if strings.Contains(got, "staticcheck") {
		t.Error("SkipStaticAnalysis=true: analysis section should be omitted")
	}
}

func TestBuildDiffPromptMaxPRBodyRunes(t *testing.T) {
	in := minInput()
	in.PR.Body = strings.Repeat("a", 2000)
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{MaxPRBodyRunes: 100})
	_, after, ok := strings.Cut(got, "## Description\n\n")
	if !ok {
		t.Fatal("expected Description section")
	}
	bodySection, _, _ := strings.Cut(after, "\n##")
	bodySection = strings.TrimSpace(bodySection)
	if len([]rune(bodySection)) > 102 {
		t.Errorf("PR body not truncated: got %d runes, want ≤101", len([]rune(bodySection)))
	}
}

func TestBuildDiffPromptMaxPRBodyRunesMultibyte(t *testing.T) {
	in := minInput()
	in.PR.Body = strings.Repeat("日", 200) // 200 × 3-byte rune = 600 bytes
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{MaxPRBodyRunes: 50})
	_, after, ok := strings.Cut(got, "## Description\n\n")
	if !ok {
		t.Fatal("expected Description section")
	}
	bodySection, _, _ := strings.Cut(after, "\n##")
	bodySection = strings.TrimSpace(bodySection)
	runeCount := len([]rune(bodySection))
	if runeCount > 52 { // 50 runes + "…" + small margin
		t.Errorf("multibyte PR body not truncated correctly: got %d runes", runeCount)
	}
}

func TestBuildDiffPromptPriorFindingsCapped(t *testing.T) {
	in := minInput()
	for i := range 150 {
		in.PriorFindings = append(in.PriorFindings, tools.PriorFinding{
			Tool: "review", File: "a.go", LineStart: i + 1, Severity: "warn",
			Title: fmt.Sprintf("finding %d", i),
		})
	}
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{})
	// Match the entry format only ("— finding N"), not the prose mentions of "finding".
	count := strings.Count(got, "— finding ")
	if count > 100 {
		t.Errorf("PriorFindings not capped: got %d entries, want ≤100", count)
	}
	if count == 0 {
		t.Errorf("PriorFindings section missing: got 0 entries, want 100")
	}
}

func TestBuildDiffPromptIncludesMarkdownBrief(t *testing.T) {
	in := minInput()
	in.Config.Markdown = "# Our review guide\n\nReject any new global variable."
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{})
	if !strings.Contains(got, "Reject any new global variable.") {
		t.Error("expected .cadoo.md brief to be injected into the prompt")
	}
	if !strings.Contains(got, ".cadoo.md") {
		t.Error("expected a labelled section header naming .cadoo.md")
	}
}

func TestBuildDiffPromptOmitsEmptyMarkdownBrief(t *testing.T) {
	in := minInput()
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{})
	if strings.Contains(got, ".cadoo.md") {
		t.Error("no .cadoo.md section should appear when Markdown is empty")
	}
}

func TestBuildDiffPromptMarkdownBriefTruncated(t *testing.T) {
	in := minInput()
	in.Config.Markdown = strings.Repeat("x", 50000)
	got := tools.BuildDiffPrompt(in, tools.PromptOptions{})
	if !strings.Contains(got, "…") {
		t.Error("expected oversized .cadoo.md brief to be truncated with an ellipsis")
	}
	if strings.Count(got, "x") >= 50000 {
		t.Error("oversized .cadoo.md brief was not truncated")
	}
}
