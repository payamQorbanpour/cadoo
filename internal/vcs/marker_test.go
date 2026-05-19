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

func TestInlineMarkerRoundTripWithNT(t *testing.T) {
	in := MarkerData{
		Tool: "improve",
		SK:   "9f3a1c2b7d4e5061",
		Sev:  "nit",
		NT:   "suggestions: fail fast on kafka producer init error",
	}
	body := "**Suggestions:**\n- Fail fast on Kafka producer init error\n\n```suggestion\nx := 1\n```"
	stamped := body + "\n\n" + InlineMarker(in)

	got, stripped, ok := ParseInlineMarker(stamped)
	if !ok {
		t.Fatalf("ParseInlineMarker(%q) ok=false; want true", stamped)
	}
	if got.NT != in.NT {
		t.Errorf("NT = %q; want %q", got.NT, in.NT)
	}
	if got.SK != in.SK || got.Tool != in.Tool || got.Sev != in.Sev {
		t.Errorf("marker fields mismatch: %+v; want %+v", got, in)
	}
	if stripped != body {
		t.Errorf("stripped = %q; want %q", stripped, body)
	}
}

func TestInlineMarkerLegacyNoNT(t *testing.T) {
	// A legacy marker (no nt= field) must still parse successfully with NT=="".
	legacy := "Fix the leak.\n\n<!-- cadoo:fp v=1 tool=review sk=abc123 sev=warn -->"
	md, stripped, ok := ParseInlineMarker(legacy)
	if !ok {
		t.Fatalf("legacy marker parse failed")
	}
	if md.NT != "" {
		t.Errorf("legacy marker NT = %q; want empty", md.NT)
	}
	if md.SK != "abc123" {
		t.Errorf("SK = %q; want abc123", md.SK)
	}
	if stripped != "Fix the leak." {
		t.Errorf("stripped = %q; want %q", stripped, "Fix the leak.")
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
