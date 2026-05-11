// Package changelog implements /changelog — propose CHANGELOG entries.
package changelog

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/tools"
)

const systemPrompt = `You are Cadoo. Propose CHANGELOG entries for this PR following Keep-a-Changelog conventions.

Respond with ONLY a JSON object:
{
  "entries": [
    {"category": "Added|Changed|Deprecated|Removed|Fixed|Security", "text": "<one-line entry, past tense>"}
  ]
}

Rules:
- Empty entries array is fine if the PR is purely internal (refactor, tests, docs).
- Each entry: <verb> <what>, scoped to user-visible behaviour.
- Skip churn-only changes (formatting, internal renames).`

// Entry is one CHANGELOG line.
type Entry struct {
	Category string `json:"category"`
	Text     string `json:"text"`
}

// Output is the structured response.
type Output struct {
	Entries []Entry `json:"entries"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "changelog" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	user := tools.BuildDiffPrompt(in)
	var out Output
	sys := tools.EffectivePrompt("changelog", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	if len(out.Entries) == 0 {
		return &tools.Result{
			Summary: "## Cadoo: suggested CHANGELOG entries\n\n_None — this PR appears internal-only._",
		}, nil
	}
	bucket := map[string][]string{}
	for _, e := range out.Entries {
		bucket[e.Category] = append(bucket[e.Category], e.Text)
	}
	var b strings.Builder
	b.WriteString("## Cadoo: suggested CHANGELOG entries\n\n")
	for _, cat := range []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"} {
		if items := bucket[cat]; len(items) > 0 {
			fmt.Fprintf(&b, "### %s\n", cat)
			for _, t := range items {
				fmt.Fprintf(&b, "- %s\n", t)
			}
			b.WriteString("\n")
		}
	}
	return &tools.Result{Summary: strings.TrimRight(b.String(), "\n") + "\n"}, nil
}
