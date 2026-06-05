package changelog_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/changelog"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// updateGolden, when true, regenerates the golden file(s) instead of comparing.
// Run with: go test -run TestChangelogGolden -update
var updateGolden = flag.Bool("update", false, "regenerate golden files")

// fixtureRC builds a deterministic ReleaseContext for golden tests. LLM is nil
// so the deterministic render path is exercised. The grouped model contains
// feat/fix/perf/breaking entries in canonical section order.
func fixtureRC() releasedocs.ReleaseContext {
	commits := []vcs.Commit{
		{SHA: "aaa0001", Message: "feat!: drop legacy API\n\nBREAKING CHANGE: removed /v1 endpoint", Author: "alice"},
		{SHA: "aaa0002", Message: "feat: add dark mode support", Author: "bob"},
		{SHA: "aaa0003", Message: "fix: correct timezone handling in scheduler", Author: "carol"},
		{SHA: "aaa0004", Message: "perf: reduce DB round-trips in listing query", Author: "dave"},
	}
	cfg := config.ReleaseDocs{
		Artifacts: config.ReleaseArtifacts{
			Changelog: config.ArtifactConfig{
				Enabled: true,
				When:    "always",
			},
		},
	}
	grouped := releasedocs.BuildGroupedModel(commits, nil, cfg)
	return releasedocs.ReleaseContext{
		Repo:         "owner/repo",
		FromRef:      "v1.0.0",
		ToRef:        "v1.1.0",
		Bump:         releasedocs.BumpMinor,
		Commits:      commits,
		Config:       cfg,
		LLM:          nil, // deterministic path
		GroupedModel: grouped,
	}
}

// TestChangelogGolden verifies that Generate with nil LLM renders the grouped
// model to a markdown section that is byte-for-byte identical to
// testdata/basic.golden.
//
// Run with -update to regenerate the golden file.
func TestChangelogGolden(t *testing.T) {
	t.Parallel()
	g := changelog.New()
	rc := fixtureRC()

	got, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	goldenPath := filepath.Join("testdata", "basic.golden")

	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got.Content, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("golden file updated: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to regenerate)", goldenPath, err)
	}
	if string(got.Content) != string(want) {
		t.Errorf("Generate output does not match golden\n--- want ---\n%s\n--- got ---\n%s", want, got.Content)
	}
}

// TestChangelogDeterministic verifies that calling Generate twice with nil LLM
// produces identical output (Pitfall 3 — non-deterministic changelog breaks
// golden tests).
func TestChangelogDeterministic(t *testing.T) {
	t.Parallel()
	g := changelog.New()
	rc := fixtureRC()

	got1, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate (1st): %v", err)
	}
	got2, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate (2nd): %v", err)
	}
	if string(got1.Content) != string(got2.Content) {
		t.Errorf("Generate is not deterministic:\nrun1:\n%s\nrun2:\n%s", got1.Content, got2.Content)
	}
}

// TestChangelogEnabled verifies that Enabled(cfg, bump) correctly honors the
// enabled+when: gate by delegating to the Plan-02 releasedocs.Enabled helper.
func TestChangelogEnabled(t *testing.T) {
	t.Parallel()
	g := changelog.New()

	tests := []struct {
		name    string
		enabled bool
		when    string
		bump    releasedocs.SemverBump
		want    bool
	}{
		{"disabled always false", false, "always", releasedocs.BumpMinor, false},
		{"enabled always true", true, "always", releasedocs.BumpMinor, true},
		{"enabled major only minor bump", true, "major", releasedocs.BumpMinor, false},
		{"enabled major only major bump", true, "major", releasedocs.BumpMajor, true},
		{"enabled minor_or_above patch bump", true, "minor_or_above", releasedocs.BumpPatch, false},
		{"enabled minor_or_above minor bump", true, "minor_or_above", releasedocs.BumpMinor, true},
		{"enabled patch_or_above patch bump", true, "patch_or_above", releasedocs.BumpPatch, true},
		{"enabled empty when defaults to always", true, "", releasedocs.BumpPatch, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.ReleaseDocs{
				Artifacts: config.ReleaseArtifacts{
					Changelog: config.ArtifactConfig{
						Enabled: tc.enabled,
						When:    tc.when,
					},
				},
			}
			got := g.Enabled(cfg, tc.bump)
			if got != tc.want {
				t.Errorf("Enabled() = %v; want %v", got, tc.want)
			}
		})
	}
}

// stubLLM is a minimal llm.Provider test double that records calls and returns
// a canned response. Used to verify that with a nil LLM no Chat is attempted.
type stubLLM struct {
	calls int
	resp  string
}

// Chat implements llm.Provider.
func (s *stubLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	s.calls++
	return &llm.ChatResponse{Content: s.resp}, nil
}

// TestChangelogLLMPolishSkipped verifies that with a nil LLM, Generate never
// attempts a Chat call — the deterministic render is returned verbatim and the
// output is non-empty.
func TestChangelogLLMPolishSkipped(t *testing.T) {
	t.Parallel()
	g := changelog.New()
	rc := fixtureRC()
	// rc.LLM is already nil (set by fixtureRC); no Chat call should occur.

	got, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got.Content) == 0 {
		t.Error("Generate returned empty content with nil LLM")
	}
}
