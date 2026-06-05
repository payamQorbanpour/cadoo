package pages_test

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/pages"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// minimalProvider implements only vcs.Provider with no optional capabilities.
// When passed to the pages publisher, type-assertion to vcs.BranchCommitter will
// return (nil, false), triggering graceful degradation (D-15).
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

// enabledPages returns a config.ReleaseDocs with pages publishing enabled and
// defaults (empty branch/dir so publisher applies "gh-pages"/"docs").
func enabledPages() config.ReleaseDocs {
	return config.ReleaseDocs{
		Publish: config.ReleasePublish{
			Pages: config.PagesPublishTarget{Enabled: true},
		},
	}
}

// enabledPagesCustom returns a config.ReleaseDocs with custom branch and dir.
func enabledPagesCustom(branch, dir string) config.ReleaseDocs {
	return config.ReleaseDocs{
		Publish: config.ReleasePublish{
			Pages: config.PagesPublishTarget{Enabled: true, Branch: branch, Dir: dir},
		},
	}
}

// TestTarget verifies Publisher.Target() returns TargetPages.
func TestTarget(t *testing.T) {
	t.Parallel()
	p := pages.Publisher{}
	if got := p.Target(); got != releasedocs.TargetPages {
		t.Errorf("Target() = %q; want %q", got, releasedocs.TargetPages)
	}
}

// TestDeterministicPaths verifies that with default dir, all three artifact
// kinds produce paths "docs/releases/v1.2.3/{kind}.md".
func TestDeterministicPaths(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
		Config:   enabledPages(),
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindChangelog, Content: []byte("changelog content")},
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("release notes content")},
		{Kind: releasedocs.KindBlog, Content: []byte("blog content")},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if fake.UpsertFileCalls != 3 {
		t.Fatalf("UpsertFileCalls = %d; want 3", fake.UpsertFileCalls)
	}

	wantPaths := map[string]bool{
		"docs/releases/v1.2.3/changelog.md":     false,
		"docs/releases/v1.2.3/release_notes.md": false,
		"docs/releases/v1.2.3/blog.md":          false,
	}
	for _, f := range fake.CapturedFiles {
		if _, ok := wantPaths[f.Path]; !ok {
			t.Errorf("unexpected path %q in UpsertFile calls", f.Path)
		} else {
			wantPaths[f.Path] = true
		}
	}
	for path, seen := range wantPaths {
		if !seen {
			t.Errorf("expected path %q not seen in UpsertFile calls", path)
		}
	}
}

// TestConfiguredBranchAndDir verifies that cfg.Branch and cfg.Dir are
// respected: UpsertFile is called with the configured branch and path prefix.
func TestConfiguredBranchAndDir(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
		Config:   enabledPagesCustom("docs-site", "site"),
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindChangelog, Content: []byte("changelog content")},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if fake.UpsertFileCalls != 1 {
		t.Fatalf("UpsertFileCalls = %d; want 1", fake.UpsertFileCalls)
	}
	if fake.CapturedBranch != "docs-site" {
		t.Errorf("branch = %q; want %q", fake.CapturedBranch, "docs-site")
	}
	if len(fake.CapturedFiles) == 0 {
		t.Fatal("CapturedFiles is empty")
	}
	wantPath := "site/releases/v1.2.3/changelog.md"
	if fake.CapturedFiles[0].Path != wantPath {
		t.Errorf("path = %q; want %q", fake.CapturedFiles[0].Path, wantPath)
	}
}

// TestIdempotentOverwrite verifies that calling Publish twice with identical
// inputs issues UpsertFile to the SAME deterministic path each time. The path
// must be identical across runs (UpsertFile is the idempotency mechanism).
func TestIdempotentOverwrite(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
		Config:   enabledPages(),
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindChangelog, Content: []byte("changelog content")},
	}

	ctx := context.Background()

	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	if fake.UpsertFileCalls != 2 {
		t.Fatalf("UpsertFileCalls = %d; want 2 (once per Publish call)", fake.UpsertFileCalls)
	}

	// Both calls must target the identical path (idempotent overwrite).
	if len(fake.CapturedFiles) != 2 {
		t.Fatalf("CapturedFiles len = %d; want 2", len(fake.CapturedFiles))
	}
	if fake.CapturedFiles[0].Path != fake.CapturedFiles[1].Path {
		t.Errorf("paths differ across runs: %q vs %q (must be identical for idempotent overwrite)",
			fake.CapturedFiles[0].Path, fake.CapturedFiles[1].Path)
	}
}

// TestCapabilityAbsent verifies that when the provider does NOT implement
// vcs.BranchCommitter, Publish returns nil and issues zero UpsertFile calls
// (graceful degradation; D-15).
func TestCapabilityAbsent(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: &minimalProvider{},
		Config:   enabledPages(),
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindChangelog, Content: []byte("changelog")},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish with missing BranchCommitter: got error %v; want nil", err)
	}
	// No UpsertFile calls possible since the provider is a plain minimalProvider.
	// Test passes as long as Publish returns nil without panicking.
}

// TestDisabled verifies that when cfg.Enabled is false, Publish returns nil
// and issues zero UpsertFile calls (disabled no-op).
func TestDisabled(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
		Config: config.ReleaseDocs{
			Publish: config.ReleasePublish{
				Pages: config.PagesPublishTarget{Enabled: false},
			},
		},
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindChangelog, Content: []byte("changelog")},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish with disabled pages: got error %v; want nil", err)
	}
	if fake.UpsertFileCalls != 0 {
		t.Errorf("UpsertFileCalls = %d; want 0 (disabled)", fake.UpsertFileCalls)
	}
}

// TestEmptyContentSkipped verifies that artifacts with empty Content are not
// passed to UpsertFile.
func TestEmptyContentSkipped(t *testing.T) {
	t.Parallel()

	p := pages.Publisher{}
	fake, provider := releasedocstest.NewFake()

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
		Config:   enabledPages(),
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindChangelog, Content: []byte("changelog content")},
		{Kind: releasedocs.KindBlog, Content: nil}, // empty — must be skipped
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// Only the non-empty artifact should trigger UpsertFile.
	if fake.UpsertFileCalls != 1 {
		t.Errorf("UpsertFileCalls = %d; want 1 (empty blog skipped)", fake.UpsertFileCalls)
	}
}
