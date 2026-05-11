// Package deepreview implements /deep_review — an agentic version of /review
// that lets the model read additional files and grep across the diff before
// emitting findings.
package deepreview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/agent"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)


const systemPrompt = `You are Cadoo, an expert code reviewer with code-exploration tools.

You may call:
- read_file(path, line_start?, line_end?) — fetch additional context beyond the diff
- grep(pattern) — find usages of a symbol in the PR's changed files

Use tools sparingly: at most 2-3 calls per substantive finding you want to make. Stop calling tools and emit a final answer the moment you have a confident review.

Final answer must be a JSON object only:
{
  "summary": "<short prose: PR intent + overall quality + confidence after exploration>",
  "findings": [
    {
      "file": "<path as shown in the diff>",
      "line_start": <int, 1-based new-file line; 0 if file-level>,
      "line_end":   <int, 0 or equal to line_start for single-line>,
      "severity":   "block" | "warn" | "nit",
      "title":      "<one-line headline>",
      "body":       "<markdown explanation including a fix when feasible>"
    }
  ]
}

Rules:
- Cite line numbers from the new file.
- Don't invent code that isn't in the diff or fetched files.
- Skip generic findings ("looks good", "consider tests").`

// Finding mirrors the /review tool's schema for portability.
type Finding struct {
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

// Output is the agent's final structured answer.
type Output struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "deep_review" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	if in.Reader == nil {
		return nil, errors.New("/deep_review requires a VCS adapter that supports file fetch")
	}
	files := make([]string, 0, len(in.Files))
	for _, f := range in.Files {
		files = append(files, f.Path)
	}
	loop := &agent.Loop{
		LLM:    in.LLM,
		Model:  in.Model,
		System: systemPrompt,
		Tools: []agent.Tool{
			agent.ReadFileTool(in.Reader),
			agent.GrepTool(in.Reader, files),
		},
		MaxIter:     6,
		MaxTokens:   4096,
		Temperature: 0.2,
	}

	user := tools.BuildDiffPrompt(in)
	res, err := loop.Run(ctx, user)
	if err != nil {
		return nil, err
	}

	var parsed Output
	if err := tools.ExtractJSON(res.Content, &parsed); err != nil {
		return nil, fmt.Errorf("parse deep_review output: %w", err)
	}

	var b strings.Builder
	b.WriteString("## Cadoo deep review\n\n")
	if parsed.Summary != "" {
		b.WriteString(parsed.Summary)
		b.WriteString("\n\n")
	}
	fmt.Fprintf(&b, "_Agent ran %d iteration(s), executed %d tool call(s)._\n", res.Iterations, res.ToolCalls)

	return &tools.Result{
		Summary:        b.String(),
		InlineComments: convertFindings(parsed.Findings),
	}, nil
}

func convertFindings(findings []Finding) []vcs.InlineComment {
	out := make([]vcs.InlineComment, 0, len(findings))
	for _, f := range findings {
		body := f.Body
		if f.Title != "" {
			body = "**" + f.Title + "**\n\n" + body
		}
		out = append(out, vcs.InlineComment{
			File:      f.File,
			LineStart: f.LineStart,
			LineEnd:   f.LineEnd,
			Body:      body,
			Severity:  vcs.Severity(strings.ToLower(f.Severity)),
		})
	}
	return out
}
