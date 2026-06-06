// Package releasedocstest provides shared test helpers for the release-docs
// subsystem. It is a regular (non-_test.go) package so it can be imported from
// sibling packages and other test binaries. It depends only on internal/vcs and
// internal/releasedocs — never on orchestrator (D-01).
package releasedocstest

import (
	"context"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// Fake is a test double that implements vcs.Provider plus the three optional
// release-docs capability interfaces (vcs.ReleaseRangeReader,
// vcs.ReleasePublisher, vcs.BranchCommitter) and releasedocs.FileFetcher.
// It records every call so tests can assert on side-effects without talking
// to a real VCS provider.
//
// Use NewFake with functional options to control which capabilities the
// returned value exposes; consumers' type-assertions will fail exactly for
// omitted capabilities.
type Fake struct {
	// kind is returned by Kind().
	kind vcs.Kind

	// --- Call counters and captured values ---

	// ListCommitsCalls is the number of times ListCommits was called.
	ListCommitsCalls int
	// ListMergedPRsCalls is the number of times ListMergedPRs was called.
	ListMergedPRsCalls int
	// LatestTagBeforeCalls is the number of times LatestTagBefore was called.
	LatestTagBeforeCalls int
	// GetReleaseCalls is the number of times GetReleaseByTag was called.
	GetReleaseCalls int
	// UpdateReleaseBodyCalls is the number of times UpdateReleaseBody was called.
	UpdateReleaseBodyCalls int
	// UpsertFileCalls is the number of times UpsertFile was called.
	UpsertFileCalls int
	// OpenOrUpdatePRCalls is the number of times OpenOrUpdatePR was called.
	OpenOrUpdatePRCalls int
	// FetchFileFromRefCalls is the number of times FetchFileFromRef was called.
	FetchFileFromRefCalls int

	// CapturedReleaseBody is the last body passed to UpdateReleaseBody.
	CapturedReleaseBody string
	// CapturedPRBody is the last body passed to OpenOrUpdatePR.
	CapturedPRBody string
	// CapturedBranch is the last branch name passed to UpsertFile or OpenOrUpdatePR.
	CapturedBranch string
	// CapturedFiles is the last set of FileWrite values passed to UpsertFile.
	CapturedFiles []vcs.FileWrite

	// --- Configurable return values ---

	// Commits is returned by ListCommits.
	Commits []vcs.Commit
	// MergedPRs is returned by ListMergedPRs.
	MergedPRs []vcs.MergedPR
	// LatestTag is returned by LatestTagBefore.
	LatestTag string
	// Release is returned by GetReleaseByTag.
	Release *vcs.Release
	// PRNumber is returned by OpenOrUpdatePR.
	PRNumber int64
	// FileContent is returned by FetchFileFromRef when FetchErr is nil.
	FileContent []byte
	// FetchErr, when non-nil, is returned as the error from FetchFileFromRef
	// instead of FileContent. Use this to simulate missing-file (404) or other
	// fetch failures in tests.
	FetchErr error
}

// options holds the capability omission flags for NewFake.
type options struct {
	omitRangeReader    bool
	omitReleasePublish bool
	omitBranchCommit   bool
	omitFileFetcher    bool
	kind               vcs.Kind
}

// Option is a functional option for NewFake.
type Option func(*options)

// OmitRangeReader removes the vcs.ReleaseRangeReader capability from the
// returned provider. Type-assertions to vcs.ReleaseRangeReader will return
// (nil, false).
func OmitRangeReader() Option { return func(o *options) { o.omitRangeReader = true } }

// OmitReleasePublisher removes the vcs.ReleasePublisher capability from the
// returned provider. Type-assertions to vcs.ReleasePublisher will return
// (nil, false).
func OmitReleasePublisher() Option { return func(o *options) { o.omitReleasePublish = true } }

// OmitBranchCommitter removes the vcs.BranchCommitter capability from the
// returned provider. Type-assertions to vcs.BranchCommitter will return
// (nil, false).
func OmitBranchCommitter() Option { return func(o *options) { o.omitBranchCommit = true } }

// OmitFileFetcher removes the releasedocs.FileFetcher capability from the
// returned provider. Type-assertions to releasedocs.FileFetcher will return
// (nil, false).
func OmitFileFetcher() Option { return func(o *options) { o.omitFileFetcher = true } }

// WithKind sets the vcs.Kind returned by the fake's Kind() method. Defaults
// to vcs.KindGitHub.
func WithKind(k vcs.Kind) Option { return func(o *options) { o.kind = k } }

// NewFake constructs a Fake and returns it wrapped in a vcs.Provider that
// exposes only the capabilities NOT omitted by the supplied options. This
// ensures that consumers' type-assertions fail exactly when a capability is
// omitted, enabling graceful-degradation tests without a real VCS adapter.
//
// The underlying *Fake pointer is accessible via the returned value (cast when
// all capabilities are present) or by storing it separately before passing
// opts.
func NewFake(opts ...Option) (*Fake, vcs.Provider) {
	o := &options{kind: vcs.KindGitHub}
	for _, opt := range opts {
		opt(o)
	}
	f := &Fake{kind: o.kind}

	// Build the narrowest wrapper that satisfies exactly the requested caps.
	switch {
	case o.omitRangeReader && o.omitReleasePublish && o.omitBranchCommit && o.omitFileFetcher:
		return f, &providerOnly{f}
	case o.omitRangeReader && o.omitReleasePublish && o.omitBranchCommit:
		return f, &withFileFetcher{f}
	case o.omitRangeReader && o.omitReleasePublish && o.omitFileFetcher:
		return f, &withBranchCommitter{f}
	case o.omitRangeReader && o.omitBranchCommit && o.omitFileFetcher:
		return f, &withReleasePublisher{f}
	case o.omitReleasePublish && o.omitBranchCommit && o.omitFileFetcher:
		return f, &withRangeReader{f}
	case o.omitRangeReader && o.omitReleasePublish:
		return f, &withBranchCommitterFileFetcher{f}
	case o.omitRangeReader && o.omitBranchCommit:
		return f, &withReleasePublisherFileFetcher{f}
	case o.omitRangeReader && o.omitFileFetcher:
		return f, &withReleasePublisherBranchCommitter{f}
	case o.omitReleasePublish && o.omitBranchCommit:
		return f, &withRangeReaderFileFetcher{f}
	case o.omitReleasePublish && o.omitFileFetcher:
		return f, &withRangeReaderBranchCommitter{f}
	case o.omitBranchCommit && o.omitFileFetcher:
		return f, &withRangeReaderReleasePublisher{f}
	case o.omitRangeReader:
		return f, &withReleasePublisherBranchCommitterFileFetcher{f}
	case o.omitReleasePublish:
		return f, &withRangeReaderBranchCommitterFileFetcher{f}
	case o.omitBranchCommit:
		return f, &withRangeReaderReleasePublisherFileFetcher{f}
	case o.omitFileFetcher:
		return f, &withRangeReaderReleasePublisherBranchCommitter{f}
	default:
		// All capabilities exposed — return the full *Fake directly.
		return f, f
	}
}

// ---- vcs.Provider base implementation ----

// Kind implements vcs.Provider.
func (f *Fake) Kind() vcs.Kind { return f.kind }

// FetchPullRequest implements vcs.Provider.
func (f *Fake) FetchPullRequest(_ context.Context, _ string, number int64) (*vcs.PullRequest, error) {
	return &vcs.PullRequest{Number: number}, nil
}

// ListChangedFiles implements vcs.Provider.
func (f *Fake) ListChangedFiles(_ context.Context, _ *vcs.PullRequest) ([]vcs.FileChange, error) {
	return nil, nil
}

// PostSummaryComment implements vcs.Provider.
func (f *Fake) PostSummaryComment(_ context.Context, _ *vcs.PullRequest, _ string) (string, error) {
	return "fake-comment-id", nil
}

// UpdateSummaryComment implements vcs.Provider.
func (f *Fake) UpdateSummaryComment(_ context.Context, _ *vcs.PullRequest, _, _ string) error {
	return nil
}

// PostInlineComments implements vcs.Provider.
func (f *Fake) PostInlineComments(_ context.Context, _ *vcs.PullRequest, comments []vcs.InlineComment) ([]vcs.PostedInlineRef, error) {
	refs := make([]vcs.PostedInlineRef, len(comments))
	for i, c := range comments {
		refs[i] = vcs.PostedInlineRef{Comment: c}
	}
	return refs, nil
}

// ResolveThread implements vcs.Provider.
func (f *Fake) ResolveThread(_ context.Context, _ *vcs.PullRequest, _ string) error { return nil }

// EditPullRequestBody implements vcs.Provider.
func (f *Fake) EditPullRequestBody(_ context.Context, _ *vcs.PullRequest, _ string) error {
	return nil
}

// UpsertCheckRun implements vcs.Provider.
func (f *Fake) UpsertCheckRun(_ context.Context, _ *vcs.PullRequest, _ vcs.CheckRun) error {
	return nil
}

// ---- vcs.ReleaseRangeReader implementation ----

// ListCommits implements vcs.ReleaseRangeReader.
func (f *Fake) ListCommits(_ context.Context, _, _, _ string) ([]vcs.Commit, error) {
	f.ListCommitsCalls++
	if f.Commits != nil {
		return f.Commits, nil
	}
	return []vcs.Commit{{SHA: "abc1234", Message: "feat: example", Author: "author", Date: time.Now()}}, nil
}

// ListMergedPRs implements vcs.ReleaseRangeReader.
func (f *Fake) ListMergedPRs(_ context.Context, _, _, _ string) ([]vcs.MergedPR, error) {
	f.ListMergedPRsCalls++
	if f.MergedPRs != nil {
		return f.MergedPRs, nil
	}
	return []vcs.MergedPR{{Number: 1, Title: "feat: example PR", Author: "author"}}, nil
}

// LatestTagBefore implements vcs.ReleaseRangeReader.
func (f *Fake) LatestTagBefore(_ context.Context, _, _, _ string) (string, error) {
	f.LatestTagBeforeCalls++
	return f.LatestTag, nil
}

// ---- vcs.ReleasePublisher implementation ----

// GetReleaseByTag implements vcs.ReleasePublisher.
func (f *Fake) GetReleaseByTag(_ context.Context, _, tag string) (*vcs.Release, error) {
	f.GetReleaseCalls++
	if f.Release != nil {
		return f.Release, nil
	}
	return &vcs.Release{ID: 1, TagName: tag}, nil
}

// UpdateReleaseBody implements vcs.ReleasePublisher.
func (f *Fake) UpdateReleaseBody(_ context.Context, _ string, _ int64, body string) error {
	f.UpdateReleaseBodyCalls++
	f.CapturedReleaseBody = body
	return nil
}

// ---- vcs.BranchCommitter implementation ----

// UpsertFile implements vcs.BranchCommitter.
func (f *Fake) UpsertFile(_ context.Context, _, branch, _ string, file vcs.FileWrite) error {
	f.UpsertFileCalls++
	f.CapturedBranch = branch
	f.CapturedFiles = append(f.CapturedFiles, file)
	return nil
}

// OpenOrUpdatePR implements vcs.BranchCommitter.
func (f *Fake) OpenOrUpdatePR(_ context.Context, _, branch, _, _, body string) (int64, error) {
	f.OpenOrUpdatePRCalls++
	f.CapturedBranch = branch
	f.CapturedPRBody = body
	if f.PRNumber != 0 {
		return f.PRNumber, nil
	}
	return 42, nil
}

// ---- releasedocs.FileFetcher implementation ----

// FetchFileFromRef implements releasedocs.FileFetcher.
func (f *Fake) FetchFileFromRef(_ context.Context, _, _, _ string) ([]byte, error) {
	f.FetchFileFromRefCalls++
	if f.FetchErr != nil {
		return nil, f.FetchErr
	}
	return f.FileContent, nil
}

// ---- Compile-time capability assertions ----

var _ vcs.Provider = (*Fake)(nil)
var _ vcs.ReleaseRangeReader = (*Fake)(nil)
var _ vcs.ReleasePublisher = (*Fake)(nil)
var _ vcs.BranchCommitter = (*Fake)(nil)
var _ releasedocs.FileFetcher = (*Fake)(nil)

// ============================================================
// Narrow wrapper types — each exposes a specific subset of
// capabilities so type-assertions fail for omitted ones.
// ============================================================

// providerOnly exposes only the base vcs.Provider interface.
type providerOnly struct{ *Fake }

// withRangeReader exposes vcs.Provider + ReleaseRangeReader.
type withRangeReader struct{ *Fake }

// withReleasePublisher exposes vcs.Provider + ReleasePublisher.
type withReleasePublisher struct{ *Fake }

// withBranchCommitter exposes vcs.Provider + BranchCommitter.
type withBranchCommitter struct{ *Fake }

// withFileFetcher exposes vcs.Provider + releasedocs.FileFetcher.
type withFileFetcher struct{ *Fake }

// withRangeReaderFileFetcher exposes Provider + RangeReader + FileFetcher.
type withRangeReaderFileFetcher struct{ *Fake }

// withRangeReaderBranchCommitter exposes Provider + RangeReader + BranchCommitter.
type withRangeReaderBranchCommitter struct{ *Fake }

// withRangeReaderReleasePublisher exposes Provider + RangeReader + ReleasePublisher.
type withRangeReaderReleasePublisher struct{ *Fake }

// withReleasePublisherFileFetcher exposes Provider + ReleasePublisher + FileFetcher.
type withReleasePublisherFileFetcher struct{ *Fake }

// withReleasePublisherBranchCommitter exposes Provider + ReleasePublisher + BranchCommitter.
type withReleasePublisherBranchCommitter struct{ *Fake }

// withBranchCommitterFileFetcher exposes Provider + BranchCommitter + FileFetcher.
type withBranchCommitterFileFetcher struct{ *Fake }

// withRangeReaderReleasePublisherFileFetcher exposes Provider + RangeReader + ReleasePublisher + FileFetcher.
type withRangeReaderReleasePublisherFileFetcher struct{ *Fake }

// withRangeReaderBranchCommitterFileFetcher exposes Provider + RangeReader + BranchCommitter + FileFetcher.
type withRangeReaderBranchCommitterFileFetcher struct{ *Fake }

// withReleasePublisherBranchCommitterFileFetcher exposes Provider + ReleasePublisher + BranchCommitter + FileFetcher.
type withReleasePublisherBranchCommitterFileFetcher struct{ *Fake }

// withRangeReaderReleasePublisherBranchCommitter exposes Provider + RangeReader + ReleasePublisher + BranchCommitter.
type withRangeReaderReleasePublisherBranchCommitter struct{ *Fake }

// Compile-time checks for all wrapper types implementing vcs.Provider.
var (
	_ vcs.Provider = (*providerOnly)(nil)
	_ vcs.Provider = (*withRangeReader)(nil)
	_ vcs.Provider = (*withReleasePublisher)(nil)
	_ vcs.Provider = (*withBranchCommitter)(nil)
	_ vcs.Provider = (*withFileFetcher)(nil)
	_ vcs.Provider = (*withRangeReaderFileFetcher)(nil)
	_ vcs.Provider = (*withRangeReaderBranchCommitter)(nil)
	_ vcs.Provider = (*withRangeReaderReleasePublisher)(nil)
	_ vcs.Provider = (*withReleasePublisherFileFetcher)(nil)
	_ vcs.Provider = (*withReleasePublisherBranchCommitter)(nil)
	_ vcs.Provider = (*withBranchCommitterFileFetcher)(nil)
	_ vcs.Provider = (*withRangeReaderReleasePublisherFileFetcher)(nil)
	_ vcs.Provider = (*withRangeReaderBranchCommitterFileFetcher)(nil)
	_ vcs.Provider = (*withReleasePublisherBranchCommitterFileFetcher)(nil)
	_ vcs.Provider = (*withRangeReaderReleasePublisherBranchCommitter)(nil)
)
