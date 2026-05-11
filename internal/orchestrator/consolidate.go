package orchestrator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/findings"
)

// HTML markers delimit Cadoo-managed regions in PR bodies and the
// consolidated review comment. Keeping them invisible (HTML comments) means
// the wrapper renders cleanly in both GitHub and GitLab markdown while
// remaining machine-greppable on subsequent runs.
const (
	wrapperBegin   = "<!-- cadoo:wrapper:begin -->"
	wrapperEnd     = "<!-- cadoo:wrapper:end -->"
	prSectionBegin = "<!-- cadoo:pr-body:begin -->"
	prSectionEnd   = "<!-- cadoo:pr-body:end -->"
)

// sectionTitle maps tool names to the display label shown in the
// consolidated comment's section header. Tools without an explicit label
// fall back to the tool name title-cased.
var sectionTitle = map[string]string{
	"review":      "Review",
	"deep_review": "Deep review",
	"improve":     "Suggested improvements",
	"changelog":   "Changelog",
	"add_tests":   "Tests to add",
	"add_docs":    "Docs to add",
	"plan":        "Implementation plan",
	"ask":         "Q&A",
	"check":       "Custom checks",
}

// sectionEmoji prefixes each section's <summary> so the consolidated comment
// is easy to scan at a glance. Mirrors the visual cues Qodo Merge uses.
var sectionEmoji = map[string]string{
	"review":      "🔍",
	"deep_review": "🔬",
	"improve":     "💡",
	"changelog":   "📝",
	"add_tests":   "🧪",
	"add_docs":    "📚",
	"plan":        "🗺",
	"ask":         "❓",
	"check":       "✅",
}

// renderConsolidated builds the single comment body that wraps every tool's
// section. Sections are rendered as collapsible <details> blocks so the
// comment stays compact by default but exposes detail on demand.
func renderConsolidated(sections []findings.Section) string {
	sort.SliceStable(sections, func(i, j int) bool {
		// Review first, then alphabetical by display title.
		ti, tj := sections[i].Tool, sections[j].Tool
		if ti == "review" {
			return tj != "review"
		}
		if tj == "review" {
			return false
		}
		return ti < tj
	})

	var b strings.Builder
	b.WriteString(wrapperBegin)
	b.WriteString("\n## Cadoo\n\n")
	for _, s := range sections {
		b.WriteString(renderSection(s))
		b.WriteString("\n")
	}
	b.WriteString(wrapperEnd)
	return b.String()
}

func renderSection(s findings.Section) string {
	title := sectionTitle[s.Tool]
	if title == "" {
		title = titleCase(strings.ReplaceAll(s.Tool, "_", " "))
	}
	emoji := sectionEmoji[s.Tool]
	if emoji == "" {
		emoji = "•"
	}
	body := strings.TrimSpace(s.Body)
	return fmt.Sprintf("<details><summary>%s <strong>%s</strong></summary>\n\n%s\n\n</details>\n",
		emoji, title, body)
}

// spliceCadooBody returns the new PR/MR body to send when /describe wants to
// inject `section` while preserving whatever the user originally wrote. The
// user's text stays on top, untouched; Cadoo's section is wrapped in
// idempotent markers so subsequent runs replace it cleanly.
func spliceCadooBody(original, section string) string {
	section = strings.TrimSpace(section)
	startIdx := strings.Index(original, prSectionBegin)
	endIdx := strings.Index(original, prSectionEnd)

	// Already a managed body: replace just the inner section.
	if startIdx >= 0 && endIdx > startIdx {
		head := strings.TrimRight(original[:startIdx], " \n\t")
		tail := original[endIdx+len(prSectionEnd):]
		return joinBody(head, section, tail)
	}

	// First-time write: append the section after the user's text.
	return joinBody(strings.TrimRight(original, " \n\t"), section, "")
}

// titleCase uppercases the first rune of each ASCII word — sufficient for
// tool names which are always lowercase ASCII identifiers.
func titleCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	upper := true
	for _, r := range s {
		if r == ' ' {
			upper = true
			b.WriteRune(r)
			continue
		}
		if upper && r >= 'a' && r <= 'z' {
			b.WriteRune(r - 32)
		} else {
			b.WriteRune(r)
		}
		upper = false
	}
	return b.String()
}

func joinBody(userText, section, tail string) string {
	var b strings.Builder
	if userText != "" {
		b.WriteString(userText)
		b.WriteString("\n\n")
	}
	b.WriteString(prSectionBegin)
	b.WriteString("\n## Cadoo description\n\n")
	b.WriteString(section)
	b.WriteString("\n")
	b.WriteString(prSectionEnd)
	if tail = strings.TrimLeft(tail, " \n\t"); tail != "" {
		b.WriteString("\n\n")
		b.WriteString(tail)
	}
	return b.String()
}
