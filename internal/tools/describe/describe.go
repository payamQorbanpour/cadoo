// Package describe implements /describe — propose a clearer PR title and body.
package describe

import (
	"context"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
)

const systemPrompt = `You are Cadoo. Propose a concise, reviewer-friendly description for this pull request.

Respond with ONLY a JSON object:
{
  "title":   "<≤70-char imperative-mood title>",
  "intent":  "<one-sentence summary of what this PR does and why>",
  "type":    "<comma-separated labels: Bug fix | Enhancement | Refactor | Tests | Docs | Chore>",
  "changes": [ "<short bullet — one per meaningful change, ≤90 chars>" ],
  "risks":   "<one sentence; '' if low-risk>"
}

Rules:
- changes: 2-6 bullets max. Skip trivial moves.
- Do not invent files or behaviour not in the diff.
- Keep every field tight — the reader skims this.`

// Output is the structured response.
type Output struct {
	Title   string   `json:"title"`
	Intent  string   `json:"intent"`
	Type    string   `json:"type"`
	Changes []string `json:"changes"`
	Risks   string   `json:"risks"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "describe" }

// Run implements tools.Tool. Edits the PR body in place: the user's original
// description stays on top, Cadoo's section is appended (and replaced in
// place on subsequent dispatches via the marker pair the orchestrator
// recognises).
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	user := tools.BuildDiffPrompt(in)
	var out Output
	sys := tools.EffectivePrompt("describe", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	section := buildSection(out)
	return &tools.Result{EditPRBody: &section}, nil
}

func buildSection(o Output) string {
	var b strings.Builder
	if o.Title != "" {
		b.WriteString("**Title:** ")
		b.WriteString(o.Title)
		b.WriteString("\n\n")
	}
	if o.Intent != "" {
		b.WriteString(o.Intent)
		b.WriteString("\n\n")
	}
	if o.Type != "" {
		b.WriteString("**Type:** ")
		b.WriteString(o.Type)
		b.WriteString("\n\n")
	}
	if len(o.Changes) > 0 {
		b.WriteString("**Changes**\n\n")
		for _, c := range o.Changes {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(c)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if r := strings.TrimSpace(o.Risks); r != "" {
		b.WriteString("**Risks:** ")
		b.WriteString(r)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
