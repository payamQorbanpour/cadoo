// Package vcs defines the provider-agnostic interface every Git-host adapter
// implements (github, github_enterprise, gitlab, ...). The orchestrator and
// review tools depend only on this interface.
package vcs

import (
	"context"
	"time"
)

// Kind identifies a VCS provider.
type Kind string

// Recognized provider kinds.
const (
	KindGitHub           Kind = "github"
	KindGitHubEnterprise Kind = "github_enterprise"
	KindGitLab           Kind = "gitlab"
)

// PullRequest is the normalized model Cadoo operates on (covers GH PRs and
// GL MRs).
type PullRequest struct {
	Provider     Kind
	InstallID    string
	RepoFullName string
	Number       int64
	Title        string
	Body         string
	Author       string
	BaseSHA      string
	HeadSHA      string
	BaseRef      string
	HeadRef      string
	State        string
	URL          string
	UpdatedAt    time.Time
}

// FileChange is one file's diff in a PR.
type FileChange struct {
	Path      string
	PrevPath  string
	Status    string // added | modified | removed | renamed
	Patch     string
	Additions int
	Deletions int
	IsBinary  bool
}

// Severity for inline findings.
type Severity string

// Severity levels — keep in sync with the CHECK constraint on findings.
const (
	SeverityBlock Severity = "block"
	SeverityWarn  Severity = "warn"
	SeverityNit   Severity = "nit"
)

// InlineComment is anchored to a specific line range in a file.
type InlineComment struct {
	File      string
	LineStart int
	LineEnd   int
	Body      string
	Severity  Severity
}

// PostedInlineRef pairs an InlineComment with the provider-side identifier
// the adapter created for it (GitLab discussion ID, GitHub review-comment
// ID, etc.). Adapters that can't surface a per-comment ID in one call (the
// classic example: GitHub's review-create API) leave ExternalID empty —
// callers that need the ID for follow-up actions (resolve, update) must
// degrade gracefully when it's missing.
type PostedInlineRef struct {
	Comment    InlineComment
	ExternalID string
}

// CheckRunStatus is the high-level outcome posted to the VCS as a check.
type CheckRunStatus string

// Check run statuses.
const (
	CheckQueued    CheckRunStatus = "queued"
	CheckRunning   CheckRunStatus = "in_progress"
	CheckSucceeded CheckRunStatus = "success"
	CheckFailed    CheckRunStatus = "failure"
	CheckNeutral   CheckRunStatus = "neutral"
)

// CheckRun is the status reported back to the VCS.
type CheckRun struct {
	Name    string
	Status  CheckRunStatus
	Title   string
	Summary string
	URL     string
}

// Provider is the surface every VCS adapter implements.
type Provider interface {
	Kind() Kind

	FetchPullRequest(ctx context.Context, repo string, number int64) (*PullRequest, error)
	ListChangedFiles(ctx context.Context, pr *PullRequest) ([]FileChange, error)

	PostSummaryComment(ctx context.Context, pr *PullRequest, body string) (id string, err error)
	UpdateSummaryComment(ctx context.Context, pr *PullRequest, id, body string) error
	PostInlineComments(ctx context.Context, pr *PullRequest, comments []InlineComment) ([]PostedInlineRef, error)

	// ResolveThread marks a previously-posted inline discussion as resolved.
	// Adapters whose provider has no notion of resolved threads, or whose
	// PostInlineComments couldn't capture per-comment IDs, may return nil
	// without doing anything. threadID is the same value PostInlineComments
	// returned in PostedInlineRef.ExternalID.
	ResolveThread(ctx context.Context, pr *PullRequest, threadID string) error

	// EditPullRequestBody replaces the PR/MR description. The orchestrator
	// uses this to inject Cadoo-managed sections (e.g. /describe) while
	// preserving the user's original text inside an explicit section block.
	EditPullRequestBody(ctx context.Context, pr *PullRequest, body string) error

	UpsertCheckRun(ctx context.Context, pr *PullRequest, run CheckRun) error
}

// PriorReviewReader is an OPTIONAL capability. Adapters that can enumerate
// Cadoo's own previously-posted artifacts on a PR/MR implement it so that
// stateless CI-mode (no DB) can rebuild dedup state from the PR itself.
// The orchestrator type-asserts for it; providers that don't implement it
// fall back to non-idempotent behaviour.
type PriorReviewReader interface {
	ListCadooArtifacts(ctx context.Context, pr *PullRequest) (PriorReview, error)
}

// PriorReview is a normalized snapshot of Cadoo's prior footprint on a PR.
type PriorReview struct {
	SummaryCommentID string // overview comment/note id (found via SummaryWrapperBegin); "" if none
	Inline           []PriorInline
}

// PriorInline is one previously-posted Cadoo inline finding.
type PriorInline struct {
	Tool            string
	File            string
	Severity        string
	StructuralKey   string // parsed from the hidden marker (authoritative)
	Title           string // first visible line of the original body
	NormalizedTitle string // full-body normalizeTitle result from marker nt= field; "" for legacy markers
	ExternalID      string // discussion/thread id for ResolveThread; "" if unrecoverable
	Resolved        bool   // already resolved upstream — don't re-resolve
	// Line is the anchor line number for this finding (n.Position.NewLine for
	// GitLab, reviewThread.line for GitHub). Zero means the adapter could not
	// recover it (e.g. deleted-line thread or legacy marker with no position).
	Line int
	// EndLine is the end of the anchor range. Currently set equal to Line;
	// reserved for future multi-line anchor support.
	EndLine int
}

