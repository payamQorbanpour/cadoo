package tools

import (
	"fmt"
	"strings"
)

// BuildDiffPrompt formats the PR header + diff for the user-message half of
// a tool's prompt. Most tools use this verbatim; /ask appends a Question
// section after.
func BuildDiffPrompt(in Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pull Request\n\n**%s** by %s\n\n", in.PR.Title, in.PR.Author)
	if in.PR.Body != "" {
		fmt.Fprintf(&b, "## Description\n\n%s\n\n", in.PR.Body)
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
	if len(in.Issues) > 0 {
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
	if in.Slop != nil && in.Slop.IsSlop {
		fmt.Fprintf(&b, "## Pre-review signal: this PR scored %.2f for low-quality / AI-slop\n\n", in.Slop.Score)
		for _, r := range in.Slop.Reasons {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}
	if len(in.Analysis) > 0 {
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
	b.WriteString("## Diff\n\n")
	for _, f := range in.Packed.Files {
		fmt.Fprintf(&b, "### %s (%s, +%d -%d)\n```diff\n%s\n```\n\n",
			f.Path, f.Status, f.Additions, f.Deletions, f.Patch)
	}
	return b.String()
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
