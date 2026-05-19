// Package ask implements /ask — interactive Q&A about a pull request.
package ask

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
)

const systemPrompt = `You are Cadoo, answering a developer's question about a pull request.

- Be concise (1-3 short paragraphs).
- Reference specific files and lines from the diff when relevant.
- If the diff doesn't contain enough context, say so plainly rather than guessing.`

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "ask" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	question := strings.TrimSpace(in.Args)
	if question == "" {
		return &tools.Result{
			Summary: "Cadoo `/ask` requires a question. Example: `/ask why was this approach chosen?`",
		}, nil
	}
	user := tools.BuildDiffPrompt(in, tools.PromptOptions{}) + "\n\n## Question\n\n" + question
	sys := tools.EffectivePrompt("ask", systemPrompt, in.Config)
	answer, err := tools.CallText(ctx, in.LLM, in.Model, sys, user)
	if err != nil {
		return nil, err
	}
	return &tools.Result{
		Summary: fmt.Sprintf("**You asked:** %s\n\n%s", question, answer),
	}, nil
}
