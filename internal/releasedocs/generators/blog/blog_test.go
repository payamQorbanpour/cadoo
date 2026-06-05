package blog_test

import (
	"context"
	"errors"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/llm"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/generators/blog"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// fakeNoChatLLM is a fake llm.Provider that fails the test if Chat is invoked.
// Use it for nil-LLM code paths to prove no Chat call occurs.
type fakeNoChatLLM struct {
	t *testing.T
}

// Chat implements llm.Provider — fails the test unconditionally.
func (f *fakeNoChatLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	f.t.Helper()
	f.t.Fatal("Chat must not be called when LLM is expected to be unused")
	return nil, errors.New("unexpected Chat call")
}

// countingLLM records how many times Chat is invoked and returns a canned response.
type countingLLM struct {
	calls int
	resp  string
}

// Chat implements llm.Provider.
func (c *countingLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.calls++
	return &llm.ChatResponse{Content: c.resp}, nil
}

// errorLLM always returns an error from Chat to exercise the fallback path.
type errorLLM struct {
	calls int
}

// Chat implements llm.Provider — always returns an error.
func (e *errorLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	e.calls++
	return nil, errors.New("simulated LLM error")
}

// fixtureBlogRC builds a deterministic ReleaseContext for blog generator tests.
func fixtureBlogRC(llmProvider llm.Provider, bump releasedocs.SemverBump, blogEnabled bool, when string) releasedocs.ReleaseContext {
	commits := []vcs.Commit{
		{SHA: "abc0001", Message: "feat: new dashboard feature", Author: "alice"},
		{SHA: "abc0002", Message: "feat: improved onboarding flow", Author: "bob"},
		{SHA: "abc0003", Message: "fix: resolved sign-in edge case", Author: "carol"},
	}
	cfg := config.ReleaseDocs{
		Artifacts: config.ReleaseArtifacts{
			Blog: config.ArtifactConfig{
				Enabled: blogEnabled,
				When:    when,
			},
		},
	}
	grouped := releasedocs.BuildGroupedModel(commits, nil, cfg)
	return releasedocs.ReleaseContext{
		Repo:         "owner/repo",
		FromRef:      "v1.1.0",
		ToRef:        "v1.2.0",
		Bump:         bump,
		Commits:      commits,
		Config:       cfg,
		LLM:          llmProvider,
		Model:        "gpt-4o",
		GroupedModel: grouped,
	}
}

// TestBlogKind verifies that Kind() returns KindBlog.
func TestBlogKind(t *testing.T) {
	t.Parallel()
	g := blog.New()
	if got := g.Kind(); got != releasedocs.KindBlog {
		t.Errorf("Kind() = %q; want %q", got, releasedocs.KindBlog)
	}
}

// TestBlogEnabledMinorOrAboveDefault verifies that when When is empty, the blog
// generator applies "minor_or_above" as its default — different from the shared
// releasedocs.Enabled which treats empty When as "always". So with Enabled=true
// and When="", BumpMinor and BumpMajor return true, BumpPatch and BumpNone return false.
func TestBlogEnabledMinorOrAboveDefault(t *testing.T) {
	t.Parallel()
	g := blog.New()

	tests := []struct {
		name string
		bump releasedocs.SemverBump
		want bool
	}{
		{"minor bump enabled by default", releasedocs.BumpMinor, true},
		{"major bump enabled by default", releasedocs.BumpMajor, true},
		{"patch bump skipped by default", releasedocs.BumpPatch, false},
		{"none bump skipped by default", releasedocs.BumpNone, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.ReleaseDocs{
				Artifacts: config.ReleaseArtifacts{
					Blog: config.ArtifactConfig{
						Enabled: true,
						When:    "", // empty → blog defaults to minor_or_above
					},
				},
			}
			got := g.Enabled(cfg, tc.bump)
			if got != tc.want {
				t.Errorf("Enabled(When=%q, bump=%q) = %v; want %v", "", tc.bump, got, tc.want)
			}
		})
	}
}

// TestBlogEnabledExplicitAlways verifies that When="always" with Enabled=true
// returns true for all bumps (user override respected).
func TestBlogEnabledExplicitAlways(t *testing.T) {
	t.Parallel()
	g := blog.New()

	bumps := []releasedocs.SemverBump{
		releasedocs.BumpMajor,
		releasedocs.BumpMinor,
		releasedocs.BumpPatch,
		releasedocs.BumpNone,
	}
	for _, bump := range bumps {
		bump := bump
		t.Run(string(bump), func(t *testing.T) {
			t.Parallel()
			cfg := config.ReleaseDocs{
				Artifacts: config.ReleaseArtifacts{
					Blog: config.ArtifactConfig{
						Enabled: true,
						When:    "always",
					},
				},
			}
			got := g.Enabled(cfg, bump)
			if !got {
				t.Errorf("Enabled(When=%q, bump=%q) = false; want true", "always", bump)
			}
		})
	}
}

