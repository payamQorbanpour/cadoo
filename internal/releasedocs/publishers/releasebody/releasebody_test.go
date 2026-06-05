package releasebody_test

import (
	"context"
	"testing"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/publishers/releasebody"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs/releasedocstest"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// minimalProvider implements only vcs.Provider with no optional capabilities.
// It is used for degradation tests: type-assertions to vcs.ReleasePublisher,
// vcs.BranchCommitter, etc. will return (nil, false) as intended.
// Note: releasedocstest.NewFake(OmitReleasePublisher()) does NOT achieve this
// because the wrapper embeds *Fake which promotes all methods (see plan-02
// SUMMARY deviation note).
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

// TestSplicePreserves verifies the releasebody publisher:
//  1. Injects the Cadoo-managed block into a release body that has user content
//     outside the markers.
//  2. Running Publish twice produces exactly one managed block (idempotent).
//  3. When the spliced body equals the current body, UpdateReleaseBody is NOT called.
func TestSplicePreserves(t *testing.T) {
	t.Parallel()

	p := releasebody.Publisher{}

	if p.Target() != releasedocs.TargetReleaseBody {
		t.Fatalf("Target() = %q; want %q", p.Target(), releasedocs.TargetReleaseBody)
	}

	// Fake provider with full capabilities.
	fake, provider := releasedocstest.NewFake()
	fake.Release = &vcs.Release{
		ID:      7,
		TagName: "v1.2.3",
		Body:    "My hand-written intro.",
	}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: provider,
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("## Release Notes\n\nSome notes.")},
	}

	ctx := context.Background()

	// --- First Publish ---
	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	if fake.UpdateReleaseBodyCalls != 1 {
		t.Fatalf("first Publish: UpdateReleaseBody called %d times; want 1", fake.UpdateReleaseBodyCalls)
	}

	// User content must be preserved in the captured body.
	if got := fake.CapturedReleaseBody; !contains(got, "My hand-written intro.") {
		t.Errorf("user content not preserved after first Publish; body = %q", got)
	}
	// Managed section must be present.
	if got := fake.CapturedReleaseBody; !contains(got, releasedocs.ReleaseNotesBegin) {
		t.Errorf("release-notes begin marker missing; body = %q", got)
	}

	// --- Second Publish (idempotency): update fake.Release so it reflects what was written ---
	fake.Release = &vcs.Release{
		ID:      7,
		TagName: "v1.2.3",
		Body:    fake.CapturedReleaseBody, // provider now returns the already-spliced body
	}

	if err := p.Publish(ctx, rc, arts); err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	// The body has not changed, so UpdateReleaseBody must not be called again.
	if fake.UpdateReleaseBodyCalls != 1 {
		t.Fatalf("second Publish: UpdateReleaseBody called %d times; want 1 (no-op)", fake.UpdateReleaseBodyCalls)
	}

	// Marker appears exactly once.
	firstIdx := indexOf(fake.CapturedReleaseBody, releasedocs.ReleaseNotesBegin)
	lastIdx := lastIndexOf(fake.CapturedReleaseBody, releasedocs.ReleaseNotesBegin)
	if firstIdx != lastIdx {
		t.Errorf("marker appears more than once after two Publish calls; body = %q", fake.CapturedReleaseBody)
	}
}

// TestReleaseBodyDegrades verifies that when the provider does NOT implement
// vcs.ReleasePublisher, Publish returns nil (graceful degradation; D-15) and
// does not attempt any write.
//
// We use an inline minimalProvider rather than releasedocstest.NewFake(OmitReleasePublisher())
// because the fake's wrapper types embed *Fake which promotes all methods, making
// type assertions to capability interfaces always succeed (see plan-02 SUMMARY).
func TestReleaseBodyDegrades(t *testing.T) {
	t.Parallel()

	p := releasebody.Publisher{}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.2.3",
		Provider: &minimalProvider{},
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("notes")},
	}

	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish with missing ReleasePublisher capability: got error %v; want nil", err)
	}
	// Nothing further to assert — the test passes if Publish returned nil
	// without panicking (the minimalProvider has no UpdateReleaseBody to call).
}

// gitLabStyleProvider is a test double that behaves like GitLab: GetReleaseByTag
// returns a Release with ID=0 and TagName set, and the provider also implements
// vcs.TagReleasePublisher so the CR-01 fix path is exercised.
type gitLabStyleProvider struct {
	minimalProvider
	release               vcs.Release
	updateByTagCalls      int
	capturedTag           string
	capturedBody          string
	updateReleaseBodyCalls int
}

func (g *gitLabStyleProvider) GetReleaseByTag(_ context.Context, _, _ string) (*vcs.Release, error) {
	return &g.release, nil
}

func (g *gitLabStyleProvider) UpdateReleaseBody(_ context.Context, _ string, _ int64, _ string) error {
	g.updateReleaseBodyCalls++
	return nil
}

func (g *gitLabStyleProvider) UpdateReleaseBodyByTag(_ context.Context, _, tag, body string) error {
	g.updateByTagCalls++
	g.capturedTag = tag
	g.capturedBody = body
	return nil
}

