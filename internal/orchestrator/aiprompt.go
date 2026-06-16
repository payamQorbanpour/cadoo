package orchestrator

import (
	"fmt"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// aiIconPath is the repo-relative path of the AI agent icon served as a raw
// URL inside the <details> summary line.
const aiIconPath = "docs/assets/AI.png"

// baseURLer is satisfied by VCS adapters that expose their instance host URL.
// Kept unexported to avoid polluting the vcs.Provider interface.
type baseURLer interface {
	BaseURL() string
}

// aiIconURL constructs the raw-content URL for the AI icon from the provider
// kind and the adapter's host URL. Returns "" when the adapter does not
// implement baseURLer (e.g. test stubs), causing buildAIPromptBlock to omit
// the <img> tag gracefully.
func aiIconURL(provider vcs.Provider, pr *vcs.PullRequest) string {
	bu, ok := provider.(baseURLer)
	if !ok {
		return ""
	}
	return vcs.RawContentURL(pr.Provider, bu.BaseURL(), pr.RepoFullName, "main", aiIconPath)
}

// buildAIPromptBlock returns the collapsed <details> block appended to every
// inline comment wire copy. The prompt is fenced in a code block so GitHub
// and GitLab render a native copy button. iconURL may be empty; when it is,
// the <img> tag is omitted from the summary line.
func buildAIPromptBlock(c vcs.InlineComment, iconURL string) string {
	summaryLabel := "Prompt for AI Agents"
	if iconURL != "" {
		summaryLabel = fmt.Sprintf(
			`<img src="%s" width="16" height="16" alt="AI"/> %s`,
			iconURL, summaryLabel,
		)
	}

	lineRef := fmt.Sprintf("lines %d–%d", c.LineStart, c.LineEnd)
	if c.LineStart == c.LineEnd {
		lineRef = fmt.Sprintf("line %d", c.LineStart)
	}

	prompt := fmt.Sprintf(
		"Fix the following code review finding.\n\n**File:** `%s` %s\n**Severity:** %s\n\n%s",
		c.File, lineRef, string(c.Severity), c.Body,
	)

	return fmt.Sprintf(
		"<details>\n<summary>%s</summary>\n\n```\n%s\n```\n\n</details>",
		summaryLabel, prompt,
	)
}
