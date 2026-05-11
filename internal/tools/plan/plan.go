// Package plan implements /plan — turn a PRD into a step-by-step
// implementation plan that a developer (or AI coding assistant) can execute.
package plan

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
)

const systemPrompt = `You are Cadoo. The user has provided a PRD-style description; produce a concrete step-by-step implementation plan.

Respond with ONLY a JSON object:
{
  "summary": "<one-paragraph overview of the proposed approach>",
  "steps": [
    {
      "order":   <int, 1-based>,
      "title":   "<short imperative phrase>",
      "details": "<markdown explaining what to do and why>",
      "files":   ["<path likely to change>"]
    }
  ],
  "open_questions": ["<question that must be answered before starting>"]
}

Rules:
- Steps should be small and reviewable as discrete commits.
- Prefer concrete file paths when the diff or repo signals make them obvious.
- If something is genuinely unclear, list it under open_questions instead of guessing.`

// Step is one entry in the plan.
type Step struct {
	Order   int      `json:"order"`
	Title   string   `json:"title"`
	Details string   `json:"details"`
	Files   []string `json:"files"`
}

// Output is the structured response.
type Output struct {
	Summary       string   `json:"summary"`
	Steps         []Step   `json:"steps"`
	OpenQuestions []string `json:"open_questions"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "plan" }

// Run implements tools.Tool. The PRD comes from `/plan <text>` args; if
// empty it falls back to the PR body.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	prd := strings.TrimSpace(in.Args)
	if prd == "" {
		prd = strings.TrimSpace(in.PR.Body)
	}
	if prd == "" {
		return &tools.Result{
			Summary: "## Cadoo `/plan`\n\nProvide a PRD as `/plan <description>` or in the PR body.",
		}, nil
	}
	user := fmt.Sprintf("# PRD\n\n%s\n\n# Existing diff context\n\n%s", prd, tools.BuildDiffPrompt(in))
	var out Output
	sys := tools.EffectivePrompt("plan", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("## Cadoo plan\n\n")
	if out.Summary != "" {
		b.WriteString(out.Summary)
		b.WriteString("\n\n")
	}
	for _, s := range out.Steps {
		fmt.Fprintf(&b, "### %d. %s\n\n%s\n", s.Order, s.Title, s.Details)
		if len(s.Files) > 0 {
			fmt.Fprintf(&b, "\n_Files:_ %s\n", strings.Join(s.Files, ", "))
		}
		b.WriteString("\n")
	}
	if len(out.OpenQuestions) > 0 {
		b.WriteString("### Open questions\n\n")
		for _, q := range out.OpenQuestions {
			fmt.Fprintf(&b, "- %s\n", q)
		}
	}
	return &tools.Result{Summary: b.String()}, nil
}