var _ vcs.ReleasePublisher = (*gitLabStyleProvider)(nil)
var _ vcs.TagReleasePublisher = (*gitLabStyleProvider)(nil)

// noTagPublisherProvider implements ReleasePublisher returning ID=0 but does NOT
// implement TagReleasePublisher. Used to test the fallback-error path.
type noTagPublisherProvider struct {
	minimalProvider
	release vcs.Release
}

func (n *noTagPublisherProvider) GetReleaseByTag(_ context.Context, _, _ string) (*vcs.Release, error) {
	return &n.release, nil
}

func (n *noTagPublisherProvider) UpdateReleaseBody(_ context.Context, _ string, _ int64, _ string) error {
	return nil
}

var _ vcs.ReleasePublisher = (*noTagPublisherProvider)(nil)

// TestGitHubPath verifies that when the provider returns a release with a non-zero
// numeric ID, Publish routes through UpdateReleaseBody (the existing numeric-ID
// path) and does not call UpdateReleaseBodyByTag.
func TestGitHubPath(t *testing.T) {
	t.Parallel()

	fake, provider := releasedocstest.NewFake()
	fake.Release = &vcs.Release{
		ID:      42,
		TagName: "v2.0.0",
		Body:    "",
	}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v2.0.0",
		Provider: provider,
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("## Notes\nContent.")},
	}

	p := releasebody.Publisher{}
	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if fake.UpdateReleaseBodyCalls != 1 {
		t.Errorf("UpdateReleaseBody calls = %d; want 1", fake.UpdateReleaseBodyCalls)
	}
}

// TestGitLabPath verifies that when the provider returns a release with ID=0 and
// TagName != "" and implements vcs.TagReleasePublisher, Publish routes through
// UpdateReleaseBodyByTag with the release's tag name (CR-01 fix).
func TestGitLabPath(t *testing.T) {
	t.Parallel()

	prov := &gitLabStyleProvider{
		release: vcs.Release{
			ID:      0,
			TagName: "v3.1.0",
			Body:    "",
		},
	}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v3.1.0",
		Provider: prov,
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("## Release Notes\nContent.")},
	}

	p := releasebody.Publisher{}
	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	// Must have called UpdateReleaseBodyByTag, not UpdateReleaseBody.
	if prov.updateByTagCalls != 1 {
		t.Errorf("UpdateReleaseBodyByTag calls = %d; want 1", prov.updateByTagCalls)
	}
	if prov.updateReleaseBodyCalls != 0 {
		t.Errorf("UpdateReleaseBody calls = %d; want 0 (should use tag path)", prov.updateReleaseBodyCalls)
	}
	if prov.capturedTag != "v3.1.0" {
		t.Errorf("capturedTag = %q; want %q", prov.capturedTag, "v3.1.0")
	}
}

// TestFallbackError verifies that when the provider returns a release with ID=0
// but does NOT implement vcs.TagReleasePublisher, Publish returns a non-nil error
// naming the missing capability.
func TestFallbackError(t *testing.T) {
	t.Parallel()

	prov := &noTagPublisherProvider{
		release: vcs.Release{
			ID:      0,
			TagName: "v4.0.0",
			Body:    "",
		},
	}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v4.0.0",
		Provider: prov,
	}
	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("## Notes\nContent.")},
	}

	p := releasebody.Publisher{}
	err := p.Publish(context.Background(), rc, arts)
	if err == nil {
		t.Fatal("Publish: expected non-nil error for zero-ID release without TagReleasePublisher; got nil")
	}
}

// TestNoOp verifies that when the spliced body is identical to the current body,
// neither UpdateReleaseBody nor UpdateReleaseBodyByTag is called.
func TestNoOp(t *testing.T) {
	t.Parallel()

	// Use the Fake to easily control what GetReleaseByTag returns.
	fake, provider := releasedocstest.NewFake()

	arts := []releasedocs.Artifact{
		{Kind: releasedocs.KindReleaseNotes, Content: []byte("## Notes\nContent.")},
	}

	// Build the body that would result from a first publish, so the second call is a no-op.
	// We compute the expected spliced body using releasedocs.SpliceReleaseBody.
	preSplicedBody := releasedocs.SpliceReleaseBody("", "## Notes\nContent.")
	fake.Release = &vcs.Release{
		ID:      5,
		TagName: "v1.0.0",
		Body:    preSplicedBody,
	}

	rc := releasedocs.ReleaseContext{
		Repo:     "owner/repo",
		Org:      "org1",
		ToRef:    "v1.0.0",
		Provider: provider,
	}

	p := releasebody.Publisher{}
	if err := p.Publish(context.Background(), rc, arts); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if fake.UpdateReleaseBodyCalls != 0 {
		t.Errorf("UpdateReleaseBody calls = %d; want 0 (body unchanged, no-op expected)", fake.UpdateReleaseBodyCalls)
	}
}

// --- helpers ---

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastIndexOf(s, sub string) int {
	last := -1
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			last = i
		}
	}
	return last
}