// TestBlogEnabledPatchSkipped verifies the patch skip: with default When and
// Enabled=true, BumpPatch returns false.
func TestBlogEnabledPatchSkipped(t *testing.T) {
	t.Parallel()
	g := blog.New()
	cfg := config.ReleaseDocs{
		Artifacts: config.ReleaseArtifacts{
			Blog: config.ArtifactConfig{
				Enabled: true,
				When:    "", // default is minor_or_above
			},
		},
	}
	if g.Enabled(cfg, releasedocs.BumpPatch) {
		t.Error("Enabled(BumpPatch) = true; want false (patch should be skipped by default)")
	}
}

// TestBlogDisabled verifies that Enabled=false returns false regardless of bump
// or When value.
func TestBlogDisabled(t *testing.T) {
	t.Parallel()
	g := blog.New()

	bumps := []releasedocs.SemverBump{
		releasedocs.BumpMajor,
		releasedocs.BumpMinor,
		releasedocs.BumpPatch,
		releasedocs.BumpNone,
	}
	whens := []string{"", "always", "minor_or_above", "major"}
	for _, bump := range bumps {
		for _, when := range whens {
			bump, when := bump, when
			t.Run(string(bump)+"/"+when, func(t *testing.T) {
				t.Parallel()
				cfg := config.ReleaseDocs{
					Artifacts: config.ReleaseArtifacts{
						Blog: config.ArtifactConfig{
							Enabled: false,
							When:    when,
						},
					},
				}
				got := g.Enabled(cfg, bump)
				if got {
					t.Errorf("Enabled(Enabled=false, When=%q, bump=%q) = true; want false", when, bump)
				}
			})
		}
	}
}

// TestBlogGenerateNilLLMSkeleton verifies that when rc.LLM is nil, Generate
// returns a non-empty deterministic skeleton artifact with Kind=KindBlog, and
// that no Chat call is made (enforced by fakeNoChatLLM which fails the test on
// any Chat invocation).
func TestBlogGenerateNilLLMSkeleton(t *testing.T) {
	t.Parallel()
	g := blog.New()

	// Use nil LLM — not fakeNoChatLLM — to strictly test the nil path.
	rc := fixtureBlogRC(nil, releasedocs.BumpMinor, true, "")

	got1, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate (nil LLM): %v", err)
	}

	if len(got1.Content) == 0 {
		t.Error("Generate returned empty content with nil LLM")
	}
	if got1.Kind != releasedocs.KindBlog {
		t.Errorf("Kind = %q; want %q", got1.Kind, releasedocs.KindBlog)
	}

	// Determinism: two calls should produce identical output.
	got2, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate 2nd run (nil LLM): %v", err)
	}
	if string(got1.Content) != string(got2.Content) {
		t.Errorf("Generate is not deterministic with nil LLM:\nrun1:\n%s\nrun2:\n%s", got1.Content, got2.Content)
	}
}

// TestBlogGenerateSingleChatCall verifies that when rc.LLM is non-nil, Generate
// calls Chat exactly once and the returned content reflects the LLM narrative.
func TestBlogGenerateSingleChatCall(t *testing.T) {
	t.Parallel()
	g := blog.New()

	fake := &countingLLM{resp: "We are thrilled to announce the release of v1.2.0!"}
	rc := fixtureBlogRC(fake, releasedocs.BumpMinor, true, "")

	got, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate (with LLM): %v", err)
	}

	if fake.calls != 1 {
		t.Errorf("Chat called %d times; want exactly 1", fake.calls)
	}
	if got.Kind != releasedocs.KindBlog {
		t.Errorf("Kind = %q; want %q", got.Kind, releasedocs.KindBlog)
	}
	if len(got.Content) == 0 {
		t.Error("Generate returned empty content with non-nil LLM")
	}
}

// TestBlogGenerateLLMErrorFallback verifies that when Chat returns an error,
// Generate falls back to the deterministic skeleton without propagating the error.
func TestBlogGenerateLLMErrorFallback(t *testing.T) {
	t.Parallel()
	g := blog.New()

	errLLM := &errorLLM{}
	rc := fixtureBlogRC(errLLM, releasedocs.BumpMinor, true, "")

	got, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate (LLM error) should not propagate error; got: %v", err)
	}
	if errLLM.calls != 1 {
		t.Errorf("Chat called %d times; want 1 (fallback, not retry)", errLLM.calls)
	}
	if len(got.Content) == 0 {
		t.Error("Generate returned empty content even on LLM error fallback")
	}
	if got.Kind != releasedocs.KindBlog {
		t.Errorf("Kind = %q; want %q", got.Kind, releasedocs.KindBlog)
	}
}

// TestBlogGenerateOutputKindBlog verifies that Generate output Kind matches
// both g.Kind() and the KindBlog constant.
func TestBlogGenerateOutputKindBlog(t *testing.T) {
	t.Parallel()
	g := blog.New()
	rc := fixtureBlogRC(nil, releasedocs.BumpMajor, true, "")

	got, err := g.Generate(context.Background(), rc)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got.Kind != releasedocs.KindBlog {
		t.Errorf("Generate().Kind = %q; want %q", got.Kind, releasedocs.KindBlog)
	}
	if got.Kind != g.Kind() {
		t.Errorf("Generate().Kind %q != g.Kind() %q", got.Kind, g.Kind())
	}
}
