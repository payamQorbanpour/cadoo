package template_test

import (
	"context"
	"errors"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	rdtemplate "github.com/payamqorbanpour/cadoo/internal/releasedocs/template"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// noFileFetcherProvider is a minimal vcs.Provider that does NOT implement
// releasedocs.FileFetcher, so type-assertions against that interface return
// (nil, false). Used to test the graceful fallback path in Resolve when the
// provider cannot fetch arbitrary repo files.
type noFileFetcherProvider struct{}

func (noFileFetcherProvider) Kind() vcs.Kind { return vcs.KindGitHub }
func (noFileFetcherProvider) FetchPullRequest(_ context.Context, _ string, _ int64) (*vcs.PullRequest, error) {
	return nil, nil
}
func (noFileFetcherProvider) ListChangedFiles(_ context.Context, _ *vcs.PullRequest) ([]vcs.FileChange, error) {
	return nil, nil
}
func (noFileFetcherProvider) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, _ string) (string, error) {
	return "", nil
}
func (noFileFetcherProvider) UpdateSummaryComment(_ context.Context, _ *vcs.PullRequest, _, _ string) error {
	return nil
}
func (noFileFetcherProvider) PostInlineComments(_ context.Context, _ *vcs.PullRequest, _ []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	return nil, nil
}
func (noFileFetcherProvider) ResolveThread(_ context.Context, _ *vcs.PullRequest, _ string) error {
	return nil
}
func (noFileFetcherProvider) EditPullRequestBody(_ context.Context, _ *vcs.PullRequest, _ string) error {
	return nil
}
func (noFileFetcherProvider) UpsertCheckRun(_ context.Context, _ *vcs.PullRequest, _ vcs.CheckRun) error {
	return nil
}

// Compile-time assertion: noFileFetcherProvider implements vcs.Provider.
var _ vcs.Provider = noFileFetcherProvider{}

// fixtureData returns a TemplateData fixture suitable for testing all presets.
func fixtureData() rdtemplate.Data {
	return rdtemplate.Data{
		ToRef:   "v1.2.3",
		FromRef: "v1.2.2",
		Groups: []rdtemplate.ChangeGroup{
			{
				Title: "Features",
				Items: []rdtemplate.ChangeItem{
					{Summary: "add widget support", Author: "alice", PR: &rdtemplate.PRRef{Number: 42, URL: "https://example.com/pr/42"}},
					{Summary: "improve dashboard", Author: "bob"},
				},
			},
			{
				Title: "Bug Fixes",
				Items: []rdtemplate.ChangeItem{
					{Summary: "fix nil pointer in handler", Author: "carol", PR: &rdtemplate.PRRef{Number: 43, URL: "https://example.com/pr/43"}},
				},
			},
		},
	}
}

// TestEmbeddedPresets verifies that every embedded preset loads without error,
// renders non-empty output against the fixture data, and produces identical
// output on two successive renders (determinism).
func TestEmbeddedPresets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind releasedocs.ArtifactKind
		tone string
	}{
		{name: "changelog", kind: releasedocs.KindChangelog, tone: ""},
		{name: "release-notes-concise", kind: releasedocs.KindReleaseNotes, tone: "concise"},
		{name: "release-notes-detailed", kind: releasedocs.KindReleaseNotes, tone: "detailed"},
		{name: "release-notes-marketing", kind: releasedocs.KindReleaseNotes, tone: "marketing"},
		{name: "release-notes-default-tone", kind: releasedocs.KindReleaseNotes, tone: ""},
	}

	data := fixtureData()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpl, err := rdtemplate.LoadPreset(tc.kind, tc.tone)
			if err != nil {
				t.Fatalf("LoadPreset(%q, %q) returned error: %v", tc.kind, tc.tone, err)
			}
			if tmpl == nil {
				t.Fatal("LoadPreset returned nil template")
			}

			// First render.
			out1, err := rdtemplate.Render(tmpl, data)
			if err != nil {
				t.Fatalf("first Render error: %v", err)
			}
			if out1 == "" {
				t.Fatal("first Render returned empty output")
			}

			// Second render — must be identical (determinism).
			out2, err := rdtemplate.Render(tmpl, data)
			if err != nil {
				t.Fatalf("second Render error: %v", err)
			}
			if out1 != out2 {
				t.Fatalf("non-deterministic output:\nfirst:\n%s\nsecond:\n%s", out1, out2)
			}
		})
	}
}

