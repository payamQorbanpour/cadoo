// Package learn implements /learn — explicit user signal recording a rule as
// a positive learning for the repo. Mirror tool /unlearn lives in
// internal/tools/unlearn.
package learn

import (
	"context"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/learnings"
	"github.com/payamqorbanpour/cadoo/internal/tools"
)

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "learn" }

// Run implements tools.Tool. Args is the rule text the user wants Cadoo to
// internalize. Stored with reaction Accept so subsequent /review prompts
// surface it as team-preferred guidance.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	rule := strings.TrimSpace(in.Args)
	if rule == "" {
		return &tools.Result{
			Summary: "## Cadoo `/learn`\n\nUsage: `/learn <rule text>` — Cadoo will treat this rule as authoritative on future reviews.",
		}, nil
	}
	if in.LearningsStore == nil || in.RepoKey == "" {
		return &tools.Result{
			Summary: "## Cadoo `/learn`\n\nKnowledge layer is not configured (DATABASE_URL unset). The rule was not stored.",
		}, nil
	}
	if err := in.LearningsStore.Record(ctx, in.RepoKey, rule, learnings.Accept); err != nil {
		return nil, err
	}
	return &tools.Result{
		Summary: "## Cadoo `/learn`\n\nRecorded as team-preferred rule for this repo:\n\n> " + rule,
	}, nil
}
