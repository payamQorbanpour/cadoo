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
	iconURL := "https://raw.githubusercontent.com/org/repo/main/docs/assets/AI.png"
	block := buildAIPromptBlock(c, iconURL)

	must := []struct {
		label string
		want  string
	}{
		{"opening details tag", "<details>"},
		{"opening summary tag", "<summary>"},
		{"img tag with src", `<img src="` + iconURL + `"`},
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
