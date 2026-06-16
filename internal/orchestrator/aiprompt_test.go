package orchestrator

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestBuildAIPromptBlockContainsRequiredParts(t *testing.T) {
	c := vcs.InlineComment{
		File:      "internal/auth/middleware.go",
		LineStart: 42,
		LineEnd:   45,
		Severity:  vcs.SeverityWarn,
		Body:      "**Missing capability check**\n\nThe handler lacks an admin scope guard.",
	}
	block := buildAIPromptBlock(c, cadooAIIconURL)

	must := []struct {
		label string
		want  string
	}{
		{"opening details tag", "<details>"},
		{"opening summary tag", "<summary>"},
		{"img tag with src", `<img src="` + cadooAIIconURL + `"`},
		{"prompt label", "Prompt for AI Agents"},
		{"closing summary tag", "</summary>"},
		{"file path in backticks", "`internal/auth/middleware.go`"},
		{"line range with en-dash", "lines 42–45"},
		{"severity label", "warn"},
		{"body title verbatim", "**Missing capability check**"},
		{"opening code fence", "```"},
		{"closing details tag", "</details>"},
	}
	for _, m := range must {
		if !strings.Contains(block, m.want) {
			t.Errorf("missing %s: want %q in output:\n%s", m.label, m.want, block)
		}
	}
	if strings.Contains(block, "<details open") {
		t.Error("block must be collapsed by default (no 'open' attribute on <details>)")
	}
}

func TestBuildAIPromptBlockSingleLine(t *testing.T) {
	c := vcs.InlineComment{
		File:      "main.go",
		LineStart: 10,
		LineEnd:   10,
		Severity:  vcs.SeverityNit,
		Body:      "Rename for clarity.",
	}
	block := buildAIPromptBlock(c, "")
	if !strings.Contains(block, "line 10") {
		t.Errorf("single-line comment should use singular 'line 10', got:\n%s", block)
	}
	if strings.Contains(block, "lines 10") {
		t.Errorf("single-line comment must not say 'lines 10–10', got:\n%s", block)
	}
}

func TestBuildAIPromptBlockEmptyIconOmitsImgTag(t *testing.T) {
	c := vcs.InlineComment{File: "x.go", LineStart: 1, LineEnd: 1, Severity: vcs.SeverityWarn, Body: "issue"}
	block := buildAIPromptBlock(c, "")
	if strings.Contains(block, "<img") {
		t.Errorf("empty iconURL: <img> tag must be absent, got:\n%s", block)
	}
	if !strings.Contains(block, "Prompt for AI Agents") {
		t.Error("summary label 'Prompt for AI Agents' must still be present without icon")
	}
}

func TestBuildAIPromptBlockZeroLineStartOmitsLineRef(t *testing.T) {
	c := vcs.InlineComment{File: "x.go", LineStart: 0, LineEnd: 0, Severity: vcs.SeverityWarn, Body: "file-level issue"}
	block := buildAIPromptBlock(c, "")
	if strings.Contains(block, "line 0") {
		t.Errorf("unanchored comment must not emit 'line 0', got:\n%s", block)
	}
	if strings.Contains(block, "lines 0") {
		t.Errorf("unanchored comment must not emit 'lines 0', got:\n%s", block)
	}
}

func TestBuildAIPromptBlockEscapesTripleBacktick(t *testing.T) {
	c := vcs.InlineComment{
		File:      "main.go",
		LineStart: 5,
		LineEnd:   7,
		Severity:  vcs.SeverityBlock,
		Body:      "Use\n```go\nfoo()\n```\ninstead.",
	}
	block := buildAIPromptBlock(c, "")
	// The outer fence is the first and last ``` in the block after the newline
	// following </summary>. Triple-backtick sequences inside c.Body must be
	// escaped so the outer fence is not closed prematurely.
	_, inner, ok := strings.Cut(block, "```\n")
	if !ok {
		t.Fatal("expected at least one opening code fence in block")
	}
	innerContent, afterClose, ok := strings.Cut(inner, "\n```")
	if !ok {
		t.Fatal("expected closing code fence in block")
	}
	if !strings.Contains(afterClose, "</details>") {
		t.Errorf("closing </details> not found after the code fence close, got:\n%s", block)
	}
	// The raw ``` from the body must not appear unescaped inside the fenced block
	if strings.Contains(innerContent, "```go") {
		t.Errorf("unescaped ```go in prompt body would break the fenced block:\n%s", block)
	}
}

func TestCadooAIIconURLPointsToCadooRepo(t *testing.T) {
	// The icon must point to the Cadoo repo itself, not to the reviewed repo.
	// This is a regression guard: using pr.RepoFullName would point to the
	// customer repo (which almost certainly lacks docs/assets/AI.png).
	if !strings.Contains(cadooAIIconURL, "payamqorbanpour/cadoo") {
		t.Errorf("cadooAIIconURL must point to the Cadoo repo, got: %s", cadooAIIconURL)
	}
	if !strings.HasPrefix(cadooAIIconURL, "https://") {
		t.Errorf("cadooAIIconURL must be an absolute HTTPS URL, got: %s", cadooAIIconURL)
	}
}
