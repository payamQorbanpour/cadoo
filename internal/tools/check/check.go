// Package check implements /check — runs the user's custom natural-language
// rules from `.cadoo.yaml` `checks:` against the PR diff.
package check

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

const systemPrompt = `You are Cadoo running a single user-supplied custom check. Apply the rule strictly — only flag violations that the rule actually targets.

Respond with ONLY a JSON object:
{
  "findings": [
    {
      "file":       "<path as shown in the diff>",
      "line_start": <int, 1-based new-file line; 0 if file-level>,
      "line_end":   <int, 0 or equal to line_start for single-line>,
      "title":      "<one-line headline>",
      "body":       "<markdown explanation, ideally with a fix>"
    }
  ]
}

Rules:
- Empty findings array is fine when the rule is satisfied.
- Don't flag changes that were already in the codebase before this PR.`

type checkOutput struct {
	Findings []struct {
		File      string `json:"file"`
		LineStart int    `json:"line_start"`
		LineEnd   int    `json:"line_end"`
		Title     string `json:"title"`
		Body      string `json:"body"`
	} `json:"findings"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "check" }

// Run implements tools.Tool. Iterates config.Checks; one LLM call per check,
// all rules evaluated concurrently via errgroup. Original rule order is
// preserved in the output by writing into fixed-position slots.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	if len(in.Config.Checks) == 0 {
		return &tools.Result{
			Summary: "## Cadoo `/check`\n\nNo custom checks configured in `.cadoo.yaml`.",
		}, nil
	}

	type result struct {
		name     string
		inlines  []vcs.InlineComment
		checkRun vcs.CheckRun
	}
	results := make([]result, len(in.Config.Checks))

	g, gctx := errgroup.WithContext(ctx)
	for i, c := range in.Config.Checks {
		g.Go(func() error {
			findings, err := runOne(gctx, in.LLM, in.Model, c, in)
			if err != nil {
				return nil // keep-going: failing rule is skipped, matches prior behavior
			}
			sev := vcs.Severity(strings.ToLower(c.Severity))
			if sev == "" {
				sev = vcs.SeverityWarn
			}
			var ruleInlines []vcs.InlineComment
			for _, f := range findings.Findings {
				body := f.Body
				title := f.Title
				if title == "" {
					title = c.Name
				}
				ruleInlines = append(ruleInlines, vcs.InlineComment{
					File:      f.File,
					LineStart: f.LineStart,
					LineEnd:   f.LineEnd,
					Body:      "**" + title + "** _(" + c.Name + ")_\n\n" + body,
					Severity:  sev,
				})
			}
			// Per-rule check run so users can wire branch protection per check.
			status := vcs.CheckSucceeded
			crTitle := "no findings"
			if len(ruleInlines) > 0 {
				crTitle = fmt.Sprintf("%d finding(s)", len(ruleInlines))
				if sev == vcs.SeverityBlock {
					status = vcs.CheckFailed
				}
			}
			results[i] = result{
				name:    c.Name,
				inlines: ruleInlines,
				checkRun: vcs.CheckRun{
					Name:    "cadoo/check/" + c.Name,
					Status:  status,
					Title:   crTitle,
					Summary: "Custom check: " + c.Name,
				},
			}
			return nil
		})
	}
	_ = g.Wait()

	// Merge in original rule order for deterministic output.
	var (
		ranNames  []string
		inlines   []vcs.InlineComment
		checkRuns []vcs.CheckRun
	)
	for _, r := range results {
		if r.name == "" {
			continue // rule errored out — skip
		}
		ranNames = append(ranNames, r.name)
		inlines = append(inlines, r.inlines...)
		checkRuns = append(checkRuns, r.checkRun)
	}

	var b strings.Builder
	b.WriteString("## Cadoo `/check`\n\n")
	if len(ranNames) == 0 {
		b.WriteString("_No checks ran (LLM errors only)._")
	} else {
		fmt.Fprintf(&b, "Ran %d check(s): %s.\nPosted %d finding(s).\n",
			len(ranNames), strings.Join(ranNames, ", "), len(inlines))
	}
	return &tools.Result{
		Summary:        b.String(),
		InlineComments: inlines,
		CheckRuns:      checkRuns,
	}, nil
}

func runOne(ctx context.Context, p llm.Provider, model string, c config.Check, in tools.Input) (*checkOutput, error) {
	scope := "all changed files"
	if len(c.Paths) > 0 {
		scope = strings.Join(c.Paths, ", ")
	}
	user := fmt.Sprintf(`# Custom check: %s

Apply ONLY to files matching: %s

## Rule

%s

## Diff

%s`, c.Name, scope, c.Prompt, tools.BuildDiffPrompt(in, tools.PromptOptions{}))

	var out checkOutput
	sys := tools.EffectivePrompt("check", systemPrompt, in.Config)
	if err := tools.CallJSON(ctx, p, model, sys, user, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