// Commit is a normalized VCS commit as used by the release-docs subsystem.
type Commit struct {
	// SHA is the full commit hash.
	SHA string
	// Message is the full commit message (subject + optional body).
	Message string
	// Author is the display name or login of the commit author.
	Author string
	// Date is when the commit was authored.
	Date time.Time
}

// MergedPR is a normalized merged pull-request / merge-request as returned by
// the release range read operations.
type MergedPR struct {
	// Number is the PR/MR number.
	Number int64
	// Title is the PR/MR title.
	Title string
	// Body is the PR/MR description.
	Body string
	// Author is the display name or login of the PR/MR author.
	Author string
	// Labels is the set of label names applied to the PR/MR.
	Labels []string
	// MergedAt is when the PR/MR was merged.
	MergedAt time.Time
	// MergeSHA is the merge commit SHA.
	MergeSHA string
}

// Release is a normalized VCS release (GitHub Release, GitLab Release, etc.).
type Release struct {
	// ID is the provider-side numeric identifier for the release.
	ID int64
	// TagName is the tag associated with this release (e.g. "v1.2.3").
	TagName string
	// Body is the release description / notes.
	Body string
	// Draft indicates this release has not been published yet.
	Draft bool
	// Prerelease indicates this is a pre-release version.
	Prerelease bool
}

// FileWrite describes a file to be upserted on a VCS branch.
type FileWrite struct {
	// Path is the repository-relative path of the file (e.g. "CHANGELOG.md").
	Path string
	// Content is the file's raw byte content.
	Content []byte
	// Mode is the file mode (e.g. "100644"). Empty means the provider default.
	Mode string
}

// ReleaseRangeReader is an OPTIONAL capability. Adapters that can enumerate
// commits and merged pull-requests between two refs implement this interface
// so the release-docs dispatcher can build a ReleaseContext without importing
// provider-specific packages. The dispatcher type-asserts for it; providers
// that don't implement it degrade gracefully with a logged reason (D-15).
type ReleaseRangeReader interface {
	// ListCommits returns all commits reachable from ToRef but not FromRef,
	// in reverse-chronological order.
	ListCommits(ctx context.Context, repo, fromRef, toRef string) ([]Commit, error)
	// ListMergedPRs returns all pull-requests / merge-requests that were
	// merged in the commit range defined by FromRef..ToRef.
	ListMergedPRs(ctx context.Context, repo, fromRef, toRef string) ([]MergedPR, error)
	// LatestTagBefore returns the most recent tag whose name matches
	// tagPattern (a glob like "v*") and that precedes toRef in the
	// repository's tag history. Returns "" when no matching tag exists.
	LatestTagBefore(ctx context.Context, repo, toRef, tagPattern string) (string, error)
}

// ReleasePublisher is an OPTIONAL capability. Adapters that can read and
// update VCS releases implement this interface so the release-body publisher
// can splice release notes idempotently into the release body. The dispatcher
// type-asserts for it; providers that don't implement it degrade gracefully
// with a logged reason (D-15).
type ReleasePublisher interface {
	// GetReleaseByTag fetches a release by its tag name. Returns an error
	// when the tag does not have an associated release.
	GetReleaseByTag(ctx context.Context, repo, tag string) (*Release, error)
	// UpdateReleaseBody replaces the body field of the identified release.
	// Other release fields (draft, prerelease, tag) are left unchanged.
	UpdateReleaseBody(ctx context.Context, repo string, releaseID int64, body string) error
}

// TagReleasePublisher is an OPTIONAL capability. Adapters whose releases are
// identified by tag name rather than a numeric ID (e.g. GitLab) implement this
// interface so the releasebody publisher can update a release without a numeric
// ID. The releasebody publisher type-asserts for it when rel.ID == 0 and falls
// back to the numeric-ID ReleasePublisher path when the assertion fails.
// Providers that do not implement this interface but do implement ReleasePublisher
// continue to work through the numeric-ID path unchanged.
type TagReleasePublisher interface {
	// UpdateReleaseBodyByTag replaces the body field of the release identified
	// by tag. Other release fields (draft, prerelease) are left unchanged.
	UpdateReleaseBodyByTag(ctx context.Context, repo, tag, body string) error
}

// BranchCommitter is an OPTIONAL capability. Adapters that can create or
// update files on a branch and manage pull-requests implement this interface
// so the changelog-PR publisher can operate without knowing the underlying
// provider SDK. The dispatcher type-asserts for it; providers that don't
// implement it degrade gracefully with a logged reason (D-15).
type BranchCommitter interface {
	// UpsertFile creates or updates a single file on the named branch.
	// If the branch does not exist it is created from baseSHA. commitMessage
	// is used as the VCS commit message.
	UpsertFile(ctx context.Context, repo, branch, commitMessage string, f FileWrite) error
	// OpenOrUpdatePR ensures exactly one open pull-request exists for the
	// given branch. If a PR is already open it is updated (title + body);
	// otherwise a new PR is created from branch → base. Returns the PR number.
	OpenOrUpdatePR(ctx context.Context, repo, branch, base, title, body string) (int64, error)
}
