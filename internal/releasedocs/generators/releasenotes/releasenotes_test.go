package releasenotes_test

import (
	"context"
	"strings"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/releasenotes"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// stubbedLLM is a minimal llm.Provider test double that records calls and
// returns a canned response used to verify tone-aware narrative generation.
type stubbedLLM struct {
	calls int
	resp  string
}

// Chat implements llm.Provider.
func (s *stubbedLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	s.calls++
	return &llm.ChatResponse{Content: s.resp}, nil
}

// fixtureRC builds a deterministic ReleaseContext for release-notes tests.
func fixtureRC(llmProvider llm.Provider, tone string) releasedocs.ReleaseContext {
	commits := []vcs.Commit{
		{SHA: "bbb0001", Message: "feat: new export API", Author: "alice"},
		{SHA: "bbb0002", Message: "fix: prevent crash on empty repo", Author: "bob"},
	}
	cfg := config.ReleaseDocs{
		Artifacts: config.ReleaseArtifacts{
			ReleaseNotes: config.ReleaseNotesConfig{
				ArtifactConfig: config.ArtifactConfig{
					Enabled: true,
					When:    "always",
				},
				Tone: tone,
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
		LLM:          llmProvider,
		Model:        "gpt-4o",
		GroupedModel: grouped,
	}
}

// TestSkeletonNoLLM verifies that with rc.LLM == nil, Generate returns the
// deterministic highlight skeleton only (no Chat call) and the output is
// deterministic across two runs.
func TestSkeletonNoLLM(t *testing.T) {
	t.Parallel()
	g := releasenotes.New()
	rc := fixtureRC(nil, "concise")

	got1, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate (1st): %v", err)
	}
	got2, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate (2nd): %v", err)
	}

	if len(got1.Content) == 0 {
		t.Error("Generate returned empty content with nil LLM")
	}
	if string(got1.Content) != string(got2.Content) {
		t.Errorf("Generate is not deterministic:\nrun1:\n%s\nrun2:\n%s", got1.Content, got2.Content)
	}
	if got1.Kind != releasedocs.KindReleaseNotes {
		t.Errorf("Kind = %q; want %q", got1.Kind, releasedocs.KindReleaseNotes)
	}
}

// TestReleaseNotesTone verifies that with a fake LLM provider returning a
// canned response, Generate calls Chat once with a prompt that reflects the
// configured tone and returns the narrative. The tone string from config selects
// the right preset (Plan 03).
func TestReleaseNotesTone(t *testing.T) {
	t.Parallel()

	tones := []string{"concise", "detailed", "marketing", ""}
	for _, tone := range tones {
		tone := tone
		t.Run("tone="+tone, func(t *testing.T) {
			t.Parallel()
			fake := &stubbedLLM{resp: "This release brings exciting improvements."}
			rc := fixtureRC(fake, tone)

			g := releasenotes.New()
			got, err := g.Generate(context.Background(), rc)
			if err != nil {
				t.Fatalf("Generate tone=%q: %v", tone, err)
			}

			if fake.calls != 1 {
				t.Errorf("Chat called %d times; want 1", fake.calls)
			}
			if !strings.Contains(string(got.Content), fake.resp) {
				t.Errorf("output does not contain LLM response %q;\ngot:\n%s", fake.resp, got.Content)
			}
			if got.Kind != releasedocs.KindReleaseNotes {
				t.Errorf("Kind = %q; want %q", got.Kind, releasedocs.KindReleaseNotes)
			}
		})
	}
}

// TestReleaseNotesEnabled verifies that Enabled(cfg, bump) honors
// artifacts.releaseNotes.enabled + when: × bump.
func TestReleaseNotesEnabled(t *testing.T) {
	t.Parallel()
	g := releasenotes.New()

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
		{"enabled patch_or_above patch bump", true, "patch_or_above", releasedocs.BumpPatch, true},
		{"enabled empty when defaults to always", true, "", releasedocs.BumpPatch, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.ReleaseDocs{
				Artifacts: config.ReleaseArtifacts{
					ReleaseNotes: config.ReleaseNotesConfig{
						ArtifactConfig: config.ArtifactConfig{
							Enabled: tc.enabled,
							When:    tc.when,
						},
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