// TestLoadPresetUnknownKind verifies ErrUnknownKind is returned for an
// unrecognized artifact kind.
func TestLoadPresetUnknownKind(t *testing.T) {
	t.Parallel()
	_, err := rdtemplate.LoadPreset("unknown_kind", "")
	if !errors.Is(err, rdtemplate.ErrUnknownKind) {
		t.Fatalf("expected ErrUnknownKind, got %v", err)
	}
}

// TestTemplateOverride exercises the three Resolve paths:
// 1. Override present and successfully fetched → override template used.
// 2. Override path set but fetch returns missing-file (404) → falls back to preset.
// 3. No override path → preset used directly.
// 4. Provider does not implement FileFetcher → falls back to preset.
func TestTemplateOverride(t *testing.T) {
	t.Parallel()

	data := fixtureData()

	t.Run("override_used_when_present", func(t *testing.T) {
		t.Parallel()

		customTmpl := `CUSTOM:{{ .ToRef }}`
		fake, _ := releasedocstest.NewFake()
		fake.FileContent = []byte(customTmpl)

		rc := releasedocs.ReleaseContext{
			Repo:     "owner/repo",
			ToRef:    "v1.2.3",
			Provider: fake,
		}

		tmpl, err := rdtemplate.Resolve(context.Background(), rc, releasedocs.KindChangelog, ".cadoo/changelog.tmpl", "")
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}

		out, err := rdtemplate.Render(tmpl, data)
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if out != "CUSTOM:v1.2.3" {
			t.Fatalf("expected override output %q, got %q", "CUSTOM:v1.2.3", out)
		}
	})

	t.Run("fallback_on_missing_file", func(t *testing.T) {
		t.Parallel()

		fake, _ := releasedocstest.NewFake()
		// Return a 404-style error to trigger the missing-file fallback.
		fake.FetchErr = errors.New("404: not found")

		rc := releasedocs.ReleaseContext{
			Repo:     "owner/repo",
			ToRef:    "v1.2.3",
			Provider: fake,
		}

		tmpl, err := rdtemplate.Resolve(context.Background(), rc, releasedocs.KindChangelog, ".cadoo/changelog.tmpl", "")
		if err != nil {
			t.Fatalf("Resolve error on missing-file: %v", err)
		}

		out, err := rdtemplate.Render(tmpl, data)
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if out == "" {
			t.Fatal("expected non-empty fallback output")
		}
	})

	t.Run("no_override_uses_preset", func(t *testing.T) {
		t.Parallel()

		fake, _ := releasedocstest.NewFake()
		rc := releasedocs.ReleaseContext{
			Repo:     "owner/repo",
			ToRef:    "v1.2.3",
			Provider: fake,
		}

		// No override path → LoadPreset called directly.
		tmpl, err := rdtemplate.Resolve(context.Background(), rc, releasedocs.KindChangelog, "", "")
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		out, err := rdtemplate.Render(tmpl, data)
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if out == "" {
			t.Fatal("expected non-empty preset output")
		}

		// Verify we get the same output as a direct LoadPreset call (same template).
		presetTmpl, err := rdtemplate.LoadPreset(releasedocs.KindChangelog, "")
		if err != nil {
			t.Fatalf("LoadPreset error: %v", err)
		}
		presetOut, err := rdtemplate.Render(presetTmpl, data)
		if err != nil {
			t.Fatalf("LoadPreset Render error: %v", err)
		}
		if out != presetOut {
			t.Fatalf("Resolve(no override) output differs from LoadPreset output:\nResolve:\n%s\nLoadPreset:\n%s", out, presetOut)
		}
	})

	t.Run("provider_without_file_fetcher_uses_preset", func(t *testing.T) {
		t.Parallel()

		// noFileFetcherProvider does NOT implement releasedocs.FileFetcher, so
		// the type-assertion in Resolve returns (nil, false) and the embedded
		// preset is used.
		rc := releasedocs.ReleaseContext{
			Repo:     "owner/repo",
			ToRef:    "v1.2.3",
			Provider: noFileFetcherProvider{},
		}

		tmpl, err := rdtemplate.Resolve(context.Background(), rc, releasedocs.KindChangelog, ".cadoo/changelog.tmpl", "")
		if err != nil {
			t.Fatalf("Resolve error: %v", err)
		}
		out, err := rdtemplate.Render(tmpl, data)
		if err != nil {
			t.Fatalf("Render error: %v", err)
		}
		if out == "" {
			t.Fatal("expected non-empty preset output when FileFetcher absent")
		}
	})
}
