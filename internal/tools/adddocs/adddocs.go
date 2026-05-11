// Package adddocs implements /add_docs — generate docstrings for newly
// introduced public symbols.
package adddocs

import (
	"context"
	"fmt"

	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

const systemPrompt = `You are Cadoo. For each undocumented public symbol introduced in this diff, generate a docstring suggestion.

Respond with ONLY a JSON object:
{
  "summary": "<one-line summary of what was documented>",
  "suggestions": [
    {
      "file": "<path as shown in the diff>",
      "line_start": <int, line where the symbol declaration appears in the new file>,
      "line_end":   <int, same as line_start for a single-line replacement>,
      "code":       "<replacement text: docstring + the original declaration line(s)>"
    }
  ]
}

Rules:
- Only consider PUBLIC symbols introduced in the diff.
- Skip symbols that already have a docstring.
- Match the language's conventional docstring style:
  - Go: leading "// <Name> ..." comment, single line preferred.
  - Python: """triple-quoted string""" inside the function body.
  - JS/TS: /** JSDoc */ block.
- "code" must be a complete drop-in replacement for the [line_start, line_end] range — no diff markers.
- If nothing qualifies, return suggestions: [].`

// Suggestion is one proposed docstring insertion.
type Suggestion struct {
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
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
func (Tool) Name() string { return "add_docs" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	user := tools.BuildDiffPrompt(in)
	var out Output
	sys := tools.EffectivePrompt("add_docs", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	if len(out.Suggestions) == 0 {
		return &tools.Result{
			Summary: "## Cadoo `/add_docs`\n\nNo undocumented public symbols found in this diff.",
		}, nil
	}
	inlines := make([]vcs.InlineComment, 0, len(out.Suggestions))
	for _, s := range out.Suggestions {
		body := fmt.Sprintf("**Documentation**\n\n```suggestion\n%s\n```", s.Code)
		inlines = append(inlines, vcs.InlineComment{
			File:      s.File,
			LineStart: s.LineStart,
			LineEnd:   s.LineEnd,
			Body:      body,
		})
	}
	summary := out.Summary
	if summary != "" {
		summary = "## Cadoo `/add_docs`\n\n" + summary
	}
	return &tools.Result{Summary: summary, InlineComments: inlines}, nil
}
