package orchestrator

import (
	"fmt"
	"strings"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// cadooAIIconURL is the stable raw-content URL for the AI agent icon embedded
// in the <details> summary line. It points to the Cadoo repository itself, not
// the reviewed repository, so it resolves correctly regardless of which repo is
// under review. In air-gapped environments the <img> simply fails to load;
// the prompt block and copy button remain fully functional.
const cadooAIIconURL = "https://raw.githubusercontent.com/payamqorbanpour/cadoo/main/docs/assets/AI.png"

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

	var lineRef string
	switch c.LineStart {
	case 0:
		// unanchored (file-level) comment — omit line reference
	case c.LineEnd:
		lineRef = fmt.Sprintf(" line %d", c.LineStart)
	default:
		lineRef = fmt.Sprintf(" lines %d–%d", c.LineStart, c.LineEnd)
	}

	prompt := fmt.Sprintf(
		"Fix the following code review finding.\n\n**File:** `%s`%s\n**Severity:** %s\n\n%s",
		c.File, lineRef, string(c.Severity), c.Body,
	)
	// Escape any triple-backtick sequences in the body so they cannot close
	// the outer fenced code block prematurely.
	prompt = strings.ReplaceAll(prompt, "```", "` ``")

	return fmt.Sprintf(
		"<details>\n<summary>%s</summary>\n\n```\n%s\n```\n\n</details>",
		summaryLabel, prompt,
	)
}
