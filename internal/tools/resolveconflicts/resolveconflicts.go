// Package resolveconflicts implements /resolve_conflicts — proposes
// resolutions for git conflict markers left in the PR's diff.
package resolveconflicts

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

const systemPrompt = `You are Cadoo. The user's PR contains literal git merge-conflict markers (<<<<<<<, =======, >>>>>>>). For each conflict block, propose a resolution.

Respond with ONLY a JSON object:
{
  "summary": "<short overview of how you resolved each conflict>",
  "resolutions": [
    {
      "file": "<path as shown in the diff>",
      "line_start": <int, 1-based new-file line of the <<<<<<< marker>,
      "line_end":   <int, 1-based new-file line of the >>>>>>> marker>,
      "rationale":  "<one-line reason for picking this side / merge>",
      "code":       "<replacement text with markers removed; must be the complete drop-in for [line_start, line_end]>"
    }
  ]
}

Rules:
- Only emit resolutions for blocks that contain all three markers in the diff.
- "code" replaces the entire range INCLUDING the marker lines — do not include them in the output.
- If both sides look intentional, prefer the side that semantically matches the PR's stated intent (title + body).`

// Resolution is one proposed conflict fix.
type Resolution struct {
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Rationale string `json:"rationale"`
	Code      string `json:"code"`
}

// Output is the structured response.
type Output struct {
	Summary     string       `json:"summary"`
	Resolutions []Resolution `json:"resolutions"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "resolve_conflicts" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	if !HasConflicts(in.Packed.Files) {
		return &tools.Result{
			Summary: "## Cadoo `/resolve_conflicts`\n\nNo merge-conflict markers found in this PR's diff.",
		}, nil
	}
	user := tools.BuildDiffPrompt(in)
	var out Output
	sys := tools.EffectivePrompt("resolve_conflicts", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	inlines := make([]vcs.InlineComment, 0, len(out.Resolutions))
	for _, r := range out.Resolutions {
		body := fmt.Sprintf("**Conflict resolution** — %s\n\n```suggestion\n%s\n```", r.Rationale, r.Code)
		inlines = append(inlines, vcs.InlineComment{
			File:      r.File,
			LineStart: r.LineStart,
			LineEnd:   r.LineEnd,
			Body:      body,
		})
	}
	summary := "## Cadoo `/resolve_conflicts`\n\n"
	if out.Summary != "" {
		summary += out.Summary
	}
	if len(inlines) == 0 {
		summary += "\n\n_No actionable resolutions emitted._"
	}
	return &tools.Result{Summary: summary, InlineComments: inlines}, nil
}

// HasConflicts returns true if any patch contains a conflict marker.
func HasConflicts(files []vcs.FileChange) bool {
	for _, f := range files {
		if strings.Contains(f.Patch, "<<<<<<<") {
			return true
		}
	}
	return false
}
