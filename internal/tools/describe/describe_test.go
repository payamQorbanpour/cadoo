package describe

import (
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

func TestRenderWalkthroughBucketsByLabel(t *testing.T) {
	files := []vcs.FileChange{
		{Path: "internal/kb/service.go", Additions: 40, Deletions: 3},
		{Path: "internal/kb/service_test.go", Additions: 146, Deletions: 8},
		{Path: "internal/kb/guardrail_test.go", Additions: 25, Deletions: 2},
		{Path: ".github/workflows/ci.yaml", Additions: 5, Deletions: 1},
		{Path: "README.md", Additions: 12, Deletions: 0},
		{Path: "internal/kb/util.go", Additions: 1, Deletions: 1}, // unlabeled → Additional
	}
	items := []WalkthroughFile{
		{Path: "internal/kb/service.go", Label: "Enhancement", Description: "Wire autoRag pipeline into EditKnowledgeBase"},
		{Path: "internal/kb/service_test.go", Label: "Tests", Description: "Add unit tests for EditKnowledgeBase with autoRag scenarios"},
		{Path: "internal/kb/guardrail_test.go", Label: "tests", Description: "Update mock repository with new interface methods"},
		{Path: ".github/workflows/ci.yaml", Label: "Configuration changes", Description: "Bump go to 1.23"},
		{Path: "README.md", Label: "documentation", Description: "Document new env vars"},
	}
	got := renderWalkthrough(items, files, true)
	if got == "" {
		t.Fatal("expected non-empty walkthrough")
	}

	// Must wrap the entire section in a collapsible details block titled
	// "File Walkthrough" — this is the marker the README points to.
	if !strings.Contains(got, "<strong>File Walkthrough</strong>") {
		t.Errorf("missing File Walkthrough header:\n%s", got)
	}

	// Each populated category must appear exactly once.
	for _, label := range []string{
		"Enhancement", "Tests", "Documentation",
		"Configuration changes", "Additional files",
	} {
		if c := strings.Count(got, "<strong>"+label+"</strong>"); c != 1 {
			t.Errorf("label %q: want 1 occurrence, got %d", label, c)
		}
	}
	// Bug fix and Formatting were not in the input — they must not appear.
	for _, label := range []string{"Bug fix", "Formatting"} {
		if strings.Contains(got, "<strong>"+label+"</strong>") {
			t.Errorf("label %q should not appear when bucket is empty", label)
		}
	}

	// Tests bucket should hold both _test.go files (one was labeled "tests"
	// lowercase — canonicalLabel must normalize).
	if !strings.Contains(got, "service_test.go") || !strings.Contains(got, "guardrail_test.go") {
		t.Errorf("Tests bucket missing files:\n%s", got)
	}

	// Unlabeled util.go must fall into Additional files.
	addIdx := strings.Index(got, "<strong>Additional files</strong>")
	if addIdx < 0 {
		t.Fatalf("Additional files row missing")
	}
	if !strings.Contains(got[addIdx:], "util.go") {
		t.Errorf("util.go expected in Additional files bucket")
	}

	// Diff counts must round-trip from the FileChange.
	if !strings.Contains(got, "+146/-8") {
		t.Errorf("expected service_test.go diff counts +146/-8")
	}
}

func TestRenderWalkthroughEmptyWhenNoFiles(t *testing.T) {
	if got := renderWalkthrough(nil, nil, true); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderWalkthroughIgnoresPhantomPaths(t *testing.T) {
	// LLM hallucinated a file that isn't in the actual diff — it must be
	// dropped silently rather than rendered (preventing fake rows).
	files := []vcs.FileChange{
		{Path: "real.go", Additions: 1, Deletions: 0},
	}
	items := []WalkthroughFile{
		{Path: "hallucinated.go", Label: "Enhancement", Description: "Not real"},
		{Path: "real.go", Label: "Enhancement", Description: "Real change"},
	}
	got := renderWalkthrough(items, files, true)
	if strings.Contains(got, "hallucinated.go") {
		t.Errorf("phantom path leaked into output:\n%s", got)
	}
	if !strings.Contains(got, "real.go") {
		t.Errorf("real path missing:\n%s", got)
	}
}

func TestBuildSectionPutsWalkthroughLast(t *testing.T) {
	out := Output{
		Title:   "Refactor KB service",
		Intent:  "Tighten the EditKnowledgeBase path.",
		Type:    "Enhancement",
		Changes: []string{"Wire autoRag flag through service"},
		Risks:   []string{"Low — covered by new tests."},
		Walkthrough: []WalkthroughFile{
			{Path: "kb.go", Label: "Enhancement", Description: "Add flag"},
		},
	}
	files := []vcs.FileChange{{Path: "kb.go", Additions: 5, Deletions: 1}}
	got := buildSection(out, files, true)

	changesIdx := strings.Index(got, "**Changes**")
	risksIdx := strings.Index(got, "**Risks**")
	walkIdx := strings.Index(got, "File Walkthrough")
	if changesIdx < 0 || risksIdx < 0 || walkIdx < 0 {
		t.Fatalf("missing sections:\n%s", got)
	}
	if changesIdx >= risksIdx || risksIdx >= walkIdx {
		t.Errorf("expected order Changes → Risks → Walkthrough, got indices %d/%d/%d", changesIdx, risksIdx, walkIdx)
	}
	if !strings.Contains(got, risksIcon) {
		t.Errorf("expected Risks header to include the risks icon:\n%s", got)
	}
}

func TestBuildSectionWithoutImageStripsWalkthroughIcon(t *testing.T) {
	out := Output{
		Title:   "x",
		Changes: []string{"c"},
		Walkthrough: []WalkthroughFile{
			{Path: "a.go", Label: "Enhancement", Description: "d"},
		},
	}
	files := []vcs.FileChange{{Path: "a.go"}}

	withImg := buildSection(out, files, true)
	withoutImg := buildSection(out, files, false)

	if !strings.Contains(withImg, filesIcon) {
		t.Errorf("expected PR-body variant to keep walkthrough image")
	}
	if strings.Contains(withoutImg, "<img") {
		t.Errorf("comment variant must not contain <img>:\n%s", withoutImg)
	}
	if !strings.Contains(withoutImg, "<strong>File Walkthrough</strong>") {
		t.Errorf("comment variant must keep walkthrough table:\n%s", withoutImg)
	}
}

func TestCanonicalLabel(t *testing.T) {
	cases := map[string]string{
		"Enhancement":           labelEnhancement,
		"  enhancement  ":       labelEnhancement,
		"feat":                  labelEnhancement,
		"Bug fix":               labelBugFix,
		"bugfix":                labelBugFix,
		"tests":                 labelTests,
		"Test":                  labelTests,
		"docs":                  labelDocs,
		"Documentation":         labelDocs,
		"Configuration changes": labelConfig,
		"config":                labelConfig,
		"Formatting":            labelFormatting,
		"style":                 labelFormatting,
		"Additional files":      labelAdditional,
		"":                      "",
		"random-unknown-label":  "",
	}
	for in, want := range cases {
		if got := canonicalLabel(in); got != want {
			t.Errorf("canonicalLabel(%q) = %q; want %q", in, got, want)
		}
	}
}
