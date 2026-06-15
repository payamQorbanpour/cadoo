package orchestrator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/findings"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// HTML markers delimit Cadoo-managed regions in PR bodies and the
// consolidated review comment. Keeping them invisible (HTML comments) means
// the wrapper renders cleanly in both GitHub and GitLab markdown while
// remaining machine-greppable on subsequent runs.
const (
	wrapperBegin   = vcs.SummaryWrapperBegin
	wrapperEnd     = "<!-- cadoo:wrapper:end -->"
	prSectionBegin = "<!-- cadoo:pr-body:begin -->"
	prSectionEnd   = "<!-- cadoo:pr-body:end -->"

	// descriptionAvatar is the icon used on the "Cadoo" header
	// injected into PR/MR bodies. Differentiating from the brand mark makes
	// the description block visually distinct from the consolidated review.
	descriptionAvatar = `<img src="https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/Description.png" height="28" align="absmiddle" alt="Description">`
)

// sectionTitle maps tool names to the display label shown in the
// consolidated comment's section header. Tools without an explicit label
// fall back to the tool name title-cased.
var sectionTitle = map[string]string{
	"describe":    "Description",
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
// Tools without a dedicated brand asset fall back to a unicode emoji.
var sectionEmoji = map[string]string{
	"describe":    `<img src="https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/Description.png" height="20" align="absmiddle" alt="Description">`,
	"review":      `<img src="https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/Magnifier.png" height="20" align="absmiddle" alt="Review">`,
	"deep_review": "🔬",
	"improve":     `<img src="https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/Improvement.png" height="20" align="absmiddle" alt="Improve">`,
	"changelog":   "📝",
	"add_tests":   "🧪",
	"add_docs":    "📚",
	"plan":        "🗺",
	"ask":         "❓",
	"check":       `<img src="https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/Flash.png" height="20" align="absmiddle" alt="Check">`,
}

// renderConsolidated builds the single comment body that wraps every tool's
// section. Sections are rendered as collapsible <details> blocks so the
// comment stays compact by default but exposes detail on demand. headSHA,
// when non-empty, is embedded as a <!-- cadoo:reviewed-sha:<sha> --> marker
// immediately after wrapperBegin so subsequent stateless CI runs can fetch
// only the incremental diff since the last review (T-08-C3: marker stays
// INSIDE the wrapper so SummaryWrapperBegin remains the first wrapper token).
func renderConsolidated(sections []findings.Section, headSHA string) string {
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
	b.WriteString("\n")
	if headSHA != "" {
		b.WriteString(vcs.RenderReviewedSHA(headSHA))
		b.WriteString("\n")
	}
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
	b.WriteString("\n## " + descriptionAvatar + " Cadoo Description\n\n")
	b.WriteString(section)
	b.WriteString("\n")
	b.WriteString(prSectionEnd)
	if tail = strings.TrimLeft(tail, " \n\t"); tail != "" {
		b.WriteString("\n\n")
		b.WriteString(tail)
	}
	return b.String()
}
