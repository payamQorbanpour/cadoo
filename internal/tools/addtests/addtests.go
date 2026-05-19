// Package addtests implements /add_tests — generate unit-test scaffolds for
// changed functions. Output is rendered as code blocks in the summary
// comment so reviewers can copy/paste into the right test file.
package addtests

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
)

const systemPrompt = `You are Cadoo. For the changed functions in this PR, generate concise unit-test scaffolds covering the happy path and 1-2 obvious edge cases.

Respond with ONLY a JSON object:
{
  "summary": "<one-line overview of what was tested>",
  "tests": [
    {
      "language":  "go|python|typescript|javascript|rust|...",
      "file_hint": "<suggested filename if a clear convention exists>",
      "code":      "<self-contained test code>"
    }
  ]
}

Rules:
- Match the project's existing test framework when discernible: table-driven for Go, pytest for Python, jest/vitest for TS, cargo-test for Rust.
- Test only NEW or CHANGED functions in the diff. Skip if you cannot infer a clear test strategy.
- Keep each test snippet focused and small; avoid multi-file fixtures.`

// Test is one proposed test snippet.
type Test struct {
	Language string `json:"language"`
	FileHint string `json:"file_hint"`
	Code     string `json:"code"`
}

// Output is the structured response.
type Output struct {
	Summary string `json:"summary"`
	Tests   []Test `json:"tests"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "add_tests" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	user := tools.BuildDiffPrompt(in, tools.PromptOptions{})
	var out Output
	sys := tools.EffectivePrompt("add_tests", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	if len(out.Tests) == 0 {
		return &tools.Result{
			Summary: "## Cadoo `/add_tests`\n\n_No clear tests to add for this diff._",
		}, nil
	}
	var b strings.Builder
	b.WriteString("## Cadoo `/add_tests`\n\n")
	if out.Summary != "" {
		b.WriteString(out.Summary)
		b.WriteString("\n\n")
	}
	for _, t := range out.Tests {
		hint := t.FileHint
		if hint == "" {
			hint = "(file: choose your test location)"
		}
		fmt.Fprintf(&b, "### %s\n\n```%s\n%s\n```\n\n", hint, t.Language, t.Code)
	}
	return &tools.Result{Summary: b.String()}, nil
}
