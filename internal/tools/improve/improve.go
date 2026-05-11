// Package improve implements /improve — concrete code suggestions posted as
// inline GitHub `suggestion` blocks.
package improve

import (
	"context"
	"fmt"

	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

const systemPrompt = `You are Cadoo. Suggest concrete, high-leverage improvements for the changed code.

Respond with ONLY a JSON object:
{
  "summary": "<short overview of the suggested improvements>",
  "suggestions": [
    {
      "file": "<path as shown in the diff>",
      "line_start": <int, 1-based, line in the new file>,
      "line_end":   <int, end of the range to replace; equal to line_start for single line>,
      "rationale":  "<short explanation of why this is better>",
      "code":       "<exact replacement for the line range — no diff markers, no surrounding lines>"
    }
  ]
}

Rules:
- Only suggest changes that touch lines present in the diff.
- "code" must be a complete replacement for the [line_start, line_end] range.
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
		body := fmt.Sprintf("**Suggestion** — %s\n\n```suggestion\n%s\n```", s.Rationale, s.Code)
		inlines = append(inlines, vcs.InlineComment{
			File:      s.File,
			LineStart: s.LineStart,
			LineEnd:   s.LineEnd,
			Body:      body,
		})
	}
	summary := out.Summary
	if summary == "" && len(inlines) == 0 {
		summary = "## Cadoo: no high-leverage improvements suggested for this diff."
	} else if summary != "" {
		summary = "## Cadoo: suggested improvements\n\n" + summary
	}
	return &tools.Result{Summary: summary, InlineComments: inlines}, nil
}
