// Package improve implements /improve — concrete code suggestions posted as
// inline GitHub `suggestion` blocks.
package improve

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

const systemPrompt = `You are Cadoo. Suggest concrete, high-leverage improvements for the changed code.

Respond with ONLY a JSON object:
{
  "summary": "<one-sentence overview of the suggestion set>",
  "suggestions": [
    {
      "file":       "<path as shown in the diff>",
      "line_start": <int, 1-based new-file line>,
      "line_end":   <int, end of the range to replace; equal to line_start for single line>,
      "rationale":  "<≤90-char imperative-mood action — what to do, not why. Example: 'Use pinned digest instead of latest'>",
      "code":       "<exact replacement for the line range — no diff markers, no surrounding lines>"
    }
  ]
}

Rules:
- Only suggest changes on lines explicitly marked with a leading + in the diff above. Context lines (no +/- prefix) are out of scope even though they appear in the diff block.
- "code" must be a complete replacement for the [line_start, line_end] range.
- "rationale" is the one-line action a reviewer would write in a thread: terse, imperative, no explanation paragraphs.
- Return AT MOST 5 suggestions. Rank all candidates by impact; drop everything outside the top 5.
- Only suggest a change if it materially improves correctness, performance, security, or API clarity — not cosmetic rewrites, renames, or adding comments.
- If you can't propose a concrete improvement, return suggestions: [].`

// Suggestion is one proposed code change.
type Suggestion struct {
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Rationale string `json:"rationale"`
	Code      string `json:"code"`
}

// Output is the structured response.
type Output struct {
	Summary     string       `json:"summary"`
	Suggestions []Suggestion `json:"suggestions"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "improve" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	// Limit PriorFindings to improve's own history. Describe/review findings
	// from the same CI run would inflate the prompt past the context limit on
	// large PRs without adding dedup value for this tool.
	own := make([]tools.PriorFinding, 0, len(in.PriorFindings))
	for _, pf := range in.PriorFindings {
		if pf.Tool == "improve" {
			own = append(own, pf)
		}
	}
	in.PriorFindings = own
	user := tools.BuildDiffPrompt(in, tools.PromptOptions{SkipTrackerIssues: true, SkipSlopSignal: true, MaxPRBodyRunes: 800})
	var out Output
	sys := tools.EffectivePrompt("improve", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	changedMap := tools.BuildChangedMap(in.Packed.Files)
	inlines := make([]vcs.InlineComment, 0, len(out.Suggestions))
	for _, s := range out.Suggestions {
		// Hard diff-anchor filter: drop suggestions placed on context,
		// unchanged, or removed lines (the model treats them as fair game).
		if !tools.InChangedLines(changedMap[s.File], s.LineStart) {
			continue
		}
		inlines = append(inlines, vcs.InlineComment{
			File:      s.File,
			LineStart: s.LineStart,
			LineEnd:   s.LineEnd,
			Body:      renderSuggestionBody(s),
		})
	}
	return &tools.Result{Summary: buildSection(out), InlineComments: inlines}, nil
}

// renderSuggestionBody formats one inline-comment body. The action lives in
// the bullet — reviewers can read it without expanding the suggestion block;
// the suggestion block underneath gives them one-click apply.
func renderSuggestionBody(s Suggestion) string {
	action := strings.TrimSpace(s.Rationale)
	if action == "" {
		action = "Apply suggested change."
	}
	return fmt.Sprintf("**Suggestions:**\n- %s\n\n```suggestion\n%s\n```", action, s.Code)
}

// buildSection renders the body-only fragment the orchestrator wraps inside
// the consolidated Cadoo comment. Anchor lists are deliberately omitted: the
// inline suggestions themselves carry file:line, and the dispatcher dedups
// them across runs via posted_findings — re-listing here would duplicate the
// inline comments and lie after dedup skips them.
func buildSection(out Output) string {
	intent := strings.TrimSpace(out.Summary)
	if intent != "" {
		return intent
	}
	if len(out.Suggestions) == 0 {
		return "No high-leverage improvements found in this diff."
	}
	return ""
}
