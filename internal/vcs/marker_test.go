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

func TestParseInlineMarkerWithTrailingContent(t *testing.T) {
	body := "Fix the leak."
	marker := InlineMarker(MarkerData{Tool: "review", SK: "abc123", Sev: "warn"})
	// Simulate an AI prompt block appended after the fp marker.
	full := body + "\n\n" + marker + "\n\n<details><summary>Prompt for AI Agents</summary>\n\ncontent\n\n</details>"

	got, stripped, ok := ParseInlineMarker(full)
	if !ok {
		t.Fatalf("ParseInlineMarker ok=false; want true")
	}
	if got.SK != "abc123" {
		t.Errorf("SK = %q; want abc123", got.SK)
	}
	if stripped != body {
		t.Errorf("stripped = %q; want %q", stripped, body)
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

func TestRenderReviewedSHA(t *testing.T) {
	sha := "aabbccddeeff00112233445566778899aabbccdd"
	got := RenderReviewedSHA(sha)
	want := "<!-- cadoo:reviewed-sha:" + sha + " -->"
	if got != want {
		t.Errorf("RenderReviewedSHA(%q) = %q; want %q", sha, got, want)
	}
}

func TestParseReviewedSHA(t *testing.T) {
	validSHA := "aabbccddeeff00112233445566778899aabbccdd"
	body := SummaryWrapperBegin + "\n" + RenderReviewedSHA(validSHA) + "\nsome text"

	// valid SHA round-trips
	if got := ParseReviewedSHA(body); got != validSHA {
		t.Errorf("ParseReviewedSHA(valid) = %q; want %q", got, validSHA)
	}

	// absent marker returns ""
	if got := ParseReviewedSHA("no marker here"); got != "" {
		t.Errorf("ParseReviewedSHA(absent) = %q; want \"\"", got)
	}

	// invalid: too short (39 chars)
	short := "aabbccddeeff00112233445566778899aabbcc"
	if got := ParseReviewedSHA("<!-- cadoo:reviewed-sha:" + short + " -->"); got != "" {
		t.Errorf("ParseReviewedSHA(39-char) = %q; want \"\"", got)
	}

	// invalid: too long (41 chars)
	long := "aabbccddeeff00112233445566778899aabbccdde"
	if got := ParseReviewedSHA("<!-- cadoo:reviewed-sha:" + long + " -->"); got != "" {
		t.Errorf("ParseReviewedSHA(41-char) = %q; want \"\"", got)
	}

	// invalid: uppercase
	upper := "AABBCCDDEEFF00112233445566778899AABBCCDD"
	if got := ParseReviewedSHA("<!-- cadoo:reviewed-sha:" + upper + " -->"); got != "" {
		t.Errorf("ParseReviewedSHA(uppercase) = %q; want \"\"", got)
	}

	// invalid: non-hex char (g is not hex)
	nonhex := "aabbccddeeff00112233445566778899aabbccgg"
	if got := ParseReviewedSHA("<!-- cadoo:reviewed-sha:" + nonhex + " -->"); got != "" {
		t.Errorf("ParseReviewedSHA(non-hex) = %q; want \"\"", got)
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
