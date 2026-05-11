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
- Only suggest changes that touch lines present in the diff.
- "code" must be a complete replacement for the [line_start, line_end] range.
- "rationale" is the one-line action a reviewer would write in a thread: terse, imperative, no explanation paragraphs.
- Prefer 2-5 high-leverage suggestions over many trivial ones.
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
	user := tools.BuildDiffPrompt(in)
	var out Output
	sys := tools.EffectivePrompt("improve", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	inlines := make([]vcs.InlineComment, 0, len(out.Suggestions))
	for _, s := range out.Suggestions {
		inlines = append(inlines, vcs.InlineComment{
			File:      s.File,
			LineStart: s.LineStart,
			LineEnd:   s.LineEnd,
			Body:      renderSuggestionBody(s),
		})
	}
	summary := buildSection(out, inlines)
	return &tools.Result{Summary: summary, InlineComments: inlines}, nil
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
// the consolidated Cadoo comment. Keeps things short: one-line intent + a
// bullet list of file anchors so reviewers can jump to inline suggestions.
func buildSection(out Output, inlines []vcs.InlineComment) string {
	intent := strings.TrimSpace(out.Summary)
	if intent == "" && len(inlines) == 0 {
		return "No high-leverage improvements found in this diff."
	}
	var b strings.Builder
	if intent != "" {
		b.WriteString(intent)
		b.WriteString("\n")
	}
	if len(inlines) > 0 {
		fmt.Fprintf(&b, "\n%d inline suggestion(s) posted:\n", len(inlines))
		for _, c := range inlines {
			loc := c.File
			if c.LineStart > 0 {
				fmt.Fprintf(&b, "- `%s:%d`\n", c.File, c.LineStart)
				continue
			}
			fmt.Fprintf(&b, "- `%s`\n", loc)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
