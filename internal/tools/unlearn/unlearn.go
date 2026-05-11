// Package unlearn implements /unlearn — explicit user signal demoting a
// previously-recorded rule. Mirror of /learn (internal/tools/learn).
package unlearn

import (
	"context"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/learnings"
	"github.com/payamqorbanpour/cadoo/internal/tools"
)

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "unlearn" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	rule := strings.TrimSpace(in.Args)
	if rule == "" {
		return &tools.Result{
			Summary: "## Cadoo `/unlearn`\n\nUsage: `/unlearn <rule text>` — drops the rule below the active threshold so it stops appearing in future reviews.",
		}, nil
	}
	if in.LearningsStore == nil || in.RepoKey == "" {
		return &tools.Result{
			Summary: "## Cadoo `/unlearn`\n\nKnowledge layer is not configured (DATABASE_URL unset).",
		}, nil
	}
	if err := in.LearningsStore.Record(ctx, in.RepoKey, rule, learnings.Reject); err != nil {
		return nil, err
	}
	return &tools.Result{
		Summary: "## Cadoo `/unlearn`\n\nRule weight reduced for this repo:\n\n> " + rule,
	}, nil
}
