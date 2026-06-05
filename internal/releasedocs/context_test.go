package releasedocs_test

import (
	"context"
	"testing"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// minimalProvider is a test helper that implements only vcs.Provider (no
// optional capabilities). It is used to test graceful degradation when the
// provider lacks vcs.ReleaseRangeReader.
type minimalProvider struct{}

func (m *minimalProvider) Kind() vcs.Kind { return vcs.KindGitHub }
func (m *minimalProvider) FetchPullRequest(_ context.Context, _ string, _ int64) (*vcs.PullRequest, error) {
	return &vcs.PullRequest{}, nil
}
func (m *minimalProvider) ListChangedFiles(_ context.Context, _ *vcs.PullRequest) ([]vcs.FileChange, error) {
	return nil, nil
}
func (m *minimalProvider) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, _ string) (string, error) {
	return "", nil
}
func (m *minimalProvider) UpdateSummaryComment(_ context.Context, _ *vcs.PullRequest, _, _ string) error {
	return nil
}
func (m *minimalProvider) PostInlineComments(_ context.Context, _ *vcs.PullRequest, _ []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	return nil, nil
}
func (m *minimalProvider) ResolveThread(_ context.Context, _ *vcs.PullRequest, _ string) error {
	return nil
}
func (m *minimalProvider) EditPullRequestBody(_ context.Context, _ *vcs.PullRequest, _ string) error {
	return nil
}
func (m *minimalProvider) UpsertCheckRun(_ context.Context, _ *vcs.PullRequest, _ vcs.CheckRun) error {
	return nil
}

var _ vcs.Provider = (*minimalProvider)(nil)

// TestBump verifies that ComputeBump returns the correct SemverBump for tag
// pairs and edge cases (first-release, malformed tags).
func TestBump(t *testing.T) {
	cases := []struct {
		name    string
		fromRef string
		toRef   string
		want    releasedocs.SemverBump
	}{
		{
			name:    "major bump",
			fromRef: "v1.2.3",
			toRef:   "v2.0.0",
			want:    releasedocs.BumpMajor,
		},
		{
			name:    "minor bump",
			fromRef: "v1.2.3",
			toRef:   "v1.3.0",
			want:    releasedocs.BumpMinor,
		},
		{
			name:    "patch bump",
			fromRef: "v1.2.3",
			toRef:   "v1.2.4",
			want:    releasedocs.BumpPatch,
		},
		{
			name:    "no bump same version",
			fromRef: "v1.2.3",
			toRef:   "v1.2.3",
			want:    releasedocs.BumpNone,
		},
		{
			name:    "first release empty fromRef",
			fromRef: "",
			toRef:   "v1.0.0",
			want:    releasedocs.BumpMajor,
		},
		{
			name:    "tags without v prefix normalized",
			fromRef: "1.2.3",
			toRef:   "1.3.0",
			want:    releasedocs.BumpMinor,
		},
		{
			name:    "malformed toRef yields BumpNone not panic",
			fromRef: "v1.0.0",
			toRef:   "notasemver",
			want:    releasedocs.BumpNone,
		},
		{
			name:    "malformed fromRef yields BumpMajor (treat as first release)",
			fromRef: "notasemver",
			toRef:   "v2.0.0",
			want:    releasedocs.BumpMajor,
		},
		{
			name:    "major multi-digit",
			fromRef: "v9.99.0",
			toRef:   "v10.0.0",
			want:    releasedocs.BumpMajor,
		},
		{
			name:    "minor only differs in minor",
			fromRef: "v2.1.0",
			toRef:   "v2.2.0",
			want:    releasedocs.BumpMinor,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := releasedocs.ComputeBump(tc.fromRef, tc.toRef)
			if got != tc.want {
				t.Errorf("ComputeBump(%q, %q) = %q; want %q", tc.fromRef, tc.toRef, got, tc.want)
			}
		})
	}
}

