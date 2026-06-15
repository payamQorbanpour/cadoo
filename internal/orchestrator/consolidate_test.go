package orchestrator

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/findings"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestRenderConsolidatedOrdersReviewFirst(t *testing.T) {
	got := renderConsolidated([]findings.Section{
		{Tool: "improve", Body: "imp"},
		{Tool: "review", Body: "rev"},
	}, "")
	if !strings.Contains(got, wrapperBegin) || !strings.Contains(got, wrapperEnd) {
		t.Errorf("missing wrapper markers: %s", got)
	}
	if i, j := strings.Index(got, "Review"), strings.Index(got, "Suggested improvements"); i < 0 || j < 0 || i > j {
		t.Errorf("review section should appear before improve: %s", got)
	}
	if !strings.Contains(got, "<details>") {
		t.Errorf("sections should be collapsible: %s", got)
	}
}

func TestRenderConsolidatedEmbedsReviewedSHA(t *testing.T) {
	sha := "aabbccddeeff00112233445566778899aabbccdd"
	got := renderConsolidated([]findings.Section{
		{Tool: "review", Body: "findings"},
	}, sha)
	// SummaryWrapperBegin must be the first wrapper token
	wrapperIdx := strings.Index(got, wrapperBegin)
	if wrapperIdx < 0 {
		t.Fatalf("wrapperBegin absent: %s", got)
	}
	// marker must be present and INSIDE the wrapper (after wrapperBegin)
	markerStr := vcs.RenderReviewedSHA(sha)
	markerIdx := strings.Index(got, markerStr)
	if markerIdx < 0 {
		t.Fatalf("reviewed-sha marker absent: %s", got)
	}
	if markerIdx < wrapperIdx {
		t.Errorf("marker must appear AFTER wrapperBegin; wrapperIdx=%d markerIdx=%d", wrapperIdx, markerIdx)
	}
	// round-trip: ParseReviewedSHA must recover the SHA
	if parsed := vcs.ParseReviewedSHA(got); parsed != sha {
		t.Errorf("ParseReviewedSHA round-trip = %q; want %q", parsed, sha)
	}
	// empty headSHA → no marker
	noSHA := renderConsolidated([]findings.Section{{Tool: "review", Body: "x"}}, "")
	if strings.Contains(noSHA, "cadoo:reviewed-sha") {
		t.Errorf("empty headSHA should not embed marker: %s", noSHA)
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
