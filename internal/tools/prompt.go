package tools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxPriorFindings caps the number of prior-finding lines appended to any
// tool prompt. Each entry is ~60-80 chars (~20 tokens); 100 entries ≈ 2K
// tokens — enough for dedup context without risking context-window overflow.
const maxPriorFindings = 100

// maxMarkdownBriefRunes caps the free-form .cadoo.md review brief spliced into
// the prompt. ~16K runes ≈ 4-5K tokens — generous for a project review guide
// while bounding the blast radius of an accidentally-huge file.
const maxMarkdownBriefRunes = 16000

// PromptOptions controls which optional sections BuildDiffPrompt includes.
// The zero value includes all sections (backward-compatible default).
type PromptOptions struct {
	SkipTrackerIssues  bool // omit the ## Linked tracker issues section
	SkipSlopSignal     bool // omit the ## Pre-review signal section
	SkipStaticAnalysis bool // omit the ## Static analysis findings section
	MaxPRBodyRunes     int  // truncate PR description body; 0 = unlimited
}

// BuildDiffPrompt formats the PR header + diff for the user-message half of
// a tool's prompt. Most tools use this verbatim; /ask appends a Question
// section after.
func BuildDiffPrompt(in Input, opts PromptOptions) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pull Request\n\n**%s** by %s\n\n", in.PR.Title, in.PR.Author)
	if in.PR.Body != "" {
		body := in.PR.Body
		if opts.MaxPRBodyRunes > 0 && utf8.RuneCountInString(body) > opts.MaxPRBodyRunes {
			body = string([]rune(body)[:opts.MaxPRBodyRunes]) + "…"
		}
		fmt.Fprintf(&b, "## Description\n\n%s\n\n", body)
	}
	if brief := strings.TrimSpace(in.Config.Markdown); brief != "" {
		b.WriteString("## Project review guide (from .cadoo.md — treat as authoritative)\n\n")
		b.WriteString(truncateText(brief, maxMarkdownBriefRunes))
		b.WriteString("\n\n")
	}
	if len(in.Config.Conventions) > 0 {
		b.WriteString("## Team conventions (treat as authoritative; flag any violation)\n\n")
		for _, c := range in.Config.Conventions {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}
	if len(in.Config.StyleGuides) > 0 {
		b.WriteString("## Per-language style guidance\n\n")
		for lang, guide := range in.Config.StyleGuides {
			fmt.Fprintf(&b, "- **%s:** %s\n", lang, guide)
		}
		b.WriteString("\n")
	}
	if len(in.Config.PathInstructions) > 0 {
		b.WriteString("## Path-specific guidance\n\n")
		for _, pi := range in.Config.PathInstructions {
			fmt.Fprintf(&b, "- paths %v: %s\n", pi.Paths, pi.Instructions)
		}
		b.WriteString("\n")
	}
	if len(in.Packed.Truncated) > 0 || len(in.Packed.Skipped) > 0 {
		b.WriteString("## Coverage notes\n\n")
		if len(in.Packed.Truncated) > 0 {
			fmt.Fprintf(&b, "Truncated: %s\n", strings.Join(in.Packed.Truncated, ", "))
		}
		if len(in.Packed.Skipped) > 0 {
			fmt.Fprintf(&b, "Skipped: %s\n", strings.Join(in.Packed.Skipped, ", "))
		}
		b.WriteString("\n")
	}
	if !opts.SkipTrackerIssues && len(in.Issues) > 0 {
		b.WriteString("## Linked tracker issues (validate the PR addresses these)\n\n")
		for _, iss := range in.Issues {
			fmt.Fprintf(&b, "### %s — %s (%s, status: %s)\n", iss.Key, iss.Title, iss.Tracker, iss.Status)
			if iss.Assignee != "" {
				fmt.Fprintf(&b, "Assignee: %s. ", iss.Assignee)
			}
			if iss.URL != "" {
				fmt.Fprintf(&b, "<%s>\n", iss.URL)
			}
			if body := strings.TrimSpace(iss.Body); body != "" {
				fmt.Fprintf(&b, "\n%s\n", truncateText(body, 600))
			}
			b.WriteString("\n")
		}
	}
	if !opts.SkipSlopSignal && in.Slop != nil && in.Slop.IsSlop {
		fmt.Fprintf(&b, "## Pre-review signal: this PR scored %.2f for low-quality / AI-slop\n\n", in.Slop.Score)
		for _, r := range in.Slop.Reasons {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}
	if !opts.SkipStaticAnalysis && len(in.Analysis) > 0 {
		b.WriteString("## Static analysis findings (pre-narrowed; reason about real impact)\n\n")
		for _, f := range in.Analysis {
			fmt.Fprintf(&b, "- %s [%s] %s:%d — %s\n",
				f.Linter, f.Rule, f.File, f.LineStart, f.Message)
		}
		b.WriteString("\n")
	}
	if len(in.Learnings) > 0 {
		b.WriteString("## Team-specific guidance (from past reactions on Cadoo comments)\n\n")
		for _, r := range in.Learnings {
			fmt.Fprintf(&b, "- (weight %.2f) %s\n", r.Weight, r.Text)
		}
		b.WriteString("\n")
	}
	if len(in.KBHits) > 0 {
		b.WriteString("## Relevant docs from the knowledge base\n\n")
		for _, h := range in.KBHits {
			fmt.Fprintf(&b, "### %s (source: %s, similarity %.2f)\n%s\n\n",
				h.Title, h.Source, 1-h.Distance, truncateText(h.Text, 600))
		}
	}
	if len(in.PriorFindings) > 0 {
		pf := in.PriorFindings
		if len(pf) > maxPriorFindings {
			pf = pf[:maxPriorFindings]
		}
		b.WriteString("## Already posted on this PR — DO NOT restate or rephrase\n\n")
		b.WriteString("Cadoo (you, in prior runs) has already left these inline comments. Skip any finding that is the same issue at the same location, even if you'd word it differently. Only surface a finding here if it is genuinely new (different bug, different location, or substantively different concern).\n\n")
		for _, p := range pf {
			loc := p.File
			if p.LineStart > 0 {
				if p.LineEnd > 0 && p.LineEnd != p.LineStart {
					fmt.Fprintf(&b, "- [%s] %s:%d-%d — %s\n", p.Severity, loc, p.LineStart, p.LineEnd, p.Title)
				} else {
					fmt.Fprintf(&b, "- [%s] %s:%d — %s\n", p.Severity, loc, p.LineStart, p.Title)
				}
			} else {
				fmt.Fprintf(&b, "- [%s] %s — %s\n", p.Severity, loc, p.Title)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("## Diff\n\n")
	for _, f := range in.Packed.Files {
		fmt.Fprintf(&b, "### %s (%s, +%d -%d)\n```diff\n%s\n```\n\n",
			f.Path, f.Status, f.Additions, f.Deletions, f.Patch)
	}
	b.WriteString(scopeConstraintSection(in.Packed.Files))
	return b.String()
}

func truncateText(s string, n int) string {
	if len(s) <= n { // fast path: ASCII-length check is fine as lower bound
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