// TestEnabledMatrix verifies the Enabled gate logic: enabled flag, when:
// condition, and bump combinations per D-08.
func TestEnabledMatrix(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		when    string
		bump    releasedocs.SemverBump
		want    bool
	}{
		// disabled flag overrides everything
		{name: "disabled regardless of bump", enabled: false, when: "", bump: releasedocs.BumpMajor, want: false},
		{name: "disabled with when=major", enabled: false, when: "major", bump: releasedocs.BumpMajor, want: false},

		// enabled=true, empty when means always
		{name: "enabled no when always", enabled: true, when: "", bump: releasedocs.BumpMajor, want: true},
		{name: "enabled no when patch", enabled: true, when: "", bump: releasedocs.BumpPatch, want: true},
		{name: "enabled no when none", enabled: true, when: "", bump: releasedocs.BumpNone, want: true},

		// when=always
		{name: "when=always major", enabled: true, when: "always", bump: releasedocs.BumpMajor, want: true},
		{name: "when=always patch", enabled: true, when: "always", bump: releasedocs.BumpPatch, want: true},
		{name: "when=always none", enabled: true, when: "always", bump: releasedocs.BumpNone, want: true},

		// when=major
		{name: "when=major + major bump", enabled: true, when: "major", bump: releasedocs.BumpMajor, want: true},
		{name: "when=major + minor bump", enabled: true, when: "major", bump: releasedocs.BumpMinor, want: false},
		{name: "when=major + patch bump", enabled: true, when: "major", bump: releasedocs.BumpPatch, want: false},
		{name: "when=major + none bump", enabled: true, when: "major", bump: releasedocs.BumpNone, want: false},

		// when=minor
		{name: "when=minor + major bump", enabled: true, when: "minor", bump: releasedocs.BumpMajor, want: false},
		{name: "when=minor + minor bump", enabled: true, when: "minor", bump: releasedocs.BumpMinor, want: true},
		{name: "when=minor + patch bump", enabled: true, when: "minor", bump: releasedocs.BumpPatch, want: false},

		// when=patch
		{name: "when=patch + patch bump", enabled: true, when: "patch", bump: releasedocs.BumpPatch, want: true},
		{name: "when=patch + major bump", enabled: true, when: "patch", bump: releasedocs.BumpMajor, want: false},

		// when=minor_or_above
		{name: "when=minor_or_above + major", enabled: true, when: "minor_or_above", bump: releasedocs.BumpMajor, want: true},
		{name: "when=minor_or_above + minor", enabled: true, when: "minor_or_above", bump: releasedocs.BumpMinor, want: true},
		{name: "when=minor_or_above + patch", enabled: true, when: "minor_or_above", bump: releasedocs.BumpPatch, want: false},
		{name: "when=minor_or_above + none", enabled: true, when: "minor_or_above", bump: releasedocs.BumpNone, want: false},

		// when=patch_or_above
		{name: "when=patch_or_above + major", enabled: true, when: "patch_or_above", bump: releasedocs.BumpMajor, want: true},
		{name: "when=patch_or_above + minor", enabled: true, when: "patch_or_above", bump: releasedocs.BumpMinor, want: true},
		{name: "when=patch_or_above + patch", enabled: true, when: "patch_or_above", bump: releasedocs.BumpPatch, want: true},
		{name: "when=patch_or_above + none", enabled: true, when: "patch_or_above", bump: releasedocs.BumpNone, want: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			artifactCfg := config.ArtifactConfig{
				Enabled: tc.enabled,
				When:    tc.when,
			}
			got := releasedocs.Enabled(artifactCfg, tc.bump)
			if got != tc.want {
				t.Errorf("Enabled({enabled:%v, when:%q}, %q) = %v; want %v",
					tc.enabled, tc.when, tc.bump, got, tc.want)
			}
		})
	}
}

