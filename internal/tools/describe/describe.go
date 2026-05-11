// Package describe implements /describe — propose a clearer PR title and body.
package describe

import (
	"context"
	"fmt"

	"github.com/payamqorbanpour/cadoo/internal/tools"
)

const systemPrompt = `You are Cadoo. Propose a clearer pull-request title and body for this change.

Respond with ONLY a JSON object:
{
  "title": "<concise PR title in the imperative mood>",
  "body":  "<markdown body: 1-line intent, then bullet list of key changes, then 'Risks' and 'Test plan' sections>"
}

Keep the title under 70 characters. Body should be useful to a reviewer who has not seen the diff.`

// Output is the structured response.
type Output struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "describe" }

// Run implements tools.Tool. Posts a summary comment with the proposed
// description; Phase 2.x will optionally edit the PR body in place.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	user := tools.BuildDiffPrompt(in)
	var out Output
	sys := tools.EffectivePrompt("describe", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	body := fmt.Sprintf("## Cadoo: suggested PR description\n\n**Title:** %s\n\n%s", out.Title, out.Body)
	return &tools.Result{Summary: body}, nil
}
