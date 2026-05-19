package vcs

import "testing"

func TestInlineMarkerRoundTrip(t *testing.T) {
	in := MarkerData{Tool: "review", SK: "9f3a1c2b7d4e5061", Sev: "warn"}
	body := "Something is wrong here.\nUse a buffered writer."
	stamped := body + "\n\n" + InlineMarker(in)

	got, stripped, ok := ParseInlineMarker(stamped)
	if !ok {
		t.Fatalf("ParseInlineMarker(%q) ok=false; want true", stamped)
	}
	if got != in {
		t.Errorf("marker = %+v; want %+v", got, in)
	}
	if stripped != body {
		t.Errorf("stripped = %q; want %q", stripped, body)
	}
}

func TestParseInlineMarkerAbsent(t *testing.T) {
	if _, _, ok := ParseInlineMarker("plain comment, no marker"); ok {
		t.Errorf("ParseInlineMarker on unmarked body: ok=true; want false")
	}
}

func TestFirstLine(t *testing.T) {
	if got := FirstLine("a\nb\nc"); got != "a" {
		t.Errorf("FirstLine = %q; want %q", got, "a")
	}
	if got := FirstLine("only"); got != "only" {
		t.Errorf("FirstLine = %q; want %q", got, "only")
	}
}

func TestPriorReviewShape(t *testing.T) {
	pr := PriorReview{
		SummaryCommentID: "42",
		Inline: []PriorInline{{
			Tool: "review", File: "a.go", Severity: "warn",
			StructuralKey: "abc", Title: "boom", ExternalID: "d1", Resolved: false,
		}},
	}
	if pr.Inline[0].StructuralKey != "abc" || pr.SummaryCommentID != "42" {
		t.Fatalf("unexpected PriorReview round-trip: %+v", pr)
	}
}