// TestBuildContext verifies that BuildContext correctly builds a ReleaseContext
// from a fake provider.
func TestBuildContext(t *testing.T) {
	t.Run("resolves from ref when empty via LatestTagBefore", func(t *testing.T) {
		fake, provider := releasedocstest.NewFake()
		fake.LatestTag = "v1.0.0"
		fake.Commits = []vcs.Commit{
			{SHA: "abc", Message: "feat: initial", Author: "alice", Date: time.Now()},
		}

		job := releasedocs.ReleaseJob{
			Provider: vcs.KindGitHub,
			Repo:     "owner/repo",
			Org:      "org1",
			FromRef:  "", // empty — should be resolved
			ToRef:    "v1.1.0",
		}
		cfg := config.ReleaseDocs{
			Grouping: config.ReleaseGrouping{
				Source:   "conventional",
				Sections: []string{"Features"},
			},
		}

		rc, err := releasedocs.BuildContext(context.Background(), provider, job, cfg, nil, "")
		if err != nil {
			t.Fatalf("BuildContext returned error: %v", err)
		}
		if rc.FromRef != "v1.0.0" {
			t.Errorf("FromRef: want %q, got %q", "v1.0.0", rc.FromRef)
		}
		if rc.ToRef != "v1.1.0" {
			t.Errorf("ToRef: want %q, got %q", "v1.1.0", rc.ToRef)
		}
		if fake.LatestTagBeforeCalls != 1 {
			t.Errorf("LatestTagBefore: want 1 call, got %d", fake.LatestTagBeforeCalls)
		}
		if fake.ListCommitsCalls != 1 {
			t.Errorf("ListCommits: want 1 call, got %d", fake.ListCommitsCalls)
		}
		if fake.ListMergedPRsCalls != 1 {
			t.Errorf("ListMergedPRs: want 1 call, got %d", fake.ListMergedPRsCalls)
		}
	})

	t.Run("uses provided fromRef without calling LatestTagBefore", func(t *testing.T) {
		fake, provider := releasedocstest.NewFake()
		fake.Commits = []vcs.Commit{
			{SHA: "def", Message: "fix: small fix", Author: "bob", Date: time.Now()},
		}

		job := releasedocs.ReleaseJob{
			Provider: vcs.KindGitHub,
			Repo:     "owner/repo",
			Org:      "org1",
			FromRef:  "v2.0.0",
			ToRef:    "v2.0.1",
		}
		cfg := config.ReleaseDocs{
			Grouping: config.ReleaseGrouping{
				Source:   "conventional",
				Sections: []string{"Bug Fixes"},
			},
		}

		rc, err := releasedocs.BuildContext(context.Background(), provider, job, cfg, nil, "")
		if err != nil {
			t.Fatalf("BuildContext returned error: %v", err)
		}
		if fake.LatestTagBeforeCalls != 0 {
			t.Errorf("LatestTagBefore: want 0 calls when fromRef provided, got %d", fake.LatestTagBeforeCalls)
		}
		if rc.Bump != releasedocs.BumpPatch {
			t.Errorf("Bump: want BumpPatch, got %q", rc.Bump)
		}
	})

	t.Run("computes bump from fromRef and toRef", func(t *testing.T) {
		_, provider := releasedocstest.NewFake()

		job := releasedocs.ReleaseJob{
			Provider: vcs.KindGitHub,
			Repo:     "owner/repo",
			Org:      "org1",
			FromRef:  "v1.0.0",
			ToRef:    "v2.0.0",
		}
		cfg := config.ReleaseDocs{
			Grouping: config.ReleaseGrouping{
				Source:   "conventional",
				Sections: []string{"Breaking Changes", "Features"},
			},
		}

		rc, err := releasedocs.BuildContext(context.Background(), provider, job, cfg, nil, "")
		if err != nil {
			t.Fatalf("BuildContext returned error: %v", err)
		}
		if rc.Bump != releasedocs.BumpMajor {
			t.Errorf("Bump: want BumpMajor, got %q", rc.Bump)
		}
	})

	t.Run("degrades gracefully when provider lacks ReleaseRangeReader", func(t *testing.T) {
		// Use a provider that genuinely does not implement vcs.ReleaseRangeReader
		// (the releasedocstest.Fake uses embedding which promotes all methods).
		provider := &minimalProvider{}

		job := releasedocs.ReleaseJob{
			Provider: vcs.KindGitHub,
			Repo:     "owner/repo",
			Org:      "org1",
			FromRef:  "v1.0.0",
			ToRef:    "v1.1.0",
		}
		cfg := config.ReleaseDocs{}

		// Should return an error because range reader is required for generation.
		_, err := releasedocs.BuildContext(context.Background(), provider, job, cfg, nil, "")
		if err == nil {
			t.Error("expected error when provider lacks ReleaseRangeReader, got nil")
		}
	})

	t.Run("grouped model is built once in context", func(t *testing.T) {
		fake, provider := releasedocstest.NewFake()
		fake.Commits = []vcs.Commit{
			{SHA: "abc", Message: "feat: new feature", Author: "alice", Date: time.Now()},
			{SHA: "def", Message: "fix: bug fix", Author: "bob", Date: time.Now()},
		}
		job := releasedocs.ReleaseJob{
			Provider: vcs.KindGitHub,
			Repo:     "owner/repo",
			Org:      "org1",
			FromRef:  "v1.0.0",
			ToRef:    "v1.1.0",
		}
		cfg := config.ReleaseDocs{
			Grouping: config.ReleaseGrouping{
				Source:   "conventional",
				Sections: []string{"Features", "Bug Fixes"},
			},
		}

		rc, err := releasedocs.BuildContext(context.Background(), provider, job, cfg, nil, "")
		if err != nil {
			t.Fatalf("BuildContext returned error: %v", err)
		}
		if rc.GroupedModel.Sections == nil {
			t.Error("GroupedModel should be populated in ReleaseContext")
		}
		// Should have at least Features and Bug Fixes sections
		found := map[string]bool{}
		for _, s := range rc.GroupedModel.Sections {
			found[s.Title] = true
		}
		if !found["Features"] {
			t.Error("expected Features section in GroupedModel")
		}
		if !found["Bug Fixes"] {
			t.Error("expected Bug Fixes section in GroupedModel")
		}
	})
}
