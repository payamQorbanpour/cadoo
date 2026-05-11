// Package review implements Cadoo's /review tool.
package review

import (
	"context"
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/tools"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// CheckRunName is the GitHub check-run name posted by /review.
const CheckRunName = "cadoo/review"

// DefaultPrompt is the system prompt used by /review when no
// `prompts.review.override` is configured. Exported so external integrations
// can compose against it (e.g. in tests or `prompts.review.addendum`).
const DefaultPrompt = `You are a senior code reviewer with 15+ years of experience across security, performance, distributed systems, testing, and operational maturity. You review pull requests the way a careful staff engineer would: focused, specific, and unwilling to spam noise.

Your single most important rule: ONLY post a finding if a careful human reviewer would consider it worth raising in a real PR conversation. If you wouldn't say it out loud in a code-review meeting, do not write it. When in doubt, do not flag.

## Categories to consider (apply with discipline)

1. **Correctness bugs** — off-by-one, nil-deref, race condition, wrong type assertion, missing error check that hides a failure path, broken invariant, integer overflow, time-zone confusion.

2. **Security & vulnerability** — injection (SQL, command, template, NoSQL, XPath), missing auth/authz check, hardcoded secret, weak/legacy cryptography, unsafe deserialization, SSRF, path traversal, missing input validation at a trust boundary, sensitive data leaked into logs or error messages, CSRF where applicable, unsafe redirects.

3. **Performance** — N+1 queries, unbounded memory growth, blocking I/O on a hot path, missing pagination, accidental O(n²) where it matters at scale, missing index hints, synchronous calls in a request path that should be async.

4. **Bug-generation risk** — fragile invariants, hidden coupling, change that silently breaks an unrelated caller (cite the caller path), API change without deprecation, behavioural change disguised as a refactor.

5. **Missing tests** — only flag when the changed code (a) has clear branching not exercised by existing tests, (b) is security-sensitive or hits external systems, or (c) replaces previously-tested behaviour. Do NOT say "consider adding tests" without naming a specific branch that isn't covered.

6. **Missing lint / static analysis** — only when a real bug class is being missed (not formatting). If you'd recommend a specific linter or rule, name it.

7. **Maintainability** — only when objectively bad: confusing naming that will trip a future reader, dead code being added, duplicated logic that already exists nearby (cite the duplicate), wrong abstraction that will cost more to undo than to fix now.

## Anti-patterns — DO NOT POST findings that are

- Praise or "looks good". Approval is the right channel, not comments.
- Generic suggestions like "consider tests", "you might want to refactor", "add error handling" without a concrete failure path.
- Style preferences (var-name aesthetics, comment style, line length) that don't hide a real issue.
- Things outside the diff that you noticed while reading context.
- Speculation: "if X happened, this could break" — only flag concrete, demonstrable failure modes.
- Suggestions to use a different language feature when both work fine.
- Findings whose body would be < 20 characters of substance.

If no finding meets the bar, return findings: [] and a one-line summary saying so. Do not invent findings to fill space.

## Severity rubric (be honest about confidence)

- **block** — correctness bug, security issue, data-loss risk, or anything you'd hold a merge for. You should be confident.
- **warn**  — likely problem or missed best practice with concrete evidence the author should respond to.
- **nit**   — true style issue. Reserve for STRICTNESS = strict/pedantic; otherwise suppress entirely.

## Output

Respond with ONLY a JSON object — no prose before or after — matching this schema:

{
  "summary": "<2-3 sentence prose: PR's intent, your overall assessment, anything blocking>",
  "findings": [
    {
      "file":       "<path as shown in the diff>",
      "line_start": <int, 1-based new-file line; 0 if file-level>,
      "line_end":   <int, 0 or equal to line_start for single-line>,
      "severity":   "block" | "warn" | "nit",
      "title":      "<one-line headline (≤80 chars)>",
      "body":       "<markdown: what the issue is, why it matters, concrete fix>"
    }
  ]
}

Cite line numbers from the new file. If you cannot pinpoint a line, set line_start=0 and explain in body. Do not repeat the diff or invent code that is not present.`

// Finding is one issue surfaced by the model.
type Finding struct {
	File      string `json:"file"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

// Output is the structured response.
type Output struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Tool implements tools.Tool.
type Tool struct{}

// Name implements tools.Tool.
func (Tool) Name() string { return "review" }

// Run implements tools.Tool.
func (Tool) Run(ctx context.Context, in tools.Input) (*tools.Result, error) {
	user := tools.BuildDiffPrompt(in)
	sys := tools.EffectivePrompt("review", DefaultPrompt, in.Config)
	var out Output
	if err := tools.CallJSON(ctx, in.LLM, in.Model, sys, user, &out); err != nil {
		return nil, err
	}
	inline := convertFindings(out.Findings, in.Config)
	p := in.Config.CommentPolicy

	// Clean run: zero post-threshold findings.
	if len(inline) == 0 && p.SilentOnClean {
		return &tools.Result{
			CheckRun: &vcs.CheckRun{
				Name:    CheckRunName,
				Status:  vcs.CheckSucceeded,
				Title:   "no findings",
				Summary: out.Summary,
			},
		}, nil
	}

	// Findings exist but policy suppresses them (nit-only / below min).
	if shouldSuppress(inline, p) {
		return &tools.Result{
			CheckRun: &vcs.CheckRun{
				Name:    CheckRunName,
				Status:  vcs.CheckSucceeded,
				Title:   silentTitle(inline),
				Summary: out.Summary,
			},
		}, nil
	}

	status := vcs.CheckSucceeded
	if hasBlocking(out.Findings, in.Config.Review.RequestChangesOn) {
		status = vcs.CheckFailed
	}
	return &tools.Result{
		InlineComments: inline,
		CheckRun: &vcs.CheckRun{
			Name:    CheckRunName,
			Status:  status,
			Title:   fmt.Sprintf("%d findings", len(inline)),
			Summary: out.Summary,
		},
	}, nil
}

// shouldSuppress applies the comment policy to non-empty result sets:
// returns true when /review should drop the summary + inline comments
// and emit a check-run only (e.g. nit-only or below min-findings).
// The clean-run case (zero post-threshold findings) is handled inline
// in Run so it can branch on StatsOnClean separately.
func shouldSuppress(inline []vcs.InlineComment, p config.CommentPolicy) bool {
	if len(inline) == 0 {
		return false
	}
	if p.SkipIfOnlyNits {
		onlyNits := true
		for _, c := range inline {
			if c.Severity != vcs.SeverityNit {
				onlyNits = false
				break
			}
		}
		if onlyNits {
			return true
		}
	}
	if p.MinFindingsToPost > 0 && len(inline) < p.MinFindingsToPost {
		return true
	}
	return false
}

func silentTitle(inline []vcs.InlineComment) string {
	if len(inline) == 0 {
		return "no issues"
	}
	return fmt.Sprintf("%d finding(s) below post threshold", len(inline))
}

func convertFindings(findings []Finding, cfg config.Repo) []vcs.InlineComment {
	threshold := severityRank(cfg.Review.SeverityThreshold)
	maxComments := cfg.Review.MaxComments
	if maxComments <= 0 {
		maxComments = 30
	}
	out := make([]vcs.InlineComment, 0, len(findings))
	for _, f := range findings {
		sev := vcs.Severity(strings.ToLower(f.Severity))
		if severityRank(string(sev)) < threshold {
			continue
		}
		body := f.Body
		if f.Title != "" {
			body = "**" + f.Title + "**\n\n" + body
		}
		out = append(out, vcs.InlineComment{
			File:      f.File,
			LineStart: f.LineStart,
			LineEnd:   f.LineEnd,
			Body:      body,
			Severity:  sev,
		})
		if len(out) >= maxComments {
			break
		}
	}
	return out
}

func severityRank(s string) int {
	switch strings.ToLower(s) {
	case "block":
		return 3
	case "warn":
		return 2
	case "nit":
		return 1
	}
	return 1
}

func hasBlocking(findings []Finding, blockOn []string) bool {
	if len(blockOn) == 0 {
		blockOn = []string{"block"}
	}
	set := map[string]bool{}
	for _, s := range blockOn {
		set[strings.ToLower(s)] = true
	}
	for _, f := range findings {
		if set[strings.ToLower(f.Severity)] {
			return true
		}
	}
	return false
}
