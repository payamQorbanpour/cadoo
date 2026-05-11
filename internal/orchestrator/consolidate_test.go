package orchestrator

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/findings"
)

func TestRenderConsolidatedOrdersReviewFirst(t *testing.T) {
	got := renderConsolidated([]findings.Section{
		{Tool: "improve", Body: "imp"},
		{Tool: "review", Body: "rev"},
	})
	if !strings.Contains(got, "## Cadoo") {
		t.Errorf("missing wrapper header: %s", got)
	}
	if i, j := strings.Index(got, "Review"), strings.Index(got, "Suggested improvements"); i < 0 || j < 0 || i > j {
		t.Errorf("review section should appear before improve: %s", got)
	}
	if !strings.Contains(got, "<details>") {
		t.Errorf("sections should be collapsible: %s", got)
	}
}

func TestSpliceCadooBodyFirstWrite(t *testing.T) {
	got := spliceCadooBody("user wrote this", "cadoo says hi")
	if !strings.HasPrefix(got, "user wrote this") {
		t.Errorf("user text should stay on top: %s", got)
	}
	if !strings.Contains(got, prSectionBegin) || !strings.Contains(got, prSectionEnd) {
		t.Errorf("markers missing: %s", got)
	}
	if !strings.Contains(got, "cadoo says hi") {
		t.Errorf("section body missing: %s", got)
	}
}

func TestSpliceCadooBodyIdempotentReplace(t *testing.T) {
	original := "user content\n\n" + prSectionBegin + "\n## Cadoo description\n\nold cadoo\n" + prSectionEnd
	got := spliceCadooBody(original, "new cadoo")
	if strings.Contains(got, "old cadoo") {
		t.Errorf("old section should be replaced: %s", got)
	}
	if !strings.Contains(got, "new cadoo") {
		t.Errorf("new section missing: %s", got)
	}
	if strings.Count(got, prSectionBegin) != 1 {
		t.Errorf("expected exactly one marker pair: %s", got)
	}
}

func TestSpliceCadooBodyEmptyUserOK(t *testing.T) {
	got := spliceCadooBody("", "section")
	if strings.HasPrefix(got, "\n") {
		t.Errorf("should not start with newline: %q", got)
	}
	if !strings.Contains(got, "section") {
		t.Errorf("section missing: %s", got)
	}
}
